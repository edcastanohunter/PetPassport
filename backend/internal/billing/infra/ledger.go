package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// Ledger es la persistencia operativa del cobro (PP-F5-ARQ-002, SEC-002): intentos
// de pago, eventos de proveedor ya vistos, pagos confirmados y discrepancias de
// conciliación. No es un read model reconstruible desde eventos: es el registro
// de lo hablado con el proveedor.
//
// Los métodos usan la transacción del contexto (UnitOfWork) cuando existe, y el
// pool en caso contrario.
type Ledger struct {
	pool *pgxpool.Pool
}

// NewLedger construye el ledger.
func NewLedger(pool *pgxpool.Pool) *Ledger { return &Ledger{pool: pool} }

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (l *Ledger) q(ctx context.Context) querier {
	if tx, err := db.TxFrom(ctx); err == nil {
		return tx
	}
	return l.pool
}

// Estados de un intento de cobro.
const (
	AttemptCreated = "created" // reservado; aún no hay respuesta del proveedor
	AttemptPending = "pending" // el proveedor lo tiene en curso
	AttemptPaid    = "paid"
	AttemptFailed  = "failed"
	AttemptVoided  = "voided"
)

// Tipos de discrepancia de conciliación.
const (
	DiscrepancyAmountMismatch    = "amount_mismatch"     // el proveedor cobró otro importe
	DiscrepancyPaidAtProvider    = "paid_at_provider"    // pagado allí, no aquí
	DiscrepancyMissingAtProvider = "missing_at_provider" // pagado aquí, no allí
	DiscrepancyVoidedAtProvider  = "voided_at_provider"  // anulado allí tras darlo por pagado
	DiscrepancyStuckAttempt      = "stuck_attempt"       // intento sin desenlace > 24 h
	DiscrepancyRefundUnconfirmed = "refund_unconfirmed"  // reembolso pedido sin confirmar
)

// ChargeRef es lo que el cobro necesita saber de un cargo (billing.charges).
type ChargeRef struct {
	ID             uuid.UUID
	SubscriptionID uuid.UUID
	AccountID      uuid.UUID
	Total          float64
	Refunded       float64
	Currency       string
	Status         string
	Reason         string
	PaymentRef     string
	Provider       string
	FirstFailedAt  *time.Time
	PaidAt         *time.Time
	ScheduledAt    time.Time
	ManualBilling  bool
	Amount         float64
	Tax            float64
	Lines          []domain.ChargeLine
	PeriodStart    *time.Time
	PeriodEnd      *time.Time
}

