package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Cobro, facturas y administración de facturación (PP-F5-API-004, API-005, API-007).

const (
	presignTTL   = 15 * time.Minute
	defaultLimit = 50
	minReason    = 10 // el motivo de una acción administrativa queda en el audit log
)

// Presigner emite URLs de descarga prefirmadas del object storage.
type Presigner interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Scope resuelve a qué organizaciones pertenece un recurso de facturación: es
// lo que la política compara con las membresías del principal.
type Scope interface {
	// AccountOrgs devuelve las organizaciones de la cuenta (vacío si no existe).
	AccountOrgs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
	// SubscriptionAccount devuelve la cuenta de la suscripción (uuid.Nil si no existe).
	SubscriptionAccount(ctx context.Context, subID uuid.UUID) (uuid.UUID, error)
}

type queryScope struct{ queries *infra.Queries }

func (s queryScope) AccountOrgs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	acc, err := s.queries.GetAccount(ctx, accountID)
	if err != nil || acc == nil {
		return nil, err
	}
	return acc.Organizations, nil
}

func (s queryScope) SubscriptionAccount(ctx context.Context, subID uuid.UUID) (uuid.UUID, error) {
	sub, err := s.queries.GetSubscription(ctx, subID)
	if err != nil || sub == nil {
		return uuid.Nil, err
	}
	return sub.AccountID, nil
}

// PaymentDeps son las dependencias de cobro y facturación de los handlers.
type PaymentDeps struct {
	Payments  *app.PaymentService
	Invoicing *app.InvoicingService
	Ledger    *infra.Ledger
	Invoices  *infra.InvoiceStore
	Objects   Presigner
	Coupons   *infra.CouponStore
	// Invitations debe ser nil (no un *InvitationStore nil) cuando no hay almacén.
	Invitations Invitations
}

// WithPayments conecta los servicios de cobro y facturación (M5.2).
func (h *BillingHandlers) WithPayments(d PaymentDeps) *BillingHandlers {
	h.pay, h.inv, h.ledger, h.invoices, h.objects = d.Payments, d.Invoicing, d.Ledger, d.Invoices, d.Objects
	h.coupons, h.invitations = d.Coupons, d.Invitations
	return h
}

// WithScope sustituye la resolución cuenta → organizaciones (pruebas).
func (h *BillingHandlers) WithScope(s Scope) *BillingHandlers {
	h.scope = s
	return h
}

// accountRes construye el recurso de autorización de una cuenta. Si la
// resolución falla, el recurso queda sin organizaciones y la política deniega
// a todo el que no sea SuperAdmin (falla cerrado).
func (h *BillingHandlers) accountRes(ctx context.Context, accountID uuid.UUID) authz.BillingResource {
	res := authz.BillingResource{AccountID: accountID}
	orgs, err := h.scope.AccountOrgs(ctx, accountID)
	if err != nil {
		h.logger.ErrorContext(ctx, "billing: no se pudo resolver el alcance de la cuenta", "account_id", accountID.String(), "error", err)
		return res
	}
	res.OrgIDs = orgs
	return res
}

// subscriptionRes construye el recurso de autorización de una suscripción a
// partir de la cuenta que la posee.
func (h *BillingHandlers) subscriptionRes(ctx context.Context, subID uuid.UUID) authz.BillingResource {
	accountID, err := h.scope.SubscriptionAccount(ctx, subID)
	if err != nil {
		h.logger.ErrorContext(ctx, "billing: no se pudo resolver el alcance de la suscripción", "subscription_id", subID.String(), "error", err)
		return authz.BillingResource{}
	}
	return h.accountRes(ctx, accountID)
}

func (h *BillingHandlers) authorize(ctx context.Context, action authz.Action, res authz.Resource) (auth.Principal, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return auth.Principal{}, err
	}
	if d := authz.Can(p, action, res); !d.Allow {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: d.Reason}
	}
	return p, nil
}

