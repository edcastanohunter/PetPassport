// Package providers define el puerto PaymentProvider (PP-F5-DOM-011, ARQ-002,
// ADR-022 §3) y el Router que elige adaptador por país de la cuenta. Los
// adaptadores (wompi, manual, fake) reciben DTOs y no conocen el dominio.
//
// PetPassport nunca recibe datos de tarjeta (SEC-001): el widget del proveedor
// tokeniza en su dominio y aquí solo circulan tokens, last4 y vencimiento.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ChargeStatus es el resultado normalizado de un cobro en el proveedor.
type ChargeStatus string

// Resultados de cobro.
const (
	ChargePaid    ChargeStatus = "paid"
	ChargePending ChargeStatus = "pending" // el desenlace llega por webhook o consulta
	ChargeFailed  ChargeStatus = "failed"
	ChargeVoided  ChargeStatus = "voided" // anulado o reembolsado en el proveedor
)

// Errores del puerto.
var (
	// ErrUnavailable: el proveedor no responde o devuelve 5xx. No es un rechazo
	// del pago: el cargo sigue pendiente y se reintenta (RN-33).
	ErrUnavailable = errors.New("payments: proveedor no disponible")
	// ErrInvalidSignature: webhook con firma ausente, inválida o fuera de ventana.
	ErrInvalidSignature = errors.New("payments: firma de webhook inválida")
	// ErrIgnoredEvent: webhook auténtico de un tipo que no nos interesa.
	ErrIgnoredEvent = errors.New("payments: evento ignorado")
	// ErrPaymentMethodInvalid: el proveedor rechazó el token del método de pago.
	ErrPaymentMethodInvalid = errors.New("payments: método de pago inválido")
	// ErrNotSupported: el adaptador no ofrece la operación.
	ErrNotSupported = errors.New("payments: operación no soportada por el proveedor")
	// ErrUnknownProvider: el routing apunta a un adaptador no registrado.
	ErrUnknownProvider = errors.New("payments: proveedor no registrado")
)

// Customer identifica al titular ante el proveedor.
type Customer struct {
	AccountID uuid.UUID
	Email     string
	Name      string
}

// SetupRequest pide la configuración del widget/redirect de tokenización.
type SetupRequest struct {
	AccountID uuid.UUID
	Kind      string // card | pse
	Email     string
}

// SetupConfig son los datos que la SPA necesita para abrir el widget del
// proveedor. Solo contiene valores públicos.
type SetupConfig struct {
	Provider     string
	ClientConfig map[string]string
}

// AttachRequest convierte el token de un solo uso del widget en un método de
// pago reutilizable.
type AttachRequest struct {
	AccountID       uuid.UUID
	Email           string
	ProviderToken   string
	AcceptanceToken string // aceptación de términos del proveedor mostrada al usuario
	PersonalAuth    string // autorización de datos personales (CO)
}

// PaymentMethod es el método tokenizado. Token es la referencia reutilizable en
// el proveedor; nunca un PAN.
type PaymentMethod struct {
	Token     string
	Kind      string
	Last4     string
	Brand     string
	ExpiresAt *time.Time
}

// ChargeRequest es un intento de cobro. La clave de idempotencia es
// (ChargeID, Attempt): repetir el mismo intento no cobra dos veces.
type ChargeRequest struct {
	ChargeID uuid.UUID
	Attempt  int
	Amount   float64
	Currency string
	Email    string
	Token    string
}

// Reference es la referencia que viaja al proveedor y vuelve en sus webhooks.
func (r ChargeRequest) Reference() string { return Reference(r.ChargeID, r.Attempt) }

// Reference compone la referencia de un intento de cobro.
func Reference(chargeID uuid.UUID, attempt int) string {
	return fmt.Sprintf("%s-%d", chargeID, attempt)
}

