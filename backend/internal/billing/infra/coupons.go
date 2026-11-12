package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// Cupones comerciales (PP-F5-PRD-022, API-007).

var (
	// ErrCouponInvalid: el cupón no existe, está inactivo, venció, agotó sus usos
	// o no aplica al plan. No se distingue el motivo para no enumerar códigos.
	ErrCouponInvalid = errors.New("billing: cupón no válido")
	// ErrCouponExists: ya hay un cupón con ese código.
	ErrCouponExists = errors.New("billing: el cupón ya existe")
)

// Coupon es una fila de billing.coupons.
type Coupon struct {
	Code           string     `json:"code"`
	Kind           string     `json:"kind"` // pct | amount
	Value          float64    `json:"value"`
	DurationMonths *int       `json:"duration_months,omitempty"`
	MaxUses        *int       `json:"max_uses,omitempty"`
	Uses           int        `json:"uses"`
	ValidUntil     *time.Time `json:"valid_until,omitempty"`
	Plans          []string   `json:"plans,omitempty"`
	Active         bool       `json:"active"`
}

// Applied convierte el cupón en el descuento de una suscripción contratada en
// now: con duración, se aplica hasta now + duración (también en renovaciones);
// sin duración, solo al primer cargo.
func (c Coupon) Applied(now time.Time) domain.CouponApplied {
	out := domain.CouponApplied{Code: c.Code, Kind: c.Kind, Value: c.Value}
	if c.DurationMonths != nil && *c.DurationMonths > 0 {
		until := now.UTC().AddDate(0, *c.DurationMonths, 0)
		out.Until = &until
	}
	return out
}

// CouponStore persiste los cupones.
type CouponStore struct{ pool *pgxpool.Pool }

// NewCouponStore construye el almacén.
func NewCouponStore(pool *pgxpool.Pool) *CouponStore { return &CouponStore{pool: pool} }

func (s *CouponStore) q(ctx context.Context) querier {
	if tx, err := db.TxFrom(ctx); err == nil {
		return tx
	}
	return s.pool
}

// usable es la condición de un cupón canjeable para el plan $3 en la fecha $2.
const usable = `active
  AND (valid_until IS NULL OR valid_until >= $2::date)
  AND (max_uses IS NULL OR uses < max_uses)
  AND (plans IS NULL OR cardinality(plans) = 0 OR $3 = ANY(plans))`

const couponCols = `code, kind, value::float8, duration_months, max_uses, uses, valid_until, plans, active`

func scanCoupon(row pgx.Row) (*Coupon, error) {
	var c Coupon
	if err := row.Scan(&c.Code, &c.Kind, &c.Value, &c.DurationMonths, &c.MaxUses, &c.Uses, &c.ValidUntil, &c.Plans, &c.Active); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCouponInvalid
		}
		return nil, fmt.Errorf("billing coupon: %w", err)
	}
	return &c, nil
}

// Check devuelve el cupón si es canjeable para el plan, sin consumir un uso
// (previsualización).
func (s *CouponStore) Check(ctx context.Context, code, planCode string, now time.Time) (*Coupon, error) {
	if s == nil || s.pool == nil {
		return nil, ErrCouponInvalid
	}
	return scanCoupon(s.q(ctx).QueryRow(ctx,
		`SELECT `+couponCols+` FROM billing.coupons WHERE code = $1 AND `+usable, code, now.UTC(), planCode))
}

// Redeem consume un uso del cupón dentro de la transacción del contexto. Es
// atómico: dos contrataciones simultáneas no superan max_uses.
func (s *CouponStore) Redeem(ctx context.Context, code, planCode string, now time.Time) (*Coupon, error) {
	if s == nil || s.pool == nil {
		return nil, ErrCouponInvalid
	}
	return scanCoupon(s.q(ctx).QueryRow(ctx,
		`UPDATE billing.coupons SET uses = uses + 1 WHERE code = $1 AND `+usable+` RETURNING `+couponCols,
		code, now.UTC(), planCode))
}

// Create da de alta un cupón (SuperAdmin).
func (s *CouponStore) Create(ctx context.Context, c Coupon) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.q(ctx).Exec(ctx, `
INSERT INTO billing.coupons (code, kind, value, duration_months, max_uses, valid_until, plans, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, true)`,
		c.Code, c.Kind, c.Value, c.DurationMonths, c.MaxUses, c.ValidUntil, c.Plans)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrCouponExists
	}
	if err != nil {
		return fmt.Errorf("billing coupon create: %w", err)
	}
	return nil
}

// List devuelve los cupones, los más recientes en uso primero.
func (s *CouponStore) List(ctx context.Context, limit int) ([]Coupon, error) {
	if s == nil || s.pool == nil {
		return []Coupon{}, nil
	}
	rows, err := s.q(ctx).Query(ctx, `SELECT `+couponCols+` FROM billing.coupons ORDER BY active DESC, code LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("billing coupons: %w", err)
	}
	defer rows.Close()
	out := []Coupon{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