func limitOf(l *gen.BillingLimit) int {
	if l == nil || *l < 1 {
		return defaultLimit
	}
	return int(*l)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mapPaymentError traduce los errores de cobro y facturación a problem+json.
func mapPaymentError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrAccountNotFound):
		return &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	case errors.Is(err, domain.ErrChargeNotFound):
		return &kernel.NotFoundError{Resource: "Cargo"}
	case errors.Is(err, providers.ErrPaymentMethodInvalid):
		return kernel.NewDomainError("PAYMENT_METHOD_INVALID", "El proveedor rechazó el método de pago.")
	case errors.Is(err, providers.ErrUnavailable), errors.Is(err, providers.ErrUnknownProvider):
		return kernel.NewDomainError("PAYMENT_PROVIDER_UNAVAILABLE", "El proveedor de pagos no está disponible. Inténtalo de nuevo en unos minutos.")
	case errors.Is(err, providers.ErrNotSupported):
		return kernel.NewDomainError("PAYMENT_OPERATION_NOT_SUPPORTED", "El proveedor de este pago no admite la operación.")
	case errors.Is(err, domain.ErrChargeNotPaid):
		return kernel.NewDomainError("CHARGE_NOT_PAID", "El cargo no está pagado.")
	case errors.Is(err, domain.ErrChargeNotPending):
		return kernel.NewDomainError("CHARGE_NOT_PENDING", "El cargo no está pendiente de pago.")
	case errors.Is(err, domain.ErrRefundExceeds):
		return kernel.NewDomainError("REFUND_EXCEEDS_BALANCE", "El importe supera el saldo reembolsable del cargo.")
	case errors.Is(err, app.ErrRefundInProgress):
		return kernel.NewDomainError("REFUND_IN_PROGRESS", "Hay un reembolso en curso para este cargo.")
	case errors.Is(err, domain.ErrManualAmountMismatch):
		return kernel.NewDomainError("MANUAL_AMOUNT_MISMATCH", "El importe debe coincidir con el total del cargo.")
	case errors.Is(err, app.ErrDuplicatePaymentRef):
		return kernel.NewDomainError("PAYMENT_REFERENCE_DUPLICATE", "La referencia de pago ya está registrada.")
	case errors.Is(err, domain.ErrInvalidAdjustment):
		return kernel.NewDomainError("ADJUSTMENT_INVALID", "El ajuste no es aplicable a esta suscripción.")
	case errors.Is(err, domain.ErrPaymentMethodInUse):
		return kernel.NewDomainError("PAYMENT_METHOD_IN_USE", "El método de pago respalda una suscripción vigente: cancélala o cambia de método primero.")
	case errors.Is(err, infra.ErrCouponInvalid):
		return kernel.NewDomainError("COUPON_INVALID", "El cupón no es válido para este plan.")
	case errors.Is(err, infra.ErrCouponExists):
		return kernel.NewDomainError("COUPON_EXISTS", "Ya existe un cupón con ese código.")
	case errors.Is(err, app.ErrInvoiceNotRetryable):
		return kernel.NewDomainError("INVOICE_NOT_RETRYABLE", "Solo se pueden reemitir facturas rechazadas.")
	default:
		return mapDomainError(err)
	}
}

// -----------------------------------------------------------------------------
// Métodos de pago y webhook (PP-F5-API-004)
// -----------------------------------------------------------------------------

// SetupPaymentMethod devuelve la configuración del widget del proveedor.
func (h *BillingHandlers) SetupPaymentMethod(ctx context.Context, req gen.SetupPaymentMethodRequestObject) (gen.SetupPaymentMethodResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	if req.Body == nil || h.svc == nil {
		return gen.SetupPaymentMethod200JSONResponse{ClientConfig: map[string]string{}}, nil
	}
	cfg, err := h.svc.SetupPaymentMethod(ctx, req.Id, string(req.Body.Kind))
	if err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.SetupPaymentMethod200JSONResponse{Provider: cfg.Provider, ClientConfig: cfg.ClientConfig}, nil
}

// AttachPaymentMethod registra el método tokenizado como método por defecto.
func (h *BillingHandlers) AttachPaymentMethod(ctx context.Context, req gen.AttachPaymentMethodRequestObject) (gen.AttachPaymentMethodResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	if req.Body == nil || h.svc == nil {
		return gen.AttachPaymentMethod200JSONResponse{}, nil
	}
	cmd := app.AttachPaymentMethodCmd{ProviderToken: strings.TrimSpace(req.Body.ProviderToken)}
	if cmd.ProviderToken == "" {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "provider_token", Code: "REQUIRED"}}}
	}
	if req.Body.AcceptanceToken != nil {
		cmd.AcceptanceToken = *req.Body.AcceptanceToken
	}
	if req.Body.PersonalAuthToken != nil {
		cmd.PersonalAuth = *req.Body.PersonalAuthToken
	}
	pm, err := h.svc.AttachPaymentMethod(ctx, req.Id, cmd)
	if err != nil {
		return nil, mapPaymentError(err)
	}
	out := gen.AttachPaymentMethod200JSONResponse{Kind: pm.Kind, Last4: pm.Last4}
	if pm.ExpiresAt != nil {
		out.ExpiresAt = strPtr(pm.ExpiresAt.Format("01/06"))
	}
	return out, nil
}

