package providers

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing/fake"
	fakePay "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
)

// PaymentProvider es el puerto para la pasarela de pagos (PP-F5-DOM-011, ARQ-002).
type PaymentProvider interface {
	CreateCustomer(ctx context.Context, accountID uuid.UUID, email, name string) (customerID string, err error)
	CreateSetupIntent(ctx context.Context, customerID string) (clientSecret string, err error)
	Charge(ctx context.Context, chargeID uuid.UUID, customerID string, amount float64, currency string, token string) (paymentRef string, status string, err error)
	Refund(ctx context.Context, paymentRef string, amount float64, reason string) (refundRef string, err error)
	ParseWebhook(payload []byte, signature string) (eventID string, eventType string, data any, err error)
	GetPayment(ctx context.Context, providerRef string) (status string, amount float64, err error)
}

// InvoicingProvider es el puerto para la facturación fiscal DIAN / PDF (PP-F5-DOM-011, ARQ-003).
type InvoicingProvider interface {
	Issue(ctx context.Context, doc fake.InvoiceDocument) (invoiceNumber, cufe, pdfKey, xmlKey, status string, err error)
	CreditNote(ctx context.Context, doc fake.CreditNoteDocument) (noteNumber, pdfKey, xmlKey string, err error)
	Fetch(ctx context.Context, invoiceNumber string) (status string, err error)
}

// Router resuelve el proveedor de pagos y de facturación según la jurisdicción y configuración (ADR-022, ARQ-002).
type Router struct {
	fakePayment   PaymentProvider
	fakeInvoicing InvoicingProvider
}

// NewRouter crea un Router con adaptadores disponibles.
func NewRouter(fakeP PaymentProvider, fakeI InvoicingProvider) *Router {
	if fakeP == nil {
		fakeP = fakePay.NewFakePaymentProvider()
	}
	if fakeI == nil {
		fakeI = fake.NewFakeInvoicingProvider()
	}
	return &Router{
		fakePayment:   fakeP,
		fakeInvoicing: fakeI,
	}
}

// ForCountry devuelve el proveedor de pago y fiscal adecuado para el país de la cuenta.
func (r *Router) ForCountry(country string) (PaymentProvider, InvoicingProvider) {
	c := strings.ToUpper(strings.TrimSpace(country))
	switch c {
	case "CO":
		return r.fakePayment, r.fakeInvoicing
	default:
		return r.fakePayment, r.fakeInvoicing
	}
}