// Attempt es una fila de billing.payment_attempts.
type Attempt struct {
	ChargeID    uuid.UUID
	Number      int
	Provider    string
	ProviderRef string
	Status      string
	ErrorCode   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Open indica si el intento aún no tiene desenlace.
func (a Attempt) Open() bool { return a.Status == AttemptCreated || a.Status == AttemptPending }

// PaymentRow es una fila de billing.payments (pago o reembolso).
type PaymentRow struct {
	ID          uuid.UUID `json:"id"`
	ChargeID    uuid.UUID `json:"charge_id"`
	AccountID   uuid.UUID `json:"account_id"`
	Provider    string    `json:"provider"`
	ProviderRef string    `json:"provider_ref"`
	Kind        string    `json:"kind"` // payment | refund
	Amount      float64   `json:"amount"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	At          time.Time `json:"at"`
}

// Discrepancy es una fila de billing.discrepancies.
type Discrepancy struct {
	ID         uuid.UUID       `json:"id"`
	AccountID  uuid.UUID       `json:"account_id"`
	ChargeID   *uuid.UUID      `json:"charge_id,omitempty"`
	Kind       string          `json:"kind"`
	Details    json.RawMessage `json:"details"`
	Status     string          `json:"status"`
	DetectedAt time.Time       `json:"detected_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	Resolution string          `json:"resolution,omitempty"`
	ResolvedBy *uuid.UUID      `json:"resolved_by,omitempty"`
}

// Route implementa providers.RoutingReader sobre billing.provider_routing y ref.tax_rules.
func (l *Ledger) Route(ctx context.Context, country string) (providers.Route, error) {
	r := providers.Route{Country: country}
	err := l.q(ctx).QueryRow(ctx, `
SELECT r.payment_provider, r.invoicing_provider, r.tax_rule, t.rate::float8
FROM billing.provider_routing r
JOIN ref.tax_rules t ON t.code = r.tax_rule
WHERE r.country = $1`, country).Scan(&r.PaymentProvider, &r.InvoicingProvider, &r.TaxRule, &r.TaxRate)
	if errors.Is(err, pgx.ErrNoRows) {
		return providers.Route{}, fmt.Errorf("%w: sin routing para el país %q", providers.ErrUnknownProvider, country)
	}
	if err != nil {
		return providers.Route{}, fmt.Errorf("billing routing: %w", err)
	}
	return r, nil
}

const chargeRefSQL = `
SELECT c.id, c.subscription_id, c.account_id, c.total::float8, c.refunded::float8, c.currency,
       c.status, c.reason, COALESCE(c.payment_ref, ''), COALESCE(c.provider, ''),
       c.first_failed_at, c.paid_at, c.scheduled_at, COALESCE(s.manual_billing, false),
       c.amount::float8, c.tax::float8, c.lines, c.period_start, c.period_end
FROM billing.charges c
LEFT JOIN billing.subscriptions s ON s.id = c.subscription_id`

func scanChargeRef(row pgx.Row) (*ChargeRef, error) {
	var (
		c     ChargeRef
		lines []byte
	)
	err := row.Scan(&c.ID, &c.SubscriptionID, &c.AccountID, &c.Total, &c.Refunded, &c.Currency,
		&c.Status, &c.Reason, &c.PaymentRef, &c.Provider, &c.FirstFailedAt, &c.PaidAt, &c.ScheduledAt, &c.ManualBilling,
		&c.Amount, &c.Tax, &lines, &c.PeriodStart, &c.PeriodEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("billing charge: %w", err)
	}
	if err := json.Unmarshal(lines, &c.Lines); err != nil {
		return nil, fmt.Errorf("billing charge lines: %w", err)
	}
	return &c, nil
}

// Charge devuelve el cargo, o nil si no existe (aún no proyectado).
func (l *Ledger) Charge(ctx context.Context, chargeID uuid.UUID) (*ChargeRef, error) {
	return scanChargeRef(l.q(ctx).QueryRow(ctx, chargeRefSQL+` WHERE c.id = $1`, chargeID))
}

// FailedCharges devuelve los cargos fallidos de una cuenta (reintento inmediato
// al cambiar el método de pago, ARQ-006).
func (l *Ledger) FailedCharges(ctx context.Context, accountID uuid.UUID) ([]ChargeRef, error) {
	rows, err := l.q(ctx).Query(ctx, chargeRefSQL+` WHERE c.account_id = $1 AND c.status = 'failed' ORDER BY c.scheduled_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("billing failed charges: %w", err)
	}
	defer rows.Close()
	var out []ChargeRef
	for rows.Next() {
		c, err := scanChargeRef(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

const attemptCols = `charge_id, attempt, provider, COALESCE(provider_ref, ''), status, COALESCE(error_code, ''), created_at, updated_at`

func scanAttempt(row pgx.Row) (*Attempt, error) {
	var a Attempt
	err := row.Scan(&a.ChargeID, &a.Number, &a.Provider, &a.ProviderRef, &a.Status, &a.ErrorCode, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("billing attempt: %w", err)
	}
	return &a, nil
}

// OpenAttempt devuelve el intento sin desenlace del cargo, si lo hay.
func (l *Ledger) OpenAttempt(ctx context.Context, chargeID uuid.UUID) (*Attempt, error) {
	return scanAttempt(l.q(ctx).QueryRow(ctx,
		`SELECT `+attemptCols+` FROM billing.payment_attempts WHERE charge_id = $1 AND status IN ('created','pending')`, chargeID))
}

// Attempt devuelve un intento concreto.
func (l *Ledger) Attempt(ctx context.Context, chargeID uuid.UUID, number int) (*Attempt, error) {
	return scanAttempt(l.q(ctx).QueryRow(ctx,
		`SELECT `+attemptCols+` FROM billing.payment_attempts WHERE charge_id = $1 AND attempt = $2`, chargeID, number))
}

// BeginAttempt reserva el siguiente intento del cargo. El índice único parcial
// garantiza que solo hay uno abierto: si otro proceso se adelantó, devuelve el suyo.
func (l *Ledger) BeginAttempt(ctx context.Context, chargeID uuid.UUID, provider string, now time.Time) (Attempt, error) {
	a, err := scanAttempt(l.q(ctx).QueryRow(ctx, `
INSERT INTO billing.payment_attempts (charge_id, attempt, provider, status, created_at, updated_at)
SELECT $1, COALESCE(MAX(attempt), 0) + 1, $2, 'created', $3, $3
FROM billing.payment_attempts WHERE charge_id = $1
ON CONFLICT DO NOTHING
RETURNING `+attemptCols, chargeID, provider, now.UTC()))
	if err != nil {
		return Attempt{}, err
	}
	if a != nil {
		return *a, nil
	}
	open, err := l.OpenAttempt(ctx, chargeID)
	if err != nil {
		return Attempt{}, err
	}
	if open == nil {
		return Attempt{}, fmt.Errorf("billing attempt: no se pudo reservar el intento del cargo %s", chargeID)
	}
	return *open, nil
}

// UpdateAttempt fija el estado de un intento. Un intento cerrado no se reabre:
// devuelve false si no cambió nada (resultado duplicado o tardío).
func (l *Ledger) UpdateAttempt(ctx context.Context, chargeID uuid.UUID, number int, status, providerRef, errorCode string, now time.Time) (bool, error) {
	tag, err := l.q(ctx).Exec(ctx, `
UPDATE billing.payment_attempts SET
  status = $3,
  provider_ref = COALESCE(NULLIF($4, ''), provider_ref),
  error_code = NULLIF($5, ''),
  updated_at = $6
WHERE charge_id = $1 AND attempt = $2 AND status IN ('created','pending')`,
		chargeID, number, status, providerRef, errorCode, now.UTC())
	if err != nil {
		return false, fmt.Errorf("billing attempt update: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// LatestAttemptNumber devuelve el número del último intento del cargo (0 si no hay).
func (l *Ledger) LatestAttemptNumber(ctx context.Context, chargeID uuid.UUID) (int, error) {
	var n int
	if err := l.q(ctx).QueryRow(ctx,
		`SELECT COALESCE(MAX(attempt), 0) FROM billing.payment_attempts WHERE charge_id = $1`, chargeID).Scan(&n); err != nil {
		return 0, fmt.Errorf("billing attempt latest: %w", err)
	}
	return n, nil
}

// AttemptsSince devuelve los intentos creados desde since (conciliación, ARQ-002).
func (l *Ledger) AttemptsSince(ctx context.Context, since time.Time) ([]Attempt, error) {
	rows, err := l.q(ctx).Query(ctx,
		`SELECT `+attemptCols+` FROM billing.payment_attempts WHERE created_at >= $1 ORDER BY created_at`, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("billing attempts: %w", err)
	}
	defer rows.Close()
	var out []Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ClaimProviderEvent registra el evento del proveedor. Devuelve false si ya se
// había procesado (idempotencia por provider_event_id, SEC-002). Debe llamarse
// dentro de la misma transacción que aplica sus efectos.
func (l *Ledger) ClaimProviderEvent(ctx context.Context, provider, eventID string, now time.Time) (bool, error) {
	tag, err := l.q(ctx).Exec(ctx, `
INSERT INTO billing.provider_events (provider, event_id, received_at)
VALUES ($1, $2, $3) ON CONFLICT (provider, event_id) DO NOTHING`, provider, eventID, now.UTC())
	if err != nil {
		return false, fmt.Errorf("billing provider event: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// MarkProviderEvent anota el resultado del procesamiento del evento.
func (l *Ledger) MarkProviderEvent(ctx context.Context, provider, eventID, result string, now time.Time) error {
	if _, err := l.q(ctx).Exec(ctx, `
UPDATE billing.provider_events SET processed_at = $3, result = $4 WHERE provider = $1 AND event_id = $2`,
		provider, eventID, now.UTC(), result); err != nil {
		return fmt.Errorf("billing provider event mark: %w", err)
	}
	return nil
}

// InsertPayment asienta un pago o reembolso. Idempotente por (provider, provider_ref):
// devuelve false si ya existía.
func (l *Ledger) InsertPayment(ctx context.Context, p PaymentRow) (bool, error) {
	tag, err := l.q(ctx).Exec(ctx, `
INSERT INTO billing.payments (id, charge_id, account_id, provider, provider_ref, kind, amount, currency, status, reason, at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11)
ON CONFLICT (provider, provider_ref) DO NOTHING`,
		p.ID, p.ChargeID, p.AccountID, p.Provider, p.ProviderRef, p.Kind, p.Amount, p.Currency, p.Status, p.Reason, p.At.UTC())
	if err != nil {
		return false, fmt.Errorf("billing payment insert: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ConfirmRefund sustituye la referencia provisional de un reembolso solicitado
// por la definitiva del proveedor.
func (l *Ledger) ConfirmRefund(ctx context.Context, id uuid.UUID, providerRef string, now time.Time) error {
	if _, err := l.q(ctx).Exec(ctx, `
UPDATE billing.payments SET provider_ref = $2, status = 'confirmed', at = $3 WHERE id = $1 AND kind = 'refund'`,
		id, providerRef, now.UTC()); err != nil {
		return fmt.Errorf("billing refund confirm: %w", err)
	}
	return nil
}

// DeletePendingRefund retira un reembolso solicitado que el proveedor rechazó.
func (l *Ledger) DeletePendingRefund(ctx context.Context, id uuid.UUID) error {
	if _, err := l.q(ctx).Exec(ctx,
		`DELETE FROM billing.payments WHERE id = $1 AND kind = 'refund' AND status = 'requested'`, id); err != nil {
		return fmt.Errorf("billing refund delete: %w", err)
	}
	return nil
}

const paymentCols = `id, charge_id, COALESCE(account_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, provider_ref,
       kind, amount::float8, COALESCE(currency, ''), status, COALESCE(reason, ''), at`

func scanPayments(rows pgx.Rows) ([]PaymentRow, error) {
	defer rows.Close()
	out := []PaymentRow{}
	for rows.Next() {
		var p PaymentRow
		if err := rows.Scan(&p.ID, &p.ChargeID, &p.AccountID, &p.Provider, &p.ProviderRef, &p.Kind,
			&p.Amount, &p.Currency, &p.Status, &p.Reason, &p.At); err != nil {
			return nil, fmt.Errorf("billing payments scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PaymentOfCharge devuelve el pago confirmado de un cargo, si existe.
func (l *Ledger) PaymentOfCharge(ctx context.Context, chargeID uuid.UUID) (*PaymentRow, error) {
	rows, err := l.q(ctx).Query(ctx,
		`SELECT `+paymentCols+` FROM billing.payments WHERE charge_id = $1 AND kind = 'payment' ORDER BY at DESC LIMIT 1`, chargeID)
	if err != nil {
		return nil, fmt.Errorf("billing payment of charge: %w", err)
	}
	ps, err := scanPayments(rows)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

// ListPayments devuelve pagos y reembolsos de una cuenta, del más reciente al más antiguo.
func (l *Ledger) ListPayments(ctx context.Context, accountID uuid.UUID, limit int) ([]PaymentRow, error) {
	if l == nil || l.pool == nil {
		return []PaymentRow{}, nil
	}
	rows, err := l.q(ctx).Query(ctx,
		`SELECT `+paymentCols+` FROM billing.payments WHERE account_id = $1 ORDER BY at DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("billing payments: %w", err)
	}
	return scanPayments(rows)
}

// PendingRefunds devuelve los reembolsos solicitados antes de before que siguen sin confirmar.
func (l *Ledger) PendingRefunds(ctx context.Context, before time.Time) ([]PaymentRow, error) {
	rows, err := l.q(ctx).Query(ctx,
		`SELECT `+paymentCols+` FROM billing.payments WHERE kind = 'refund' AND status = 'requested' AND at < $1`, before.UTC())
	if err != nil {
		return nil, fmt.Errorf("billing pending refunds: %w", err)
	}
	return scanPayments(rows)
}

// OpenDiscrepancy abre una discrepancia. Idempotente por (cargo, tipo) mientras
// siga abierta: devuelve false si ya lo estaba.
func (l *Ledger) OpenDiscrepancy(ctx context.Context, id, accountID, chargeID uuid.UUID, kind string, details map[string]any, now time.Time) (bool, error) {
	raw, err := json.Marshal(details)
	if err != nil {
		return false, err
	}
	tag, err := l.q(ctx).Exec(ctx, `
INSERT INTO billing.discrepancies (id, account_id, charge_id, kind, details, status, detected_at)
VALUES ($1, $2, $3, $4, $5, 'open', $6)
ON CONFLICT (charge_id, kind) WHERE status = 'open' AND charge_id IS NOT NULL DO NOTHING`,
		id, accountID, chargeID, kind, raw, now.UTC())
	if err != nil {
		return false, fmt.Errorf("billing discrepancy open: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// HasOpenDiscrepancy indica si la cuenta tiene discrepancias sin resolver: bloquea
// la restricción automática (ARQ-002, ADR-022 §5).
func (l *Ledger) HasOpenDiscrepancy(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var open bool
	if err := l.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM billing.discrepancies WHERE account_id = $1 AND status = 'open')`, accountID).Scan(&open); err != nil {
		return false, fmt.Errorf("billing discrepancy check: %w", err)
	}
	return open, nil
}

// CountOpenDiscrepancies alimenta billing_discrepancies_open (INF-003).
func (l *Ledger) CountOpenDiscrepancies(ctx context.Context) (int, error) {
	var n int
	if err := l.q(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM billing.discrepancies WHERE status = 'open'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("billing discrepancy count: %w", err)
	}
	return n, nil
}

// ListDiscrepancies devuelve discrepancias por estado ("" = todas).
func (l *Ledger) ListDiscrepancies(ctx context.Context, status string, limit int) ([]Discrepancy, error) {
	if l == nil || l.pool == nil {
		return []Discrepancy{}, nil
	}
	rows, err := l.q(ctx).Query(ctx, `
SELECT id, account_id, charge_id, kind, details, status, detected_at, resolved_at, COALESCE(resolution, ''), resolved_by
FROM billing.discrepancies
WHERE ($1 = '' OR status = $1)
ORDER BY detected_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("billing discrepancies: %w", err)
	}
	defer rows.Close()
	out := []Discrepancy{}
	for rows.Next() {
		var d Discrepancy
		var details []byte
		if err := rows.Scan(&d.ID, &d.AccountID, &d.ChargeID, &d.Kind, &details, &d.Status, &d.DetectedAt,
			&d.ResolvedAt, &d.Resolution, &d.ResolvedBy); err != nil {
			return nil, fmt.Errorf("billing discrepancies scan: %w", err)
		}
		d.Details = details
		out = append(out, d)
	}
	return out, rows.Err()
}

// ResolveDiscrepancy cierra una discrepancia con la resolución del SuperAdmin.
// Devuelve false si no existe o ya estaba resuelta.
func (l *Ledger) ResolveDiscrepancy(ctx context.Context, id uuid.UUID, resolution string, by uuid.UUID, now time.Time) (bool, error) {
	tag, err := l.q(ctx).Exec(ctx, `
UPDATE billing.discrepancies SET status = 'resolved', resolved_at = $2, resolution = $3, resolved_by = $4
WHERE id = $1 AND status = 'open'`, id, now.UTC(), resolution, by)
	if err != nil {
		return false, fmt.Errorf("billing discrepancy resolve: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// LockCharge bloquea la fila del cargo hasta el fin de la transacción: serializa
// operaciones que no deben solaparse (dos reembolsos simultáneos).
func (l *Ledger) LockCharge(ctx context.Context, chargeID uuid.UUID) error {
	var one int
	err := l.q(ctx).QueryRow(ctx, `SELECT 1 FROM billing.charges WHERE id = $1 FOR UPDATE`, chargeID).Scan(&one)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("billing charge lock: %w", err)
	}
	return nil
}

// HasPendingRefund indica si el cargo tiene un reembolso solicitado sin confirmar.
func (l *Ledger) HasPendingRefund(ctx context.Context, chargeID uuid.UUID) (bool, error) {
	var pending bool
	if err := l.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM billing.payments WHERE charge_id = $1 AND kind = 'refund' AND status = 'requested')`,
		chargeID).Scan(&pending); err != nil {
		return false, fmt.Errorf("billing pending refund: %w", err)
	}
	return pending, nil
}
