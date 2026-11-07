package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// Recorder implementa ocrdomain.MetricsRecorder sobre PostgreSQL (PP-F4-OCR-010, DAT-004).
type Recorder struct {
	pool *pgxpool.Pool
}

func NewRecorder(pool *pgxpool.Pool) *Recorder {
	return &Recorder{pool: pool}
}

// RecordDiff calcula estadísticas a partir del diff de confirmación humana y actualiza ocr.metrics_daily.
func (r *Recorder) RecordDiff(ctx context.Context, day time.Time, diff map[string]any, originalItems []ocrdomain.ExtractedItem) error {
	if r.pool == nil || diff == nil {
		return nil
	}

	dateOnly := day.Format("2006-01-02")

	var accepted, edited, removed, added int
	fieldEdits := make(map[string]int)

	if itemsRaw, ok := diff["items"].([]any); ok {
		for _, raw := range itemsRaw {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			action, _ := m["action"].(string)
			switch action {
			case "accepted":
				accepted++
			case "edited":
				edited++
				if fields, ok := m["fields"].([]any); ok {
					for _, f := range fields {
						if fs, ok := f.(string); ok {
							fieldEdits[fs]++
						}
					}
				}
			case "removed":
				removed++
			case "added":
				added++
			default:
				accepted++
			}
		}
	} else {
		// Si no hay diff detallado, todo se asume aceptado
		accepted = len(originalItems)
	}

	total := accepted + edited + removed + added
	if total == 0 {
		total = len(originalItems)
		accepted = total
	}

	// Calcular tasas porcentuales por campo (0 a 1)
	fieldRates := make(map[string]float64)
	if total > 0 {
		for k, v := range fieldEdits {
			fieldRates[k] = float64(v) / float64(total)
		}
	}

	fieldRatesJSON, _ := json.Marshal(fieldRates)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO ocr.metrics_daily (
			day, items_total, items_accepted, items_edited, items_removed, items_added, field_edit_rates
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (day) DO UPDATE SET
			items_total = ocr.metrics_daily.items_total + EXCLUDED.items_total,
			items_accepted = ocr.metrics_daily.items_accepted + EXCLUDED.items_accepted,
			items_edited = ocr.metrics_daily.items_edited + EXCLUDED.items_edited,
			items_removed = ocr.metrics_daily.items_removed + EXCLUDED.items_removed,
			items_added = ocr.metrics_daily.items_added + EXCLUDED.items_added,
			field_edit_rates = EXCLUDED.field_edit_rates
	`, dateOnly, total, accepted, edited, removed, added, fieldRatesJSON)
	if err != nil {
		return fmt.Errorf("actualizar metricas diff: %w", err)
	}

	return nil
}

// RecordJobStatus actualiza contadores de estado de trabajos de extracción en ocr.metrics_daily.
func (r *Recorder) RecordJobStatus(ctx context.Context, day time.Time, status string, latencyMs int) error {
	if r.pool == nil {
		return nil
	}

	dateOnly := day.Format("2006-01-02")
	vol := 1
	var done, reupload, failed, refused int

	switch status {
	case "done":
		done = 1
	case "reupload_requested":
		reupload = 1
	case "failed":
		failed = 1
	case "refused":
		refused = 1
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO ocr.metrics_daily (
			day, volume, done, reupload, failed, refused, avg_latency_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (day) DO UPDATE SET
			volume = ocr.metrics_daily.volume + EXCLUDED.volume,
			done = ocr.metrics_daily.done + EXCLUDED.done,
			reupload = ocr.metrics_daily.reupload + EXCLUDED.reupload,
			failed = ocr.metrics_daily.failed + EXCLUDED.failed,
			refused = ocr.metrics_daily.refused + EXCLUDED.refused,
			avg_latency_ms = CASE
				WHEN ocr.metrics_daily.volume + EXCLUDED.volume > 0
				THEN (ocr.metrics_daily.avg_latency_ms * ocr.metrics_daily.volume + EXCLUDED.avg_latency_ms) / (ocr.metrics_daily.volume + EXCLUDED.volume)
				ELSE EXCLUDED.avg_latency_ms
			END
	`, dateOnly, vol, done, reupload, failed, refused, latencyMs)
	if err != nil {
		return fmt.Errorf("actualizar status diario de OCR: %w", err)
	}

	return nil
}

var _ ocrdomain.MetricsRecorder = (*Recorder)(nil)
