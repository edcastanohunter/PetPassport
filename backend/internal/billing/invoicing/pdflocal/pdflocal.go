// Package pdflocal es el InvoicingProvider local (PP-F5-ARQ-003): genera el PDF
// de la factura con Typst sin pasar por ningún proveedor fiscal. Se usa como
// contingencia cuando el proveedor de factura electrónica lleva más de 24 h
// caído y para países sin factura electrónica obligatoria. No produce XML ni
// CUFE: un documento de contingencia no es una factura electrónica validada.
package pdflocal

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
)

// Name es el nombre del adaptador en billing.provider_routing.
const Name = "pdf_local"

//go:embed templates/invoice.typ
var invoiceTemplate []byte

// Renderer es el motor de PDF (pdfrender.Typst).
type Renderer interface {
	Render(ctx context.Context, template, data []byte, files ...pdfrender.File) ([]byte, error)
}

// Provider implementa invoicing.Provider.
type Provider struct{ renderer Renderer }

var _ invoicing.Provider = (*Provider)(nil)

// New construye el adaptador.
func New(renderer Renderer) *Provider { return &Provider{renderer: renderer} }

// Name implementa Provider.
func (*Provider) Name() string { return Name }

type line struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	Amount      string `json:"amount"`
}

// data es lo que lee la plantilla. Todo son cadenas: el contenido nunca se
// interpreta como marcado Typst.
type data struct {
	Title           string `json:"title"`
	Number          string `json:"number"`
	IssuedAt        string `json:"issued_at"`
	Notice          string `json:"notice"`
	CustomerName    string `json:"customer_name"`
	CustomerTaxID   string `json:"customer_tax_id"`
	CustomerAddress string `json:"customer_address"`
	CustomerEmail   string `json:"customer_email"`
	Lines           []line `json:"lines"`
	Subtotal        string `json:"subtotal"`
	TaxLabel        string `json:"tax_label"`
	Tax             string `json:"tax"`
	Total           string `json:"total"`
	Currency        string `json:"currency"`
	Period          string `json:"period"`
}

func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// number deriva un número estable del id del documento: el adaptador no lleva
// numeración fiscal (eso es del proveedor de factura electrónica).
func number(prefix string, id fmt.Stringer) string {
	return prefix + "-" + strings.ToUpper(strings.ReplaceAll(id.String(), "-", "")[:12])
}

func (p *Provider) render(ctx context.Context, d data) ([]byte, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	pdf, err := p.renderer.Render(ctx, invoiceTemplate, raw)
	if err != nil {
		return nil, fmt.Errorf("pdflocal: %w", err)
	}
	return pdf, nil
}

func customer(d *data, c invoicing.Party) {
	d.CustomerName = c.LegalName
	d.CustomerTaxID = strings.TrimSpace(c.TaxIDKind + " " + c.TaxID)
	d.CustomerAddress = c.Address
	d.CustomerEmail = c.Email
}

// Issue implementa Provider. Idempotente: el número depende solo del InvoiceID.
func (p *Provider) Issue(ctx context.Context, doc invoicing.Document) (invoicing.Issued, error) {
	d := data{
		Title: "Factura", Number: number("PP", doc.InvoiceID), IssuedAt: doc.IssuedAt.UTC().Format("2006-01-02"),
		Subtotal: money(doc.Totals.Subtotal), Tax: money(doc.Totals.Tax), Total: money(doc.Totals.Total),
		Currency: doc.Totals.Currency,
		TaxLabel: "Impuestos (" + strconv.FormatFloat(doc.Totals.TaxRate*100, 'f', -1, 64) + " %)",
		Lines:    make([]line, 0, len(doc.Lines)),
	}
	if doc.Contingency {
		d.Title = "Factura de contingencia"
		d.Number = number("CTG", doc.InvoiceID)
		d.Notice = "Documento pendiente de validación por la autoridad fiscal. La factura electrónica definitiva lo sustituirá."
	}
	customer(&d, doc.Customer)
	for _, l := range doc.Lines {
		d.Lines = append(d.Lines, line{Description: l.Description, Quantity: strconv.Itoa(l.Quantity), Amount: money(l.Amount)})
	}
	if doc.PeriodStart != nil && doc.PeriodEnd != nil {
		d.Period = doc.PeriodStart.UTC().Format("2006-01-02") + " a " + doc.PeriodEnd.UTC().Format("2006-01-02")
	}
	pdf, err := p.render(ctx, d)
	if err != nil {
		return invoicing.Issued{}, err
	}
	// Sin autoridad que valide, el documento local queda aceptado al emitirse.
	return invoicing.Issued{Number: d.Number, Status: invoicing.StatusAccepted, PDF: pdf}, nil
}

// CreditNote implementa Provider.
func (p *Provider) CreditNote(ctx context.Context, doc invoicing.CreditNoteDocument) (invoicing.Issued, error) {
	d := data{
		Title: "Nota crédito", Number: number("NC", doc.CreditNoteID), IssuedAt: doc.IssuedAt.UTC().Format("2006-01-02"),
		Notice:   "Afecta a la factura " + doc.InvoiceNumber + ".",
		Lines:    []line{{Description: doc.Reason, Quantity: "1", Amount: money(doc.Amount)}},
		Subtotal: money(doc.Amount), TaxLabel: "Impuestos", Tax: money(0), Total: money(doc.Amount), Currency: doc.Currency,
	}
	customer(&d, doc.Customer)
	pdf, err := p.render(ctx, d)
	if err != nil {
		return invoicing.Issued{}, err
	}
	return invoicing.Issued{Number: d.Number, Status: invoicing.StatusAccepted, PDF: pdf}, nil
}

// Fetch implementa Provider: un documento local no tiene validación pendiente.
func (*Provider) Fetch(context.Context, string) (invoicing.State, error) {
	return invoicing.State{Status: invoicing.StatusAccepted}, nil
}
