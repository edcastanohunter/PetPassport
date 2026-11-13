package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	invfake "github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
)

// localStub hace de adaptador pdf_local sin Typst.
type localStub struct{ issued int }

func (*localStub) Name() string { return LocalInvoicingProvider }
func (l *localStub) Issue(_ context.Context, doc invoicing.Document) (invoicing.Issued, error) {
	l.issued++
	if !doc.Contingency {
		return invoicing.Issued{}, errors.New("solo contingencia en esta prueba")
	}
	return invoicing.Issued{Number: "CTG-1", Status: invoicing.StatusAccepted, PDF: []byte("%PDF contingencia")}, nil
}
func (*localStub) CreditNote(context.Context, invoicing.CreditNoteDocument) (invoicing.Issued, error) {
	return invoicing.Issued{}, nil
}
func (*localStub) Fetch(context.Context, string) (invoicing.State, error) {
	return invoicing.State{}, nil
}

// invWorld añade a world el proceso de facturación en memoria.
type invWorld struct {
	*world
	inv      *invfake.FakeInvoicingProvider
	local    *localStub
	invoices map[uuid.UUID]*infra.InvoiceRow // por cargo
	notes    map[string]*infra.CreditNoteRow
	objects  map[string][]byte
	svcInv   *InvoicingService
}

func newInvWorld(t *testing.T) *invWorld {
	w := &invWorld{
		world: newWorld(t), inv: invfake.NewFakeInvoicingProvider(), local: &localStub{},
		invoices: map[uuid.UUID]*infra.InvoiceRow{}, notes: map[string]*infra.CreditNoteRow{}, objects: map[string][]byte{},
	}
	w.account.LegalEntity.TaxID = domain.TaxID{Country: "CO", Kind: "NIT", Value: "900123456"}
	router := providers.NewRouter(staticRouting{}, []providers.PaymentProvider{w.pay}, []invoicing.Provider{w.inv, w.local})
	w.svcInv = NewInvoicingService(InvoicingOptions{
		Charges: w.world, Reader: w.world, Store: w, Router: router, Archive: w, UoW: w.world, Jobs: w.world,
		Clock: func() time.Time { return w.now },
	})
	return w
}

func (w *invWorld) Put(_ context.Context, key, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	w.objects[key] = b
	return err
}

func (w *invWorld) Get(_ context.Context, id uuid.UUID) (*infra.InvoiceRow, error) {
	for _, inv := range w.invoices {
		if inv.ID == id {
			cp := *inv
			return &cp, nil
		}
	}
	return nil, nil
}

func (w *invWorld) ByCharge(_ context.Context, chargeID uuid.UUID) (*infra.InvoiceRow, error) {
	inv, ok := w.invoices[chargeID]
	if !ok {
		return nil, nil
	}
	cp := *inv
	return &cp, nil
}

func (w *invWorld) CreateDraft(ctx context.Context, inv infra.InvoiceRow) (*infra.InvoiceRow, error) {
	if _, ok := w.invoices[inv.ChargeID]; !ok {
		inv.Status = infra.InvoiceDraft
		w.invoices[inv.ChargeID] = &inv
	}
	return w.ByCharge(ctx, inv.ChargeID)
}

func (w *invWorld) Save(_ context.Context, inv *infra.InvoiceRow, _ time.Time) error {
	cp := *inv
	w.invoices[inv.ChargeID] = &cp
	return nil
}

func (w *invWorld) CreateCreditNote(_ context.Context, cn infra.CreditNoteRow) (*infra.CreditNoteRow, error) {
	if _, ok := w.notes[cn.RefundRef]; !ok {
		cn.Status = "draft"
		w.notes[cn.RefundRef] = &cn
	}
	cp := *w.notes[cn.RefundRef]
	return &cp, nil
}

func (w *invWorld) SaveCreditNote(_ context.Context, id uuid.UUID, number, status, _, _ string, _ time.Time) error {
	for _, cn := range w.notes {
		if cn.ID == id {
			cn.Number, cn.Status = number, status
		}
	}
	return nil
}