// PaymentsWebhook recibe los eventos del proveedor de pagos. La autenticación es
// la firma del propio evento (SEC-002); el cuerpo se verifica crudo, tal como llegó.
func (h *BillingHandlers) PaymentsWebhook(ctx context.Context, req gen.PaymentsWebhookRequestObject) (gen.PaymentsWebhookResponseObject, error) {
	if h.pay == nil {
		return nil, &kernel.NotFoundError{Resource: "webhook"}
	}
	headers := http.Header{}
	for name, v := range map[string]*string{
		"X-Event-Checksum": req.Params.XEventChecksum,
		"X-Fake-Signature": req.Params.XFakeSignature,
		"X-Fake-Timestamp": req.Params.XFakeTimestamp,
	} {
		if v != nil {
			headers.Set(name, *v)
		}
	}
	_, err := h.pay.HandleWebhook(ctx, req.Provider, headers, httpserver.RawBodyFrom(ctx))
	switch {
	case err == nil:
		return gen.PaymentsWebhook204Response{}, nil
	case errors.Is(err, providers.ErrUnknownProvider):
		return nil, &kernel.NotFoundError{Resource: "webhook"}
	case errors.Is(err, providers.ErrInvalidSignature):
		return nil, kernel.NewDomainError("WEBHOOK_SIGNATURE_INVALID", "La firma del webhook no es válida.")
	case errors.Is(err, app.ErrChargeNotReady):
		// 409: el proveedor reintenta cuando el intento ya sea visible.
		return nil, kernel.NewDomainError("WEBHOOK_NOT_READY", "El cargo aún no está disponible.")
	default:
		return nil, err
	}
}

// -----------------------------------------------------------------------------
// Facturas y pagos (PP-F5-API-005)
// -----------------------------------------------------------------------------

func toGenInvoice(inv infra.InvoiceRow, notes []infra.CreditNoteRow) gen.Invoice {
	subtotal, tax := float32(inv.Totals.Subtotal), float32(inv.Totals.Tax)
	out := gen.Invoice{
		Id: inv.ID, AccountId: inv.AccountID, ChargeId: inv.ChargeID, CreatedAt: inv.CreatedAt,
		Number: strPtr(inv.Number), Provider: strPtr(inv.Provider), Status: gen.InvoiceStatus(inv.Status),
		IssuedAt: inv.IssuedAt, Subtotal: &subtotal, Tax: &tax, Total: float32(inv.Totals.Total),
		Currency: inv.Totals.Currency, HasPdf: inv.HasPDF, HasXml: inv.HasXML,
		RejectionReason: strPtr(inv.RejectionReason), CreditNotes: []gen.InvoiceCreditNote{},
	}
	if inv.HasPDF {
		out.PdfUrl = strPtr("/invoices/" + inv.ID.String() + "/pdf")
	}
	if inv.HasXML {
		out.XmlUrl = strPtr("/invoices/" + inv.ID.String() + "/xml")
	}
	for _, cn := range notes {
		out.CreditNotes = append(out.CreditNotes, gen.InvoiceCreditNote{
			Id: cn.ID, Number: strPtr(cn.Number), Status: cn.Status, Amount: float32(cn.Amount), Reason: strPtr(cn.Reason),
		})
	}
	return out
}

func (h *BillingHandlers) invoicesWithNotes(ctx context.Context, rows []infra.InvoiceRow) ([]gen.Invoice, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	notes, err := h.invoices.CreditNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Invoice, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGenInvoice(r, notes[r.ID]))
	}
	return out, nil
}

func toGenPayment(p infra.PaymentRow) gen.BillingPayment {
	return gen.BillingPayment{
		Id: p.ID, ChargeId: p.ChargeID, Kind: gen.BillingPaymentKind(p.Kind), Amount: float32(p.Amount),
		Currency: p.Currency, Status: p.Status, Provider: p.Provider, Reference: strPtr(p.ProviderRef),
		Reason: strPtr(p.Reason), At: p.At,
	}
}

