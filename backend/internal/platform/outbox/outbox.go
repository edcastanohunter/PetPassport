// Package outbox escribe la tabla es.outbox en la misma transacción que el
// Append de eventos (PP-F1-ARQ-006, DAT-003). El outbox se reserva a los
// process managers con efectos externos (ADR-009 §2); su consumidor vive en
// platform/process (PP-F2-ARQ-008): lectura por global_position con
// FOR UPDATE SKIP LOCKED, orden por stream, reintentos y dead-letter.
package outbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

// Row es un evento recién persistido listo para encolar.
type Row struct {
	GlobalPosition int64
	EventID        uuid.UUID
	EventType      string
	StreamID       uuid.UUID // orden por stream del consumidor (PP-F2-DAT-001)
}

// Enqueue inserta las filas del outbox. Idempotente por PK (global_position).
func Enqueue(ctx context.Context, tx pgx.Tx, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`INSERT INTO es.outbox (global_position, event_id, event_type, stream_id) VALUES ($1, $2, $3, $4)
		             ON CONFLICT (global_position) DO NOTHING`, r.GlobalPosition, r.EventID, r.EventType, r.StreamID)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("outbox: enqueue: %w", err)
	}
	return nil
}

// RowQuerier es lo necesario para contar pendientes (un pool sirve).
type RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PendingGauge expone outbox_pending como GaugeFunc consultando la tabla en
// cada scrape (índice parcial outbox_pending_idx). Excluye dead-letter.
func PendingGauge(q RowQuerier) prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "outbox_pending",
		Help: "Eventos en es.outbox pendientes de publicar (sin contar dead-letter).",
	}, func() float64 {
		var n int64
		if err := q.QueryRow(context.Background(), `SELECT count(*) FROM es.outbox WHERE published_at IS NULL AND NOT dead_letter`).Scan(&n); err != nil {
			return -1
		}
		return float64(n)
	})
}

// AgeGauge expone outbox_age_seconds: antigüedad del pendiente más antiguo
// (alerta OutboxStuck, PP-F2-INF-002/003). 0 si no hay pendientes.
func AgeGauge(q RowQuerier) prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "outbox_age_seconds",
		Help: "Antigüedad en segundos del evento pendiente más antiguo del outbox (sin dead-letter).",
	}, func() float64 {
		var age float64
		if err := q.QueryRow(context.Background(), `
			SELECT coalesce(extract(epoch FROM now() - min(enqueued_at)), 0)::float8
			  FROM es.outbox WHERE published_at IS NULL AND NOT dead_letter`).Scan(&age); err != nil {
			return -1
		}
		return age
	})
}
