// Package fake es el PaymentProvider de dev y CI (PP-F5-ARQ-002, QA-002): mismo
// contrato que los adaptadores reales, con escenarios (éxito, rechazo, pendiente,
// proveedor caído, webhook duplicado o tardío) y webhooks firmados con HMAC.
package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// Name es el nombre del adaptador en billing.provider_routing.
const Name = "fake"

// Cabeceras del webhook simulado.
const (
	HeaderSignature = "X-Fake-Signature"
	HeaderTimestamp = "X-Fake-Timestamp"
)

// WebhookTolerance es la ventana temporal aceptada para un webhook.
const WebhookTolerance = 5 * time.Minute

// Scenario define el comportamiento simulado del proveedor.
type Scenario string

// Escenarios. Los tokens "tok_decline", "tok_pending" y "tok_down" fuerzan el
// escenario para ese método de pago (útil en E2E sin tocar el proceso).
const (
	ScenarioSuccess  Scenario = "success"
	ScenarioDeclined Scenario = "declined"
	ScenarioPending  Scenario = "pending"
	ScenarioFailure  Scenario = "failure" // proveedor caído
)

// FakePaymentProvider simula la pasarela de pagos.
type FakePaymentProvider struct { //nolint:revive // nombre heredado de M5.1
	mu       sync.Mutex
	secret   []byte
	scenario Scenario
	payments map[string]providers.Payment // por ref
	byRef    map[string]string            // referencia → ref
	charges  int                          // cobros realmente creados (idempotencia)
	refunded map[string]float64
	seq      int
}

var _ providers.PaymentProvider = (*FakePaymentProvider)(nil)

// NewFakePaymentProvider construye el proveedor en modo éxito.
func NewFakePaymentProvider() *FakePaymentProvider {
	return &FakePaymentProvider{
		secret:   []byte("fake-webhook-secret"),
		scenario: ScenarioSuccess,
		payments: map[string]providers.Payment{},
		byRef:    map[string]string{},
		refunded: map[string]float64{},
	}
}

// WithSecret fija el secreto con el que se firman y verifican los webhooks.
func (f *FakePaymentProvider) WithSecret(secret string) *FakePaymentProvider {
	if secret != "" {
		f.secret = []byte(secret)
	}
	return f
}

// SetScenario cambia el comportamiento de los siguientes cobros.
func (f *FakePaymentProvider) SetScenario(s Scenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenario = s
}

// ChargesCreated devuelve cuántas transacciones distintas se han creado.
func (f *FakePaymentProvider) ChargesCreated() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charges
}

// Name implementa PaymentProvider.
func (*FakePaymentProvider) Name() string { return Name }

// CreateCustomer implementa PaymentProvider.
func (*FakePaymentProvider) CreateCustomer(_ context.Context, c providers.Customer) (string, error) {
	return "cus_fake_" + c.AccountID.String()[:8], nil
}

// SetupPaymentMethod implementa PaymentProvider.
func (*FakePaymentProvider) SetupPaymentMethod(_ context.Context, req providers.SetupRequest) (providers.SetupConfig, error) {
	return providers.SetupConfig{Provider: Name, ClientConfig: map[string]string{"mode": "fake", "kind": req.Kind}}, nil
}

// AttachPaymentMethod implementa PaymentProvider. "tok_invalid" simula un token rechazado.
func (*FakePaymentProvider) AttachPaymentMethod(_ context.Context, req providers.AttachRequest) (providers.PaymentMethod, error) {
	if req.ProviderToken == "" || req.ProviderToken == "tok_invalid" {
		return providers.PaymentMethod{}, providers.ErrPaymentMethodInvalid
	}
	exp := time.Date(2030, 12, 31, 0, 0, 0, 0, time.UTC)
	return providers.PaymentMethod{Token: req.ProviderToken, Kind: "card", Last4: "4242", Brand: "VISA", ExpiresAt: &exp}, nil
}

func (f *FakePaymentProvider) scenarioFor(token string) Scenario {
	switch token {
	case "tok_decline":
		return ScenarioDeclined
	case "tok_pending":
		return ScenarioPending
	case "tok_down":
		return ScenarioFailure
	}
	return f.scenario
}