func toGenPayments(rows []infra.PaymentRow) []gen.BillingPayment {
	out := make([]gen.BillingPayment, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGenPayment(r))
	}
	return out
}

// ListAccountInvoices lista las facturas emitidas de la cuenta.
func (h *BillingHandlers) ListAccountInvoices(ctx context.Context, req gen.ListAccountInvoicesRequestObject) (gen.ListAccountInvoicesResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingRead, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	rows, err := h.invoices.ListByAccount(ctx, req.Id, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	data, err := h.invoicesWithNotes(ctx, rows)
	if err != nil {
		return nil, err
	}
	return gen.ListAccountInvoices200JSONResponse{Data: data}, nil
}

// ListAccountPayments lista los pagos y reembolsos de la cuenta.
func (h *BillingHandlers) ListAccountPayments(ctx context.Context, req gen.ListAccountPaymentsRequestObject) (gen.ListAccountPaymentsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingRead, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	rows, err := h.ledger.ListPayments(ctx, req.Id, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListAccountPayments200JSONResponse{Data: toGenPayments(rows)}, nil
}

// invoiceLink autoriza la lectura de la factura y devuelve la URL prefirmada del
// documento que key selecciona. Una factura inexistente, en borrador o sin ese
// documento es 404.
func (h *BillingHandlers) invoiceLink(ctx context.Context, id uuid.UUID, key func(*infra.InvoiceRow) string) (string, error) {
	inv, err := h.invoices.Get(ctx, id)
	if err != nil {
		return "", err
	}
	res := authz.BillingResource{}
	if inv != nil {
		res = h.accountRes(ctx, inv.AccountID)
	}
	if _, err := h.authorize(ctx, authz.BillingRead, res); err != nil {
		return "", err
	}
	notFound := &kernel.NotFoundError{Resource: "Factura"}
	if inv == nil || inv.Status == infra.InvoiceDraft || key(inv) == "" || h.objects == nil {
		return "", notFound
	}
	return h.objects.PresignGet(ctx, key(inv), presignTTL)
}

func wantsJSON(accept *gen.AcceptLink) bool {
	return accept != nil && strings.Contains(strings.ToLower(*accept), "application/json")
}

// GetInvoicePdf redirige a la representación gráfica de la factura.
func (h *BillingHandlers) GetInvoicePdf(ctx context.Context, req gen.GetInvoicePdfRequestObject) (gen.GetInvoicePdfResponseObject, error) {
	url, err := h.invoiceLink(ctx, req.Id, func(i *infra.InvoiceRow) string { return i.PDFKey })
	if err != nil {
		return nil, err
	}
	if wantsJSON(req.Params.Accept) {
		return gen.GetInvoicePdf200JSONResponse{Url: url, ExpiresAt: time.Now().UTC().Add(presignTTL)}, nil
	}
	return gen.GetInvoicePdf302Response{Headers: gen.GetInvoicePdf302ResponseHeaders{Location: &url}}, nil
}

// GetInvoiceXml redirige al XML firmado de la factura electrónica.
func (h *BillingHandlers) GetInvoiceXml(ctx context.Context, req gen.GetInvoiceXmlRequestObject) (gen.GetInvoiceXmlResponseObject, error) {
	url, err := h.invoiceLink(ctx, req.Id, func(i *infra.InvoiceRow) string { return i.XMLKey })
	if err != nil {
		return nil, err
	}
	if wantsJSON(req.Params.Accept) {
		return gen.GetInvoiceXml200JSONResponse{Url: url, ExpiresAt: time.Now().UTC().Add(presignTTL)}, nil
	}
	return gen.GetInvoiceXml302Response{Headers: gen.GetInvoiceXml302ResponseHeaders{Location: &url}}, nil
}

// -----------------------------------------------------------------------------
// Administración de facturación (PP-F5-API-007)
// -----------------------------------------------------------------------------

// adminWrite autoriza una acción administrativa, valida el motivo y la deja en
// el audit log antes de ejecutarla.
func (h *BillingHandlers) adminWrite(ctx context.Context, verb, targetType string, targetID uuid.UUID, reason string, details map[string]any) error {
	p, err := h.authorize(ctx, authz.AdminBillingWrite, nil)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < minReason {
		return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reason", Code: "REASON_REQUIRED"}}}
	}
	actor := p.UserID
	entry := audit.Entry{
		ActorID: &actor, ActorRoles: p.GlobalRoles, Action: "admin.billing." + verb,
		TargetType: targetType, TargetID: &targetID, Reason: reason, Details: details,
	}
	if id, err := uuid.Parse(reqctx.From(ctx).CorrelationID); err == nil {
		entry.CorrelationID = &id
	}
	return h.ledger.Audit(ctx, entry)
}

func toGenDiscrepancy(d infra.Discrepancy) gen.BillingDiscrepancy {
	out := gen.BillingDiscrepancy{
		Id: d.ID, AccountId: d.AccountID, ChargeId: d.ChargeID, Kind: d.Kind,
		Status: gen.BillingDiscrepancyStatus(d.Status), DetectedAt: d.DetectedAt,
		ResolvedAt: d.ResolvedAt, Resolution: strPtr(d.Resolution),
	}
	var details map[string]any
	if len(d.Details) > 0 && json.Unmarshal(d.Details, &details) == nil && details != nil {
		out.Details = &details
	}
	return out
}

func toGenDiscrepancies(rows []infra.Discrepancy) []gen.BillingDiscrepancy {
	out := make([]gen.BillingDiscrepancy, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGenDiscrepancy(r))
	}
	return out
}

// AdminListBillingAccounts busca cuentas de facturación.
func (h *BillingHandlers) AdminListBillingAccounts(ctx context.Context, req gen.AdminListBillingAccountsRequestObject) (gen.AdminListBillingAccountsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingRead, nil); err != nil {
		return nil, err
	}
	f := infra.AccountFilter{Limit: limitOf(req.Params.Limit)}
	if req.Params.Q != nil {
		f.Query = strings.TrimSpace(*req.Params.Q)
	}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if req.Params.Plan != nil {
		f.Plan = *req.Params.Plan
	}
	if req.Params.Country != nil {
		f.Country = *req.Params.Country
	}
	rows, err := h.queries.ListAccounts(ctx, f)
	if err != nil {
		return nil, err
	}
	data := make([]gen.AdminBillingAccountSummary, 0, len(rows))
	for _, r := range rows {
		item := gen.AdminBillingAccountSummary{
			Id: r.ID, LegalName: r.LegalName, Country: r.Country, Currency: r.Currency, Status: r.Status, CreatedAt: r.CreatedAt,
			Mrr: float32(r.MRR),
		}
		raw, err := json.Marshal(r.Subscriptions)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Subscriptions); err != nil {
			return nil, err
		}
		data = append(data, item)
	}
	return gen.AdminListBillingAccounts200JSONResponse{Data: data}, nil
}

