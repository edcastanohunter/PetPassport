package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	prescriptionapp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	prescriptiondomain "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PipelineOptions configura las dependencias del pipeline OCR (PP-F4-OCR-001, ARQ-012).
type PipelineOptions struct {
	Preprocessor      ocrdomain.Preprocessor
	Extractor         ocrdomain.DocumentExtractor
	FallbackExtractor ocrdomain.DocumentExtractor
	Matcher           ocrdomain.CatalogMatcher
	Budget            ocrdomain.BudgetService
	Metrics           ocrdomain.MetricsRecorder
	Jobs              ocrdomain.JobRepository
	Thresholds        ocrdomain.ThresholdsConfig
	PrescriptionRepo  prescriptionapp.Repository
	UoW               prescriptionapp.TxRunner
	Projector         prescriptionapp.ProjectionRunner
	Logger            *slog.Logger
	Clock             kernel.Clock
}

// PipelineService orquesta el ciclo de análisis asíncrono de recetas (PP-F4-OCR-001, OCR-002, OCR-003, OCR-005, OCR-006).
type PipelineService struct {
	preprocessor     ocrdomain.Preprocessor
	extractor        ocrdomain.DocumentExtractor
	fallbackExt      ocrdomain.DocumentExtractor
	matcher          ocrdomain.CatalogMatcher
	budget           ocrdomain.BudgetService
	metrics          ocrdomain.MetricsRecorder
	jobs             ocrdomain.JobRepository
	thresholds       ocrdomain.ThresholdsConfig
	prescriptionRepo prescriptionapp.Repository
	uow              prescriptionapp.TxRunner
	projector        prescriptionapp.ProjectionRunner
	logger           *slog.Logger
	clock            kernel.Clock
}

func NewPipelineService(opts PipelineOptions) *PipelineService {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := opts.Clock
	if clock == nil {
		clock = kernel.RealClock{}
	}
	thresholds := opts.Thresholds
	if thresholds.MinQualityScore == 0 {
		thresholds = ocrdomain.DefaultThresholds()
	}

	return &PipelineService{
		preprocessor:     opts.Preprocessor,
		extractor:        opts.Extractor,
		fallbackExt:      opts.FallbackExtractor,
		matcher:          opts.Matcher,
		budget:           opts.Budget,
		metrics:          opts.Metrics,
		jobs:             opts.Jobs,
		thresholds:       thresholds,
		prescriptionRepo: opts.PrescriptionRepo,
		uow:              opts.UoW,
		projector:        opts.Projector,
		logger:           logger,
		clock:            clock,
	}
}

// ProcessDocumentInput contiene los datos de entrada para ejecutar el pipeline.
type ProcessDocumentInput struct {
	PrescriptionID uuid.UUID
	Attempt        int
	RawPages       [][]byte
	Country        string
	PetSpecies     string
	PetName        string
	GuardianName   string
}

