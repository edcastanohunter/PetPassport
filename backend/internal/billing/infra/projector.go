package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// BillingProjector mantiene los read models de billing.accounts, subscriptions, charges y entitlements (PP-F5-DAT-001).
// Es idempotente ante reproyecciones.
type BillingProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*BillingProjector)(nil)

// NewBillingProjector construye el proyector síncrono del módulo de facturación.
func NewBillingProjector(reg *eventstore.Registry) *BillingProjector {
	return &BillingProjector{reg: reg}
}

func (*BillingProjector) Name() string { return "billing.read_billing" }

func (*BillingProjector) Handles() []string {
	return []string{
		"BillingAccountOpened",
		"LegalEntityUpdated",
		"OrganizationAttached",
		"OrganizationDetached",
		"PaymentMethodSet",
		"PaymentMethodRemoved",
		"BillingAccountClosed",
		"TrialStarted",
		"Subscribed",
		"PlanChangeApplied",
		"PlanChangeScheduled",
		"SeatsAdjusted",
		"PeriodRenewed",
		"ChargeScheduled",
		"ChargePaid",
		"ChargeFailed",
		"ChargeRefunded",
		"SubscriptionGraceStarted",
		"SubscriptionRestricted",
		"SubscriptionReactivated",
		"SubscriptionCancellationScheduled",
		"SubscriptionCancelled",
		"SubscriptionResumed",
		"TrialExpired",
		"AdminAdjustmentApplied",
		"CouponAppliedEvent",
	}
}