// AdminGetBillingAccount devuelve la cuenta con su historial de cobro.
func (h *BillingHandlers) AdminGetBillingAccount(ctx context.Context, req gen.AdminGetBillingAccountRequestObject) (gen.AdminGetBillingAccountResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingRead, nil); err != nil {
		return nil, err
	}
	acc, err := h.queries.GetAccount(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	}
	subs, err := h.queries.ListAccountSubscriptions(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	payments, err := h.ledger.ListPayments(ctx, req.Id, defaultLimit)
	if err != nil {
		return nil, err
	}
	invRows, err := h.invoices.ListByAccount(ctx, req.Id, defaultLimit)
	if err != nil {
		return nil, err
	}
	invoices, err := h.invoicesWithNotes(ctx, invRows)
	if err != nil {
		return nil, err
	}
	discrepancies, err := h.ledger.DiscrepanciesByAccount(ctx, req.Id, defaultLimit)
	if err != nil {
		return nil, err
	}
	targets := []uuid.UUID{acc.ID}
	for i := range subs {
		targets = append(targets, subs[i].ID)
		for _, c := range subs[i].Charges {
			targets = append(targets, c.ID)
		}
	}
	for _, inv := range invRows {
		targets = append(targets, inv.ID)
	}
	for _, d := range discrepancies {
		targets = append(targets, d.ID)
	}
	trail, err := h.ledger.AuditFor(ctx, targets, defaultLimit)
	if err != nil {
		return nil, err
	}
	out := gen.AdminGetBillingAccount200JSONResponse{
		Account: toGenAccount(acc), Subscriptions: make([]gen.Subscription, 0, len(subs)),
		Payments: toGenPayments(payments), Invoices: invoices, Discrepancies: toGenDiscrepancies(discrepancies),
		Audit: toGenAudit(trail),
	}
	for i := range subs {
		out.Subscriptions = append(out.Subscriptions, toGenSubscription(&subs[i]))
	}
	return out, nil
}

