package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// LocalInvoicingProvider es el nombre del adaptador de contingencia (PDF local).
const LocalInvoicingProvider = "pdf_local"

// ContingencyAfter: con el proveedor fiscal caído más de este tiempo se entrega
// una factura de contingencia "pendiente de validación" (ARQ-003).
const ContingencyAfter = 24 * time.Hour

// ErrInvoiceNotRetryable: solo se reintentan facturas rechazadas.
var ErrInvoiceNotRetryable = errors.New("billing: la factura no está rechazada")

var invoicePollDelays = []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour, 24 * time.Hour}

// InvoiceStore persiste el proceso de emisión (infra.InvoiceStore).
type InvoiceStore interface {
	Get(ctx context.Context, id uuid.UUID) (*infra.InvoiceRow, error)
	ByCharge(ctx context.Context, chargeID uuid.UUID) (*infra.InvoiceRow, error)
	CreateDraft(ctx context.Context, inv infra.InvoiceRow) (*infra.InvoiceRow, error)
	Save(ctx context.Context, inv *infra.InvoiceRow, now time.Time) error
	CreateCreditNote(ctx context.Context, cn infra.CreditNoteRow) (*infra.CreditNoteRow, error)
	SaveCreditNote(ctx context.Context, id uuid.UUID, number, status, pdfKey, xmlKey string, now time.Time) error
}

// DocumentArchive guarda los PDF/XML fiscales (object storage con retención).
type DocumentArchive interface {
	Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error
}

// ChargeReader lee el cargo a facturar.
type ChargeReader interface {
	Charge(ctx context.Context, chargeID uuid.UUID) (*infra.ChargeRef, error)
}

// InvoicingService emite la factura electrónica de cada cargo (PP-F5-ARQ-003,
// PRD-005, CMP-009): borrador con la foto de los datos fiscales, emisión
// idempotente en el proveedor, validación, contingencia y notas crédito.
type InvoicingService struct {
	charges ChargeReader
	reader  BillingReader
	store   InvoiceStore
	router  *providers.Router
	archive DocumentArchive
	uow     UnitOfWork
	jobs    CommandScheduler
	metrics *Metrics
	logger  *slog.Logger
	clock   func() time.Time
	newID   func() uuid.UUID
}

// InvoicingOptions son las dependencias de InvoicingService.
type InvoicingOptions struct {
	Charges ChargeReader
	Reader  BillingReader
	Store   InvoiceStore
	Router  *providers.Router
	Archive DocumentArchive
	UoW     UnitOfWork
	Jobs    CommandScheduler
	Metrics *Metrics
	Logger  *slog.Logger
	Clock   func() time.Time
	NewID   func() uuid.UUID
}

