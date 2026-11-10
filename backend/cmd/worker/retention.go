package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
)

// Retención y limpieza diaria (PP-F2-DAT-008, PP-F4-DAT-006).
const (
	// auditRetention: el audit_log de accesos clínicos se conserva 2 años.
	auditRetention = 2 * 365 * 24 * time.Hour
	// deliveriesRetention: entregas terminales (enviadas/suprimidas) 180 días.
	// Las fallidas y en dead-letter se conservan hasta resolverse.
	deliveriesRetention = 180 * 24 * time.Hour
	// scheduledRetention: recordatorios ya despachados o cancelados, 30 días.
	scheduledRetention = 30 * 24 * time.Hour

	// partnerCallsRetention: llamadas de partners B2B (12 meses).
	partnerCallsRetention = 365 * 24 * time.Hour
	// webhookPayloadRetention: payloads de webhooks para reintento (7 días).
	webhookPayloadRetention = 7 * 24 * time.Hour
	// webhookMetadataRetention: metadatos de entregas de webhooks (180 días).
	webhookMetadataRetention = 180 * 24 * time.Hour
	// extractionArtifactRetention: artefactos JSON de extracción OCR (90 días).
	extractionArtifactRetention = 90 * 24 * time.Hour
	// orderPIIRetention: PII de pedidos cerrados (dirección, teléfono) anonimizada a 180 días.
	orderPIIRetention = 180 * 24 * time.Hour
)

// retentionResult cuenta lo que borró una pasada.
type retentionResult struct {
	AuditPartitions           int
	Deliveries                int64
	Scheduled                 int64
	PartnerCallsPartitions    int
	WebhookPayloadsPurged     int64
	WebhookDeliveriesDeleted  int64
	OrdersPIIAnonymized       int64
	ExtractionArtifactsPurged int64
	ImagesDeleted             int64
}

