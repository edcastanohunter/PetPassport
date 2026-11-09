package domain

import (
	"time"

	"github.com/google/uuid"
)

// FulfillmentMode representa la modalidad de entrega de la cotización o pedido (ADR-020).
type FulfillmentMode string

const (
	ModePickup   FulfillmentMode = "pickup"
	ModeDelivery FulfillmentMode = "delivery"
)

// QuoteRequestStatus representa el estado global del agregado QuoteRequest (DOM-008).
type QuoteRequestStatus string

const (
	QuoteRequestStatusOpen     QuoteRequestStatus = "open"
	QuoteRequestStatusSelected QuoteRequestStatus = "selected"
	QuoteRequestStatusClosed   QuoteRequestStatus = "closed"
)

// InvitationStatus representa el estado de la invitación a una farmacia específica (DOM-008).
type InvitationStatus string

const (
	InvitationPending  InvitationStatus = "pending"
	InvitationSent     InvitationStatus = "sent"
	InvitationQuoted   InvitationStatus = "quoted"
	InvitationDeclined InvitationStatus = "declined"
	InvitationFailed   InvitationStatus = "failed"
	InvitationExpired  InvitationStatus = "expired"
)

// Availability representa si la farmacia ofrece el ítem completo, parcial o nulo (DOM-008).
type Availability string

const (
	AvailabilityFull    Availability = "full"
	AvailabilityPartial Availability = "partial"
	AvailabilityNone    Availability = "none"
)

// OrderStatus representa las etapas del pedido (DOM-009, ADR-020).
type OrderStatus string

const (
	OrderStatusPlaced         OrderStatus = "placed"
	OrderStatusConfirmed      OrderStatus = "confirmed"
	OrderStatusPreparing      OrderStatus = "preparing"
	OrderStatusReady          OrderStatus = "ready"
	OrderStatusOutForDelivery OrderStatus = "out_for_delivery"
	OrderStatusDelivered      OrderStatus = "delivered"
	OrderStatusCancelled      OrderStatus = "cancelled"
	OrderStatusRejected       OrderStatus = "rejected"
)

// RequestedItem representa un ítem de receta incluido en la solicitud de cotización (DOM-008).
type RequestedItem struct {
	ItemID              string     `json:"item_id"`
	CatalogID           *uuid.UUID `json:"catalog_id,omitempty"`
	NameSnapshot        string     `json:"name_snapshot"`
	Form                string     `json:"form,omitempty"`
	Concentration       string     `json:"concentration,omitempty"`
	FreeText            *string    `json:"free_text,omitempty"`
	Dose                string     `json:"dose"`
	Frequency           string     `json:"frequency"`
	Quantity            float64    `json:"quantity"`
	Unit                string     `json:"unit"`
	SubstitutionAllowed bool       `json:"substitution_allowed"`
	Controlled          bool       `json:"controlled"`
}

// Area representa la zona geográfica aproximada para la cotización (DOM-008, ARQ-013).
type Area struct {
	City string   `json:"city"`
	Zone *string  `json:"zone,omitempty"`
	Lat  *float64 `json:"lat,omitempty"`
	Lon  *float64 `json:"lon,omitempty"`
}

// Invitation representa la relación entre la solicitud de cotización y una farmacia invitada (DOM-008).
type Invitation struct {
	OrgID       uuid.UUID        `json:"organization_id"`
	Status      InvitationStatus `json:"status"`
	SentAt      *time.Time       `json:"sent_at,omitempty"`
	Channel     string           `json:"channel,omitempty"`
	Attempts    int              `json:"attempts"`
	LastError   *string          `json:"last_error,omitempty"`
	RespondedAt *time.Time       `json:"responded_at,omitempty"`
}

// QuoteSubstitution representa un producto equivalente propuesto por la farmacia (DOM-008, ADR-020).
type QuoteSubstitution struct {
	CatalogID uuid.UUID `json:"catalog_id"`
	Name      string    `json:"name"`
	UnitPrice float64   `json:"unit_price"`
	Reason    string    `json:"reason"`
}

