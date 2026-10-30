package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Retención y limpieza diaria (PP-F2-DAT-008).
const (
	// auditRetention: el audit_log de accesos clínicos se conserva 2 años.
	auditRetention = 2 * 365 * 24 * time.Hour
	// deliveriesRetention: entregas terminales (enviadas/suprimidas) 180 días.
	// Las fallidas y en dead-letter se conservan hasta resolverse.
	deliveriesRetention = 180 * 24 * time.Hour
	// scheduledRetention: recordatorios ya despachados o cancelados, 30 días.
	scheduledRetention = 30 * 24 * time.Hour
)

// retentionResult cuenta lo que borró una pasada.
type retentionResult struct {
	AuditPartitions int
	Deliveries      int64
	Scheduled       int64
}

// runRetention aplica la retención. Cada paso es independiente: un fallo se
// devuelve junto al resto de resultados para que el llamador lo registre sin
// impedir los demás.
func runRetention(ctx context.Context, pool *pgxpool.Pool, now time.Time) (retentionResult, error) {
	var res retentionResult
	var firstErr error
	keep := func(step string, err error) {
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("retención: %s: %w", step, err)
		}
	}

	cutoff := now.Add(-auditRetention).UTC().Format("2006-01-02")
	keep("audit_log", pool.QueryRow(ctx,
		`SELECT platform.drop_audit_partitions_before($1::date)`, cutoff).Scan(&res.AuditPartitions))

	tag, err := pool.Exec(ctx, `
		DELETE FROM notifications.deliveries
		 WHERE status IN ('sent', 'suppressed') AND NOT dead_letter AND created_at < $1`,
		now.Add(-deliveriesRetention))
	keep("deliveries", err)
	res.Deliveries = tag.RowsAffected()

	tag, err = pool.Exec(ctx, `
		DELETE FROM notifications.scheduled
		 WHERE status IN ('dispatched', 'cancelled') AND updated_at < $1`,
		now.Add(-scheduledRetention))
	keep("scheduled", err)
	res.Scheduled = tag.RowsAffected()
	return res, firstErr
}

// logRetention ejecuta la retención y deja el resultado en el log.
func logRetention(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	res, err := runRetention(ctx, pool, time.Now())
	if err != nil {
		logger.ErrorContext(ctx, "retención diaria con errores", "error", err)
	}
	logger.InfoContext(ctx, "retención diaria",
		"audit_partitions_dropped", res.AuditPartitions,
		"deliveries_deleted", res.Deliveries, "scheduled_deleted", res.Scheduled)
}
