package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
)

// Versionado de planes de SuperAdmin (PP-F5-API-001): cada PUT crea una versión
// en draft; publish la hace vigente desde effective_from y retire la saca del
// catálogo sin tocar las suscripciones que ya la fijaron (B10).

// ErrPlanNotFound: el plan no tiene versión publicada vigente.
var ErrPlanNotFound = errors.New("billing: plan no encontrado")

// ErrPlanVersionState: la versión no está en el estado que exige la operación.
var ErrPlanVersionState = errors.New("billing: la versión del plan no admite esta operación")

const planColumns = `code, version, kind, name, pricing, trial_days, seats, quotas, features, status, effective_from, published_at`

func scanPlan(row pgx.Row) (domain.PlanDefinition, error) {
	var (
		p                                     domain.PlanDefinition
		nameRaw, pricingRaw, seatsRaw, quotas []byte
	)
	if err := row.Scan(&p.Code, &p.Version, &p.Kind, &nameRaw, &pricingRaw, &p.TrialDays,
		&seatsRaw, &quotas, &p.Features, &p.Status, &p.EffectiveFrom, &p.PublishedAt); err != nil {
		return p, err
	}
	if err := json.Unmarshal(nameRaw, &p.Name); err != nil {
		return p, fmt.Errorf("plan %s: name: %w", p.Code, err)
	}
	if err := json.Unmarshal(pricingRaw, &p.Pricing); err != nil {
		return p, fmt.Errorf("plan %s: pricing: %w", p.Code, err)
	}
	if len(seatsRaw) > 0 && string(seatsRaw) != "null" {
		var s domain.PlanSeatsRange
		if err := json.Unmarshal(seatsRaw, &s); err != nil {
			return p, fmt.Errorf("plan %s: seats: %w", p.Code, err)
		}
		p.Seats = &s
	}
	p.Quotas = map[string]int64{}
	if len(quotas) > 0 {
		if err := json.Unmarshal(quotas, &p.Quotas); err != nil {
			return p, fmt.Errorf("plan %s: quotas: %w", p.Code, err)
		}
	}
	return p, nil
}

// ListPlanVersions devuelve todas las versiones de todos los planes, en
// cualquier estado, para la consola de SuperAdmin.
func (q *Queries) ListPlanVersions(ctx context.Context) ([]domain.PlanDefinition, error) {
	if q == nil || q.pool == nil {
		return nil, nil
	}
	rows, err := q.pool.Query(ctx, `SELECT `+planColumns+` FROM ref.plans ORDER BY kind, code, version DESC`)
	if err != nil {
		return nil, fmt.Errorf("list plan versions: %w", err)
	}
	defer rows.Close()
	var out []domain.PlanDefinition
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlanVersion devuelve una versión concreta en cualquier estado (nil si no existe).
func (q *Queries) PlanVersion(ctx context.Context, code string, version int) (*domain.PlanDefinition, error) {
	if q == nil || q.pool == nil { // stub sin base de datos, como GetPlan
		return &domain.PlanDefinition{Code: code, Version: version, Kind: "clinic", Status: "draft", Name: map[string]string{"es": code}}, nil
	}
	p, err := scanPlan(q.pool.QueryRow(ctx, `SELECT `+planColumns+` FROM ref.plans WHERE code = $1 AND version = $2`, code, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plan version: %w", err)
	}
	return &p, nil
}

// NextPlanVersion es la siguiente versión libre del plan (1 si no existe).
func (q *Queries) NextPlanVersion(ctx context.Context, code string) (int, error) {
	if q == nil || q.pool == nil { // stub sin base de datos: GetPlan simula la v1
		return 2, nil
	}
	var n int
	err := q.pool.QueryRow(ctx, `SELECT coalesce(max(version), 0) + 1 FROM ref.plans WHERE code = $1`, code).Scan(&n)
	return n, err
}

// LiveSubscriptionsOnOtherVersions cuenta las suscripciones vigentes del plan
// fijadas en una versión distinta de la indicada (RN-39: preaviso de 30 días).
func (q *Queries) LiveSubscriptionsOnOtherVersions(ctx context.Context, code string, version int) (int, error) {
	if q == nil || q.pool == nil {
		return 0, nil
	}
	var n int
	err := q.pool.QueryRow(ctx,
		`SELECT count(*) FROM billing.subscriptions
		  WHERE plan_code = $1 AND plan_version <> $2 AND status IN ('trialing','active','grace','restricted')`,
		code, version).Scan(&n)
	return n, err
}

// PublishPlanVersion pasa una versión draft a published desde effectiveFrom.
func (q *Queries) PublishPlanVersion(ctx context.Context, code string, version int, effectiveFrom time.Time, by uuid.UUID) error {
	if q == nil || q.pool == nil {
		return nil
	}
	tag, err := q.pool.Exec(ctx,
		`UPDATE ref.plans SET status = 'published', effective_from = $3, published_at = now(), published_by = $4
		  WHERE code = $1 AND version = $2 AND status = 'draft'`,
		code, version, effectiveFrom, by)
	if err != nil {
		return fmt.Errorf("publish plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPlanVersionState
	}
	return nil
}

// RetirePlanVersion saca del catálogo una versión publicada. Las suscripciones
// que la tienen fijada siguen con ella (B10).
func (q *Queries) RetirePlanVersion(ctx context.Context, code string, version int) error {
	if q == nil || q.pool == nil {
		return nil
	}
	tag, err := q.pool.Exec(ctx,
		`UPDATE ref.plans SET status = 'retired' WHERE code = $1 AND version = $2 AND status = 'published'`, code, version)
	if err != nil {
		return fmt.Errorf("retire plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPlanVersionState
	}
	return nil
}