func jsonRaw(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

func (p *BillingProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("billing projector decode: %w", err)
	}

	switch ev := decoded.(type) {
	case domain.BillingAccountOpened:
		leJSON, err := jsonRaw(ev.LegalEntity)
		if err != nil {
			return err
		}
		const q = `
INSERT INTO billing.accounts (
  id, version, owner_user_id, legal_entity, country, currency,
  payment_method, provider, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, 'active', $7, $7)
ON CONFLICT (id) DO UPDATE SET
  version = EXCLUDED.version,
  legal_entity = EXCLUDED.legal_entity,
  updated_at = EXCLUDED.updated_at`
		if _, err := tx.Exec(ctx, q, ev.AccountID, e.StreamVersion, ev.OwnerUserID, leJSON, ev.Country, ev.Currency, ev.At); err != nil {
			return fmt.Errorf("project BillingAccountOpened: %w", err)
		}

	case domain.LegalEntityUpdated:
		leJSON, err := jsonRaw(ev.LegalEntity)
		if err != nil {
			return err
		}
		const q = `UPDATE billing.accounts SET version = $2, legal_entity = $3, updated_at = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.AccountID, e.StreamVersion, leJSON, ev.At); err != nil {
			return fmt.Errorf("project LegalEntityUpdated: %w", err)
		}

	case domain.OrganizationAttached:
		const q = `
INSERT INTO billing.account_organizations (account_id, organization_id, seat_pool, joined_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (account_id, organization_id) DO UPDATE SET seat_pool = EXCLUDED.seat_pool`
		if _, err := tx.Exec(ctx, q, ev.AccountID, ev.OrganizationID, ev.SeatPool, ev.At); err != nil {
			return fmt.Errorf("project OrganizationAttached: %w", err)
		}
		// Recalcular entitlements para la nueva organización vinculada
		if err := p.recalculateEntitlementsForAccount(ctx, tx, ev.AccountID); err != nil {
			return err
		}

	case domain.OrganizationDetached:
		const q = `DELETE FROM billing.account_organizations WHERE account_id = $1 AND organization_id = $2`
		if _, err := tx.Exec(ctx, q, ev.AccountID, ev.OrganizationID); err != nil {
			return fmt.Errorf("project OrganizationDetached: %w", err)
		}
		// Borrar o degradar entitlements de la organización desvinculada
		const delEnt = `DELETE FROM billing.entitlements WHERE organization_id = $1`
		if _, err := tx.Exec(ctx, delEnt, ev.OrganizationID); err != nil {
			return err
		}

	case domain.PaymentMethodSet:
		pmJSON, err := jsonRaw(ev.PaymentMethod)
		if err != nil {
			return err
		}
		const q = `UPDATE billing.accounts SET version = $2, payment_method = $3, updated_at = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.AccountID, e.StreamVersion, pmJSON, ev.At); err != nil {
			return fmt.Errorf("project PaymentMethodSet: %w", err)
		}

	case domain.PaymentMethodRemoved:
		const q = `UPDATE billing.accounts SET version = $2, payment_method = NULL, updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.AccountID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project PaymentMethodRemoved: %w", err)
		}

	case domain.BillingAccountClosed:
		const q = `UPDATE billing.accounts SET version = $2, status = 'closed', updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.AccountID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project BillingAccountClosed: %w", err)
		}

	case domain.TrialStarted:
		const q = `
INSERT INTO billing.subscriptions (
  id, version, account_id, kind, plan_code, plan_version, seats_purchased,
  tier, cycle, status, trial_ends_at, period_start, period_end,
  manual_billing, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, 'monthly', 'trialing', $7, $8, $9, false, $10, $10)
ON CONFLICT (account_id, kind) DO UPDATE SET
  version = EXCLUDED.version,
  status = EXCLUDED.status,
  trial_ends_at = EXCLUDED.trial_ends_at,
  period_start = EXCLUDED.period_start,
  period_end = EXCLUDED.period_end,
  updated_at = EXCLUDED.updated_at`
		if _, err := tx.Exec(ctx, q,
			ev.SubscriptionID, e.StreamVersion, ev.AccountID, string(ev.Kind), ev.PlanCode, ev.PlanVersion,
			ev.TrialEndsAt, ev.CurrentPeriod.Start, ev.CurrentPeriod.End, ev.At,
		); err != nil {
			return fmt.Errorf("project TrialStarted: %w", err)
		}
		if err := p.recalculateEntitlementsForAccount(ctx, tx, ev.AccountID); err != nil {
			return err
		}

	case domain.Subscribed:
		seatsVal := 0
		if ev.Seats != nil {
			seatsVal = ev.Seats.Purchased
		}
		tierVal := ""
		if ev.Tier != nil {
			tierVal = *ev.Tier
		}
		couponJSON, _ := jsonRaw(ev.Coupon)

		const q = `
INSERT INTO billing.subscriptions (
  id, version, account_id, kind, plan_code, plan_version, seats_purchased,
  tier, cycle, status, trial_ends_at, period_start, period_end,
  coupon, manual_billing, cancel_at_period_end, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'active', NULL, $10, $11, $12, $13, false, $14, $14)
ON CONFLICT (account_id, kind) DO UPDATE SET
  version = EXCLUDED.version,
  plan_code = EXCLUDED.plan_code,
  plan_version = EXCLUDED.plan_version,
  seats_purchased = EXCLUDED.seats_purchased,
  tier = EXCLUDED.tier,
  cycle = EXCLUDED.cycle,
  status = 'active',
  trial_ends_at = NULL,
  period_start = EXCLUDED.period_start,
  period_end = EXCLUDED.period_end,
  coupon = EXCLUDED.coupon,
  manual_billing = EXCLUDED.manual_billing,
  cancel_at_period_end = false,
  updated_at = EXCLUDED.updated_at`
		if _, err := tx.Exec(ctx, q,
			ev.SubscriptionID, e.StreamVersion, ev.AccountID, string(ev.Kind), ev.PlanCode, ev.PlanVersion,
			seatsVal, tierVal, string(ev.Cycle), ev.CurrentPeriod.Start, ev.CurrentPeriod.End,
			couponJSON, ev.ManualBilling, ev.At,
		); err != nil {
			return fmt.Errorf("project Subscribed: %w", err)
		}
		if err := p.recalculateEntitlementsForAccount(ctx, tx, ev.AccountID); err != nil {
			return err
		}

	case domain.PlanChangeApplied:
		seatsVal := 0
		if ev.Seats != nil {
			seatsVal = ev.Seats.Purchased
		}
		tierVal := ""
		if ev.Tier != nil {
			tierVal = *ev.Tier
		}
		const q = `
UPDATE billing.subscriptions SET
  version = $2, plan_code = $3, plan_version = $4, seats_purchased = $5, tier = $6, cycle = $7,
  pending_change = NULL, updated_at = $8
WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.PlanCode, ev.PlanVersion, seatsVal, tierVal, string(ev.Cycle), ev.At); err != nil {
			return fmt.Errorf("project PlanChangeApplied: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.PlanChangeScheduled:
		pcJSON, _ := jsonRaw(ev.PendingChange)
		const q = `UPDATE billing.subscriptions SET version = $2, pending_change = $3, updated_at = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, pcJSON, ev.At); err != nil {
			return fmt.Errorf("project PlanChangeScheduled: %w", err)
		}

	case domain.SeatsAdjusted:
		const q = `UPDATE billing.subscriptions SET version = $2, seats_purchased = $3, updated_at = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.Seats.Purchased, ev.At); err != nil {
			return fmt.Errorf("project SeatsAdjusted: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.PeriodRenewed:
		const q = `
