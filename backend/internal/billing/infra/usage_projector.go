package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pharmadomain "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// UsageProjector mide el consumo de recursos a partir de eventos de negocio (PP-F5-ARQ-005).
// Aplica idempotencia estricta mediante billing.usage_applied.
type UsageProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*UsageProjector)(nil)

// NewUsageProjector construye el proyector de contadores de uso.
func NewUsageProjector(reg *eventstore.Registry) *UsageProjector {
	return &UsageProjector{reg: reg}
}

func (*UsageProjector) Name() string { return "billing.usage_counters" }

func (*UsageProjector) Handles() []string {
	return []string{
		"QuoteReceived",
	}
}

func (p *UsageProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	// 1. Idempotencia por (projection, event_id) (ARQ-005)
	const checkApplied = `
INSERT INTO billing.usage_applied (projection, event_id)
VALUES ('billing.usage_counters', $1)
ON CONFLICT DO NOTHING`

	tag, err := tx.Exec(ctx, checkApplied, e.EventID)
	if err != nil {
		return fmt.Errorf("usage_applied insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // Ya procesado, no-op
	}

	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("usage projector decode: %w", err)
	}

	var (
		orgID    uuid.UUID
		quotaKey string
		delta    int64 = 1
	)

	switch ev := decoded.(type) {
	case pharmadomain.QuoteReceived:
		// Contar respuestas a cotizaciones para la farmacia (DOM-008)
		orgID = ev.OrganizationID
		quotaKey = "quotes_per_month"
	default:
		return nil
	}

	if orgID == uuid.Nil {
		return nil
	}

	// 2. Determinar periodo alineado al ciclo de suscripción (o mes natural si no tiene) (ARQ-005)
	var (
		pStart time.Time
		pEnd   time.Time
	)

	err = tx.QueryRow(ctx, `
SELECT s.period_start, s.period_end
FROM billing.subscriptions s
JOIN billing.account_organizations o ON o.account_id = s.account_id
WHERE o.organization_id = $1
ORDER BY s.created_at DESC
LIMIT 1`, orgID).Scan(&pStart, &pEnd)

	if err == pgx.ErrNoRows {
		// Mes natural si no tiene suscripción activa
		now := e.OccurredAt.UTC()
		pStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		pEnd = pStart.AddDate(0, 1, 0)
	} else if err != nil {
		return fmt.Errorf("query sub period for usage: %w", err)
	}

	// 3. Upsert en billing.usage_counters
	const upsertCounter = `
INSERT INTO billing.usage_counters (organization_id, quota_key, period_start, period_end, used, updated_at)
VALUES ($1, $2, $3::date, $4::date, $5, $6)
ON CONFLICT (organization_id, quota_key, period_start)
DO UPDATE SET
  used = billing.usage_counters.used + EXCLUDED.used,
  updated_at = EXCLUDED.updated_at`

	if _, err := tx.Exec(ctx, upsertCounter, orgID, quotaKey, pStart, pEnd, delta, e.OccurredAt); err != nil {
		return fmt.Errorf("upsert usage counter: %w", err)
	}

	return nil
}
