package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Alert describe una alerta de seguridad por acceso anómalo (PP-F3-SEC-006).
type Alert struct {
	ID             uuid.UUID
	Rule           string
	ActorID        *uuid.UUID
	OrganizationID *uuid.UUID
	Details        map[string]any
	CreatedAt      time.Time
}

// Mailer es el puerto para notificar al SuperAdmin.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Detector analiza platform.audit_log periódicamente para detectar abusos y anomalías.
type Detector struct {
	pool            *pgxpool.Pool
	mailer          Mailer
	superAdminEmail string
	logger          *slog.Logger
}

func NewDetector(pool *pgxpool.Pool, mailer Mailer, superAdminEmail string, logger *slog.Logger) *Detector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Detector{
		pool:            pool,
		mailer:          mailer,
		superAdminEmail: superAdminEmail,
		logger:          logger,
	}
}

// CheckAnomalies ejecuta todas las reglas de detección sobre la ventana especificada.
func (d *Detector) CheckAnomalies(ctx context.Context, now time.Time) ([]Alert, error) {
	if d.pool == nil {
		return nil, nil
	}

	var alerts []Alert

	// 1. Umbrales diarios de fichas distintas por rol (24h)
	thresholdAlerts, err := d.checkDailyRoleThresholds(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking daily role thresholds", "error", err)
	} else {
		alerts = append(alerts, thresholdAlerts...)
	}

	// 2. Accesos nocturnos fuera de horario (00:00–05:00)
	offHoursAlerts, err := d.checkOffHoursBurst(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking off-hours burst", "error", err)
	} else {
		alerts = append(alerts, offHoursAlerts...)
	}

	// 3. Ráfagas de exportación (> 5 exportaciones en 1 hora)
	exportAlerts, err := d.checkExportBurst(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking export burst", "error", err)
	} else {
		alerts = append(alerts, exportAlerts...)
	}

	// 4. Cancelaciones abusivas de pedidos por tutor (> 3 en 30 días, PP-F4-SEC-010)
	cancellationAlerts, err := d.checkAbusiveOrderCancellations(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking abusive order cancellations", "error", err)
	} else {
		alerts = append(alerts, cancellationAlerts...)
	}

	// 5. Farmacias con tasa de rechazo anómala (> 50 %, PP-F4-SEC-010)
	declineAlerts, err := d.checkHighPharmacyDeclineRate(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking high pharmacy decline rate", "error", err)
	} else {
		alerts = append(alerts, declineAlerts...)
	}

	// 6. Partners con webhooks en fallo sostenido > 7 días -> suspender integración (PP-F4-SEC-010)
	webhookAlerts, err := d.checkFailingWebhooksSuspension(ctx, now)
	if err != nil {
		d.logger.ErrorContext(ctx, "error checking failing webhooks suspension", "error", err)
	} else {
		alerts = append(alerts, webhookAlerts...)
	}

	// Guardar y notificar
	for _, a := range alerts {
		if err := d.recordAndNotify(ctx, a); err != nil {
			d.logger.ErrorContext(ctx, "failed to record/notify security alert", "rule", a.Rule, "error", err)
		}
	}

	return alerts, nil
}

func (d *Detector) checkDailyRoleThresholds(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-24 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT actor_id, actor_roles, count(DISTINCT target_id) as distinct_targets
		FROM platform.audit_log
		WHERE occurred_at >= $1 AND action LIKE '%.read'
		GROUP BY actor_id, actor_roles
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var actorID *uuid.UUID
		var roles []string
		var count int64
		if err := rows.Scan(&actorID, &roles, &count); err != nil {
			continue
		}
		if actorID == nil {
			continue
		}

		isVet := contains(roles, "veterinarian")
		isReceptionist := contains(roles, "receptionist")

		if isVet && count > 200 {
			alerts = append(alerts, Alert{
				ID:      uuid.New(),
				Rule:    "vet_daily_threshold_exceeded",
				ActorID: actorID,
				Details: map[string]any{
					"threshold": 200,
					"count":     count,
					"roles":     roles,
				},
				CreatedAt: now,
			})
		} else if isReceptionist && count > 150 {
			alerts = append(alerts, Alert{
				ID:      uuid.New(),
				Rule:    "receptionist_daily_threshold_exceeded",
				ActorID: actorID,
				Details: map[string]any{
					"threshold": 150,
					"count":     count,
					"roles":     roles,
				},
				CreatedAt: now,
			})
		}
	}
	return alerts, nil
}

func (d *Detector) checkOffHoursBurst(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-24 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT actor_id, count(*) as off_hours_count
		FROM platform.audit_log
		WHERE occurred_at >= $1 
		  AND EXTRACT(HOUR FROM occurred_at) >= 0 AND EXTRACT(HOUR FROM occurred_at) < 5
		  AND action LIKE '%.read'
		GROUP BY actor_id
		HAVING count(*) > 20
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var actorID *uuid.UUID
		var count int64
		if err := rows.Scan(&actorID, &count); err != nil {
			continue
		}
		if actorID != nil {
			alerts = append(alerts, Alert{
				ID:      uuid.New(),
				Rule:    "off_hours_burst",
				ActorID: actorID,
				Details: map[string]any{
					"threshold": 20,
					"count":     count,
					"hours":     "00:00-05:00",
				},
				CreatedAt: now,
			})
		}
	}
	return alerts, nil
}

