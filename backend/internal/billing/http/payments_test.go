package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

var (
	ownOrg   = uuid.MustParse("01930000-0000-7000-8000-00000000c001")
	otherOrg = uuid.MustParse("01930000-0000-7000-8000-00000000c003")
)

// fixedScope: la cuenta agrupa exactamente las organizaciones dadas.
type fixedScope struct{ orgs []uuid.UUID }

func (s fixedScope) AccountOrgs(context.Context, uuid.UUID) ([]uuid.UUID, error) { return s.orgs, nil }
func (s fixedScope) SubscriptionAccount(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}

type fakePresigner struct{}

func (fakePresigner) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.invalid/" + key, nil
}

func clinicAdmin(org uuid.UUID) context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{
		UserID: uuid.New(), EmailVerified: true,
		Memberships: []auth.Membership{{OrganizationID: org, OrgType: "clinic", Role: "clinic_admin", OrgStatus: "active"}},
	})
}

func superAdmin() context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{UserID: uuid.New(), EmailVerified: true, GlobalRoles: []string{"super_admin"}})
}

func handlers(orgs ...uuid.UUID) *BillingHandlers {
	return NewBillingHandlers(nil, infra.NewQueries(nil), nil).
		WithScope(fixedScope{orgs: orgs}).
		WithPayments(PaymentDeps{Objects: fakePresigner{}})
}

func status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	return problem.From(err).Status
}

// PP-F5-API-004 / API-005: una cuenta solo es visible para quien administra una
// de sus organizaciones; conocer el id de una cuenta ajena no basta.
func TestPPF5API004_AccountRoutesRequireAdminOfAnAccountOrganization(t *testing.T) {
	id := uuid.New()
	calls := map[string]func(h *BillingHandlers, ctx context.Context) error{
		"setupPaymentMethod": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.SetupPaymentMethod(ctx, gen.SetupPaymentMethodRequestObject{Id: id})
			return err
		},
		"attachPaymentMethod": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.AttachPaymentMethod(ctx, gen.AttachPaymentMethodRequestObject{Id: id})
			return err
		},
		"listAccountInvoices": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.ListAccountInvoices(ctx, gen.ListAccountInvoicesRequestObject{Id: id})
			return err
		},
		"listAccountPayments": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.ListAccountPayments(ctx, gen.ListAccountPaymentsRequestObject{Id: id})
			return err
		},
		"getInvoicePdf": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.GetInvoicePdf(ctx, gen.GetInvoicePdfRequestObject{Id: id})
			return err
		},
		"listAccountSubscriptions": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.ListAccountSubscriptions(ctx, gen.ListAccountSubscriptionsRequestObject{Id: id})
			return err
		},
		"getSubscription": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.GetSubscription(ctx, gen.GetSubscriptionRequestObject{Id: id})
			return err
		},
		"cancelSubscription": func(h *BillingHandlers, ctx context.Context) error {
			_, err := h.CancelSubscription(ctx, gen.CancelSubscriptionRequestObject{Id: id})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			h := handlers(ownOrg)
			if got := status(call(h, clinicAdmin(ownOrg))); got != http.StatusOK {
				t.Errorf("admin de una organización de la cuenta: obtuvo %d", got)
			}
			if got := status(call(h, clinicAdmin(otherOrg))); got != http.StatusForbidden {
				t.Errorf("admin de otra organización: esperaba 403, obtuvo %d", got)
			}
			if got := status(call(handlers(), clinicAdmin(ownOrg))); got != http.StatusForbidden {
				t.Errorf("cuenta sin organizaciones conocidas: esperaba 403, obtuvo %d", got)
			}
			if got := status(call(handlers(), superAdmin())); got != http.StatusOK {
				t.Errorf("super_admin: obtuvo %d", got)
			}
		})
	}
}