func toGenAudit(rows []infra.AuditRow) []gen.BillingAuditEntry {
	out := make([]gen.BillingAuditEntry, 0, len(rows))
	for _, r := range rows {
		e := gen.BillingAuditEntry{
			Id: r.ID, OccurredAt: r.OccurredAt, ActorId: r.ActorID, Action: r.Action,
			TargetType: strPtr(r.TargetType), TargetId: r.TargetID, Reason: strPtr(r.Reason),
		}
		if len(r.Details) > 0 {
			d := r.Details
			e.Details = &d
		}
		out = append(out, e)
	}
	return out
}

func (h *BillingHandlers) subscriptionView(ctx context.Context, id uuid.UUID) (gen.Subscription, error) {
	sub, err := h.queries.GetSubscription(ctx, id)
	if err != nil {
		return gen.Subscription{}, err
	}
	if sub == nil {
		return gen.Subscription{}, &kernel.NotFoundError{Resource: "Suscripción"}
	}
	return toGenSubscription(sub), nil
}

// AdminAdjustSubscription aplica un ajuste administrativo (PRD-011).
func (h *BillingHandlers) AdminAdjustSubscription(ctx context.Context, req gen.AdminAdjustSubscriptionRequestObject) (gen.AdminAdjustSubscriptionResponseObject, error) {
	body := gen.AdminAdjustSubscriptionJSONRequestBody{}
	if req.Body != nil {
		body = *req.Body
	}
	details := map[string]any{"kind": string(body.Kind)}
	cmd := app.AdjustCmd{Kind: string(body.Kind), Until: body.Until, Reason: strings.TrimSpace(body.Reason)}
	if body.Amount != nil {
		cmd.Amount = float64(*body.Amount)
		details["amount"] = cmd.Amount
	}
	if body.Until != nil {
		details["until"] = body.Until.UTC().Format(time.RFC3339)
	}
	if body.PlanCode != nil {
		details["plan_code"] = *body.PlanCode
	}
	if body.Seats != nil {
		details["seats"] = *body.Seats
	}
	if body.Cycle != nil {
		details["cycle"] = string(*body.Cycle)
	}
	if err := h.adminWrite(ctx, "subscription.adjust", "subscription", req.Id, body.Reason, details); err != nil {
		return nil, err
	}
	if h.pay == nil {
		return gen.AdminAdjustSubscription200JSONResponse{}, nil
	}
	if body.Kind == gen.ChangePlan {
		if err := h.adminChangePlan(ctx, req.Id, body); err != nil {
			return nil, err
		}
	} else if err := h.pay.AdjustSubscription(ctx, req.Id, cmd); err != nil {
		return nil, mapPaymentError(err)
	}
	view, err := h.subscriptionView(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AdminAdjustSubscription200JSONResponse(view), nil
}

// AdminCancelSubscription cancela de inmediato una suscripción.
func (h *BillingHandlers) AdminCancelSubscription(ctx context.Context, req gen.AdminCancelSubscriptionRequestObject) (gen.AdminCancelSubscriptionResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	if err := h.adminWrite(ctx, "subscription.cancel", "subscription", req.Id, reason, nil); err != nil {
		return nil, err
	}
	if h.svc == nil {
		return gen.AdminCancelSubscription200JSONResponse{}, nil
	}
	if err := h.svc.CancelSubscription(ctx, req.Id, false, strings.TrimSpace(reason)); err != nil {
		return nil, mapPaymentError(err)
	}
	view, err := h.subscriptionView(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AdminCancelSubscription200JSONResponse(view), nil
}

// AdminRecordManualPayment asienta una transferencia recibida (PRD-009).
func (h *BillingHandlers) AdminRecordManualPayment(ctx context.Context, req gen.AdminRecordManualPaymentRequestObject) (gen.AdminRecordManualPaymentResponseObject, error) {
	body := gen.AdminRecordManualPaymentJSONRequestBody{}
	if req.Body != nil {
		body = *req.Body
	}
	ref := strings.TrimSpace(body.Reference)
	details := map[string]any{"amount": float64(body.Amount), "reference": ref}
	if err := h.adminWrite(ctx, "charge.manual_payment", "charge", req.Id, body.Reason, details); err != nil {
		return nil, err
	}
	if h.pay == nil {
		return gen.AdminRecordManualPayment204Response{}, nil
	}
	if ref == "" {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "reference", Code: "REQUIRED"}}}
	}
	if err := h.pay.RecordManualPayment(ctx, req.Id, float64(body.Amount), ref); err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.AdminRecordManualPayment204Response{}, nil
}

