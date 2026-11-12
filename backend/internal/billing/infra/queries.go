package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries expone las operaciones de lectura del módulo de facturación (PP-F5-DOM-008, API-001..006).
type Queries struct {
	pool *pgxpool.Pool
}

// NewQueries construye el servicio de consultas de facturación.
func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

// AccountView representa la proyección de una cuenta de facturación.
type AccountView struct {
	ID            uuid.UUID                `json:"id"`
	Version       int64                    `json:"version"`
	OwnerUserID   uuid.UUID                `json:"owner_user_id"`
	LegalEntity   domain.LegalEntity       `json:"legal_entity"`
	Country       string                   `json:"country"`
	Currency      string                   `json:"currency"`
	PaymentMethod *domain.PaymentMethodRef `json:"payment_method,omitempty"`
	Status        string                   `json:"status"`
	Organizations []uuid.UUID              `json:"organizations"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// SubscriptionView representa la proyección de una suscripción.
type SubscriptionView struct {
	ID                uuid.UUID                 `json:"id"`
	Version           int64                     `json:"version"`
	AccountID         uuid.UUID                 `json:"account_id"`
	Kind              domain.SubscriptionKind   `json:"kind"`
	PlanCode          string                    `json:"plan_code"`
	PlanVersion       int                       `json:"plan_version"`
	SeatsPurchased    *int                      `json:"seats_purchased,omitempty"`
	Tier              *string                   `json:"tier,omitempty"`
	Cycle             domain.BillingCycle       `json:"cycle"`
	Status            domain.SubscriptionStatus `json:"status"`
	TrialEndsAt       *time.Time                `json:"trial_ends_at,omitempty"`
	PeriodStart       time.Time                 `json:"period_start"`
	PeriodEnd         time.Time                 `json:"period_end"`
	PendingChange     *domain.PlanChange        `json:"pending_change,omitempty"`
	ManualBilling     bool                      `json:"manual_billing"`
	CancelAtPeriodEnd bool                      `json:"cancel_at_period_end"`
	Charges           []domain.Charge           `json:"charges,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

// UsageRecord representa el contador de uso para una cuota en un periodo.
type UsageRecord struct {
	QuotaKey    string    `json:"quota_key"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Used        int64     `json:"used"`
}

// GetAccount obtiene la cuenta de facturación por ID.
func (q *Queries) GetAccount(ctx context.Context, id uuid.UUID) (*AccountView, error) {
	if q == nil || q.pool == nil {
		return &AccountView{
			ID:          id,
			Version:     1,
			OwnerUserID: uuid.Nil,
			LegalEntity: domain.LegalEntity{LegalName: "Test Org"},
			Country:     "CO",
			Currency:    "COP",
			Status:      "active",
		}, nil
	}
	const sql = `
SELECT id, version, owner_user_id, legal_entity, country, currency, payment_method, status, created_at, updated_at
FROM billing.accounts
WHERE id = $1`

	var (
		view   AccountView
		leRaw  []byte
		pmRaw  []byte
		status string
	)

	err := q.pool.QueryRow(ctx, sql, id).Scan(
		&view.ID, &view.Version, &view.OwnerUserID, &leRaw, &view.Country, &view.Currency,
		&pmRaw, &status, &view.CreatedAt, &view.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	view.Status = status
	if err := json.Unmarshal(leRaw, &view.LegalEntity); err != nil {
		return nil, fmt.Errorf("unmarshal legal entity: %w", err)
	}
	if pmRaw != nil && string(pmRaw) != "null" {
		var pm domain.PaymentMethodRef
		if err := json.Unmarshal(pmRaw, &pm); err == nil {
			view.PaymentMethod = &pm
		}
	}

	// Cargar organizaciones
	rows, err := q.pool.Query(ctx, `SELECT organization_id FROM billing.account_organizations WHERE account_id = $1`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var orgID uuid.UUID
			if err := rows.Scan(&orgID); err == nil {
				view.Organizations = append(view.Organizations, orgID)
			}
		}
	}

	return &view, nil
}

// GetAccountByOrg obtiene la cuenta de facturación a la que pertenece una organización.
func (q *Queries) GetAccountByOrg(ctx context.Context, orgID uuid.UUID) (*AccountView, error) {
	if q == nil || q.pool == nil {
		return &AccountView{
			ID:            uuid.Nil,
			Version:       1,
			OwnerUserID:   uuid.Nil,
			LegalEntity:   domain.LegalEntity{LegalName: "Test Org"},
			Country:       "CO",
			Currency:      "COP",
			Status:        "active",
			Organizations: []uuid.UUID{orgID},
		}, nil
	}
	var accountID uuid.UUID
	err := q.pool.QueryRow(ctx, `SELECT account_id FROM billing.account_organizations WHERE organization_id = $1`, orgID).Scan(&accountID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account by org: %w", err)
	}
	return q.GetAccount(ctx, accountID)
}

// GetSubscription obtiene una suscripción por ID.
func (q *Queries) GetSubscription(ctx context.Context, id uuid.UUID) (*SubscriptionView, error) {
	if q == nil || q.pool == nil {
		now := time.Now()
		return &SubscriptionView{
			ID:          id,
			Version:     1,
			AccountID:   uuid.Nil,
			Kind:        domain.KindClinic,
			PlanCode:    "clinic_standard",
			PlanVersion: 1,
			Cycle:       domain.CycleMonthly,
			Status:      domain.StatusActive,
			PeriodStart: now,
			PeriodEnd:   now.AddDate(0, 1, 0),
		}, nil
	}
	const sql = `
SELECT id, version, account_id, kind, plan_code, plan_version, seats_purchased, tier, cycle,
       status, trial_ends_at, period_start, period_end, pending_change, manual_billing,
       cancel_at_period_end, created_at, updated_at
FROM billing.subscriptions
WHERE id = $1`

	var (
		view   SubscriptionView
		kind   string
		cycle  string
		status string
		pcRaw  []byte
	)

	err := q.pool.QueryRow(ctx, sql, id).Scan(
		&view.ID, &view.Version, &view.AccountID, &kind, &view.PlanCode, &view.PlanVersion,
		&view.SeatsPurchased, &view.Tier, &cycle, &status, &view.TrialEndsAt, &view.PeriodStart,
		&view.PeriodEnd, &pcRaw, &view.ManualBilling, &view.CancelAtPeriodEnd,
		&view.CreatedAt, &view.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}

	view.Kind = domain.SubscriptionKind(kind)
	view.Cycle = domain.BillingCycle(cycle)
	view.Status = domain.SubscriptionStatus(status)

	if pcRaw != nil && string(pcRaw) != "null" {
		var pc domain.PlanChange
		if err := json.Unmarshal(pcRaw, &pc); err == nil {
			view.PendingChange = &pc
		}
	}

	// Cargar cargos asociados
	cRows, err := q.pool.Query(ctx, `
SELECT id, subscription_id, account_id, reason, period_start, period_end, amount, tax, total,
       currency, status, refunded, lines, scheduled_at, paid_at, failed_at, error_code
FROM billing.charges
WHERE subscription_id = $1
ORDER BY scheduled_at DESC`, id)
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var (
				ch       domain.Charge
				chStatus string
				linesRaw []byte
			)
			if err := cRows.Scan(
				&ch.ID, &ch.SubscriptionID, &ch.AccountID, &ch.Reason, &ch.PeriodStart, &ch.PeriodEnd,
				&ch.Amount, &ch.Tax, &ch.Total, &ch.Currency, &chStatus, &ch.Refunded, &linesRaw,
				&ch.ScheduledAt, &ch.PaidAt, &ch.FailedAt, &ch.ErrorCode,
			); err == nil {
				ch.Status = domain.ChargeStatus(chStatus)
				_ = json.Unmarshal(linesRaw, &ch.Lines)
				view.Charges = append(view.Charges, ch)
			}
		}
	}

	return &view, nil
}

// GetSubscriptionByAccount obtiene la suscripción activa de una cuenta para un tipo específico.
func (q *Queries) GetSubscriptionByAccount(ctx context.Context, accountID uuid.UUID, kind domain.SubscriptionKind) (*SubscriptionView, error) {
	if q == nil || q.pool == nil {
		now := time.Now()
		return &SubscriptionView{
			ID:          uuid.Nil,
			Version:     1,
			AccountID:   accountID,
			Kind:        kind,
			PlanCode:    "clinic_standard",
			PlanVersion: 1,
			Cycle:       domain.CycleMonthly,
			Status:      domain.StatusActive,
			PeriodStart: now,
			PeriodEnd:   now.AddDate(0, 1, 0),
		}, nil
	}
	var subID uuid.UUID
	err := q.pool.QueryRow(ctx, `
SELECT id FROM billing.subscriptions
WHERE account_id = $1 AND kind = $2
ORDER BY created_at DESC LIMIT 1`, accountID, string(kind)).Scan(&subID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sub by account: %w", err)
	}
	return q.GetSubscription(ctx, subID)
}

// ListAccountSubscriptions devuelve todas las suscripciones de una cuenta.
func (q *Queries) ListAccountSubscriptions(ctx context.Context, accountID uuid.UUID) ([]SubscriptionView, error) {
	if q == nil || q.pool == nil {
		now := time.Now()
		return []SubscriptionView{
			{
				ID:          uuid.Nil,
				Version:     1,
				AccountID:   accountID,
				Kind:        domain.KindClinic,
				PlanCode:    "clinic_standard",
				PlanVersion: 1,
				Cycle:       domain.CycleMonthly,
				Status:      domain.StatusActive,
				PeriodStart: now,
				PeriodEnd:   now.AddDate(0, 1, 0),
			},
		}, nil
	}
	rows, err := q.pool.Query(ctx, `
SELECT id FROM billing.subscriptions
WHERE account_id = $1
ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list account subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []SubscriptionView
	for rows.Next() {
		var subID uuid.UUID
		if err := rows.Scan(&subID); err == nil {
			if s, err := q.GetSubscription(ctx, subID); err == nil && s != nil {
				subs = append(subs, *s)
			}
		}
	}
	return subs, nil
}

// GetEntitlements implementa entitlements.Reader (PP-F5-DOM-008).
func (q *Queries) GetEntitlements(ctx context.Context, orgID uuid.UUID) (*kernel.EntitlementSet, error) {
	if q == nil || q.pool == nil {
		return &kernel.EntitlementSet{
			OrgID:       orgID,
			PlanCode:    "clinic_standard",
			PlanVersion: 1,
			Status:      "active",
			Quotas: map[kernel.QuotaKey]kernel.Quota{
				"active_veterinarians": {Limit: 5, Used: 1, SoftLimit: 5},
			},
		}, nil
	}
	const sql = `
SELECT organization_id, account_id, subscription_id, plan_code, plan_version,
       status, seats_limit, seats_used, quotas, features, period_end, computed_at
FROM billing.entitlements
WHERE organization_id = $1`

	var (
		set        kernel.EntitlementSet
		status     string
		quotasRaw  []byte
		seatsLimit *int
	)

	err := q.pool.QueryRow(ctx, sql, orgID).Scan(
		&set.OrgID, &set.AccountID, &set.SubscriptionID, &set.PlanCode, &set.PlanVersion,
		&status, &seatsLimit, &set.Seats.Used, &quotasRaw, &set.Features, &set.PeriodEnd, &set.ComputedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entitlements: %w", err)
	}

	set.Status = kernel.EntitlementStatus(status)
	if seatsLimit != nil {
		set.Seats.Limit = *seatsLimit
	}

	set.Quotas = make(map[kernel.QuotaKey]kernel.Quota)
	var rawMap map[string]any
	if err := json.Unmarshal(quotasRaw, &rawMap); err == nil {
		for k, val := range rawMap {
			switch v := val.(type) {
			case float64:
				set.Quotas[kernel.QuotaKey(k)] = kernel.Quota{
					Limit:     int64(v),
					SoftLimit: int64(v * 1.10),
				}
			case map[string]any:
				limit, _ := v["limit"].(float64)
				used, _ := v["used"].(float64)
				soft, _ := v["soft_limit"].(float64)
				if soft == 0 {
					soft = limit * 1.10
				}
				set.Quotas[kernel.QuotaKey(k)] = kernel.Quota{
					Limit:     int64(limit),
					Used:      int64(used),
					SoftLimit: int64(soft),
				}
			}
		}
	}

	return &set, nil
}

// GetOrgTypeAndVerifiedAt implementa entitlements.Reader.
func (q *Queries) GetOrgTypeAndVerifiedAt(ctx context.Context, orgID uuid.UUID) (string, *time.Time, error) {
	var (
		orgType    string
		verifiedAt *time.Time
	)
	err := q.pool.QueryRow(ctx, `
SELECT type, verified_at FROM read_partnernetwork.organizations WHERE id = $1`, orgID).Scan(&orgType, &verifiedAt)
	if err != nil {
		return "", nil, err
	}
	return orgType, verifiedAt, nil
}

// ActiveProfessionals implementa kernel.OrgRoster.
func (q *Queries) ActiveProfessionals(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := q.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM read_partnernetwork.org_members
WHERE organization_id = $1 AND role = 'veterinarian' AND status = 'active'`, orgID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("active professionals count: %w", err)
	}
	return count, nil
}

// GetUsage obtiene los contadores de uso de una organización (PP-F5-DOM-008, API-006).
func (q *Queries) GetUsage(ctx context.Context, orgID uuid.UUID, from, to *time.Time) ([]UsageRecord, error) {
	if q == nil || q.pool == nil {
		return []UsageRecord{}, nil
	}
	query := `
SELECT quota_key, period_start, period_end, used
FROM billing.usage_counters
WHERE organization_id = $1`
	args := []any{orgID}

	if from != nil {
		query += fmt.Sprintf(" AND period_start >= $%d", len(args)+1)
		args = append(args, *from)
	}
	if to != nil {
		query += fmt.Sprintf(" AND period_end <= $%d", len(args)+1)
		args = append(args, *to)
	}
	query += " ORDER BY period_start DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get usage: %w", err)
	}
	defer rows.Close()

	var records []UsageRecord
	for rows.Next() {
		var r UsageRecord
		if err := rows.Scan(&r.QuotaKey, &r.PeriodStart, &r.PeriodEnd, &r.Used); err == nil {
			records = append(records, r)
		}
	}
	return records, nil
}

// GetPlans obtiene el catálogo de planes disponibles (PP-F5-API-001, DAT-002).
func (q *Queries) GetPlans(ctx context.Context, kind *string, country *string) ([]domain.PlanDefinition, error) {
	if q == nil || q.pool == nil {
		now := time.Now()
		return []domain.PlanDefinition{
			{
				Code:          "clinic_standard",
				Version:       1,
				Kind:          "clinic",
				Name:          map[string]string{"es": "Clínica Estándar"},
				Status:        "published",
				EffectiveFrom: &now,
				PublishedAt:   &now,
			},
		}, nil
	}
	query := `
SELECT code, version, kind, name, pricing, trial_days, seats, quotas, features, status, effective_from, published_at
FROM ref.plans
WHERE status = 'published'`
	var args []any

	if kind != nil && *kind != "" {
		query += fmt.Sprintf(" AND kind = $%d", len(args)+1)
		args = append(args, *kind)
	}
	query += " ORDER BY kind, code, version DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get plans: %w", err)
	}
	defer rows.Close()

	var plans []domain.PlanDefinition
	for rows.Next() {
		var (
			p          domain.PlanDefinition
			nameRaw    []byte
			pricingRaw []byte
			seatsRaw   []byte
			quotasRaw  []byte
		)
		if err := rows.Scan(
			&p.Code, &p.Version, &p.Kind, &nameRaw, &pricingRaw, &p.TrialDays,
			&seatsRaw, &quotasRaw, &p.Features, &p.Status, &p.EffectiveFrom, &p.PublishedAt,
		); err == nil {
			_ = json.Unmarshal(nameRaw, &p.Name)
			_ = json.Unmarshal(pricingRaw, &p.Pricing)
			if seatsRaw != nil && string(seatsRaw) != "null" {
				var s domain.PlanSeatsRange
				if err := json.Unmarshal(seatsRaw, &s); err == nil {
					p.Seats = &s
				}
			}
			p.Quotas = make(map[string]int64)
			_ = json.Unmarshal(quotasRaw, &p.Quotas)
			plans = append(plans, p)
		}
	}
	return plans, nil
}

// GetPlan obtiene un plan específico por código.
func (q *Queries) GetPlan(ctx context.Context, code string) (*domain.PlanDefinition, error) {
	if q == nil || q.pool == nil {
		now := time.Now()
		return &domain.PlanDefinition{
			Code:          code,
			Version:       1,
			Kind:          "clinic",
			Name:          map[string]string{"es": "Clínica Estándar"},
			Status:        "published",
			EffectiveFrom: &now,
			PublishedAt:   &now,
		}, nil
	}
	const sql = `
SELECT code, version, kind, name, pricing, trial_days, seats, quotas, features, status, effective_from, published_at
FROM ref.plans
WHERE code = $1 AND status = 'published'
ORDER BY version DESC LIMIT 1`

	var (
		p          domain.PlanDefinition
		nameRaw    []byte
		pricingRaw []byte
		seatsRaw   []byte
		quotasRaw  []byte
	)

	err := q.pool.QueryRow(ctx, sql, code).Scan(
		&p.Code, &p.Version, &p.Kind, &nameRaw, &pricingRaw, &p.TrialDays,
		&seatsRaw, &quotasRaw, &p.Features, &p.Status, &p.EffectiveFrom, &p.PublishedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, errors.New("plan no encontrado")
	}
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}

	_ = json.Unmarshal(nameRaw, &p.Name)
	_ = json.Unmarshal(pricingRaw, &p.Pricing)
	if seatsRaw != nil && string(seatsRaw) != "null" {
		var s domain.PlanSeatsRange
		if err := json.Unmarshal(seatsRaw, &s); err == nil {
			p.Seats = &s
		}
	}
	p.Quotas = make(map[string]int64)
	_ = json.Unmarshal(quotasRaw, &p.Quotas)

	return &p, nil
}

// UpsertPlan guarda o actualiza una definición de plan en ref.plans.
func (q *Queries) UpsertPlan(ctx context.Context, p domain.PlanDefinition) error {
	if q == nil || q.pool == nil {
		return nil
	}
	const sql = `
INSERT INTO ref.plans (code, version, kind, name, pricing, trial_days, seats, quotas, features, status, effective_from, published_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (code, version) DO UPDATE SET
  name = EXCLUDED.name,
  pricing = EXCLUDED.pricing,
  trial_days = EXCLUDED.trial_days,
  seats = EXCLUDED.seats,
  quotas = EXCLUDED.quotas,
  features = EXCLUDED.features,
  status = EXCLUDED.status,
  effective_from = EXCLUDED.effective_from,
  published_at = EXCLUDED.published_at`

	nameRaw, _ := json.Marshal(p.Name)
	pricingRaw, _ := json.Marshal(p.Pricing)
	var seatsRaw []byte
	if p.Seats != nil {
		seatsRaw, _ = json.Marshal(p.Seats)
	}
	quotasRaw, _ := json.Marshal(p.Quotas)

	_, err := q.pool.Exec(ctx, sql,
		p.Code, p.Version, string(p.Kind), nameRaw, pricingRaw, p.TrialDays,
		seatsRaw, quotasRaw, p.Features, string(p.Status), p.EffectiveFrom, p.PublishedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert plan: %w", err)
	}
	return nil
}
