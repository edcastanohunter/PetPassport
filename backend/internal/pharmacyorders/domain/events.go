package domain

import (
	"time"

	"github.com/google/uuid"
)

// QuoteRequested se emite al abrir una nueva solicitud de cotización (PP-F4-DOM-010).
type QuoteRequested struct {
	QuoteRequestID uuid.UUID       `json:"quote_request_id"`
	PrescriptionID uuid.UUID       `json:"prescription_id"`
	PetID          uuid.UUID       `json:"pet_id"`
	GuardianID     uuid.UUID       `json:"guardian_id"`
	Country        string          `json:"country"`
	Items          []RequestedItem `json:"items"`
	Mode           FulfillmentMode `json:"mode"`
	Area           Area            `json:"area"`
	InvitedOrgIDs  []uuid.UUID     `json:"invited_org_ids"`
	ExpiresAt      time.Time       `json:"expires_at"`
}

func (QuoteRequested) EventName() string  { return "QuoteRequested" }
func (QuoteRequested) SchemaVersion() int { return 1 }

// QuoteInvitationSent documenta el despacho exitoso de la solicitud a una farmacia.
type QuoteInvitationSent struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Channel        string    `json:"channel"`
	SentAt         time.Time `json:"sent_at"`
}

func (QuoteInvitationSent) EventName() string  { return "QuoteInvitationSent" }
func (QuoteInvitationSent) SchemaVersion() int { return 1 }

// QuoteInvitationFailed se emite cuando falla el envío de la invitación.
type QuoteInvitationFailed struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	LastError      string    `json:"last_error"`
	At             time.Time `json:"at"`
}

func (QuoteInvitationFailed) EventName() string  { return "QuoteInvitationFailed" }
func (QuoteInvitationFailed) SchemaVersion() int { return 1 }

// QuoteReceived registra la cotización remitida por una farmacia.
type QuoteReceived struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Quote          Quote     `json:"quote"`
}

func (QuoteReceived) EventName() string  { return "QuoteReceived" }
func (QuoteReceived) SchemaVersion() int { return 1 }

// QuoteDeclined registra el rechazo explícito de una farmacia a cotizar.
type QuoteDeclined struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (QuoteDeclined) EventName() string  { return "QuoteDeclined" }
func (QuoteDeclined) SchemaVersion() int { return 1 }

// QuoteSelected se emite cuando el tutor selecciona la cotización ganadora.
type QuoteSelected struct {
	QuoteRequestID        uuid.UUID              `json:"quote_request_id"`
	QuoteID               uuid.UUID              `json:"quote_id"`
	OrganizationID        uuid.UUID              `json:"organization_id"`
	SubstitutionDecisions []SubstitutionDecision `json:"substitution_decisions"`
	At                    time.Time              `json:"at"`
}

func (QuoteSelected) EventName() string  { return "QuoteSelected" }
func (QuoteSelected) SchemaVersion() int { return 1 }

// QuoteRequestCancelled se emite cuando el tutor cancela la solicitud de cotización.
type QuoteRequestCancelled struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (QuoteRequestCancelled) EventName() string  { return "QuoteRequestCancelled" }
func (QuoteRequestCancelled) SchemaVersion() int { return 1 }

// QuoteRequestExpired se emite cuando el plazo de cotización ha vencido.
type QuoteRequestExpired struct {
	QuoteRequestID  uuid.UUID  `json:"quote_request_id"`
	SelectableUntil *time.Time `json:"selectable_until,omitempty"`
	At              time.Time  `json:"at"`
}

func (QuoteRequestExpired) EventName() string  { return "QuoteRequestExpired" }
func (QuoteRequestExpired) SchemaVersion() int { return 1 }

// QuoteRequestClosed se emite al consolidar el pedido o finalizar la saga.
type QuoteRequestClosed struct {
	QuoteRequestID uuid.UUID  `json:"quote_request_id"`
	OrderID        *uuid.UUID `json:"order_id,omitempty"`
	Reason         string     `json:"reason"`
	At             time.Time  `json:"at"`
}

func (QuoteRequestClosed) EventName() string  { return "QuoteRequestClosed" }
func (QuoteRequestClosed) SchemaVersion() int { return 1 }