// paidCharge deja un cargo cobrado con líneas e importes, como lo proyecta Billing.
func (w *invWorld) paidCharge() uuid.UUID {
	w.t.Helper()
	_, chargeID := w.subscribe(false)
	must(w.t, w.svc.CollectCharge(context.Background(), chargeID))
	c := w.charges[chargeID]
	c.Amount, c.Tax = 150000, 28500
	c.Lines = []domain.ChargeLine{{Description: "Plan Clínica Pro (monthly)", Quantity: 2, Amount: 150000}}
	return chargeID
}

func TestIssueInvoice_PP_F5_ARQ_003_AcceptedAndIdempotent(t *testing.T) {
	w := newInvWorld(t)
	chargeID := w.paidCharge()
	ctx := context.Background()

	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	must(t, w.svcInv.IssueInvoice(ctx, chargeID)) // reentrega del job

	inv := w.invoices[chargeID]
	if inv.Status != infra.InvoiceAccepted || inv.Number == "" || inv.CUFE == "" || inv.AcceptedAt == nil {
		t.Fatalf("factura = %+v", inv)
	}
	if w.inv.IssuedCount() != 1 {
		t.Fatalf("emisiones en el proveedor = %d, quiero 1", w.inv.IssuedCount())
	}
	if inv.Totals.Total != w.charges[chargeID].Total || inv.Totals.TaxRule != "CO_IVA_19" || inv.Customer.TaxID != "900123456" {
		t.Fatalf("totales o cliente incorrectos: %+v %+v", inv.Totals, inv.Customer)
	}
	if len(w.objects[inv.PDFKey]) == 0 || len(w.objects[inv.XMLKey]) == 0 {
		t.Fatalf("PDF/XML sin archivar: %v", w.objects)
	}
}

func TestIssueInvoice_PP_F5_ARQ_003_SnapshotSurvivesLaterFiscalChanges(t *testing.T) {
	w := newInvWorld(t)
	chargeID := w.paidCharge()
	ctx := context.Background()
	w.inv.SetMode(invfake.ModeDown)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID)) // borrador con la foto fiscal de hoy

	w.account.LegalEntity.LegalName = "Otra Razón Social"
	w.inv.SetMode(invfake.ModeAccept)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	if got := w.invoices[chargeID].Customer.LegalName; got != "Clínica de Prueba" {
		t.Fatalf("la factura debe llevar los datos del momento del cargo, lleva %q", got)
	}
}

func TestIssueInvoice_PP_F5_ARQ_003_ContingencyAfter24hThenRealInvoice(t *testing.T) {
	w := newInvWorld(t)
	chargeID := w.paidCharge()
	ctx := context.Background()

	w.inv.SetMode(invfake.ModeDown)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	if inv := w.invoices[chargeID]; inv.Status != infra.InvoiceDraft || w.local.issued != 0 {
		t.Fatalf("antes de 24 h no hay contingencia: %s", inv.Status)
	}
	if w.jobKeys(InvoiceKey(chargeID)) != 1 {
		t.Fatalf("la emisión debe reprogramarse: %+v", w.jobs)
	}

	w.now = t0.Add(25 * time.Hour)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	inv := w.invoices[chargeID]
	if inv.Status != infra.InvoiceContingency || inv.ContingencyAt == nil || len(w.objects[inv.PDFKey]) == 0 {
		t.Fatalf("tras 24 h debe haber contingencia: %+v", inv)
	}
	// Sigue caído: no se repite la contingencia.
	w.now = t0.Add(26 * time.Hour)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	if w.local.issued != 1 {
		t.Fatalf("contingencias emitidas = %d, quiero 1", w.local.issued)
	}

	// El proveedor vuelve: la factura electrónica sustituye a la de contingencia.
	w.inv.SetMode(invfake.ModeAccept)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	inv = w.invoices[chargeID]
	if inv.Status != infra.InvoiceAccepted || inv.CUFE == "" || inv.Provider != invfake.Name {
		t.Fatalf("factura definitiva = %+v", inv)
	}
}