// NewInvoicingService construye el servicio de facturación.
func NewInvoicingService(o InvoicingOptions) *InvoicingService {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.NewID == nil {
		o.NewID = uuid.New
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &InvoicingService{
		charges: o.Charges, reader: o.Reader, store: o.Store, router: o.Router, archive: o.Archive,
		uow: o.UoW, jobs: o.Jobs, metrics: o.Metrics, logger: o.Logger, clock: o.Clock, newID: o.NewID,
	}
}

func (s *InvoicingService) now() time.Time { return s.clock().UTC() }

func party(acc *infra.AccountView) invoicing.Party {
	le := acc.LegalEntity
	p := invoicing.Party{
		Kind: le.Kind, LegalName: le.LegalName, TaxIDKind: le.TaxID.Kind, TaxID: le.TaxID.Value,
		Country: acc.Country, Address: le.Address, Email: le.Email,
	}
	if le.Phone != nil {
		p.Phone = *le.Phone
	}
	if le.TaxRegime != nil {
		p.TaxRegime = *le.TaxRegime
	}
	return p
}

func (s *InvoicingService) document(inv *infra.InvoiceRow, ch *infra.ChargeRef) invoicing.Document {
	issued := s.now()
	if inv.IssuedAt != nil {
		issued = *inv.IssuedAt
	}
	return invoicing.Document{
		InvoiceID: inv.ID, ChargeID: inv.ChargeID, Customer: inv.Customer, Lines: inv.Lines, Totals: inv.Totals,
		IssuedAt: issued, PeriodStart: ch.PeriodStart, PeriodEnd: ch.PeriodEnd,
	}
}

// IssueInvoice emite la factura del cargo. Idempotente: una factura ya emitida
// no se repite, y la emisión en el proveedor lleva el id de factura como clave.
func (s *InvoicingService) IssueInvoice(ctx context.Context, chargeID uuid.UUID) error {
	now := s.now()
	ch, err := s.charges.Charge(ctx, chargeID)
	if err != nil {
		return err
	}
	if ch == nil {
		return ErrChargeNotReady
	}
	if ch.Total <= 0 {
		return nil
	}
	acc, err := s.reader.GetAccount(ctx, ch.AccountID)
	if err != nil {
		return err
	}
	if acc == nil {
		return ErrChargeNotReady
	}
	prov, route, err := s.router.Invoicing(ctx, acc.Country)
	if err != nil {
		return err
	}

	inv, err := s.store.ByCharge(ctx, chargeID)
	if err != nil {
		return err
	}
	if inv == nil {
		lines := make([]invoicing.Line, 0, len(ch.Lines))
		for _, l := range ch.Lines {
			lines = append(lines, invoicing.Line{Description: l.Description, Quantity: l.Quantity, Amount: l.Amount})
		}
		if inv, err = s.store.CreateDraft(ctx, infra.InvoiceRow{
			ID: s.newID(), ChargeID: ch.ID, AccountID: ch.AccountID, Provider: prov.Name(), Customer: party(acc), Lines: lines,
			Totals: invoicing.Totals{
				Subtotal: ch.Amount, Tax: ch.Tax, Total: ch.Total, Currency: ch.Currency,
				TaxRule: route.TaxRule, TaxRate: route.TaxRate,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	switch inv.Status {
	case infra.InvoiceDraft, infra.InvoiceContingency:
	default:
		return nil // emitida, aceptada, acreditada o rechazada (esta última: reintento de SuperAdmin)
	}

	inv.Attempts++
	out, err := prov.Issue(ctx, s.document(inv, ch))
	if errors.Is(err, invoicing.ErrUnavailable) {
		s.metrics.invoicingProviderUp(prov.Name(), false)
		return s.providerDown(ctx, inv, ch, now)
	}
	if err != nil {
		s.metrics.invoice("error")
		return fmt.Errorf("billing: emisión de la factura del cargo %s: %w", chargeID, err)
	}
	s.metrics.invoicingProviderUp(prov.Name(), true)
	inv.Provider = prov.Name()
	return s.record(ctx, inv, out, now)
}

// providerDown reprograma la emisión y, pasado el umbral, entrega la factura de
// contingencia. La emisión real se sigue intentando hasta que el proveedor vuelva.
func (s *InvoicingService) providerDown(ctx context.Context, inv *infra.InvoiceRow, ch *infra.ChargeRef, now time.Time) error {
	if inv.Status == infra.InvoiceDraft && now.Sub(inv.CreatedAt) >= ContingencyAfter {
		local, err := s.router.InvoicingByName(LocalInvoicingProvider)
		if err != nil {
			return err
		}
		doc := s.document(inv, ch)
		doc.Contingency = true
		out, err := local.Issue(ctx, doc)
		if err != nil {
			return fmt.Errorf("billing: factura de contingencia del cargo %s: %w", ch.ID, err)
		}
		key, err := s.put(ctx, inv.AccountID, inv.ID, "contingencia.pdf", "application/pdf", out.PDF)
		if err != nil {
			return err
		}
		inv.Status, inv.PDFKey, inv.Number, inv.ContingencyAt = infra.InvoiceContingency, key, out.Number, &now
		s.metrics.invoice(infra.InvoiceContingency)
	}
	return s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.store.Save(ctx, inv, now); err != nil {
			return err
		}
		next := now.Add(time.Hour)
		return s.jobs.ScheduleCommand(ctx, next, IssueInvoice{ChargeID: inv.ChargeID},
			fmt.Sprintf("%s:%d", InvoiceKey(inv.ChargeID), next.Unix()/3600))
	})
}

func (s *InvoicingService) put(ctx context.Context, accountID, docID uuid.UUID, name, contentType string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	key := fmt.Sprintf("billing/invoices/%s/%s/%s", accountID, docID, name)
	if err := s.archive.Put(ctx, key, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
		return "", fmt.Errorf("billing: archivo fiscal %s: %w", key, err)
	}
	return key, nil
}

// record archiva los documentos y guarda el resultado de la emisión.
func (s *InvoicingService) record(ctx context.Context, inv *infra.InvoiceRow, out invoicing.Issued, now time.Time) error {
	if out.Status == invoicing.StatusRejected {
		inv.Status, inv.RejectionReason = infra.InvoiceRejected, out.RejectionReason
		s.metrics.invoice(infra.InvoiceRejected)
		s.logger.WarnContext(ctx, "billing invoice rejected", "invoice_id", inv.ID.String(), "charge_id", inv.ChargeID.String())
		return s.store.Save(ctx, inv, now)
	}
	pdfKey, err := s.put(ctx, inv.AccountID, inv.ID, "factura.pdf", "application/pdf", out.PDF)
	if err != nil {
		return err
	}
	xmlKey, err := s.put(ctx, inv.AccountID, inv.ID, "factura.xml", "application/xml", out.XML)
	if err != nil {
		return err
	}
	inv.Number, inv.CUFE, inv.RejectionReason = out.Number, out.CUFE, ""
	inv.PDFKey, inv.XMLKey = orDefault(pdfKey, inv.PDFKey), xmlKey
	inv.IssuedAt = &now
	inv.Status = infra.InvoiceIssued
	if out.Status == invoicing.StatusAccepted {
		inv.Status, inv.AcceptedAt = infra.InvoiceAccepted, &now
	}
	s.metrics.invoice(inv.Status)
	return s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.store.Save(ctx, inv, now); err != nil {
			return err
		}
		if inv.Status != infra.InvoiceIssued {
			return nil
		}
		return s.jobs.ScheduleCommand(ctx, now.Add(invoicePollDelays[0]), PollInvoice{InvoiceID: inv.ID, Try: 1},
			fmt.Sprintf("billing.invoice-poll:%s:1", inv.ID))
	})
}

// PollInvoice consulta la validación de una factura emitida.
func (s *InvoicingService) PollInvoice(ctx context.Context, invoiceID uuid.UUID, try int) error {
	now := s.now()
	inv, err := s.store.Get(ctx, invoiceID)
	if err != nil || inv == nil || inv.Status != infra.InvoiceIssued {
		return err
	}
	prov, err := s.router.InvoicingByName(inv.Provider)
	if err != nil {
		return err
	}
	st, err := prov.Fetch(ctx, inv.Number)
	if err != nil && !errors.Is(err, invoicing.ErrUnavailable) {
		return fmt.Errorf("billing: consulta de la factura %s: %w", invoiceID, err)
	}
	s.metrics.invoicingProviderUp(prov.Name(), err == nil)
	switch {
	case err == nil && st.Status == invoicing.StatusAccepted:
		inv.Status, inv.CUFE, inv.AcceptedAt = infra.InvoiceAccepted, orDefault(st.CUFE, inv.CUFE), &now
		s.metrics.invoice(infra.InvoiceAccepted)
		return s.store.Save(ctx, inv, now)
	case err == nil && st.Status == invoicing.StatusRejected:
		inv.Status, inv.RejectionReason = infra.InvoiceRejected, st.RejectionReason
		s.metrics.invoice(infra.InvoiceRejected)
		return s.store.Save(ctx, inv, now)
	}
	if try >= len(invoicePollDelays) {
		return nil
	}
	return s.uow.Do(ctx, func(ctx context.Context) error {
		return s.jobs.ScheduleCommand(ctx, now.Add(invoicePollDelays[try]), PollInvoice{InvoiceID: invoiceID, Try: try + 1},
			fmt.Sprintf("billing.invoice-poll:%s:%d", invoiceID, try+1))
	})
}

// RetryInvoice reemite una factura rechazada con los datos fiscales actuales de
// la cuenta (SuperAdmin, tras corregirlos: API-007).
func (s *InvoicingService) RetryInvoice(ctx context.Context, invoiceID uuid.UUID) error {
	now := s.now()
	inv, err := s.store.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv == nil || inv.Status != infra.InvoiceRejected {
		return ErrInvoiceNotRetryable
	}
	acc, err := s.reader.GetAccount(ctx, inv.AccountID)
	if err != nil {
		return err
	}
	if acc == nil {
		return ErrChargeNotReady
	}
	inv.Customer, inv.Status = party(acc), infra.InvoiceDraft
	return s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.store.Save(ctx, inv, now); err != nil {
			return err
		}
		return s.jobs.ScheduleCommand(ctx, now, IssueInvoice{ChargeID: inv.ChargeID},
			fmt.Sprintf("%s:retry:%d", InvoiceKey(inv.ChargeID), inv.Attempts))
	})
}

