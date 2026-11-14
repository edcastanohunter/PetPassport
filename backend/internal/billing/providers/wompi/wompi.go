// Package wompi es el adaptador PaymentProvider de Wompi (Colombia): tarjetas y
// PSE (PP-F5-ARQ-002, ADR-022 §3, SEC-001, SEC-002).
//
// PetPassport nunca ve datos de tarjeta: el widget de Wompi tokeniza en su
// dominio y aquí solo llega el token de un solo uso, que se cambia por una
// fuente de pago reutilizable (payment_source).
//
// El contrato sigue la documentación pública de Wompi (docs.wompi.co). Los
// puntos que la documentación deja abiertos están marcados con "SIN VERIFICAR"
// y los cubren las pruebas de contrato contra sandbox (QA-002), que solo corren
// con credenciales.
package wompi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// Name es el nombre del adaptador en billing.provider_routing.
const Name = "wompi"

// URLs base de la API.
const (
	SandboxURL    = "https://sandbox.wompi.co/v1"
	ProductionURL = "https://production.wompi.co/v1"
	widgetURL     = "https://checkout.wompi.co/widget.js"
)

// WebhookTolerance es la antigüedad máxima aceptada del timestamp de un evento.
// Wompi reintenta un evento hasta 24 h después, así que la ventana es amplia;
// la repetición la impide la idempotencia por evento, no la ventana.
const WebhookTolerance = 48 * time.Hour

const maxBody = 1 << 20

// Config son las credenciales del comercio. Las llaves privadas y los secretos
// vienen del entorno; nunca se registran en logs ni salen hacia la SPA.
type Config struct {
	BaseURL         string
	PublicKey       string
	PrivateKey      string
	IntegritySecret string
	EventsSecret    string
}

// Valid indica si la configuración está completa.
func (c Config) Valid() bool {
	return c.BaseURL != "" && c.PublicKey != "" && c.PrivateKey != "" && c.IntegritySecret != "" && c.EventsSecret != ""
}

// Doer es el cliente HTTP (el de egress, con allowlist).
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider implementa providers.PaymentProvider.
type Provider struct {
	cfg  Config
	http Doer
	wait func(ctx context.Context, d time.Duration) error // pausa entre consultas (pruebas)
}

var _ providers.PaymentProvider = (*Provider)(nil)

// New construye el adaptador.
func New(cfg Config, client Doer) *Provider {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Provider{cfg: cfg, http: client}
}

// Name implementa PaymentProvider.
func (*Provider) Name() string { return Name }

// apiError es un error con cuerpo de la API de Wompi.
type apiError struct {
	Status   int
	Type     string
	Messages map[string]json.RawMessage
}

func (e *apiError) Error() string {
	return fmt.Sprintf("wompi: HTTP %d %s", e.Status, e.Type)
}