func TestIssueInvoice_PP_F5_ARQ_003_PendingValidationIsPolled(t *testing.T) {
	w := newInvWorld(t)
	chargeID := w.paidCharge()
	ctx := context.Background()

	w.inv.SetMode(invfake.ModePending)
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	inv := w.invoices[chargeID]
	if inv.Status != infra.InvoiceIssued || w.jobKeys("billing.invoice-poll:") != 1 {
		t.Fatalf("emitida sin consulta programada: %s %+v", inv.Status, w.jobs)
	}
	must(t, w.svcInv.PollInvoice(ctx, inv.ID, 1)) // aún sin validar
	if w.invoices[chargeID].Status != infra.InvoiceIssued || w.jobKeys("billing.invoice-poll:") != 2 {
		t.Fatal("debe seguir emitida y reprogramar la consulta")
	}
	w.inv.Validate(inv.Number, true, "")
	must(t, w.svcInv.PollInvoice(ctx, inv.ID, 2))
	if got := w.invoices[chargeID]; got.Status != infra.InvoiceAccepted || got.CUFE == "" {
		t.Fatalf("tras validar = %+v", got)
	}
}

func TestRetryInvoice_PP_F5_API_007_RejectedIsReissuedWithFixedData(t *testing.T) {
	w := newInvWorld(t)
	w.account.LegalEntity.TaxID.Value = invfake.RejectTaxID
	chargeID := w.paidCharge()
	ctx := context.Background()

	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	inv := w.invoices[chargeID]
	if inv.Status != infra.InvoiceRejected || inv.RejectionReason == "" {
		t.Fatalf("factura = %+v", inv)
	}
	// Una rechazada no se reemite sola.
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	if w.invoices[chargeID].Status != infra.InvoiceRejected {
		t.Fatal("una factura rechazada solo se reintenta por SuperAdmin")
	}

	w.account.LegalEntity.TaxID.Value = "900123456"
	must(t, w.svcInv.RetryInvoice(ctx, inv.ID))
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	if got := w.invoices[chargeID]; got.Status != infra.InvoiceAccepted || got.Customer.TaxID != "900123456" {
		t.Fatalf("tras el reintento = %+v", got)
	}
	if err := w.svcInv.RetryInvoice(ctx, inv.ID); !errors.Is(err, ErrInvoiceNotRetryable) {
		t.Fatalf("reintento de factura aceptada: %v", err)
	}
}

func TestIssueCreditNote_PP_F5_ARQ_003_PerRefundAndCreditedWhenFull(t *testing.T) {
	w := newInvWorld(t)
	chargeID := w.paidCharge()
	ctx := context.Background()

	// La nota crédito espera a que exista la factura.
	if err := w.svcInv.IssueCreditNote(ctx, chargeID, 1000, "rf-0", "x"); !errors.Is(err, ErrChargeNotReady) {
		t.Fatalf("sin factura: %v", err)
	}
	must(t, w.svcInv.IssueInvoice(ctx, chargeID))
	total := w.charges[chargeID].Total

	ref, err := w.svc.RefundCharge(ctx, chargeID, 50000, "cortesía")
	must(t, err)
	must(t, w.svcInv.IssueCreditNote(ctx, chargeID, 50000, ref, "cortesía"))
	must(t, w.svcInv.IssueCreditNote(ctx, chargeID, 50000, ref, "cortesía")) // reentrega
	if len(w.notes) != 1 || w.notes[ref].Number == "" || w.invoices[chargeID].Status != infra.InvoiceAccepted {
		t.Fatalf("notas = %+v, factura = %s", w.notes, w.invoices[chargeID].Status)
	}

	ref2, err := w.svc.RefundCharge(ctx, chargeID, total-50000, "resto")
	must(t, err)
	must(t, w.svcInv.IssueCreditNote(ctx, chargeID, total-50000, ref2, "resto"))
	if len(w.notes) != 2 || w.invoices[chargeID].Status != infra.InvoiceCredited {
		t.Fatalf("tras el reembolso total: notas=%d factura=%s", len(w.notes), w.invoices[chargeID].Status)
	}
}

func TestIssueInvoice_PP_F5_ARQ_003_ZeroChargeHasNoInvoice(t *testing.T) {
	w := newInvWorld(t)
	_, chargeID := w.subscribe(false)
	w.charges[chargeID].Total = 0
	must(t, w.svcInv.IssueInvoice(context.Background(), chargeID))
	if len(w.invoices) != 0 || w.inv.IssuedCount() != 0 {
		t.Fatal("un cargo a cero no se factura")
	}
	_ = payfake.Name
}