// IssueCreditNote emite la nota crédito de un reembolso. Idempotente por
// referencia de reembolso. Un reembolso total deja la factura acreditada.
func (s *InvoicingService) IssueCreditNote(ctx context.Context, chargeID uuid.UUID, amount float64, refundRef, reason string) error {
	now := s.now()
	ch, err := s.charges.Charge(ctx, chargeID)
	if err != nil {
		return err
	}
	inv, err := s.store.ByCharge(ctx, chargeID)
	if err != nil {
		return err
	}
	if ch == nil || inv == nil || inv.Number == "" || inv.Status == infra.InvoiceContingency {
		return ErrChargeNotReady // la factura aún no está emitida: el job reintenta
	}
	if inv.Status == infra.InvoiceRejected || inv.Status == infra.InvoiceDraft {
		return ErrChargeNotReady
	}
	cn, err := s.store.CreateCreditNote(ctx, infra.CreditNoteRow{
		ID: s.newID(), InvoiceID: inv.ID, Amount: domain.RoundCents(amount), RefundRef: refundRef, Reason: reason,
	})
	if err != nil {
		return err
	}
	if cn.Status != "draft" {
		return nil
	}
	prov, err := s.router.InvoicingByName(inv.Provider)
	if err != nil {
		return err
	}
	out, err := prov.CreditNote(ctx, invoicing.CreditNoteDocument{
		CreditNoteID: cn.ID, InvoiceNumber: inv.Number, Customer: inv.Customer, Amount: cn.Amount,
		Currency: inv.Totals.Currency, Reason: reason, IssuedAt: now,
	})
	if err != nil {
		s.metrics.invoicingProviderUp(prov.Name(), !errors.Is(err, invoicing.ErrUnavailable))
		return fmt.Errorf("billing: nota crédito del cargo %s: %w", chargeID, err)
	}
	pdfKey, err := s.put(ctx, inv.AccountID, cn.ID, "nota-credito.pdf", "application/pdf", out.PDF)
	if err != nil {
		return err
	}
	xmlKey, err := s.put(ctx, inv.AccountID, cn.ID, "nota-credito.xml", "application/xml", out.XML)
	if err != nil {
		return err
	}
	return s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.store.SaveCreditNote(ctx, cn.ID, out.Number, out.Status, pdfKey, xmlKey, now); err != nil {
			return err
		}
		if domain.RoundCents(ch.Refunded) < domain.RoundCents(ch.Total) {
			return nil
		}
		inv.Status = infra.InvoiceCredited
		s.metrics.invoice(infra.InvoiceCredited)
		return s.store.Save(ctx, inv, now)
	})
}