// QuoteLine representa la oferta de la farmacia para un ítem solicitado (DOM-008).
type QuoteLine struct {
	ItemID       string             `json:"item_id"`
	UnitPrice    float64            `json:"unit_price"`
	Qty          float64            `json:"qty"`
	Available    Availability       `json:"available"`
	Substitution *QuoteSubstitution `json:"substitution,omitempty"`
}

// PickupRef contiene datos de punto de recogida en farmacia (DOM-008).
type PickupRef struct {
	Address string `json:"address"`
	Hours   string `json:"hours,omitempty"`
}

// Quote representa la oferta comercial de una farmacia dentro de la saga (DOM-008, ADR-020).
type Quote struct {
	ID           uuid.UUID    `json:"id"`
	OrgID        uuid.UUID    `json:"organization_id"`
	Lines        []QuoteLine  `json:"lines"`
	Subtotal     float64      `json:"subtotal"`
	DeliveryFee  *float64     `json:"delivery_fee,omitempty"`
	Total        float64      `json:"total"`
	Currency     string       `json:"currency"`
	ETA          *string      `json:"eta,omitempty"`
	Pickup       *PickupRef   `json:"pickup,omitempty"`
	ValidUntil   time.Time    `json:"valid_until"`
	Notes        *string      `json:"notes,omitempty"`
	Completeness Availability `json:"completeness"`
	ReceivedAt   time.Time    `json:"received_at"`
	Superseded   bool         `json:"superseded"`
}

// SubstitutionDecision representa la aceptación o rechazo del tutor para un ítem sustituido (DOM-008, PRD-011).
type SubstitutionDecision struct {
	ItemID string `json:"item_id"`
	Accept bool   `json:"accept"`
}

// Address representa una dirección física completa para despachos a domicilio (DOM-009, SEC-005).
type Address struct {
	Street         string  `json:"street"`
	City           string  `json:"city"`
	PostalCode     string  `json:"postal_code,omitempty"`
	Country        string  `json:"country"`
	AdditionalInfo *string `json:"additional_info,omitempty"`
}

// OrderLine representa un ítem adjudicado en el pedido final (DOM-009).
type OrderLine struct {
	ItemID          string     `json:"item_id"`
	CatalogID       *uuid.UUID `json:"catalog_id,omitempty"`
	FreeText        *string    `json:"free_text,omitempty"`
	Qty             float64    `json:"qty"`
	UnitPrice       float64    `json:"unit_price"`
	Substituted     *uuid.UUID `json:"substituted,omitempty"`
	SubstitutedName *string    `json:"substituted_name,omitempty"`
}

// OrderTotals contiene el desglose de importes calculado en el agregado (DOM-009, O8).
type OrderTotals struct {
	Subtotal    float64 `json:"subtotal"`
	DeliveryFee float64 `json:"delivery_fee"`
	Total       float64 `json:"total"`
	Currency    string  `json:"currency"`
}

// OrderTimelineEntry almacena las transiciones históricas de un pedido (DOM-009).
type OrderTimelineEntry struct {
	Status OrderStatus `json:"status"`
	At     time.Time   `json:"at"`
	Reason *string     `json:"reason,omitempty"`
}

// DispensationLine especifica la cantidad efectivamente entregada de un ítem (DOM-009, O5).
type DispensationLine struct {
	ItemID string  `json:"item_id"`
	Qty    float64 `json:"qty"`
}

// DispensationRecord documenta la dispensación en farmacia reflejada en receta (DOM-009, PP-F4-PRD-010).
type DispensationRecord struct {
	Lines []DispensationLine `json:"lines"`
	At    time.Time          `json:"at"`
}

// AnimalSnapshot contiene datos mínimos del animal requeridos por la farmacia (ARQ-013, SEC-005).
type AnimalSnapshot struct {
	Species   string   `json:"species"`
	WeightKg  *float64 `json:"weight_kg,omitempty"`
	AgeMonths *int     `json:"age_months,omitempty"`
}
