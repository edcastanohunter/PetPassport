// Package setup compone los adaptadores de Billing a partir de la configuración
// (PP-F5-ARQ-002, ARQ-003). Lo comparten el api (tokenización, webhooks,
// reembolsos) y el worker (cobro, facturación, conciliación) para que ambos
// vean exactamente los mismos proveedores.
package setup

import (
	"net/http"
	"net/url"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	invfake "github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing/pdflocal"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers/manual"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers/wompi"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/egress"
)

// allowlisted aplica la allowlist y el circuit breaker de egress a cada petición.
type allowlisted struct{ client *egress.Client }

func (a allowlisted) Do(req *http.Request) (*http.Response, error) {
	if err := a.client.CheckAllowed(req.URL.String()); err != nil {
		return nil, err
	}
	// #nosec G704 -- el host se acaba de comprobar contra la allowlist de egress.
	return a.client.HTTPClient().Do(req) //nolint:gosec // G704: ver arriba
}

// Router construye el router de proveedores:
//   - manual: siempre (Enterprise, PRD-009).
//   - wompi: solo con credenciales (PP_WOMPI_*); su único destino saliente es el
//     host configurado.
//   - pdf_local: siempre (contingencia de facturación).
//   - fake (pagos y facturación): nunca en prod; fuera de dev solo con un
//     secreto de webhook propio (PP_BILLING_FAKE_WEBHOOK_SECRET), porque el
//     secreto por defecto del fake es público y permitiría falsificar eventos.
//
// Un país cuyo routing apunta a un adaptador no registrado falla al cobrar con
// ErrUnknownProvider: no hay sustitución silenciosa.
func Router(env config.Env, cfg config.Billing, routing providers.RoutingReader, renderer pdflocal.Renderer) *providers.Router {
	payments := []providers.PaymentProvider{manual.New()}
	invoicers := []invoicing.Provider{pdflocal.New(renderer)}
	if cfg.WompiEnabled() {
		hosts := []string{}
		if u, err := url.Parse(cfg.WompiBaseURL); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
		// 15 s: el cobro corre dentro de un job del scheduler con paso de 30 s.
		client := egress.NewClient(egress.Config{AllowedHosts: hosts, RequestTimeout: 15 * time.Second, MaxResponseBytes: 1 << 20})
		payments = append(payments, wompi.New(wompi.Config{
			BaseURL: cfg.WompiBaseURL, PublicKey: cfg.WompiPublicKey, PrivateKey: cfg.WompiPrivateKey,
			IntegritySecret: cfg.WompiIntegritySecret, EventsSecret: cfg.WompiEventsSecret,
		}, allowlisted{client}))
	}
	if env == config.EnvDev || (env != config.EnvProd && cfg.FakeWebhookSecret != "") {
		payments = append(payments, payfake.NewFakePaymentProvider().WithSecret(cfg.FakeWebhookSecret))
		invoicers = append(invoicers, invfake.NewFakeInvoicingProvider())
	}
	return providers.NewRouter(routing, payments, invoicers)
}