// PP-F5-API-005: la descarga responde 302 o, con Accept JSON, el enlace prefirmado.
func TestPPF5API005_InvoiceDownloadRedirectsOrReturnsLink(t *testing.T) {
	h := handlers(ownOrg)
	resp, err := h.GetInvoicePdf(clinicAdmin(ownOrg), gen.GetInvoicePdfRequestObject{Id: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	redirect, ok := resp.(gen.GetInvoicePdf302Response)
	if !ok || redirect.Headers.Location == nil || *redirect.Headers.Location == "" {
		t.Fatalf("esperaba 302 con Location, obtuvo %#v", resp)
	}
	accept := "application/json"
	xml, err := h.GetInvoiceXml(clinicAdmin(ownOrg), gen.GetInvoiceXmlRequestObject{Id: uuid.New(), Params: gen.GetInvoiceXmlParams{Accept: &accept}})
	if err != nil {
		t.Fatal(err)
	}
	if link, ok := xml.(gen.GetInvoiceXml200JSONResponse); !ok || link.Url == "" {
		t.Fatalf("esperaba 200 con el enlace, obtuvo %#v", xml)
	}
}

func webhookHandlers(t *testing.T) (*BillingHandlers, *payfake.FakePaymentProvider) {
	t.Helper()
	fake := payfake.NewFakePaymentProvider().WithSecret("test-secret")
	router := providers.NewRouter(nil, []providers.PaymentProvider{fake}, nil)
	h := handlers().WithPayments(PaymentDeps{Payments: app.NewPaymentService(app.PaymentOptions{Router: router})})
	return h, fake
}

func webhookRequest(provider string, headers http.Header) gen.PaymentsWebhookRequestObject {
	req := gen.PaymentsWebhookRequestObject{Provider: provider}
	if v := headers.Get(payfake.HeaderSignature); v != "" {
		req.Params.XFakeSignature = &v
	}
	if v := headers.Get(payfake.HeaderTimestamp); v != "" {
		req.Params.XFakeTimestamp = &v
	}
	return req
}

// PP-F5-SEC-002: el webhook de pagos solo acepta eventos firmados y recientes.
func TestPPF5SEC002_PaymentsWebhookRejectsUnsignedAndUnknownProvider(t *testing.T) {
	h, fake := webhookHandlers(t)
	payment := providers.Payment{Ref: "tx_1", Reference: "referencia-ajena", Status: providers.ChargeStatus("paid"), Amount: 100, Currency: "COP"}
	headers, body := fake.SignedWebhook(payment, time.Now())
	ctx := httpserver.WithRawBody(context.Background(), body)

	if _, err := h.PaymentsWebhook(ctx, webhookRequest("desconocido", headers)); status(err) != http.StatusNotFound {
		t.Errorf("proveedor desconocido: esperaba 404, obtuvo %d", status(err))
	}
	if _, err := h.PaymentsWebhook(ctx, webhookRequest(fake.Name(), http.Header{})); status(err) != http.StatusUnauthorized {
		t.Errorf("sin firma: esperaba 401, obtuvo %d", status(err))
	}
	tampered := httpserver.WithRawBody(context.Background(), append([]byte(" "), body...))
	if _, err := h.PaymentsWebhook(tampered, webhookRequest(fake.Name(), headers)); status(err) != http.StatusUnauthorized {
		t.Errorf("cuerpo alterado: esperaba 401, obtuvo %d", status(err))
	}
	old, oldBody := fake.SignedWebhook(payment, time.Now().Add(-2*payfake.WebhookTolerance))
	if _, err := h.PaymentsWebhook(httpserver.WithRawBody(context.Background(), oldBody), webhookRequest(fake.Name(), old)); status(err) != http.StatusUnauthorized {
		t.Errorf("evento fuera de ventana: esperaba 401, obtuvo %d", status(err))
	}
	// Firmado y reciente, pero de una transacción que no es de Billing: se acepta y se ignora.
	resp, err := h.PaymentsWebhook(ctx, webhookRequest(fake.Name(), headers))
	if err != nil {
		t.Fatalf("evento firmado ajeno: %v", err)
	}
	if _, ok := resp.(gen.PaymentsWebhook204Response); !ok {
		t.Fatalf("esperaba 204, obtuvo %#v", resp)
	}
}

// PP-F5-API-007: las acciones administrativas exigen super_admin y un motivo.
func TestPPF5API007_AdminActionsRequireSuperAdminAndReason(t *testing.T) {
	h := handlers(ownOrg)
	id := uuid.New()
	refund := func(ctx context.Context, reason string) error {
		_, err := h.AdminRefundCharge(ctx, gen.AdminRefundChargeRequestObject{
			Id: id, Body: &gen.AdminRefundChargeJSONRequestBody{Amount: 10, Reason: reason},
		})
		return err
	}
	if got := status(refund(clinicAdmin(ownOrg), "Cobro duplicado por error del proveedor")); got != http.StatusForbidden {
		t.Errorf("admin de clínica: esperaba 403, obtuvo %d", got)
	}
	if got := status(refund(superAdmin(), "corto")); got != http.StatusUnprocessableEntity {
		t.Errorf("motivo corto: esperaba 422, obtuvo %d", got)
	}
	if got := status(refund(superAdmin(), "Cobro duplicado por error del proveedor")); got != http.StatusOK {
		t.Errorf("super_admin con motivo: obtuvo %d", got)
	}
	if _, err := h.AdminListBillingAccounts(clinicAdmin(ownOrg), gen.AdminListBillingAccountsRequestObject{}); status(err) != http.StatusForbidden {
		t.Errorf("lista de cuentas como admin de clínica: esperaba 403, obtuvo %d", status(err))
	}
}

// PP-F5-PRD-004 / SEC-001: el pagador de PSE solo vuelve a una ruta de la SPA;
// nunca a otro origen (open redirect).
func TestPPF5PRD004_PSEReturnPathStaysOnTheSPA(t *testing.T) {
	for path, want := range map[string]bool{
		"/org/x/settings/billing?pse=return": true,
		"/":                                  true,
		"":                                   false,
		"https://evil.example/":              false,
		"//evil.example/":                    false,
		`/\evil.example`:                     false,
		"/ok\r\nSet-Cookie: x":               false,
		"org/x":                              false,
	} {
		if got := validReturnPath(path); got != want {
			t.Errorf("validReturnPath(%q) = %v, quiero %v", path, got, want)
		}
	}
}