// AdminRefundCharge reembolsa total o parcialmente un cargo pagado (PRD-011).
func (h *BillingHandlers) AdminRefundCharge(ctx context.Context, req gen.AdminRefundChargeRequestObject) (gen.AdminRefundChargeResponseObject, error) {
	body := gen.AdminRefundChargeJSONRequestBody{}
	if req.Body != nil {
		body = *req.Body
	}
	if err := h.adminWrite(ctx, "charge.refund", "charge", req.Id, body.Reason, map[string]any{"amount": float64(body.Amount)}); err != nil {
		return nil, err
	}
	if h.pay == nil {
		return gen.AdminRefundCharge200JSONResponse{}, nil
	}
	if body.Amount <= 0 {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "amount", Code: "INVALID"}}}
	}
	ref, err := h.pay.RefundCharge(ctx, req.Id, float64(body.Amount), strings.TrimSpace(body.Reason))
	if err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.AdminRefundCharge200JSONResponse{RefundRef: ref}, nil
}

// AdminListBillingDiscrepancies lista las discrepancias de conciliación.
func (h *BillingHandlers) AdminListBillingDiscrepancies(ctx context.Context, req gen.AdminListBillingDiscrepanciesRequestObject) (gen.AdminListBillingDiscrepanciesResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingRead, nil); err != nil {
		return nil, err
	}
	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	rows, err := h.ledger.ListDiscrepancies(ctx, status, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.AdminListBillingDiscrepancies200JSONResponse{Data: toGenDiscrepancies(rows)}, nil
}

// AdminResolveBillingDiscrepancy cierra una discrepancia abierta.
func (h *BillingHandlers) AdminResolveBillingDiscrepancy(ctx context.Context, req gen.AdminResolveBillingDiscrepancyRequestObject) (gen.AdminResolveBillingDiscrepancyResponseObject, error) {
	body := gen.AdminResolveBillingDiscrepancyJSONRequestBody{}
	if req.Body != nil {
		body = *req.Body
	}
	resolution := strings.TrimSpace(body.Resolution)
	if err := h.adminWrite(ctx, "discrepancy.resolve", "billing_discrepancy", req.Id, body.Reason, map[string]any{"resolution": resolution}); err != nil {
		return nil, err
	}
	if h.pay == nil {
		return gen.AdminResolveBillingDiscrepancy204Response{}, nil
	}
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	done, err := h.ledger.ResolveDiscrepancy(ctx, req.Id, resolution, p.UserID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !done {
		return nil, &kernel.NotFoundError{Resource: "Discrepancia"}
	}
	return gen.AdminResolveBillingDiscrepancy204Response{}, nil
}

// AdminListBillingInvoices lista facturas por estado (por defecto, rechazadas).
func (h *BillingHandlers) AdminListBillingInvoices(ctx context.Context, req gen.AdminListBillingInvoicesRequestObject) (gen.AdminListBillingInvoicesResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingRead, nil); err != nil {
		return nil, err
	}
	status := infra.InvoiceRejected
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	rows, err := h.invoices.ListByStatus(ctx, status, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	data, err := h.invoicesWithNotes(ctx, rows)
	if err != nil {
		return nil, err
	}
	return gen.AdminListBillingInvoices200JSONResponse{Data: data}, nil
}

// AdminRetryInvoice reemite una factura rechazada con los datos fiscales actuales.
func (h *BillingHandlers) AdminRetryInvoice(ctx context.Context, req gen.AdminRetryInvoiceRequestObject) (gen.AdminRetryInvoiceResponseObject, error) {
	if _, err := h.authorize(ctx, authz.AdminBillingWrite, nil); err != nil {
		return nil, err
	}
	if h.inv == nil {
		return gen.AdminRetryInvoice202Response{}, nil
	}
	if err := h.inv.RetryInvoice(ctx, req.Id); err != nil {
		return nil, mapPaymentError(err)
	}
	return gen.AdminRetryInvoice202Response{}, nil
}