// QuoteRequestReopened se emite si un pedido fue cancelado/rechazado por farmacia y la saga se reactiva.
type QuoteRequestReopened struct {
	QuoteRequestID uuid.UUID `json:"quote_request_id"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

func (QuoteRequestReopened) EventName() string  { return "QuoteRequestReopened" }
func (QuoteRequestReopened) SchemaVersion() int { return 1 }

// OrderPlaced se emite al materializarse el pedido en el aggregate Order (DOM-010).
type OrderPlaced struct {
	OrderID         uuid.UUID       `json:"order_id"`
	QuoteRequestID  uuid.UUID       `json:"quote_request_id"`
	QuoteID         uuid.UUID       `json:"quote_id"`
	PrescriptionID  uuid.UUID       `json:"prescription_id"`
	PetID           uuid.UUID       `json:"pet_id"`
	GuardianID      uuid.UUID       `json:"guardian_id"`
	PharmacyOrgID   uuid.UUID       `json:"pharmacy_org_id"`
	Mode            FulfillmentMode `json:"mode"`
	Lines           []OrderLine     `json:"lines"`
	Totals          OrderTotals     `json:"totals"`
	DeliveryAddress *Address        `json:"delivery_address,omitempty"`
	ContactPhone    string          `json:"contact_phone"`
	At              time.Time       `json:"at"`
}

func (OrderPlaced) EventName() string  { return "OrderPlaced" }
func (OrderPlaced) SchemaVersion() int { return 1 }

// OrderConfirmed documenta la confirmación de la farmacia para preparar el pedido.
type OrderConfirmed struct {
	OrderID    uuid.UUID `json:"order_id"`
	PartnerRef *string   `json:"partner_ref,omitempty"`
	ETA        *string   `json:"eta,omitempty"`
	At         time.Time `json:"at"`
}

func (OrderConfirmed) EventName() string  { return "OrderConfirmed" }
func (OrderConfirmed) SchemaVersion() int { return 1 }

// OrderRejected documenta el rechazo del pedido por la farmacia.
type OrderRejected struct {
	OrderID uuid.UUID `json:"order_id"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

func (OrderRejected) EventName() string  { return "OrderRejected" }
func (OrderRejected) SchemaVersion() int { return 1 }

// OrderPreparing indica que la farmacia inició la preparación de medicamentos.
type OrderPreparing struct {
	OrderID uuid.UUID `json:"order_id"`
	At      time.Time `json:"at"`
}

func (OrderPreparing) EventName() string  { return "OrderPreparing" }
func (OrderPreparing) SchemaVersion() int { return 1 }

// OrderReady indica que el pedido está listo para ser recogido en sede.
type OrderReady struct {
	OrderID uuid.UUID `json:"order_id"`
	At      time.Time `json:"at"`
}

func (OrderReady) EventName() string  { return "OrderReady" }
func (OrderReady) SchemaVersion() int { return 1 }

// OrderOutForDelivery indica que el pedido va en camino a la dirección del tutor.
type OrderOutForDelivery struct {
	OrderID uuid.UUID `json:"order_id"`
	ETA     *string   `json:"eta,omitempty"`
	At      time.Time `json:"at"`
}

func (OrderOutForDelivery) EventName() string  { return "OrderOutForDelivery" }
func (OrderOutForDelivery) SchemaVersion() int { return 1 }

// OrderDelivered se emite al entregar los productos e incluye el registro de dispensación.
type OrderDelivered struct {
	OrderID      uuid.UUID          `json:"order_id"`
	Dispensation DispensationRecord `json:"dispensation"`
	At           time.Time          `json:"at"`
}

func (OrderDelivered) EventName() string  { return "OrderDelivered" }
func (OrderDelivered) SchemaVersion() int { return 1 }

// OrderCancelled documenta la cancelación por parte del tutor o de la farmacia.
type OrderCancelled struct {
	OrderID uuid.UUID `json:"order_id"`
	By      string    `json:"by"` // "guardian" | "pharmacy"
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

func (OrderCancelled) EventName() string  { return "OrderCancelled" }
func (OrderCancelled) SchemaVersion() int { return 1 }