func (p *Provider) call(ctx context.Context, method, path string, auth bool, headers map[string]string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.cfg.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+p.cfg.PrivateKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		// Timeout o red: no se sabe si la operación llegó. No es un rechazo.
		return fmt.Errorf("%w: %s %s", providers.ErrUnavailable, method, path)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%w: lectura de %s", providers.ErrUnavailable, path)
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d en %s", providers.ErrUnavailable, resp.StatusCode, path)
	case resp.StatusCode >= 400:
		ae := &apiError{Status: resp.StatusCode}
		var env struct {
			Error struct {
				Type     string                     `json:"type"`
				Messages map[string]json.RawMessage `json:"messages"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil {
			ae.Type, ae.Messages = env.Error.Type, env.Error.Messages
		}
		return ae
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("wompi: respuesta ilegible de %s: %w", path, err)
	}
	return nil
}

// CreateCustomer implementa PaymentProvider: Wompi no tiene objeto cliente; el
// titular se identifica por el correo de cada fuente de pago.
func (*Provider) CreateCustomer(_ context.Context, c providers.Customer) (string, error) {
	return c.AccountID.String(), nil
}

type merchantResponse struct {
	Data struct {
		PresignedAcceptance struct {
			AcceptanceToken string `json:"acceptance_token"`
			Permalink       string `json:"permalink"`
		} `json:"presigned_acceptance"`
		PresignedPersonalDataAuth struct {
			AcceptanceToken string `json:"acceptance_token"`
			Permalink       string `json:"permalink"`
		} `json:"presigned_personal_data_auth"`
	} `json:"data"`
}

// SetupPaymentMethod devuelve lo que la SPA necesita para abrir el widget en
// modo tokenización: la llave pública y los tokens de aceptación que el usuario
// debe aprobar (con sus enlaces a los términos). Solo valores públicos.
func (p *Provider) SetupPaymentMethod(ctx context.Context, req providers.SetupRequest) (providers.SetupConfig, error) {
	var m merchantResponse
	// SIN VERIFICAR: la documentación describe /merchants/info con la llave
	// pública en la cabecera x-merchant-public-key.
	if err := p.call(ctx, http.MethodGet, "/merchants/info", false,
		map[string]string{"x-merchant-public-key": p.cfg.PublicKey}, nil, &m); err != nil {
		return providers.SetupConfig{}, err
	}
	if m.Data.PresignedAcceptance.AcceptanceToken == "" {
		return providers.SetupConfig{}, errors.New("wompi: el comercio no devolvió token de aceptación")
	}
	if req.Kind == "pse" {
		return providers.SetupConfig{Provider: Name, ClientConfig: pseClientConfig(p.cfg.PublicKey, m)}, nil
	}
	return providers.SetupConfig{Provider: Name, ClientConfig: map[string]string{
		"widget_url":               widgetURL,
		"public_key":               p.cfg.PublicKey,
		"acceptance_token":         m.Data.PresignedAcceptance.AcceptanceToken,
		"acceptance_permalink":     m.Data.PresignedAcceptance.Permalink,
		"personal_auth_token":      m.Data.PresignedPersonalDataAuth.AcceptanceToken,
		"personal_auth_permalink":  m.Data.PresignedPersonalDataAuth.Permalink,
		"data_widget_operation":    "tokenize",
		"payment_method_kind_hint": "card",
	}}, nil
}

type paymentSourceResponse struct {
	Data struct {
		ID         json.Number `json:"id"`
		Status     string      `json:"status"`
		Type       string      `json:"type"`
		PublicData struct {
			LastFour string `json:"last_four"`
			Brand    string `json:"brand"`
			ExpMonth string `json:"exp_month"`
			ExpYear  string `json:"exp_year"`
		} `json:"public_data"`
	} `json:"data"`
}

// AttachPaymentMethod cambia el token de un solo uso por una fuente de pago.
func (p *Provider) AttachPaymentMethod(ctx context.Context, req providers.AttachRequest) (providers.PaymentMethod, error) {
	if req.ProviderToken == "" || req.AcceptanceToken == "" {
		return providers.PaymentMethod{}, providers.ErrPaymentMethodInvalid
	}
	in := map[string]any{
		"type": "CARD", "token": req.ProviderToken, "customer_email": req.Email,
		"acceptance_token": req.AcceptanceToken,
	}
	if req.PersonalAuth != "" {
		in["accept_personal_auth"] = req.PersonalAuth
	}
	var out paymentSourceResponse
	if err := p.call(ctx, http.MethodPost, "/payment_sources", true, nil, in, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && (ae.Status == http.StatusUnprocessableEntity || ae.Status == http.StatusNotFound) {
			return providers.PaymentMethod{}, providers.ErrPaymentMethodInvalid
		}
		return providers.PaymentMethod{}, err
	}
	if out.Data.ID.String() == "" || (out.Data.Status != "" && out.Data.Status != "AVAILABLE") {
		return providers.PaymentMethod{}, providers.ErrPaymentMethodInvalid
	}
	pm := providers.PaymentMethod{
		Token: out.Data.ID.String(), Kind: "card",
		Last4: out.Data.PublicData.LastFour, Brand: out.Data.PublicData.Brand,
	}
	// SIN VERIFICAR: la documentación solo garantiza last_four/exp en la respuesta
	// de tokenización; si la fuente de pago no los trae quedan vacíos.
	if m, errM := strconv.Atoi(out.Data.PublicData.ExpMonth); errM == nil && m >= 1 && m <= 12 {
		if y, errY := strconv.Atoi(out.Data.PublicData.ExpYear); errY == nil {
			if y < 100 {
				y += 2000
			}
			exp := time.Date(y, time.Month(m)+1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)
			pm.ExpiresAt = &exp
		}
	}
	return pm, nil
}

// cents convierte el importe a centavos enteros.
func cents(amount float64) int64 { return int64(math.Round(amount * 100)) }

// IntegritySignature es la firma de integridad de una transacción:
// SHA-256(referencia + importe en centavos + moneda + secreto de integridad).
func IntegritySignature(reference string, amountInCents int64, currency, secret string) string {
	sum := sha256.Sum256([]byte(reference + strconv.FormatInt(amountInCents, 10) + currency + secret))
	return hex.EncodeToString(sum[:])
}

type transaction struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Reference     string `json:"reference"`
	AmountInCents int64  `json:"amount_in_cents"`
	Currency      string `json:"currency"`
	PaymentMethod struct {
		Extra struct {
			AsyncPaymentURL string `json:"async_payment_url"`
		} `json:"extra"`
	} `json:"payment_method"`
}

type transactionResponse struct {
	Data transaction `json:"data"`
}

func status(s string) (providers.ChargeStatus, error) {
	switch s {
	case "APPROVED":
		return providers.ChargePaid, nil
	case "PENDING":
		return providers.ChargePending, nil
	case "DECLINED", "ERROR":
		return providers.ChargeFailed, nil
	case "VOIDED":
		return providers.ChargeVoided, nil
	}
	return "", fmt.Errorf("wompi: estado de transacción desconocido %q", s)
}

func payment(t transaction) (providers.Payment, error) {
	st, err := status(t.Status)
	if err != nil {
		return providers.Payment{}, err
	}
	p := providers.Payment{
		Ref: t.ID, Reference: t.Reference, Status: st,
		Amount: float64(t.AmountInCents) / 100, Currency: t.Currency,
	}
	if st == providers.ChargeFailed || st == providers.ChargeVoided {
		p.ErrorCode = t.Status
	}
	return p, nil
}

// Charge crea la transacción recurrente. La referencia (cargo + intento) es
// única en Wompi: repetir el intento no cobra dos veces.
func (p *Provider) Charge(ctx context.Context, req providers.ChargeRequest) (providers.ChargeResult, error) {
	sourceID, err := strconv.ParseInt(req.Token, 10, 64)
	if err != nil {
		return providers.ChargeResult{Status: providers.ChargeFailed, ErrorCode: "INVALID_PAYMENT_SOURCE"}, nil
	}
	amount := cents(req.Amount)
	reference := req.Reference()
	in := map[string]any{
		"amount_in_cents":   amount,
		"currency":          req.Currency,
		"customer_email":    req.Email,
		"reference":         reference,
		"payment_source_id": sourceID,
		"recurrent":         true,
		"payment_method":    map[string]any{"installments": 1},
		"signature":         IntegritySignature(reference, amount, req.Currency, p.cfg.IntegritySecret),
	}
	var out transactionResponse
	if err := p.call(ctx, http.MethodPost, "/transactions", true, nil, in, &out); err != nil {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusUnprocessableEntity {
			return providers.ChargeResult{}, err
		}
		if _, dup := ae.Messages["reference"]; dup {
			// Referencia ya usada: el intento anterior llegó a Wompi aunque no
			// vimos su respuesta. Su desenlace lo traerá el webhook o la conciliación.
			// SIN VERIFICAR: forma exacta del 422 por referencia duplicada.
			return providers.ChargeResult{Status: providers.ChargePending}, nil
		}
		return providers.ChargeResult{Status: providers.ChargeFailed, ErrorCode: "INVALID_REQUEST"}, nil
	}
	st, err := status(out.Data.Status)
	if err != nil {
		return providers.ChargeResult{}, err
	}
	res := providers.ChargeResult{Ref: out.Data.ID, Status: st}
	if st == providers.ChargeFailed || st == providers.ChargeVoided {
		res.ErrorCode = out.Data.Status
	}
	return res, nil
}

// GetPayment consulta una transacción por su id.
func (p *Provider) GetPayment(ctx context.Context, ref string) (providers.Payment, error) {
	if strings.ContainsAny(ref, "/?#") {
		return providers.Payment{}, fmt.Errorf("wompi: id de transacción inválido")
	}
	var out transactionResponse
	if err := p.call(ctx, http.MethodGet, "/transactions/"+ref, false, nil, nil, &out); err != nil {
		return providers.Payment{}, err
	}
	return payment(out.Data)
}

// Refund anula la transacción (reembolso total) o pide un reembolso parcial.
func (p *Provider) Refund(ctx context.Context, req providers.RefundRequest) (string, error) {
	if strings.ContainsAny(req.PaymentRef, "/?#") {
		return "", fmt.Errorf("wompi: id de transacción inválido")
	}
	if req.Full {
		if err := p.call(ctx, http.MethodPost, "/transactions/"+req.PaymentRef+"/void", true, nil, map[string]any{}, nil); err != nil {
			return "", err
		}
		return "void:" + req.PaymentRef, nil
	}
	// SIN VERIFICAR: /refunds solo está documentado para sandbox; en producción
	// los reembolsos parciales pueden requerir gestión con Wompi.
	var out struct {
		Data struct {
			ID json.Number `json:"id"`
		} `json:"data"`
	}
	if err := p.call(ctx, http.MethodPost, "/refunds", true, nil, map[string]any{
		"transaction_id": req.PaymentRef, "amount_in_cents": cents(req.Amount), "reason": req.Reason,
	}, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return "", fmt.Errorf("%w: reembolso parcial", providers.ErrNotSupported)
		}
		return "", err
	}
	if id := out.Data.ID.String(); id != "" {
		return "refund:" + id, nil
	}
	return "refund:" + req.RefundID.String(), nil
}

type event struct {
	Event string                     `json:"event"`
	Data  map[string]json.RawMessage `json:"data"`
	// Timestamp llega como entero (segundos).
	Timestamp json.Number `json:"timestamp"`
	Signature struct {
		Properties []string `json:"properties"`
		Checksum   string   `json:"checksum"`
	} `json:"signature"`
}

// property resuelve "transaction.id" dentro de data y devuelve su valor tal como
// viaja en el JSON (sin comillas en cadenas, números sin reformatear).
func property(data map[string]json.RawMessage, path string) (string, bool) {
	parts := strings.Split(path, ".")
	cur := data
	for i, part := range parts {
		raw, ok := cur[part]
		if !ok {
			return "", false
		}
		if i == len(parts)-1 {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				return s, true
			}
			return strings.TrimSpace(string(raw)), true
		}
		next := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &next) != nil {
			return "", false
		}
		cur = next
	}
	return "", false
}

// EventChecksum calcula el checksum de un evento: SHA-256 de los valores de las
// propiedades firmadas, en orden, seguidos del timestamp y del secreto de eventos.
func EventChecksum(values []string, timestamp, secret string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "") + timestamp + secret))
	return hex.EncodeToString(sum[:])
}

func sameChecksum(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(a)), []byte(strings.ToLower(b))) == 1
}

// ParseWebhook verifica el checksum y la antigüedad del evento antes de
// interpretarlo (SEC-002). El evento debe firmar al menos id, estado e importe
// de la transacción: un checksum sobre menos campos no autentica lo que usamos.
func (p *Provider) ParseWebhook(headers http.Header, payload []byte, now time.Time) (providers.WebhookEvent, error) {
	if len(payload) > maxBody {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	var ev event
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&ev); err != nil || ev.Signature.Checksum == "" || len(ev.Signature.Properties) == 0 {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	values := make([]string, 0, len(ev.Signature.Properties))
	signed := map[string]bool{}
	for _, prop := range ev.Signature.Properties {
		v, ok := property(ev.Data, prop)
		if !ok {
			return providers.WebhookEvent{}, providers.ErrInvalidSignature
		}
		values = append(values, v)
		signed[prop] = true
	}
	want := EventChecksum(values, ev.Timestamp.String(), p.cfg.EventsSecret)
	if !sameChecksum(want, ev.Signature.Checksum) {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	if h := headers.Get("X-Event-Checksum"); h != "" && !sameChecksum(want, h) {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	sec, err := ev.Timestamp.Int64()
	if err != nil {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	sentAt := time.Unix(sec, 0).UTC()
	if d := now.Sub(sentAt); d > WebhookTolerance || d < -5*time.Minute {
		return providers.WebhookEvent{}, providers.ErrInvalidSignature
	}
	if ev.Event != "transaction.updated" {
		return providers.WebhookEvent{}, providers.ErrIgnoredEvent
	}
	for _, required := range []string{"transaction.id", "transaction.status", "transaction.amount_in_cents"} {
		if !signed[required] {
			return providers.WebhookEvent{}, providers.ErrInvalidSignature
		}
	}
	var tx transaction
	if err := json.Unmarshal(ev.Data["transaction"], &tx); err != nil || tx.ID == "" {
		return providers.WebhookEvent{}, fmt.Errorf("wompi: evento sin transacción")
	}
	pay, err := payment(tx)
	if err != nil {
		return providers.WebhookEvent{}, err
	}
	// Wompi no envía id de evento: lo identifica la transacción y su estado.
	return providers.WebhookEvent{ID: tx.ID + ":" + tx.Status, Type: ev.Event, Payment: pay, SentAt: sentAt}, nil
}
