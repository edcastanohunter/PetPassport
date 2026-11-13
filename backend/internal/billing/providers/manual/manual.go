// Package manual es el adaptador PaymentProvider de transferencia bancaria con
// conciliación manual (ADR-022 §3, PP-F5-PRD-009). No cobra: el cargo queda
// pendiente hasta que SuperAdmin registra el pago recibido; tampoco hay
// webhooks ni reembolsos automáticos (el reembolso se hace por transferencia y
// se registra con su referencia).
package manual

import (
	"context"
	"net/http"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// Provider implementa providers.PaymentProvider.
type Provider struct{}

var _ providers.PaymentProvider = Provider{}

// New construye el adaptador.
func New() Provider { return Provider{} }

// Name implementa PaymentProvider.
func (Provider) Name() string { return providers.ManualProvider }

// CreateCustomer no registra nada fuera: el cliente es la propia cuenta.
func (Provider) CreateCustomer(_ context.Context, c providers.Customer) (string, error) {
	return c.AccountID.String(), nil
}

// SetupPaymentMethod no necesita widget.
func (Provider) SetupPaymentMethod(context.Context, providers.SetupRequest) (providers.SetupConfig, error) {
	return providers.SetupConfig{Provider: providers.ManualProvider, ClientConfig: map[string]string{}}, nil
}

// AttachPaymentMethod devuelve el método "transferencia".
func (Provider) AttachPaymentMethod(context.Context, providers.AttachRequest) (providers.PaymentMethod, error) {
	return providers.PaymentMethod{Kind: "manual"}, nil
}

// Charge deja el cargo pendiente de la transferencia del cliente.
func (Provider) Charge(context.Context, providers.ChargeRequest) (providers.ChargeResult, error) {
	return providers.ChargeResult{Status: providers.ChargePending}, nil
}

// Refund no mueve dinero: la devolución se hace por transferencia y el
// SuperAdmin la documenta en el motivo. Devuelve una referencia interna única.
func (Provider) Refund(_ context.Context, req providers.RefundRequest) (string, error) {
	return "manual-refund:" + req.RefundID.String(), nil
}

// ParseWebhook: el proveedor manual no emite webhooks.
func (Provider) ParseWebhook(http.Header, []byte, time.Time) (providers.WebhookEvent, error) {
	return providers.WebhookEvent{}, providers.ErrNotSupported
}

// GetPayment: no hay nada que consultar; la conciliación manual es la verdad.
func (Provider) GetPayment(context.Context, string) (providers.Payment, error) {
	return providers.Payment{}, providers.ErrNotSupported
}
