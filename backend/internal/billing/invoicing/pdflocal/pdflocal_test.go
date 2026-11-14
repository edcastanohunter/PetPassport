package pdflocal

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
)

// capture guarda lo que la plantilla recibiría, sin compilar.
type capture struct{ data []byte }

func (c *capture) Render(_ context.Context, _, data []byte, _ ...pdfrender.File) ([]byte, error) {
	c.data = data
	return []byte("%PDF-fake"), nil
}

func document(contingency bool) invoicing.Document {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return invoicing.Document{
		InvoiceID: uuid.MustParse("0193aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), ChargeID: uuid.New(),
		Customer: invoicing.Party{
			Kind: "company", LegalName: "Clínica Veterinaria Central SAS", TaxIDKind: "NIT", TaxID: "900123456-7",
			Country: "CO", Address: "Av. Caracas # 45-67, Bogotá", Email: "facturacion@central.co",
		},
		Lines: []invoicing.Line{
			{Description: "Plan Clínica Pro · 3 asientos", Quantity: 3, Amount: 225000},
			// Marcado Typst en datos del cliente: debe salir como texto.
			{Description: `Descuento #set text(fill: red) [*x*] $1+1$ "comillas"`, Quantity: 1, Amount: -25000},
		},
		Totals:   invoicing.Totals{Subtotal: 200000, Tax: 38000, Total: 238000, Currency: "COP", TaxRule: "CO_IVA_19", TaxRate: 0.19},
		IssuedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), PeriodStart: &start, PeriodEnd: &end,
		Contingency: contingency,
	}
}

// PP-F5-ARQ-003: la factura de contingencia se identifica como tal y lleva
// todos los datos del documento fiscal.
func TestIssue_PP_F5_ARQ_003_ContingencyDocumentData(t *testing.T) {
	c := &capture{}
	out, err := New(c).Issue(context.Background(), document(true))
	if err != nil {
		t.Fatal(err)
	}
	if out.Number != "CTG-0193AAAABBBB" || out.Status != invoicing.StatusAccepted || len(out.XML) != 0 {
		t.Fatalf("emitido = %+v", out)
	}
	for _, want := range []string{
		`"title":"Factura de contingencia"`, `"notice":"Documento pendiente de validación`,
		`"customer_tax_id":"NIT 900123456-7"`, `"tax_label":"Impuestos (19 %)"`, `"total":"238000.00"`,
		`"period":"2026-11-01 a 2026-12-01"`, `"amount":"-25000.00"`,
	} {
		if !bytes.Contains(c.data, []byte(want)) {
			t.Errorf("falta %s en %s", want, c.data)
		}
	}
}

// PP-F5-ARQ-003: render real con Typst (la misma versión que la imagen del
// worker, TYPST_VERSION en backend/Dockerfile). CI instala el binario; en local
// se omite si no está. Factura, contingencia y nota crédito compilan, y el
// marcado en los datos no se interpreta.
func TestInvoiceTemplate_PP_F5_ARQ_003_CompilesWithTypst(t *testing.T) {
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst no instalado")
	}
	p := New(pdfrender.NewTypst("", 1, 30*time.Second))
	ctx := context.Background()
	for name, issue := range map[string]func() (invoicing.Issued, error){
		"factura":      func() (invoicing.Issued, error) { return p.Issue(ctx, document(false)) },
		"contingencia": func() (invoicing.Issued, error) { return p.Issue(ctx, document(true)) },
		"nota crédito": func() (invoicing.Issued, error) {
			return p.CreditNote(ctx, invoicing.CreditNoteDocument{
				CreditNoteID: uuid.New(), InvoiceNumber: "PP-0193AAAABBBB", Customer: document(false).Customer,
				Amount: 10000, Currency: "COP", Reason: "Compensación por incidencia", IssuedAt: time.Now(),
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := issue()
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !bytes.HasPrefix(out.PDF, []byte("%PDF-")) || len(out.PDF) < 1000 {
				t.Fatalf("la salida no es un PDF (%d bytes)", len(out.PDF))
			}
		})
	}
}
