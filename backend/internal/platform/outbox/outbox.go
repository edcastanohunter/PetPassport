// Package outbox escribe la tabla es.outbox en la misma transacción que el
// Append de eventos (PP-F1-ARQ-006, DAT-003). En F1 nadie la consume: se
// expone la métrica outbox_pending para comprobar que crece. El consumidor
// llega en F2 con este contrato: lectura por global_position con
// FOR UPDATE SKIP LOCKED, publicación, marca published_at, fail-stop con
// reintentos y dead-letter (ADR-009).
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
}

// Enqueue inserta las filas del outbox. Idempotente por PK (global_position).
func Enqueue(ctx context.Context, tx pgx.Tx, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`INSERT INTO es.outbox (global_position, event_id, event_type) VALUES ($1, $2, $3)
		             ON CONFLICT (global_position) DO NOTHING`, r.GlobalPosition, r.EventID, r.EventType)
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
// cada scrape (una query por /metrics; el volumen de F1 lo permite).
func PendingGauge(q RowQuerier) prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "outbox_pending",
		Help: "Eventos en es.outbox pendientes de publicar (0 esperado en F1: nadie consume).",
	}, func() float64 {
		var n int64
		if err := q.QueryRow(context.Background(), `SELECT count(*) FROM es.outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
			return -1
		}
		return float64(n)
	})
}
