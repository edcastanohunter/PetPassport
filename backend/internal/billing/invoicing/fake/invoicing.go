package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// InvoiceDocument contiene los datos necesarios para emitir una factura electrónica.
type InvoiceDocument struct {
	ChargeID    uuid.UUID
	AccountID   uuid.UUID
	Amount      float64
	Tax         float64
	Total       float64
	Currency    string
	LegalName   string
	TaxID       string
	Address     string
	Email       string
	Description string
}

// CreditNoteDocument describe una nota de crédito.
type CreditNoteDocument struct {
	InvoiceNumber string
	Amount        float64
	Reason        string
}

// FakeInvoicingProvider simula la emisión fiscal DIAN / PDF (PP-F5-DOM-011, ARQ-003, QA-002).
type FakeInvoicingProvider struct {
	mu           sync.Mutex
	failEmission bool
	issuedCount  int
	invoices     map[uuid.UUID]string
	creditNotes  map[string]string
}

// NewFakeInvoicingProvider construye un FakeInvoicingProvider.
func NewFakeInvoicingProvider() *FakeInvoicingProvider {
	return &FakeInvoicingProvider{
		invoices:    make(map[uuid.UUID]string),
		creditNotes: make(map[string]string),
	}
}

// SetFailEmission activa o desactiva la simulación de caída del proveedor fiscal.
func (f *FakeInvoicingProvider) SetFailEmission(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failEmission = fail
}

// Issue emite una factura electrónica simulada devolviendo número, CUFE y llaves S3 de PDF/XML.
func (f *FakeInvoicingProvider) Issue(ctx context.Context, doc InvoiceDocument) (invoiceNumber, cufe, pdfKey, xmlKey, status string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failEmission {
		return "", "", "", "", "contingency", fmt.Errorf("DIAN_PROVIDER_DOWN")
	}

	f.issuedCount++
	num := fmt.Sprintf("FE-%06d", f.issuedCount)
	cufeStr := fmt.Sprintf("cufe_dian_%s_%s", num, uuid.New().String()[:8])
	pdfKeyStr := fmt.Sprintf("invoices/%s/%s.pdf", doc.AccountID, num)
	xmlKeyStr := fmt.Sprintf("invoices/%s/%s.xml", doc.AccountID, num)

	f.invoices[doc.ChargeID] = num
	return num, cufeStr, pdfKeyStr, xmlKeyStr, "accepted", nil
}

// CreditNote emite una nota de crédito para un reembolso.
func (f *FakeInvoicingProvider) CreditNote(ctx context.Context, doc CreditNoteDocument) (noteNumber, pdfKey, xmlKey string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	noteNum := fmt.Sprintf("NC-%06d", len(f.creditNotes)+1)
	pdfKeyStr := fmt.Sprintf("credit_notes/%s.pdf", noteNum)
	xmlKeyStr := fmt.Sprintf("credit_notes/%s.xml", noteNum)
	f.creditNotes[doc.InvoiceNumber] = noteNum
	return noteNum, pdfKeyStr, xmlKeyStr, nil
}

// Fetch consulta el estado de validación previa en la DIAN.
func (f *FakeInvoicingProvider) Fetch(ctx context.Context, invoiceNumber string) (string, error) {
	return "accepted", nil
}
