package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DocumentExtractor extrae la información de las imágenes de una receta médica (PP-F4-DOM-013, OCR-003, OCR-004).
type DocumentExtractor interface {
	ID() string
	Extract(ctx context.Context, pages []PreprocessedPage, hints ExtractorHints) (ExtractionResult, error)
}

// Preprocessor procesa, normaliza y analiza la calidad de las imágenes (PP-F4-OCR-002).
type Preprocessor interface {
	ProcessPage(ctx context.Context, pageNum int, rawData []byte) (PreprocessedPage, error)
}

// CatalogMatcher busca y empareja ítems detectados contra el catálogo de medicamentos (PP-F4-DOM-013, OCR-005).
type CatalogMatcher interface {
	MatchItem(ctx context.Context, productText string, rawText string, species string) ([]CandidateProduct, bool, error)
}

// BudgetService gestiona el control de presupuesto y registro de consumo de IA (PP-F4-OCR-008, DAT-004).
type BudgetService interface {
	CheckBudget(ctx context.Context) (BudgetStatus, error)
	RecordUsage(ctx context.Context, day time.Time, provider string, model string, inTokens int64, outTokens int64, costUSD float64, refused bool) error
}

// MetricsRecorder registra las métricas diarias de producción a partir de diffs (PP-F4-OCR-010, DAT-004).
type MetricsRecorder interface {
	RecordDiff(ctx context.Context, day time.Time, diff map[string]any, originalItems []ExtractedItem) error
	RecordJobStatus(ctx context.Context, day time.Time, status string, latencyMs int) error
}

// JobRepository persiste el estado de los trabajos de extracción en read_prescriptions.extraction_jobs (PP-F4-ARQ-012, DAT-001).
type JobRepository interface {
	UpdateJobStatus(ctx context.Context, rxID uuid.UUID, attempt int, status string, extractorID string, legibility *float64, warnings []string, resultKey *string, inTokens *int, outTokens *int, costUSD *float64, latencyMs *int, errMsg *string) error
	GetJob(ctx context.Context, rxID uuid.UUID) (ExtractionJobRecord, error)
}

// ExtractionJobRecord representa la fila en read_prescriptions.extraction_jobs.
type ExtractionJobRecord struct {
	PrescriptionID  uuid.UUID
	Attempt         int
	Status          string // queued, preprocessing, extracting, matching, done, reupload_requested, failed, refused
	ExtractorID     *string
	Legibility      *float64
	Warnings        []string
	ResultObjectKey *string
	InputTokens     *int
	OutputTokens    *int
	CostUSD         *float64
	LatencyMs       *int
	Error           *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