func (d *Detector) checkExportBurst(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-1 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT actor_id, count(*) as export_count
		FROM platform.audit_log
		WHERE occurred_at >= $1 AND action = 'cohort.export'
		GROUP BY actor_id
		HAVING count(*) > 5
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var actorID *uuid.UUID
		var count int64
		if err := rows.Scan(&actorID, &count); err != nil {
			continue
		}
		if actorID != nil {
			alerts = append(alerts, Alert{
				ID:      uuid.New(),
				Rule:    "cohort_export_burst",
				ActorID: actorID,
				Details: map[string]any{
					"threshold": 5,
					"count":     count,
					"window":    "1h",
				},
				CreatedAt: now,
			})
		}
	}
	return alerts, nil
}

func (d *Detector) checkAbusiveOrderCancellations(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-30 * 24 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT guardian_id, count(*) as cancellations
		  FROM read_pharmacyorders.orders
		 WHERE status = 'cancelled' AND updated_at >= $1
		 GROUP BY guardian_id
		HAVING count(*) > 3
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var guardianID uuid.UUID
		var count int64
		if err := rows.Scan(&guardianID, &count); err != nil {
			continue
		}
		alerts = append(alerts, Alert{
			ID:      uuid.New(),
			Rule:    "abusive_order_cancellations",
			ActorID: &guardianID,
			Details: map[string]any{
				"cancellations": count,
				"window":        "30d",
				"action":        "review_required",
			},
			CreatedAt: now,
		})
	}
	return alerts, nil
}

func (d *Detector) checkHighPharmacyDeclineRate(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-30 * 24 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT organization_id,
		       count(*) as total,
		       count(*) FILTER (WHERE status = 'declined') as declined
		  FROM read_pharmacyorders.invitations
		 WHERE sent_at >= $1
		 GROUP BY organization_id
		HAVING count(*) >= 5
		   AND (count(*) FILTER (WHERE status = 'declined')::float / count(*)) > 0.50
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var orgID uuid.UUID
		var total, declined int64
		if err := rows.Scan(&orgID, &total, &declined); err != nil {
			continue
		}
		rate := float64(declined) / float64(total)
		alerts = append(alerts, Alert{
			ID:             uuid.New(),
			Rule:           "high_pharmacy_decline_rate",
			OrganizationID: &orgID,
			Details: map[string]any{
				"total":        total,
				"declined":     declined,
				"decline_rate": rate,
				"threshold":    0.50,
			},
			CreatedAt: now,
		})
	}
	return alerts, nil
}

func (d *Detector) checkFailingWebhooksSuspension(ctx context.Context, now time.Time) ([]Alert, error) {
	windowStart := now.Add(-7 * 24 * time.Hour)
	rows, err := d.pool.Query(ctx, `
		SELECT pi.organization_id, min(wd.created_at) as first_failure
		  FROM read_partnernetwork.partner_integrations pi
		  JOIN read_partnernetwork.webhook_deliveries wd ON wd.organization_id = pi.organization_id
		 WHERE pi.status = 'active'
		   AND NOT EXISTS (
		       SELECT 1 FROM read_partnernetwork.webhook_deliveries s
		        WHERE s.organization_id = pi.organization_id
		          AND s.status = 'sent'
		          AND s.sent_at >= $1
		   )
		 GROUP BY pi.organization_id
		HAVING min(wd.created_at) <= $1
	`, windowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suspendedOrgs []uuid.UUID
	for rows.Next() {
		var orgID uuid.UUID
		var firstFailure time.Time
		if err := rows.Scan(&orgID, &firstFailure); err == nil {
			suspendedOrgs = append(suspendedOrgs, orgID)
		}
	}

	var alerts []Alert
	for _, orgID := range suspendedOrgs {
		_, _ = d.pool.Exec(ctx, `
			UPDATE read_partnernetwork.partner_integrations
			   SET status = 'suspended', updated_at = now()
			 WHERE organization_id = $1 AND status = 'active'
		`, orgID)

		alerts = append(alerts, Alert{
			ID:             uuid.New(),
			Rule:           "partner_webhooks_failing_suspended",
			OrganizationID: &orgID,
			Details: map[string]any{
				"status": "suspended",
				"reason": "webhooks failing continuously for > 7 days",
			},
			CreatedAt: now,
		})
	}
	return alerts, nil
}

func (d *Detector) recordAndNotify(ctx context.Context, a Alert) error {
	detailsJSON, _ := json.Marshal(a.Details)
	_, err := d.pool.Exec(ctx, `
		INSERT INTO platform.security_alerts (id, rule, actor_id, details, status, created_at)
		VALUES ($1, $2, $3, $4, 'open', $5)
	`, a.ID, a.Rule, a.ActorID, detailsJSON, a.CreatedAt)
	if err != nil {
		return err
	}

	if d.mailer != nil && d.superAdminEmail != "" {
		subj := fmt.Sprintf("[ALERTA SEGURIDAD PetPassport] %s", a.Rule)
		body := fmt.Sprintf("Se detectó anomalía de seguridad: %s\nActor ID: %v\nDetalles: %s\nHora: %s",
			a.Rule, a.ActorID, string(detailsJSON), a.CreatedAt.Format(time.RFC3339))
		_ = d.mailer.Send(ctx, d.superAdminEmail, subj, body)
	}

	return nil
}

func contains(arr []string, val string) bool {
	for _, s := range arr {
		if s == val {
			return true
		}
	}
	return false
}
