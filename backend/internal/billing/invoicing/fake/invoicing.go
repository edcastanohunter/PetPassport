// Package fake es el InvoicingProvider de dev y CI (PP-F5-ARQ-003, QA-002): emite
// facturas "DIAN" simuladas con los mismos estados que un proveedor real
// (aceptada, emitida pendiente de validación, rechazada por datos, caído).
package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
)

// Name es el nombre del adaptador en billing.provider_routing.
const Name = "fake"

// RejectTaxID es un NIT que el proveedor simulado rechaza por datos fiscales.
const RejectTaxID = "000000000"

// Mode es el comportamiento de la emisión.
type Mode string

// Modos de emisión.
const (
	ModeAccept  Mode = "accept"  // aceptada al emitir
	ModePending Mode = "pending" // emitida; Fetch la acepta después
	ModeDown    Mode = "down"    // proveedor caído
)

// FakeInvoicingProvider simula la emisión fiscal.
type FakeInvoicingProvider struct { //nolint:revive // nombre heredado de M5.1
	mu       sync.Mutex
	mode     Mode
	seq      int
	notes    int
	byID     map[uuid.UUID]invoicing.Issued
	byNumber map[string]invoicing.State
}

var _ invoicing.Provider = (*FakeInvoicingProvider)(nil)

// NewFakeInvoicingProvider construye el proveedor en modo aceptación.
func NewFakeInvoicingProvider() *FakeInvoicingProvider {
	return &FakeInvoicingProvider{mode: ModeAccept, byID: map[uuid.UUID]invoicing.Issued{}, byNumber: map[string]invoicing.State{}}
}

// SetMode cambia el comportamiento de las siguientes emisiones.
func (f *FakeInvoicingProvider) SetMode(m Mode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = m
}

// SetFailEmission conserva la API de M5.1: true = proveedor caído.
func (f *FakeInvoicingProvider) SetFailEmission(fail bool) {
	if fail {
		f.SetMode(ModeDown)
		return
	}
	f.SetMode(ModeAccept)
}

// IssuedCount devuelve cuántas facturas distintas se han emitido.
func (f *FakeInvoicingProvider) IssuedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seq
}

// Name implementa Provider.
func (*FakeInvoicingProvider) Name() string { return Name }

// Issue implementa Provider; idempotente por InvoiceID.
func (f *FakeInvoicingProvider) Issue(_ context.Context, doc invoicing.Document) (invoicing.Issued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode == ModeDown {
		return invoicing.Issued{}, invoicing.ErrUnavailable
	}
	if prev, ok := f.byID[doc.InvoiceID]; ok {
		return prev, nil
	}
	if doc.Customer.TaxID == RejectTaxID {
		return invoicing.Issued{Status: invoicing.StatusRejected, RejectionReason: "NIT del adquiriente no válido"}, nil
	}
	f.seq++
	num := fmt.Sprintf("FE-%06d", f.seq)
	out := invoicing.Issued{
		Number: num,
		PDF:    []byte("%PDF-1.4 fake " + num),
		XML:    []byte(fmt.Sprintf(`<Invoice><ID>%s</ID><Total currency=%q>%.2f</Total></Invoice>`, num, doc.Totals.Currency, doc.Totals.Total)),
	}
	if f.mode == ModePending {
		out.Status = invoicing.StatusIssued
	} else {
		out.Status = invoicing.StatusAccepted
		out.CUFE = "cufe-" + num
	}
	f.byID[doc.InvoiceID] = out
	f.byNumber[num] = invoicing.State{Status: out.Status, CUFE: out.CUFE}
	return out, nil
}

// Validate hace que la autoridad acepte (o rechace) una factura emitida en ModePending.
func (f *FakeInvoicingProvider) Validate(number string, accept bool, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if accept {
		f.byNumber[number] = invoicing.State{Status: invoicing.StatusAccepted, CUFE: "cufe-" + number}
		return
	}
	f.byNumber[number] = invoicing.State{Status: invoicing.StatusRejected, RejectionReason: reason}
}

// CreditNote implementa Provider; idempotente por CreditNoteID.
func (f *FakeInvoicingProvider) CreditNote(_ context.Context, doc invoicing.CreditNoteDocument) (invoicing.Issued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode == ModeDown {
		return invoicing.Issued{}, invoicing.ErrUnavailable
	}
	if prev, ok := f.byID[doc.CreditNoteID]; ok {
		return prev, nil
	}
	f.notes++
	num := fmt.Sprintf("NC-%06d", f.notes)
	out := invoicing.Issued{
		Number: num, Status: invoicing.StatusAccepted, CUFE: "cude-" + num,
		PDF: []byte("%PDF-1.4 fake " + num),
		XML: []byte(fmt.Sprintf(`<CreditNote><ID>%s</ID><Ref>%s</Ref></CreditNote>`, num, doc.InvoiceNumber)),
	}
	f.byID[doc.CreditNoteID] = out
	return out, nil
}

// Fetch implementa Provider.
func (f *FakeInvoicingProvider) Fetch(_ context.Context, number string) (invoicing.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode == ModeDown {
		return invoicing.State{}, invoicing.ErrUnavailable
	}
	st, ok := f.byNumber[number]
	if !ok {
		return invoicing.State{}, fmt.Errorf("fake: factura %s desconocida", number)
	}
	return st, nil
}