UPDATE billing.subscriptions SET
  version = $2, plan_code = $3, plan_version = $4, period_start = $5, period_end = $6,
  pending_change = NULL, updated_at = $7
WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.PlanCode, ev.PlanVersion, ev.CurrentPeriod.Start, ev.CurrentPeriod.End, ev.At); err != nil {
			return fmt.Errorf("project PeriodRenewed: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.ChargeScheduled:
		linesJSON, _ := jsonRaw(ev.Lines)
		var pStart, pEnd any
		if ev.Period != nil {
			pStart = ev.Period.Start
			pEnd = ev.Period.End
		}
		const q = `
INSERT INTO billing.charges (
  id, subscription_id, account_id, reason, period_start, period_end,
  amount, tax, total, currency, status, refunded, lines, scheduled_at,
  created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', 0, $11, $12, $12, $12)
ON CONFLICT (id) DO NOTHING`
		if _, err := tx.Exec(ctx, q,
			ev.ChargeID, ev.SubscriptionID, ev.AccountID, ev.Reason, pStart, pEnd,
			ev.Amount, ev.Tax, ev.Total, ev.Currency, linesJSON, ev.ScheduledAt,
		); err != nil {
			return fmt.Errorf("project ChargeScheduled: %w", err)
		}

	case domain.ChargePaid:
		const q = `
UPDATE billing.charges SET
  status = 'paid', paid_at = $2, updated_at = $2,
  payment_ref = NULLIF($3, ''), provider = NULLIF($4, '')
WHERE id = $1 AND status IN ('pending','failed','paid')`
		if _, err := tx.Exec(ctx, q, ev.ChargeID, ev.PaidAt, ev.PaymentRef, ev.Provider); err != nil {
			return fmt.Errorf("project ChargePaid: %w", err)
		}

	case domain.ChargeFailed:
		const q = `
UPDATE billing.charges SET
  status = 'failed', failed_at = $2, error_code = $3, updated_at = $2,
  first_failed_at = COALESCE(first_failed_at, $2)
WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.ChargeID, ev.FailedAt, ev.ErrorCode); err != nil {
			return fmt.Errorf("project ChargeFailed: %w", err)
		}

	case domain.ChargeRefunded:
		// Idempotente (ARQ-006): con RefundedTotal se fija el acumulado en vez de
		// sumar. Los eventos anteriores a M5.2 no lo traen y conservan la suma.
		const q = `
WITH next AS (
  SELECT CASE WHEN $4::numeric > 0 THEN GREATEST(refunded, $4::numeric) ELSE refunded + $2::numeric END AS refunded
  FROM billing.charges WHERE id = $1
)
UPDATE billing.charges c SET
  refunded = next.refunded,
  status = CASE WHEN next.refunded >= c.total THEN 'refunded' ELSE 'partially_refunded' END,
  updated_at = $3
FROM next
WHERE c.id = $1`
		if _, err := tx.Exec(ctx, q, ev.ChargeID, ev.Amount, ev.RefundedAt, ev.RefundedTotal); err != nil {
			return fmt.Errorf("project ChargeRefunded: %w", err)
		}

	case domain.SubscriptionGraceStarted:
		const q = `UPDATE billing.subscriptions SET version = $2, status = 'grace', updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionGraceStarted: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.SubscriptionRestricted:
		const q = `UPDATE billing.subscriptions SET version = $2, status = 'restricted', updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionRestricted: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.SubscriptionReactivated:
		const q = `UPDATE billing.subscriptions SET version = $2, status = 'active', updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionReactivated: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.SubscriptionCancellationScheduled:
		const q = `UPDATE billing.subscriptions SET version = $2, cancel_at_period_end = true, updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionCancellationScheduled: %w", err)
		}

	case domain.SubscriptionCancelled:
		const q = `UPDATE billing.subscriptions SET version = $2, status = 'cancelled', cancel_at_period_end = false, cancelled_at = $3, updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionCancelled: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.SubscriptionResumed:
		const q = `UPDATE billing.subscriptions SET version = $2, cancel_at_period_end = false, updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project SubscriptionResumed: %w", err)
		}

	case domain.TrialExpired:
		const q = `UPDATE billing.subscriptions SET version = $2, status = 'restricted', updated_at = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
			return fmt.Errorf("project TrialExpired: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.AdminAdjustmentApplied:
		// Solo la extensión de prueba cambia el read model; el crédito vive en el agregado.
		if ev.TrialEndsAt == nil {
			const q = `UPDATE billing.subscriptions SET version = $2, updated_at = $3 WHERE id = $1`
			if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, ev.At); err != nil {
				return fmt.Errorf("project AdminAdjustmentApplied: %w", err)
			}
			return nil
		}
		const q = `
UPDATE billing.subscriptions SET
  version = $2, status = 'trialing', trial_ends_at = $3, period_end = $3, updated_at = $4
WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, *ev.TrialEndsAt, ev.At); err != nil {
			return fmt.Errorf("project AdminAdjustmentApplied: %w", err)
		}
		if err := p.recalculateEntitlementsForSub(ctx, tx, ev.SubscriptionID); err != nil {
			return err
		}

	case domain.CouponAppliedEvent:
		couponJSON, err := jsonRaw(ev.Coupon)
		if err != nil {
			return err
		}
		const q = `UPDATE billing.subscriptions SET version = $2, coupon = $3, updated_at = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, q, ev.SubscriptionID, e.StreamVersion, couponJSON, ev.At); err != nil {
			return fmt.Errorf("project CouponAppliedEvent: %w", err)
		}
	}

	return nil
}

