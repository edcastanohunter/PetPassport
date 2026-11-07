package claude

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/edcastanohunter/petpassport/backend/internal/ocr"
	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// Config define las opciones del extractor Claude (PP-F4-OCR-003, SEC-008).
type Config struct {
	APIKey        string
	Model         string  // defecto "claude-opus-5-5"
	PromptVersion string  // "prompt-v1"
	Effort        string  // "medium"
	PriceInMTok   float64 // USD por millón de tokens de entrada (defecto 4.00 para Opus)
	PriceOutMTok  float64 // USD por millón de tokens de salida (defecto 20.00 para Opus)
	HTTPClient    *http.Client
}

// Extractor implementa ocrdomain.DocumentExtractor usando el SDK oficial de Anthropic.
type Extractor struct {
	cfg          Config
	client       anthropic.Client
	schemaMap    map[string]any
	systemPrompt string
	extractorID  string
}

// NewExtractor inicializa el extractor Claude con salida estructurada (PP-F4-OCR-003).
func NewExtractor(cfg Config) (*Extractor, error) {
	if cfg.Model == "" {
		cfg.Model = "claude-opus-5-5"
	}
	if cfg.PromptVersion == "" {
		cfg.PromptVersion = "prompt-v1"
	}
	if cfg.Effort == "" {
		cfg.Effort = "medium"
	}
	if cfg.PriceInMTok == 0 {
		if strings.Contains(cfg.Model, "sonnet") {
			cfg.PriceInMTok = 2.00
			cfg.PriceOutMTok = 10.00
		} else {
			cfg.PriceInMTok = 4.00
			cfg.PriceOutMTok = 20.00
		}
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(ocr.SchemaV1JSON, &schemaMap); err != nil {
		return nil, fmt.Errorf("parse default schema: %w", err)
	}

	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	client := anthropic.NewClient(opts...)
	extractorID := fmt.Sprintf("claude:%s:%s:schema-v1", cfg.Model, cfg.PromptVersion)

	return &Extractor{
		cfg:          cfg,
		client:       client,
		schemaMap:    schemaMap,
		systemPrompt: string(ocr.PromptV1MD),
		extractorID:  extractorID,
	}, nil
}

func (e *Extractor) ID() string {
	return e.extractorID
}

// Extract envía las imágenes a Claude con salida estructurada JSON Schema (PP-F4-OCR-003, OCR-009).
func (e *Extractor) Extract(ctx context.Context, pages []ocrdomain.PreprocessedPage, hints ocrdomain.ExtractorHints) (ocrdomain.ExtractionResult, error) {
	if len(pages) == 0 {
		return ocrdomain.ExtractionResult{}, ocrdomain.ErrInvalidDocument
	}

	start := time.Now()

	// 1. Construir bloques de contenido: imágenes en base64 seguidas de instrucción de texto
	var blocks []anthropic.ContentBlockParamUnion
	for _, page := range pages {
		b64 := base64.StdEncoding.EncodeToString(page.Data)
		mediaType := page.ContentType
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, b64))
	}

	// Instrucción para el usuario (minimizando PII: no enviamos nombre del tutor, solo especie/pistas clínicas si existen)
	var instParts []string
	instParts = append(instParts, "Analiza las páginas adjuntas de la receta médica veterinaria y extrae todos los datos estructurados conforme al esquema.")
	if hints.PetSpecies != "" {
		instParts = append(instParts, fmt.Sprintf("Especie del animal paciente: %s.", hints.PetSpecies))
	}
	if hints.Country != "" {
		instParts = append(instParts, fmt.Sprintf("País de emisión: %s.", hints.Country))
	}
	blocks = append(blocks, anthropic.NewTextBlock(strings.Join(instParts, " ")))

	// 2. Parámetros de llamada
	maxTokens := int64(8000)
	msgParams := anthropic.MessageNewParams{
		Model:     anthropic.Model(e.cfg.Model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{
				Text:         e.systemPrompt,
				CacheControl: anthropic.CacheControlEphemeralParam{},
			},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(blocks...),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(e.cfg.Effort),
			Format: anthropic.JSONOutputFormatParam{
				Schema: e.schemaMap,
			},
		},
	}

	// 3. Ejecutar llamada al SDK
	msg, err := e.client.Messages.New(ctx, msgParams)
	if err != nil {
		return ocrdomain.ExtractionResult{}, fmt.Errorf("%w: %v", ocrdomain.ErrExtractionFailed, err)
	}

	// 4. Manejo de stop_reason
	if msg.StopReason == anthropic.StopReasonRefusal {
		return ocrdomain.ExtractionResult{
			Refused:       true,
			RefusalReason: "El proveedor rechazó la solicitud por política de contenido.",
			CostUSD:       0,
			LatencyMs:     int(time.Since(start).Milliseconds()),
		}, ocrdomain.ErrExtractionRefused
	}

	// Si se truncó por max_tokens, reintentar una vez con el doble
	if msg.StopReason == anthropic.StopReasonMaxTokens {
		msgParams.MaxTokens = 16000
		msg, err = e.client.Messages.New(ctx, msgParams)
		if err != nil {
			return ocrdomain.ExtractionResult{}, fmt.Errorf("%w: reintento por max_tokens falló: %v", ocrdomain.ErrExtractionFailed, err)
		}
		if msg.StopReason == anthropic.StopReasonRefusal {
			return ocrdomain.ExtractionResult{Refused: true}, ocrdomain.ErrExtractionRefused
		}
	}

	// 5. Parsear el JSON devuelto
	textJSON := extractTextContent(msg.Content)
	if textJSON == "" {
		return ocrdomain.ExtractionResult{}, fmt.Errorf("%w: respuesta vacía del modelo", ocrdomain.ErrExtractionFailed)
	}

	var ext ocrdomain.Extraction
	if err := json.Unmarshal([]byte(textJSON), &ext); err != nil {
		return ocrdomain.ExtractionResult{}, fmt.Errorf("%w: parsear json de extracción: %v", ocrdomain.ErrExtractionFailed, err)
	}

	ext.ExtractorID = e.extractorID
	ext.Version = 1

	// 6. Contabilidad de tokens y cálculo de coste (PP-F4-OCR-008, DAT-004)
	inTokens := msg.Usage.InputTokens
	outTokens := msg.Usage.OutputTokens
	costUSD := (float64(inTokens)*e.cfg.PriceInMTok + float64(outTokens)*e.cfg.PriceOutMTok) / 1_000_000.0
	latency := int(time.Since(start).Milliseconds())

	ext.CostUSD = costUSD
	ext.LatencyMs = latency

	return ocrdomain.ExtractionResult{
		Extraction:   ext,
		InputTokens:  inTokens,
		OutputTokens: outTokens,
		CostUSD:      costUSD,
		LatencyMs:    latency,
	}, nil
}

func extractTextContent(blocks []anthropic.ContentBlockUnion) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

var _ ocrdomain.DocumentExtractor = (*Extractor)(nil)
