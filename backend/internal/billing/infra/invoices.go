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

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// Estados de billing.invoices.
const (
	InvoiceDraft       = "draft"
	InvoiceIssued      = "issued"
	InvoiceAccepted    = "accepted"
	InvoiceRejected    = "rejected"
	InvoiceCredited    = "credited"
	InvoiceContingency = "contingency"
)

// InvoiceRow es una factura (billing.invoices). No es un agregado: es el estado
// del proceso de emisión de la factura de un cargo (ARQ-003).
type InvoiceRow struct {
	ID              uuid.UUID        `json:"id"`
	ChargeID        uuid.UUID        `json:"charge_id"`
	AccountID       uuid.UUID        `json:"account_id"`
	Provider        string           `json:"provider"`
	Number          string           `json:"number,omitempty"`
	Status          string           `json:"status"`
	CUFE            string           `json:"cufe,omitempty"`
	PDFKey          string           `json:"-"`
	XMLKey          string           `json:"-"`
	Totals          invoicing.Totals `json:"totals"`
	Lines           []invoicing.Line `json:"lines"`
	Customer        invoicing.Party  `json:"-"`
	IssuedAt        *time.Time       `json:"issued_at,omitempty"`
	AcceptedAt      *time.Time       `json:"accepted_at,omitempty"`
	RejectionReason string           `json:"rejection_reason,omitempty"`
	Attempts        int              `json:"attempts"`
	ContingencyAt   *time.Time       `json:"contingency_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	HasPDF          bool             `json:"has_pdf"`
	HasXML          bool             `json:"has_xml"`
}

// CreditNoteRow es una nota crédito (billing.credit_notes).
type CreditNoteRow struct {
	ID        uuid.UUID
	InvoiceID uuid.UUID
	Number    string
	Status    string
	Amount    float64
	RefundRef string
	Reason    string
}

// InvoiceStore persiste facturas y notas crédito.
type InvoiceStore struct {
	pool *pgxpool.Pool
}

// NewInvoiceStore construye el store.
func NewInvoiceStore(pool *pgxpool.Pool) *InvoiceStore { return &InvoiceStore{pool: pool} }

func (s *InvoiceStore) q(ctx context.Context) querier {
	if tx, err := db.TxFrom(ctx); err == nil {
		return tx
	}
	return s.pool
}

const invoiceCols = `id, charge_id, account_id, provider, COALESCE(number, ''), status, COALESCE(cufe, ''),
       COALESCE(pdf_object_key, ''), COALESCE(xml_object_key, ''), totals, lines, customer_snapshot,
       issued_at, accepted_at, COALESCE(rejection_reason, ''), attempts, contingency_at, created_at`

func scanInvoice(row pgx.Row) (*InvoiceRow, error) {
	var (
		inv                     InvoiceRow
		totals, lines, customer []byte
	)
	err := row.Scan(&inv.ID, &inv.ChargeID, &inv.AccountID, &inv.Provider, &inv.Number, &inv.Status, &inv.CUFE,
		&inv.PDFKey, &inv.XMLKey, &totals, &lines, &customer, &inv.IssuedAt, &inv.AcceptedAt,
		&inv.RejectionReason, &inv.Attempts, &inv.ContingencyAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("billing invoice: %w", err)
	}
	if err := errors.Join(json.Unmarshal(totals, &inv.Totals), json.Unmarshal(lines, &inv.Lines), json.Unmarshal(customer, &inv.Customer)); err != nil {
		return nil, fmt.Errorf("billing invoice json: %w", err)
	}
	inv.HasPDF, inv.HasXML = inv.PDFKey != "", inv.XMLKey != ""
	return &inv, nil
}

// Get devuelve la factura, o nil si no existe.
func (s *InvoiceStore) Get(ctx context.Context, id uuid.UUID) (*InvoiceRow, error) {
	if s == nil || s.pool == nil {
		return &InvoiceRow{ID: id, Status: InvoiceAccepted, PDFKey: "stub.pdf", XMLKey: "stub.xml"}, nil
	}
	return scanInvoice(s.q(ctx).QueryRow(ctx, `SELECT `+invoiceCols+` FROM billing.invoices WHERE id = $1`, id))
}

// ByCharge devuelve la factura de un cargo, o nil si aún no existe.
func (s *InvoiceStore) ByCharge(ctx context.Context, chargeID uuid.UUID) (*InvoiceRow, error) {
	return scanInvoice(s.q(ctx).QueryRow(ctx, `SELECT `+invoiceCols+` FROM billing.invoices WHERE charge_id = $1`, chargeID))
}

// CreateDraft crea el borrador de la factura del cargo. Idempotente por cargo:
// si ya existe devuelve la existente.
func (s *InvoiceStore) CreateDraft(ctx context.Context, inv InvoiceRow) (*InvoiceRow, error) {
	totals, err := json.Marshal(inv.Totals)
	if err != nil {
		return nil, err
	}
	lines, err := json.Marshal(inv.Lines)
	if err != nil {
		return nil, err
	}
	customer, err := json.Marshal(inv.Customer)
	if err != nil {
		return nil, err
	}
	if _, err := s.q(ctx).Exec(ctx, `
INSERT INTO billing.invoices (id, charge_id, account_id, provider, status, totals, lines, customer_snapshot, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8, $8)
ON CONFLICT (charge_id) DO NOTHING`,
		inv.ID, inv.ChargeID, inv.AccountID, inv.Provider, totals, lines, customer, inv.CreatedAt.UTC()); err != nil {
		return nil, fmt.Errorf("billing invoice draft: %w", err)
	}
	return s.ByCharge(ctx, inv.ChargeID)
}

// Save guarda el estado de emisión de la factura.
func (s *InvoiceStore) Save(ctx context.Context, inv *InvoiceRow, now time.Time) error {
	customer, err := json.Marshal(inv.Customer)
	if err != nil {
		return err
	}
	if _, err := s.q(ctx).Exec(ctx, `
UPDATE billing.invoices SET
  provider = $2, number = NULLIF($3, ''), status = $4, cufe = NULLIF($5, ''),
  pdf_object_key = NULLIF($6, ''), xml_object_key = NULLIF($7, ''), customer_snapshot = $8,
  issued_at = $9, accepted_at = $10, rejection_reason = NULLIF($11, ''), attempts = $12,
  contingency_at = $13, updated_at = $14
WHERE id = $1`,
		inv.ID, inv.Provider, inv.Number, inv.Status, inv.CUFE, inv.PDFKey, inv.XMLKey, customer,
		inv.IssuedAt, inv.AcceptedAt, inv.RejectionReason, inv.Attempts, inv.ContingencyAt, now.UTC()); err != nil {
		return fmt.Errorf("billing invoice save: %w", err)
	}
	return nil
}

func (s *InvoiceStore) list(ctx context.Context, where string, args ...any) ([]InvoiceRow, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT `+invoiceCols+` FROM billing.invoices WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("billing invoices: %w", err)
	}
	defer rows.Close()
	out := []InvoiceRow{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// ListByAccount devuelve las facturas de una cuenta, de la más reciente a la más antigua.
func (s *InvoiceStore) ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]InvoiceRow, error) {
	if s == nil || s.pool == nil {
		return []InvoiceRow{}, nil
	}
	return s.list(ctx, `account_id = $1 AND status <> 'draft' ORDER BY created_at DESC LIMIT $2`, accountID, limit)
}

// ListByStatus devuelve facturas en un estado (cola de rechazadas de SuperAdmin).
func (s *InvoiceStore) ListByStatus(ctx context.Context, status string, limit int) ([]InvoiceRow, error) {
	if s == nil || s.pool == nil {
		return []InvoiceRow{}, nil
	}
	return s.list(ctx, `status = $1 ORDER BY created_at DESC LIMIT $2`, status, limit)
}

// CreateCreditNote reserva la nota crédito de un reembolso. Idempotente por
// (factura, referencia de reembolso): devuelve la existente si ya se reservó.
func (s *InvoiceStore) CreateCreditNote(ctx context.Context, cn CreditNoteRow) (*CreditNoteRow, error) {
	if _, err := s.q(ctx).Exec(ctx, `
INSERT INTO billing.credit_notes (id, invoice_id, status, amount, refund_ref, reason)
VALUES ($1, $2, 'draft', $3, $4, $5)
ON CONFLICT (invoice_id, refund_ref) WHERE refund_ref IS NOT NULL DO NOTHING`,
		cn.ID, cn.InvoiceID, cn.Amount, cn.RefundRef, cn.Reason); err != nil {
		return nil, fmt.Errorf("billing credit note: %w", err)
	}
	var out CreditNoteRow
	err := s.q(ctx).QueryRow(ctx, `
SELECT id, invoice_id, COALESCE(number, ''), status, amount::float8, COALESCE(refund_ref, ''), COALESCE(reason, '')
FROM billing.credit_notes WHERE invoice_id = $1 AND refund_ref = $2`, cn.InvoiceID, cn.RefundRef).
		Scan(&out.ID, &out.InvoiceID, &out.Number, &out.Status, &out.Amount, &out.RefundRef, &out.Reason)
	if err != nil {
		return nil, fmt.Errorf("billing credit note read: %w", err)
	}
	return &out, nil
}

// SaveCreditNote guarda la nota crédito ya emitida.
func (s *InvoiceStore) SaveCreditNote(ctx context.Context, id uuid.UUID, number, status, pdfKey, xmlKey string, now time.Time) error {
	if _, err := s.q(ctx).Exec(ctx, `
UPDATE billing.credit_notes SET number = NULLIF($2, ''), status = $3,
  pdf_object_key = NULLIF($4, ''), xml_object_key = NULLIF($5, ''), issued_at = $6
WHERE id = $1`, id, number, status, pdfKey, xmlKey, now.UTC()); err != nil {
		return fmt.Errorf("billing credit note save: %w", err)
	}
	return nil
}
