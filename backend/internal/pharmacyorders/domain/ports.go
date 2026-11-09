package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PrescriptionItemSnapshot modela el estado de un ítem de receta leído por el módulo (DOM-013).
type PrescriptionItemSnapshot struct {
	ItemID              string     `json:"item_id"`
	CatalogID           *uuid.UUID `json:"catalog_id,omitempty"`
	Name                string     `json:"name"`
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

// PrescriptionSnapshot modela la vista mínima de una receta requerida para cotizar (DOM-013).
type PrescriptionSnapshot struct {
	ID             uuid.UUID                  `json:"id"`
	PetID          uuid.UUID                  `json:"pet_id"`
	GuardianID     uuid.UUID                  `json:"guardian_id"`
	Country        string                     `json:"country"`
	Status         string                     `json:"status"` // issued | confirmed
	Source         string                     `json:"source"` // issued | external
	ValidUntil     time.Time                  `json:"valid_until"`
	RefillsAllowed int                        `json:"refills_allowed"`
	RefillsUsed    int                        `json:"refills_used"`
	Items          []PrescriptionItemSnapshot `json:"items"`
}

// PrescriptionReader consulta recetas vigentes en read models (DOM-013).
type PrescriptionReader interface {
	GetPrescription(ctx context.Context, id uuid.UUID) (*PrescriptionSnapshot, error)
}

// CatalogEquivalence comprueba equivalencia biofarmacéutica y sustancias de control (DOM-013).
type CatalogEquivalence interface {
	IsControlled(ctx context.Context, catalogID uuid.UUID) (bool, error)
	AreEquivalent(ctx context.Context, a, b uuid.UUID) (bool, error)
}

// PartnerCapabilities describe las capacidades operativas configuradas para una farmacia (ARQ-003).
type PartnerCapabilities struct {
	Pickup        bool `json:"pickup"`
	Delivery      bool `json:"delivery"`
	Substitutions bool `json:"substitutions"`
}

// QuoteRequestPayload es la carga minimizada enviada al partner para cotización (ARQ-013, SEC-005).
type QuoteRequestPayload struct {
	QuoteRequestID  uuid.UUID       `json:"quote_request_id"`
	PrescriptionID  uuid.UUID       `json:"prescription_id"`
	PrescriptionURL *string         `json:"prescription_url,omitempty"` // URL de verificación para issued
	Source          string          `json:"source"`                     // issued | external
	Items           []RequestedItem `json:"items"`
	Animal          AnimalSnapshot  `json:"animal"`
	Mode            FulfillmentMode `json:"mode"`
	Area            Area            `json:"area"`
	ExpiresAt       time.Time       `json:"expires_at"`
}

// OrderPayload es la carga minimizada enviada al partner tras adjudicar el pedido (ARQ-013, SEC-005).
type OrderPayload struct {
	OrderID         uuid.UUID       `json:"order_id"`
	QuoteRequestID  uuid.UUID       `json:"quote_request_id"`
	QuoteID         uuid.UUID       `json:"quote_id"`
	PrescriptionID  uuid.UUID       `json:"prescription_id"`
	PrescriptionURL *string         `json:"prescription_url,omitempty"`
	Source          string          `json:"source"`
	GuardianName    string          `json:"guardian_name"`
	ContactPhone    string          `json:"contact_phone"`
	DeliveryAddress *Address        `json:"delivery_address,omitempty"`
	Mode            FulfillmentMode `json:"mode"`
	Lines           []OrderLine     `json:"lines"`
	Totals          OrderTotals     `json:"totals"`
	Notes           *string         `json:"notes,omitempty"`
	PlacedAt        time.Time       `json:"placed_at"`
}

// SendResult representa el resultado del despacho de una solicitud o pedido a un partner.
type SendResult struct {
	Channel string
	SentAt  time.Time
}

// PartnerGateway abstrae la comunicación con farmacias en niveles 1 (portal), 2 (API) y 3 (adaptador) (ARQ-003).
type PartnerGateway interface {
	SendQuoteRequest(ctx context.Context, orgID uuid.UUID, req QuoteRequestPayload) (SendResult, error)
	NotifyOrder(ctx context.Context, orgID uuid.UUID, order OrderPayload) (SendResult, error)
	CancelOrder(ctx context.Context, orgID uuid.UUID, orderID uuid.UUID, reason string) error
	Capabilities(ctx context.Context, orgID uuid.UUID) (PartnerCapabilities, error)
}

// EligiblePharmacy modela una farmacia calificada para participar en cotizaciones (PRD-021).
type EligiblePharmacy struct {
	OrganizationID uuid.UUID           `json:"organization_id"`
	DisplayName    string              `json:"display_name"`
	City           string              `json:"city"`
	DistanceKm     *float64            `json:"distance_km,omitempty"`
	Modes          []FulfillmentMode   `json:"modes"`
	Capabilities   PartnerCapabilities `json:"capabilities"`
	AvgResponseMin *float64            `json:"avg_response_min,omitempty"`
	QuoteRate      *float64            `json:"quote_rate,omitempty"`
	Rating         *float64            `json:"rating,omitempty"`
	Linked         bool                `json:"linked"`
}

// PartnerDirectory consulta farmacias elegibles para una zona geográfica y modalidad (DOM-013, PRD-021).
type PartnerDirectory interface {
	EligiblePharmacies(ctx context.Context, country string, area Area, mode FulfillmentMode) ([]EligiblePharmacy, error)
	IsEligible(ctx context.Context, orgID uuid.UUID, area Area, mode FulfillmentMode) (bool, error)
}

// QuoteRequestQuotaChecker verifica límites de solicitudes abiertas por receta y por día (DOM-008, Q5).
type QuoteRequestQuotaChecker interface {
	CheckQuotas(ctx context.Context, guardianID uuid.UUID, prescriptionID uuid.UUID, now time.Time) error
}
