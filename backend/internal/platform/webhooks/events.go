package webhooks

import (
	"time"

	"github.com/google/uuid"
)

// Tipos de eventos de webhooks salientes (PP-F4-ARQ-004, PTN-004).
const (
	EventQuoteRequestCreated   = "quote_request.created"
	EventQuoteRequestCancelled = "quote_request.cancelled"
	EventQuoteRequestExpired   = "quote_request.expired"
	EventOrderPlaced           = "order.placed"
	EventOrderCancelled        = "order.cancelled"
	EventPrescriptionRevoked   = "prescription.revoked"
	EventPing                  = "ping"
)

// SupportedEvents lista todos los eventos que un partner puede suscribir.
var SupportedEvents = []string{
	EventQuoteRequestCreated,
	EventQuoteRequestCancelled,
	EventQuoteRequestExpired,
	EventOrderPlaced,
	EventOrderCancelled,
	EventPrescriptionRevoked,
	EventPing,
}

// QuoteRequestSummary resume los ítems y ubicación en el patrón thin webhook (PTN-004).
type QuoteRequestSummary struct {
	ItemsCount int    `json:"items_count"`
	Mode       string `json:"mode"`
	Area       string `json:"area"`
}

// QuoteRequestCreatedPayload es el payload del evento quote_request.created (patrón thin webhook).
type QuoteRequestCreatedPayload struct {
	QuoteRequestID uuid.UUID           `json:"quote_request_id"`
	ExpiresAt      time.Time           `json:"expires_at"`
	Summary        QuoteRequestSummary `json:"summary"`
}

// QuoteRequestCancelledPayload es el payload de cancelación de una solicitud.
type QuoteRequestCancelledPayload struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	Reason         string    `json:"reason,omitempty"`
}

// QuoteRequestExpiredPayload es el payload cuando una solicitud expira sin adjudicar.
type QuoteRequestExpiredPayload struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
}

// OrderPlacedPayload es el payload cuando el tutor adjudica la cotización (order.placed).
type OrderPlacedPayload struct {
	OrderID uuid.UUID `json:"order_id"`
}

// OrderCancelledPayload es el payload cuando un pedido es cancelado.
type OrderCancelledPayload struct {
	OrderID uuid.UUID `json:"order_id"`
	Reason  string    `json:"reason,omitempty"`
}

// PrescriptionRevokedPayload es el payload cuando una receta es revocada y afecta a pedidos en curso.
type PrescriptionRevokedPayload struct {
	PrescriptionID   uuid.UUID   `json:"prescription_id"`
	AffectedOrderIDs []uuid.UUID `json:"affected_order_ids,omitempty"`
}

// PingPayload es el payload para pruebas manuales o de conectividad.
type PingPayload struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
