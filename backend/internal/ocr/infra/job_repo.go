package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// JobRepo gestiona la persistencia en read_prescriptions.extraction_jobs (PP-F4-DAT-001, ARQ-012).
type JobRepo struct {
	pool *pgxpool.Pool
}

func NewJobRepo(pool *pgxpool.Pool) *JobRepo {
	return &JobRepo{pool: pool}
}

// UpdateJobStatus inserta o actualiza el registro del intento en read_prescriptions.extraction_jobs.
func (r *JobRepo) UpdateJobStatus(
	ctx context.Context,
	rxID uuid.UUID,
	attempt int,
	status string,
	extractorID string,
	legibility *float64,
	warnings []string,
	resultKey *string,
	inTokens *int,
	outTokens *int,
	costUSD *float64,
	latencyMs *int,
	errMsg *string,
) error {
	if r.pool == nil {
		return nil
	}

	warningsJSON, _ := json.Marshal(warnings)
	now := time.Now()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO read_prescriptions.extraction_jobs (
			prescription_id, attempt, status, extractor_id, legibility, warnings,
			result_object_key, input_tokens, output_tokens, cost_usd, latency_ms, error,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13
		)
		ON CONFLICT (prescription_id, attempt) DO UPDATE SET
			status = EXCLUDED.status,
			extractor_id = COALESCE(EXCLUDED.extractor_id, read_prescriptions.extraction_jobs.extractor_id),
			legibility = COALESCE(EXCLUDED.legibility, read_prescriptions.extraction_jobs.legibility),
			warnings = COALESCE(EXCLUDED.warnings, read_prescriptions.extraction_jobs.warnings),
			result_object_key = COALESCE(EXCLUDED.result_object_key, read_prescriptions.extraction_jobs.result_object_key),
			input_tokens = COALESCE(EXCLUDED.input_tokens, read_prescriptions.extraction_jobs.input_tokens),
			output_tokens = COALESCE(EXCLUDED.output_tokens, read_prescriptions.extraction_jobs.output_tokens),
			cost_usd = COALESCE(EXCLUDED.cost_usd, read_prescriptions.extraction_jobs.cost_usd),
			latency_ms = COALESCE(EXCLUDED.latency_ms, read_prescriptions.extraction_jobs.latency_ms),
			error = EXCLUDED.error,
			updated_at = EXCLUDED.updated_at
	`, rxID, attempt, status, extractorID, legibility, warningsJSON, resultKey, inTokens, outTokens, costUSD, latencyMs, errMsg, now)
	if err != nil {
		return fmt.Errorf("actualizar extraction_jobs: %w", err)
	}

	return nil
}

// GetJob obtiene el último intento de extracción para una prescripción.
func (r *JobRepo) GetJob(ctx context.Context, rxID uuid.UUID) (ocrdomain.ExtractionJobRecord, error) {
	if r.pool == nil {
		return ocrdomain.ExtractionJobRecord{}, nil
	}

	var rec ocrdomain.ExtractionJobRecord
	var warningsJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT
			prescription_id, attempt, status, extractor_id, legibility, warnings,
			result_object_key, input_tokens, output_tokens, cost_usd, latency_ms, error,
			created_at, updated_at
		FROM read_prescriptions.extraction_jobs
		WHERE prescription_id = $1
		ORDER BY attempt DESC
		LIMIT 1
	`, rxID).Scan(
		&rec.PrescriptionID, &rec.Attempt, &rec.Status, &rec.ExtractorID, &rec.Legibility, &warningsJSON,
		&rec.ResultObjectKey, &rec.InputTokens, &rec.OutputTokens, &rec.CostUSD, &rec.LatencyMs, &rec.Error,
		&rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return ocrdomain.ExtractionJobRecord{}, err
	}

	if len(warningsJSON) > 0 {
		_ = json.Unmarshal(warningsJSON, &rec.Warnings)
	}

	return rec, nil
}

var _ ocrdomain.JobRepository = (*JobRepo)(nil)
