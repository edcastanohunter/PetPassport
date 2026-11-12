package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
)

// Lecturas de administración y de cuenta (PP-F5-API-005, API-007).

// AccountFilter filtra la lista de cuentas de SuperAdmin. Los campos vacíos no filtran.
type AccountFilter struct {
	Query   string // fragmento de la razón social
	Status  string // estado de alguna suscripción
	Plan    string // plan de alguna suscripción
	Country string
	Limit   int
}

// SubscriptionBrief resume una suscripción en la lista de cuentas.
type SubscriptionBrief struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	PlanCode string    `json:"plan_code"`
	Status   string    `json:"status"`
}

// AccountSummary es una fila de la lista de cuentas de SuperAdmin.
type AccountSummary struct {
	ID            uuid.UUID
	LegalName     string
	Country       string
	Currency      string
	Status        string
	CreatedAt     time.Time
	Subscriptions []SubscriptionBrief
}

// ListAccounts busca cuentas de facturación (API-007), de la más reciente a la más antigua.
func (q *Queries) ListAccounts(ctx context.Context, f AccountFilter) ([]AccountSummary, error) {
	if q == nil || q.pool == nil {
		return []AccountSummary{}, nil
	}
	rows, err := q.pool.Query(ctx, `
SELECT a.id, COALESCE(a.legal_entity->>'legal_name', ''), a.country, a.currency, a.status, a.created_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', s.id, 'kind', s.kind, 'plan_code', s.plan_code, 'status', s.status) ORDER BY s.kind)
                 FROM billing.subscriptions s WHERE s.account_id = a.id), '[]'::jsonb)
FROM billing.accounts a
WHERE ($1 = '' OR position(lower($1) IN lower(COALESCE(a.legal_entity->>'legal_name', ''))) > 0)
  AND ($2 = '' OR a.country = $2)
  AND (($3 = '' AND $4 = '') OR EXISTS (
        SELECT 1 FROM billing.subscriptions s
        WHERE s.account_id = a.id AND ($3 = '' OR s.status = $3) AND ($4 = '' OR s.plan_code = $4)))
ORDER BY a.created_at DESC
LIMIT $5`, f.Query, f.Country, f.Status, f.Plan, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("billing accounts: %w", err)
	}
	defer rows.Close()
	out := []AccountSummary{}
	for rows.Next() {
		var (
			a    AccountSummary
			subs []byte
		)
		if err := rows.Scan(&a.ID, &a.LegalName, &a.Country, &a.Currency, &a.Status, &a.CreatedAt, &subs); err != nil {
			return nil, fmt.Errorf("billing accounts scan: %w", err)
		}
		if err := json.Unmarshal(subs, &a.Subscriptions); err != nil {
			return nil, fmt.Errorf("billing accounts subscriptions: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DiscrepanciesByAccount devuelve las discrepancias de una cuenta, de la más reciente a la más antigua.
func (l *Ledger) DiscrepanciesByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]Discrepancy, error) {
	if l == nil || l.pool == nil {
		return []Discrepancy{}, nil
	}
	rows, err := l.q(ctx).Query(ctx, `
SELECT id, account_id, charge_id, kind, details, status, detected_at, resolved_at, COALESCE(resolution, ''), resolved_by
FROM billing.discrepancies
WHERE account_id = $1
ORDER BY detected_at DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("billing account discrepancies: %w", err)
	}
	defer rows.Close()
	out := []Discrepancy{}
	for rows.Next() {
		var d Discrepancy
		var details []byte
		if err := rows.Scan(&d.ID, &d.AccountID, &d.ChargeID, &d.Kind, &details, &d.Status, &d.DetectedAt,
			&d.ResolvedAt, &d.Resolution, &d.ResolvedBy); err != nil {
			return nil, fmt.Errorf("billing account discrepancies scan: %w", err)
		}
		d.Details = details
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreditNotes devuelve las notas crédito emitidas de un conjunto de facturas,
// agrupadas por factura.
func (s *InvoiceStore) CreditNotes(ctx context.Context, invoiceIDs []uuid.UUID) (map[uuid.UUID][]CreditNoteRow, error) {
	out := map[uuid.UUID][]CreditNoteRow{}
	if s == nil || s.pool == nil || len(invoiceIDs) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).Query(ctx, `
SELECT id, invoice_id, COALESCE(number, ''), status, amount::float8, COALESCE(refund_ref, ''), COALESCE(reason, '')
FROM billing.credit_notes
WHERE invoice_id = ANY($1) AND status <> 'draft'
ORDER BY invoice_id, id`, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("billing credit notes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cn CreditNoteRow
		if err := rows.Scan(&cn.ID, &cn.InvoiceID, &cn.Number, &cn.Status, &cn.Amount, &cn.RefundRef, &cn.Reason); err != nil {
			return nil, fmt.Errorf("billing credit notes scan: %w", err)
		}
		out[cn.InvoiceID] = append(out[cn.InvoiceID], cn)
	}
	return out, rows.Err()
}

// Audit registra una acción administrativa de facturación en el audit log
// (PRD-011). Va en su propia transacción: se escribe antes de ejecutar la
// acción, de modo que un intento fallido también deja rastro.
func (l *Ledger) Audit(ctx context.Context, e audit.Entry) error {
	if l == nil || l.pool == nil {
		return nil
	}
	return pgx.BeginFunc(ctx, l.pool, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, e)
	})
}
