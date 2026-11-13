package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
)

// ManualProvider es el nombre del adaptador de transferencia con conciliación
// manual, siempre disponible para planes Enterprise (ADR-022 §3).
const ManualProvider = "manual"

// Route es la fila de billing.provider_routing de un país.
type Route struct {
	Country           string
	PaymentProvider   string
	InvoicingProvider string
	TaxRule           string
	TaxRate           float64
}

// RoutingReader lee el routing por país (billing.provider_routing + ref.tax_rules).
type RoutingReader interface {
	Route(ctx context.Context, country string) (Route, error)
}

// Router resuelve los adaptadores de pago y de facturación fiscal según el país
// de la cuenta (ADR-022 §3–4, ARQ-002). Un routing que apunta a un adaptador no
// registrado es un error: nunca se sustituye en silencio por otro.
type Router struct {
	routing   RoutingReader
	payments  map[string]PaymentProvider
	invoicers map[string]invoicing.Provider
}

// NewRouter construye el router con los adaptadores disponibles en este entorno.
func NewRouter(routing RoutingReader, payments []PaymentProvider, invoicers []invoicing.Provider) *Router {
	r := &Router{routing: routing, payments: map[string]PaymentProvider{}, invoicers: map[string]invoicing.Provider{}}
	for _, p := range payments {
		r.payments[p.Name()] = p
	}
	for _, i := range invoicers {
		r.invoicers[i.Name()] = i
	}
	return r
}

// Route devuelve el routing del país.
func (r *Router) Route(ctx context.Context, country string) (Route, error) {
	return r.routing.Route(ctx, strings.ToUpper(strings.TrimSpace(country)))
}

// Payment devuelve el adaptador de pago de la cuenta. manual = true fuerza la
// transferencia con conciliación manual (Enterprise, PRD-009).
func (r *Router) Payment(ctx context.Context, country string, manual bool) (PaymentProvider, error) {
	if manual {
		return r.PaymentByName(ManualProvider)
	}
	route, err := r.Route(ctx, country)
	if err != nil {
		return nil, err
	}
	return r.PaymentByName(route.PaymentProvider)
}

// PaymentByName devuelve un adaptador de pago registrado (webhooks, reembolsos
// y conciliación usan el proveedor que hizo el cobro, no el routing actual).
func (r *Router) PaymentByName(name string) (PaymentProvider, error) {
	p, ok := r.payments[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	return p, nil
}

// Invoicing devuelve el adaptador fiscal y el routing (regla de impuestos) del país.
func (r *Router) Invoicing(ctx context.Context, country string) (invoicing.Provider, Route, error) {
	route, err := r.Route(ctx, country)
	if err != nil {
		return nil, Route{}, err
	}
	p, err := r.InvoicingByName(route.InvoicingProvider)
	return p, route, err
}

// InvoicingByName devuelve un adaptador fiscal registrado.
func (r *Router) InvoicingByName(name string) (invoicing.Provider, error) {
	p, ok := r.invoicers[name]
	if !ok {
		return nil, fmt.Errorf("%w: facturación %q", ErrUnknownProvider, name)
	}
	return p, nil
}
