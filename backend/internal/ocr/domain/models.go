package domain

import (
	"time"

	"github.com/google/uuid"

	prescriptiondomain "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
)

// Re-exportamos y complementamos tipos del dominio de Prescription para coherencia
type Extraction = prescriptiondomain.Extraction
type ExtractedHeader = prescriptiondomain.ExtractedHeader
type ExtractedItem = prescriptiondomain.ExtractedItem
type ExtractedProduct = prescriptiondomain.ExtractedProduct
type CandidateProduct = prescriptiondomain.CandidateProduct

// PreprocessedPage representa el resultado del preprocesamiento de una página de receta (PP-F4-OCR-002).
type PreprocessedPage struct {
	PageNumber   int
	Data         []byte
	ContentType  string // image/webp o image/jpeg
	Width        int
	Height       int
	Sha256       []byte
	QualityScore float64 // 0.0 - 1.0 (Laplacian variance, brillo, cobertura de bordes)
	IsTooBlurry  bool
	IsTooDark    bool
	Warnings     []string
}

// PreprocessResult agrupa las páginas procesadas de un documento.
type PreprocessResult struct {
	Pages        []PreprocessedPage
	QualityScore float64 // Mínimo o promedio de calidad de las páginas
	Warnings     []string
}

// ExtractorHints proporciona contexto opcional para mejorar la extracción.
type ExtractorHints struct {
	Country      string
	PetSpecies   string
	PetName      string
	GuardianName string
}

// ExtractionResult es el resultado unificado devuelto por DocumentExtractor (PP-F4-OCR-003, OCR-004).
type ExtractionResult struct {
	Extraction    Extraction
	InputTokens   int64
	OutputTokens  int64
	CostUSD       float64
	LatencyMs     int
	Refused       bool
	RefusalReason string
}

// ThresholdsConfig define los umbrales configurables del pipeline (PP-F4-OCR-006).
type ThresholdsConfig struct {
	MinQualityScore   float64 // por defecto 0.35
	MinLegibility     float64 // por defecto 0.40
	MinProductConf    float64 // por defecto 0.60
	MinNumericConf    float64 // por defecto 0.50
	CandidateAutoTop1 float64 // por defecto 0.85
	CandidateAutoTop2 float64 // por defecto 0.60
}

func DefaultThresholds() ThresholdsConfig {
	return ThresholdsConfig{
		MinQualityScore:   0.35,
		MinLegibility:     0.40,
		MinProductConf:    0.60,
		MinNumericConf:    0.50,
		CandidateAutoTop1: 0.85,
		CandidateAutoTop2: 0.60,
	}
}

// BudgetStatus indica la situación del presupuesto mensual de IA (PP-F4-OCR-008).
type BudgetStatus struct {
	MonthlyUSD     float64
	CurrentUSD     float64
	HardStop       bool
	AlertPct       int
	Exceeded       bool
	AlertTriggered bool
}

// DiffStats resume las estadísticas derivadas del diff de confirmación (PP-F4-OCR-010).
type DiffStats struct {
	ItemsTotal     int
	ItemsAccepted  int
	ItemsEdited    int
	ItemsRemoved   int
	ItemsAdded     int
	FieldEditRates map[string]float64
}

// EvalSample representa una muestra de prueba del dataset de evaluación (PP-F4-OCR-007, SEC-008).
type EvalSample struct {
	ID         uuid.UUID
	ObjectKey  string
	ImageData  []byte
	Labels     Extraction
	Source     string // synthetic | donated | production_consented
	ConsentRef *string
	CreatedAt  time.Time
}

// EvalMetrics agrupa las métricas calculadas sobre una ejecución de evaluación (PP-F4-OCR-007, QA-003).
type EvalMetrics struct {
	TotalSamples     int                `json:"total_samples"`
	F1Product        float64            `json:"f1_product"`
	F1Dose           float64            `json:"f1_dose"`
	F1Frequency      float64            `json:"f1_frequency"`
	F1Duration       float64            `json:"f1_duration"`
	F1Quantity       float64            `json:"f1_quantity"`
	HeaderExactMatch float64            `json:"header_exact_match"`
	ReuploadRate     float64            `json:"reupload_rate"`
	AvgLatencyMs     int                `json:"avg_latency_ms"`
	AvgCostUSD       float64            `json:"avg_cost_usd"`
	FieldMetrics     map[string]float64 `json:"field_metrics"`
}