// ParseReference recupera el cargo y el intento de una referencia.
func ParseReference(ref string) (uuid.UUID, int, error) {
	i := strings.LastIndexByte(ref, '-')
	if i != 36 {
		return uuid.Nil, 0, fmt.Errorf("payments: referencia %q no reconocida", ref)
	}
	id, err := uuid.Parse(ref[:i])
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("payments: referencia %q no reconocida", ref)
	}
	var attempt int
	if _, err := fmt.Sscanf(ref[i+1:], "%d", &attempt); err != nil || attempt < 1 {
		return uuid.Nil, 0, fmt.Errorf("payments: referencia %q no reconocida", ref)
	}
	return id, attempt, nil
}

// ChargeResult es la respuesta síncrona del proveedor.
type ChargeResult struct {
	Ref       string // id de la transacción en el proveedor; vacío si no se creó
	Status    ChargeStatus
	ErrorCode string
}

// RefundRequest pide devolver Amount de un pago confirmado.
type RefundRequest struct {
	RefundID   uuid.UUID // clave de idempotencia del reembolso
	ChargeID   uuid.UUID
	PaymentRef string
	Amount     float64
	Full       bool // Amount cubre todo lo que queda por devolver del pago
	Reason     string
}

// Payment es el estado de una transacción consultada al proveedor.
type Payment struct {
	Ref       string
	Reference string
	Status    ChargeStatus
	Amount    float64
	Currency  string
	ErrorCode string
}

// WebhookEvent es un evento de pago ya autenticado.
type WebhookEvent struct {
	// ID identifica el evento para la idempotencia (billing.provider_events).
	ID      string
	Type    string
	Payment Payment
	SentAt  time.Time
}

// Bank es una entidad financiera disponible para pagos por transferencia (PSE).
type Bank struct {
	Code string
	Name string
}

// Tipos de pagador de PSE.
const (
	PayerPerson  = "person"
	PayerCompany = "company"
)

// StartPaymentRequest abre un pago que el pagador completa fuera de PetPassport
// (PSE: el banco). Igual que ChargeRequest, (ChargeID, Attempt) es la clave de
// idempotencia y viaja como referencia hasta el webhook.
type StartPaymentRequest struct {
	ChargeID        uuid.UUID
	Attempt         int
	Amount          float64
	Currency        string
	Email           string
	BankCode        string
	PayerType       string // person | company
	LegalIDType     string // CC | NIT | CE | PP | TI
	LegalID         string
	FullName        string
	Phone           string
	Description     string
	AcceptanceToken string
	PersonalAuth    string
	ReturnURL       string // a donde vuelve el pagador desde el banco
}

// Reference es la referencia que viaja al proveedor y vuelve en sus webhooks.
func (r StartPaymentRequest) Reference() string { return Reference(r.ChargeID, r.Attempt) }

// StartedPayment es el pago abierto: RedirectURL lleva al pagador a su banco.
// El desenlace llega por webhook o consulta, como un cobro pendiente.
type StartedPayment struct {
	Ref         string
	Status      ChargeStatus
	RedirectURL string
	ErrorCode   string
}

// PayerInitiated lo implementan los adaptadores con pagos de un solo uso que el
// pagador completa en otro sitio (PSE en CO). No hay token reutilizable: cada
// cargo se paga con su propio redirect (PP-F5-PRD-004, 03-arquitectura §2).
type PayerInitiated interface {
	Banks(ctx context.Context) ([]Bank, error)
	StartPayment(ctx context.Context, req StartPaymentRequest) (StartedPayment, error)
}

// PaymentProvider es el puerto de la pasarela de pagos.
type PaymentProvider interface {
	Name() string
	CreateCustomer(ctx context.Context, c Customer) (customerID string, err error)
	SetupPaymentMethod(ctx context.Context, req SetupRequest) (SetupConfig, error)
	AttachPaymentMethod(ctx context.Context, req AttachRequest) (PaymentMethod, error)
	Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error)
	Refund(ctx context.Context, req RefundRequest) (refundRef string, err error)
	// ParseWebhook verifica la firma y la ventana temporal antes de interpretar
	// el cuerpo (SEC-002). now permite probar la ventana con reloj simulado.
	ParseWebhook(headers http.Header, payload []byte, now time.Time) (WebhookEvent, error)
	GetPayment(ctx context.Context, ref string) (Payment, error)
}