// runRetention aplica la retención completa de la plataforma (F2 + F4).
func runRetention(ctx context.Context, pool *pgxpool.Pool, objects storage.ObjectStorage, now time.Time) (retentionResult, error) {
	var res retentionResult
	var firstErr error
	keep := func(step string, err error) {
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("retención: %s: %w", step, err)
		}
	}

	// 1. Audit log clínico (2 años)
	cutoffAudit := now.Add(-auditRetention).UTC().Format("2006-01-02")
	keep("audit_log", pool.QueryRow(ctx,
		`SELECT platform.drop_audit_partitions_before($1::date)`, cutoffAudit).Scan(&res.AuditPartitions))

	// 2. Notificaciones enviadas/suprimidas (180 días)
	tag, err := pool.Exec(ctx, `
		DELETE FROM notifications.deliveries
		 WHERE status IN ('sent', 'suppressed') AND NOT dead_letter AND created_at < $1`,
		now.Add(-deliveriesRetention))
	keep("deliveries", err)
	res.Deliveries = tag.RowsAffected()

	// 3. Recordatorios programados ya atendidos (30 días)
	tag, err = pool.Exec(ctx, `
		DELETE FROM notifications.scheduled
		 WHERE status IN ('dispatched', 'cancelled') AND updated_at < $1`,
		now.Add(-scheduledRetention))
	keep("scheduled", err)
	res.Scheduled = tag.RowsAffected()

	// 4. Partner calls particiones (12 meses, PP-F4-DAT-006)
	cutoffCalls := now.Add(-partnerCallsRetention).UTC().Format("2006-01-02")
	keep("partner_calls", pool.QueryRow(ctx,
		`SELECT read_partnernetwork.drop_partner_calls_partitions_before($1::date)`, cutoffCalls).Scan(&res.PartnerCallsPartitions))

	// 5. Payloads de webhooks (7 días)
	rows, err := pool.Query(ctx, `
		SELECT payload_object_key FROM read_partnernetwork.webhook_deliveries
		 WHERE payload_object_key IS NOT NULL AND created_at < $1`,
		now.Add(-webhookPayloadRetention))
	if err == nil {
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err == nil && k != "" {
				keys = append(keys, k)
			}
		}
		if objects != nil {
			for _, k := range keys {
				_ = objects.Delete(ctx, k)
			}
		}
		tagPayloads, errUpdate := pool.Exec(ctx, `
			UPDATE read_partnernetwork.webhook_deliveries
			   SET payload_object_key = NULL
			 WHERE payload_object_key IS NOT NULL AND created_at < $1`,
			now.Add(-webhookPayloadRetention))
		keep("webhook_payloads", errUpdate)
		res.WebhookPayloadsPurged = tagPayloads.RowsAffected()
	} else {
		keep("webhook_payloads_query", err)
	}

	// 6. Metadatos de webhook deliveries (180 días)
	tag, err = pool.Exec(ctx, `
		DELETE FROM read_partnernetwork.webhook_deliveries
		 WHERE status != 'queued' AND created_at < $1`,
		now.Add(-webhookMetadataRetention))
	keep("webhook_deliveries", err)
	res.WebhookDeliveriesDeleted = tag.RowsAffected()

	// 7. PII en pedidos cerrados (dirección, teléfono) a 180 días tras cierre (PP-F4-DAT-006)
	tag, err = pool.Exec(ctx, `
		UPDATE read_pharmacyorders.orders
		   SET delivery_address = '{"anonymized": true}'::jsonb,
		       contact_phone = 'REDACTED'
		 WHERE status IN ('delivered', 'cancelled', 'rejected')
		   AND updated_at < $1
		   AND contact_phone != 'REDACTED'`,
		now.Add(-orderPIIRetention))
	keep("order_pii", err)
	res.OrdersPIIAnonymized = tag.RowsAffected()

	// 8. Artefactos de extracción OCR (JSON, 90 días, PP-F4-DAT-006)
	rowsExt, err := pool.Query(ctx, `
		SELECT result_object_key FROM read_prescriptions.extraction_jobs
		 WHERE result_object_key IS NOT NULL AND created_at < $1`,
		now.Add(-extractionArtifactRetention))
	if err == nil {
		defer rowsExt.Close()
		var extKeys []string
		for rowsExt.Next() {
			var k string
			if err := rowsExt.Scan(&k); err == nil && k != "" {
				extKeys = append(extKeys, k)
			}
		}
		if objects != nil {
			for _, k := range extKeys {
				_ = objects.Delete(ctx, k)
			}
		}
		tagExt, errExt := pool.Exec(ctx, `
			UPDATE read_prescriptions.extraction_jobs
			   SET result_object_key = NULL
			 WHERE result_object_key IS NOT NULL AND created_at < $1`,
			now.Add(-extractionArtifactRetention))
		keep("extraction_artifacts", errExt)
		res.ExtractionArtifactsPurged = tagExt.RowsAffected()
	} else {
		keep("extraction_artifacts_query", err)
	}

	// 9. Imágenes de recetas externas según delete_after (PP-F4-DAT-006)
	rowsImg, err := pool.Query(ctx, `
		SELECT object_key FROM read_prescriptions.images
		 WHERE delete_after IS NOT NULL AND delete_after <= $1`,
		now.UTC().Format("2006-01-02"))
	if err == nil {
		defer rowsImg.Close()
		var imgKeys []string
		for rowsImg.Next() {
			var k string
			if err := rowsImg.Scan(&k); err == nil && k != "" {
				imgKeys = append(imgKeys, k)
			}
		}
		if objects != nil {
			for _, k := range imgKeys {
				_ = objects.Delete(ctx, k)
			}
		}
		tagImg, errImg := pool.Exec(ctx, `
			DELETE FROM read_prescriptions.images
			 WHERE delete_after IS NOT NULL AND delete_after <= $1`,
			now.UTC().Format("2006-01-02"))
		keep("prescription_images", errImg)
		res.ImagesDeleted = tagImg.RowsAffected()
	} else {
		keep("prescription_images_query", err)
	}

	return res, firstErr
}

// EnsurePartnerCallsPartitions asegura que existan las particiones de llamadas de partner del mes actual y el siguiente.
func EnsurePartnerCallsPartitions(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	cur := now.UTC()
	next := cur.AddDate(0, 1, 0)
	for _, m := range []time.Time{cur, next} {
		if _, err := pool.Exec(ctx, `SELECT read_partnernetwork.ensure_partner_calls_partition($1::date)`, m.Format("2006-01-02")); err != nil {
			return fmt.Errorf("ensure_partner_calls_partition(%s): %w", m.Format("2006-01-02"), err)
		}
	}
	return nil
}

// logRetention ejecuta la retención y deja el resultado en el log.
func logRetention(ctx context.Context, pool *pgxpool.Pool, objects storage.ObjectStorage, logger *slog.Logger) {
	res, err := runRetention(ctx, pool, objects, time.Now())
	if err != nil {
		logger.ErrorContext(ctx, "retención diaria con errores", "error", err)
	}
	logger.InfoContext(ctx, "retención diaria",
		"audit_partitions_dropped", res.AuditPartitions,
		"deliveries_deleted", res.Deliveries,
		"scheduled_deleted", res.Scheduled,
		"partner_calls_partitions_dropped", res.PartnerCallsPartitions,
		"webhook_payloads_purged", res.WebhookPayloadsPurged,
		"webhook_deliveries_deleted", res.WebhookDeliveriesDeleted,
		"orders_pii_anonymized", res.OrdersPIIAnonymized,
		"extraction_artifacts_purged", res.ExtractionArtifactsPurged,
		"images_deleted", res.ImagesDeleted,
	)
}