// RunPipeline ejecuta el flujo completo de preprocesado -> extracción -> matching -> comando de agregado (PP-F4-OCR-001).
func (s *PipelineService) RunPipeline(ctx context.Context, in ProcessDocumentInput) error {
	now := s.clock.Now().UTC()
	start := now

	if in.Attempt <= 0 {
		in.Attempt = 1
	}
	if in.Attempt > 3 {
		return ocrdomain.ErrRetryLimitExceeded
	}

	// 1. Etapa de Preprocesamiento (PP-F4-OCR-002)
	s.updateJob(ctx, in.PrescriptionID, in.Attempt, "preprocessing", nil, nil, nil, nil, nil, nil, nil)

	var preprocessedPages []ocrdomain.PreprocessedPage
	var totalQuality float64
	var qualityWarnings []string
	hasPoorQuality := false

	for idx, rawData := range in.RawPages {
		page, err := s.preprocessor.ProcessPage(ctx, idx+1, rawData)
		if err != nil {
			errMsg := fmt.Sprintf("Fallo en preprocesamiento de página %d: %v", idx+1, err)
			s.updateJob(ctx, in.PrescriptionID, in.Attempt, "failed", nil, nil, nil, nil, nil, nil, &errMsg)
			s.recordJobStatus(ctx, now, "failed", 0)
			return err
		}

		preprocessedPages = append(preprocessedPages, page)
		totalQuality += page.QualityScore
		if len(page.Warnings) > 0 {
			qualityWarnings = append(qualityWarnings, page.Warnings...)
		}

		if page.QualityScore < s.thresholds.MinQualityScore || page.IsTooBlurry || page.IsTooDark {
			hasPoorQuality = true
		}
	}

	avgQuality := 0.0
	if len(preprocessedPages) > 0 {
		avgQuality = totalQuality / float64(len(preprocessedPages))
	}

	// Si la calidad es insuficiente, solicitar nueva foto sin llamar al proveedor (PP-F4-OCR-002, OCR-006)
	if hasPoorQuality {
		reason := "La calidad de la imagen es baja (desenfoque o iluminación deficiente). Por favor, toma una nueva foto nítida de la receta."
		if len(qualityWarnings) > 0 {
			reason = qualityWarnings[0]
		}

		s.updateJob(ctx, in.PrescriptionID, in.Attempt, "reupload_requested", nil, &avgQuality, qualityWarnings, nil, nil, nil, nil)
		s.recordJobStatus(ctx, now, "reupload_requested", int(time.Since(start).Milliseconds()))

		// Ejecutar comando de agregado RequestReupload
		return s.executeAggregateCommand(ctx, in.PrescriptionID, func(rx *prescriptiondomain.Prescription) error {
			_, err := rx.RequestReupload(reason)
			return err
		})
	}

	// 2. Control de presupuesto de IA (PP-F4-OCR-008, DAT-004)
	if s.budget != nil {
		budgetStatus, err := s.budget.CheckBudget(ctx)
		if err != nil && errors.Is(err, ocrdomain.ErrBudgetExceeded) {
			errMsg := "Presupuesto mensual de IA alcanzado (AI_BUDGET_EXCEEDED). Requiere captura manual."
			s.updateJob(ctx, in.PrescriptionID, in.Attempt, "failed", nil, &avgQuality, qualityWarnings, nil, nil, nil, &errMsg)
			s.recordJobStatus(ctx, now, "failed", int(time.Since(start).Milliseconds()))
			return ocrdomain.ErrBudgetExceeded
		}
		if budgetStatus.AlertTriggered {
			s.logger.WarnContext(ctx, "alerta de consumo de presupuesto de IA", "consumido", budgetStatus.CurrentUSD, "limite", budgetStatus.MonthlyUSD)
		}
	}

	// 3. Etapa de Extracción (Claude / Tesseract) (PP-F4-OCR-003, OCR-004)
	s.updateJob(ctx, in.PrescriptionID, in.Attempt, "extracting", nil, &avgQuality, qualityWarnings, nil, nil, nil, nil)

	hints := ocrdomain.ExtractorHints{
		Country:      in.Country,
		PetSpecies:   in.PetSpecies,
		PetName:      in.PetName,
		GuardianName: in.GuardianName,
	}

	extractor := s.extractor
	if extractor == nil {
		extractor = s.fallbackExt
	}
	if extractor == nil {
		return errors.New("no hay extractor de OCR configurado")
	}

	extResult, err := extractor.Extract(ctx, preprocessedPages, hints)
	if err != nil {
		// Intentar fallback si el principal falló y existe fallback
		if s.fallbackExt != nil && extractor != s.fallbackExt && !extResult.Refused {
			s.logger.WarnContext(ctx, "extractor principal falló, intentando extractor de respaldo", "error", err)
			extResult, err = s.fallbackExt.Extract(ctx, preprocessedPages, hints)
		}
	}

	if err != nil {
		if extResult.Refused {
			errMsg := "Extracción rechazada por el proveedor de IA"
			s.updateJob(ctx, in.PrescriptionID, in.Attempt, "refused", nil, &avgQuality, qualityWarnings, nil, nil, nil, &errMsg)
			s.recordUsage(ctx, now, "anthropic", extractor.ID(), extResult.InputTokens, extResult.OutputTokens, extResult.CostUSD, true)
			s.recordJobStatus(ctx, now, "refused", int(time.Since(start).Milliseconds()))
			return ocrdomain.ErrExtractionRefused
		}

		errMsg := fmt.Sprintf("Fallo en extracción: %v", err)
		s.updateJob(ctx, in.PrescriptionID, in.Attempt, "failed", nil, &avgQuality, qualityWarnings, nil, nil, nil, &errMsg)
		s.recordJobStatus(ctx, now, "failed", int(time.Since(start).Milliseconds()))
		return err
	}

	// Registrar consumo exitoso en ocr.ai_usage_daily
	s.recordUsage(ctx, now, "anthropic", extractor.ID(), extResult.InputTokens, extResult.OutputTokens, extResult.CostUSD, false)

	// Comprobar legibilidad reportada por el modelo (PP-F4-OCR-006)
	if extResult.Extraction.Legibility < s.thresholds.MinLegibility {
		reason := "La legibilidad detectada en la receta es inferior al umbral mínimo requerido (0.40). Por favor, sube una foto más clara."
		s.updateJob(ctx, in.PrescriptionID, in.Attempt, "reupload_requested", ptr(extractor.ID()), &extResult.Extraction.Legibility, extResult.Extraction.Warnings, ptr(int(extResult.InputTokens)), ptr(int(extResult.OutputTokens)), &extResult.CostUSD, nil)
		s.recordJobStatus(ctx, now, "reupload_requested", int(time.Since(start).Milliseconds()))

		return s.executeAggregateCommand(ctx, in.PrescriptionID, func(rx *prescriptiondomain.Prescription) error {
			_, err := rx.RequestReupload(reason)
			return err
		})
	}

	// 4. Etapa de Emparejamiento con el Catálogo (PP-F4-OCR-005)
	s.updateJob(ctx, in.PrescriptionID, in.Attempt, "matching", ptr(extractor.ID()), &extResult.Extraction.Legibility, extResult.Extraction.Warnings, ptr(int(extResult.InputTokens)), ptr(int(extResult.OutputTokens)), &extResult.CostUSD, nil)

	if s.matcher != nil {
		for i := range extResult.Extraction.Items {
			it := &extResult.Extraction.Items[i]
			candidates, _, matchErr := s.matcher.MatchItem(ctx, it.Product.Text, it.RawText, in.PetSpecies)
			if matchErr == nil && len(candidates) > 0 {
				it.Product.Candidates = candidates
			}
		}
	}

	// 5. Aplicar al Agregado Prescription vía RecordExtraction (PP-F4-DOM-007)
	latency := int(time.Since(start).Milliseconds())
	extResult.Extraction.LatencyMs = latency

	cmdErr := s.executeAggregateCommand(ctx, in.PrescriptionID, func(rx *prescriptiondomain.Prescription) error {
		_, err := rx.RecordExtraction(extResult.Extraction)
		return err
	})
	if cmdErr != nil {
		errMsg := fmt.Sprintf("Fallo al registrar extracción en agregado: %v", cmdErr)
		s.updateJob(ctx, in.PrescriptionID, in.Attempt, "failed", ptr(extractor.ID()), &extResult.Extraction.Legibility, extResult.Extraction.Warnings, ptr(int(extResult.InputTokens)), ptr(int(extResult.OutputTokens)), &extResult.CostUSD, &errMsg)
		s.recordJobStatus(ctx, now, "failed", latency)
		return cmdErr
	}

	// 6. Marcar trabajo como 'done' y registrar métricas diarias (PP-F4-OCR-010, DAT-004)
	s.updateJob(ctx, in.PrescriptionID, in.Attempt, "done", ptr(extractor.ID()), &extResult.Extraction.Legibility, extResult.Extraction.Warnings, ptr(int(extResult.InputTokens)), ptr(int(extResult.OutputTokens)), &extResult.CostUSD, nil)
	s.recordJobStatus(ctx, now, "done", latency)

	return nil
}