func (p *BillingProjector) recalculateEntitlementsForSub(ctx context.Context, tx pgx.Tx, subID uuid.UUID) error {
	var accountID uuid.UUID
	err := tx.QueryRow(ctx, "SELECT account_id FROM billing.subscriptions WHERE id = $1", subID).Scan(&accountID)
	if err != nil {
		return err
	}
	return p.recalculateEntitlementsForAccount(ctx, tx, accountID)
}

func (p *BillingProjector) recalculateEntitlementsForAccount(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) error {
	// Obtener la suscripción más relevante de la cuenta
	var (
		subID          uuid.UUID
		planCode       string
		planVersion    int
		seatsPurchased *int
		status         string
		periodEnd      any
	)

	err := tx.QueryRow(ctx, `
SELECT id, plan_code, plan_version, seats_purchased, status, period_end
FROM billing.subscriptions
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT 1`, accountID).Scan(&subID, &planCode, &planVersion, &seatsPurchased, &status, &periodEnd)

	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == pgx.ErrNoRows {
		return nil
	}

	// Obtener cuotas y features de ref.plans
	var (
		quotasJSON   []byte
		featuresList []string
	)
	_ = tx.QueryRow(ctx, `
SELECT quotas, features
FROM ref.plans
WHERE code = $1 AND version = $2`, planCode, planVersion).Scan(&quotasJSON, &featuresList)

	if quotasJSON == nil {
		quotasJSON = []byte(`{"quotes_per_month": 50}`)
	}

	seatsLimit := 0
	if seatsPurchased != nil {
		seatsLimit = *seatsPurchased
	}

	// Actualizar billing.entitlements para cada organización vinculada
	const upsertEnt = `
INSERT INTO billing.entitlements (
  organization_id, account_id, subscription_id, plan_code, plan_version,
  status, seats_limit, seats_used, quotas, features, period_end, computed_at
)
SELECT
  o.organization_id, $1, $2, $3, $4,
  $5, $6, COALESCE((SELECT COUNT(*) FROM read_partnernetwork.org_members m WHERE m.organization_id = o.organization_id AND m.role = 'veterinarian' AND m.status = 'active'), 0),
  $7, $8, $9, now()
FROM billing.account_organizations o
WHERE o.account_id = $1
ON CONFLICT (organization_id) DO UPDATE SET
  account_id = EXCLUDED.account_id,
  subscription_id = EXCLUDED.subscription_id,
  plan_code = EXCLUDED.plan_code,
  plan_version = EXCLUDED.plan_version,
  status = EXCLUDED.status,
  seats_limit = EXCLUDED.seats_limit,
  seats_used = EXCLUDED.seats_used,
  quotas = EXCLUDED.quotas,
  features = EXCLUDED.features,
  period_end = EXCLUDED.period_end,
  computed_at = EXCLUDED.computed_at`

	_, err = tx.Exec(ctx, upsertEnt,
		accountID, subID, planCode, planVersion,
		status, seatsLimit, quotasJSON, featuresList, periodEnd,
	)
	return err
}

// RegisterSyncProjectors registra las proyecciones síncronas de facturación en el Runner.
func RegisterSyncProjectors(runner *projection.Runner, reg *eventstore.Registry) {
	runner.Register(NewBillingProjector(reg))
}

// SyncProjector adapta el runner para ejecución síncrona dentro del Unit of Work.
type SyncProjector struct {
	runner *projection.Runner
}

// NewSyncProjector construye el adaptador del runner.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply ejecuta las proyecciones registradas sobre la transacción actual.
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return a.runner.Apply(ctx, tx, events)
}