// Charge implementa PaymentProvider; idempotente por referencia (cargo + intento).
func (f *FakePaymentProvider) Charge(_ context.Context, req providers.ChargeRequest) (providers.ChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	reference := req.Reference()
	if ref, ok := f.byRef[reference]; ok {
		p := f.payments[ref]
		return providers.ChargeResult{Ref: p.Ref, Status: p.Status, ErrorCode: p.ErrorCode}, nil
	}
	sc := f.scenarioFor(req.Token)
	if sc == ScenarioFailure {
		return providers.ChargeResult{}, providers.ErrUnavailable
	}
	f.seq++
	f.charges++
	p := providers.Payment{
		Ref: "pay_fake_" + strconv.Itoa(f.seq), Reference: reference,
		Amount: req.Amount, Currency: req.Currency,
	}
	switch sc {
	case ScenarioDeclined:
		p.Status, p.ErrorCode = providers.ChargeFailed, "CARD_DECLINED"
	case ScenarioPending:
		p.Status = providers.ChargePending
	default:
		p.Status = providers.ChargePaid
	}
	f.payments[p.Ref] = p
	f.byRef[reference] = p.Ref
	return providers.ChargeResult{Ref: p.Ref, Status: p.Status, ErrorCode: p.ErrorCode}, nil
}

// Resolve fija el desenlace de un cobro pendiente (lo que haría el banco) y
// devuelve el webhook firmado que el proveedor enviaría.
func (f *FakePaymentProvider) Resolve(ref string, status providers.ChargeStatus, at time.Time) (http.Header, []byte, error) {
	f.mu.Lock()
	p, ok := f.payments[ref]
	if !ok {
		f.mu.Unlock()
		return nil, nil, fmt.Errorf("fake: pago %s desconocido", ref)
	}
	p.Status = status
	if status == providers.ChargeFailed {
		p.ErrorCode = "CARD_DECLINED"
	}
	f.payments[ref] = p
	f.mu.Unlock()
	h, body := f.SignedWebhook(p, at)
	return h, body, nil
}

// SetPayment sobrescribe el estado de un pago en el proveedor (conciliación con
// discrepancias inyectadas, QA-002).
func (f *FakePaymentProvider) SetPayment(p providers.Payment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payments[p.Ref] = p
	if p.Reference != "" {
		f.byRef[p.Reference] = p.Ref
	}
}

type webhookBody struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Payment providers.Payment `json:"payment"`
}

// SignedWebhook construye el webhook `payment.updated` firmado de un pago.
func (f *FakePaymentProvider) SignedWebhook(p providers.Payment, at time.Time) (http.Header, []byte) {
	body, _ := json.Marshal(webhookBody{ID: "evt_" + p.Ref + "_" + string(p.Status), Type: "payment.updated", Payment: p})
	ts := strconv.FormatInt(at.Unix(), 10)
	h := http.Header{}
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderSignature, f.sign(ts, body))
	return h, body
}

func (f *FakePaymentProvider) sign(ts string, body []byte) string {
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ParseWebhook implementa PaymentProvider: HMAC-SHA256 sobre "<ts>.<body>" y
// ventana de ±5 minutos.
func (f *FakePaymentProvider) ParseWebhook(headers http.Header, payload []byte, now time.Time) (providers.WebhookEvent, error) {
	ts := headers.Get(HeaderTimestamp)
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	if !hmac.Equal([]byte(f.sign(ts, payload)), []byte(headers.Get(HeaderSignature))) {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	sentAt := time.Unix(sec, 0).UTC()
	if d := now.Sub(sentAt); d > WebhookTolerance || d < -WebhookTolerance {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	var body webhookBody
	if err := json.Unmarshal(payload, &body); err != nil || body.ID == "" {
		return providers.WebhookEvent{}, fmt.Errorf("fake: cuerpo de webhook inválido")
	}
	if body.Type != "payment.updated" {
		return providers.WebhookEvent{}, providers.ErrIgnoredEvent
	}
	return providers.WebhookEvent{ID: body.ID, Type: body.Type, Payment: body.Payment, SentAt: sentAt}, nil
}

// Refund implementa PaymentProvider.
func (f *FakePaymentProvider) Refund(_ context.Context, req providers.RefundRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scenario == ScenarioFailure {
		return "", providers.ErrUnavailable
	}
	p, ok := f.payments[req.PaymentRef]
	if !ok || p.Status != providers.ChargePaid {
		return "", fmt.Errorf("fake: pago %s no reembolsable", req.PaymentRef)
	}
	f.seq++
	f.refunded[req.PaymentRef] += req.Amount
	return "ref_fake_" + strconv.Itoa(f.seq), nil
}

// GetPayment implementa PaymentProvider.
func (f *FakePaymentProvider) GetPayment(_ context.Context, ref string) (providers.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scenario == ScenarioFailure {
		return providers.Payment{}, providers.ErrUnavailable
	}
	p, ok := f.payments[ref]
	if !ok {
		// El fake vive en memoria: el api no ve los pagos creados en el worker.
		// Sin consulta posible, quien llama decide con el evento firmado.
		return providers.Payment{}, providers.ErrNotSupported
	}
	return p, nil
}