func (s *PipelineService) executeAggregateCommand(ctx context.Context, rxID uuid.UUID, fn func(rx *prescriptiondomain.Prescription) error) error {
	if s.uow == nil || s.prescriptionRepo == nil {
		return nil
	}

	return s.uow.Do(ctx, func(txCtx context.Context) error {
		rx, err := s.prescriptionRepo.Load(txCtx, rxID)
		if err != nil {
			return err
		}

		if err := fn(rx); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID:    "system",
			ActorRoles: []string{"system"},
			Source:     "worker",
		}
		events, err := s.prescriptionRepo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}

		if s.projector != nil && len(events) > 0 {
			if err := s.projector.Apply(txCtx, events); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PipelineService) updateJob(
	ctx context.Context,
	rxID uuid.UUID,
	attempt int,
	status string,
	extractorID *string,
	legibility *float64,
	warnings []string,
	inTokens *int,
	outTokens *int,
	costUSD *float64,
	errMsg *string,
) {
	if s.jobs == nil {
		return
	}
	var extID string
	if extractorID != nil {
		extID = *extractorID
	}
	_ = s.jobs.UpdateJobStatus(ctx, rxID, attempt, status, extID, legibility, warnings, nil, inTokens, outTokens, costUSD, nil, errMsg)
}

func (s *PipelineService) recordUsage(ctx context.Context, day time.Time, provider, model string, inTokens, outTokens int64, costUSD float64, refused bool) {
	if s.budget == nil {
		return
	}
	_ = s.budget.RecordUsage(ctx, day, provider, model, inTokens, outTokens, costUSD, refused)
}

func (s *PipelineService) recordJobStatus(ctx context.Context, day time.Time, status string, latencyMs int) {
	if s.metrics == nil {
		return
	}
	_ = s.metrics.RecordJobStatus(ctx, day, status, latencyMs)
}

func ptr[T any](v T) *T { return &v }
