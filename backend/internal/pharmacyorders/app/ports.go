package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// UnitOfWork asegura atomicidad transaccional entre EventStore, proyecciones y outbox (ADR-002).
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner ejecuta proyecciones síncronas de eventos generados en el comando (ARQ-014).
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// QuoteRequestRepo define la persistencia del agregado QuoteRequest (DOM-008).
type QuoteRequestRepo interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.QuoteRequest, error)
	Save(ctx context.Context, qr *domain.QuoteRequest) ([]eventstore.StoredEvent, error)
}

// OrderRepo define la persistencia del agregado Order (DOM-009).
type OrderRepo interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	Save(ctx context.Context, order *domain.Order) ([]eventstore.StoredEvent, error)
}

// PrescriptionDispenser registra dispensaciones contra el agregado Prescription (PP-F4-PRD-010).
type PrescriptionDispenser interface {
	RecordDispensation(ctx context.Context, prescriptionID uuid.UUID, orderID uuid.UUID, pharmacyOrgID uuid.UUID, lines []domain.DispensationLine) error
}

// PetReader lee datos básicos de la mascota requeridos para minimización hacia partners (ARQ-013).
type PetReader interface {
	GetAnimalSnapshot(ctx context.Context, petID uuid.UUID) (domain.AnimalSnapshot, error)
}

// UserReader lee información de contacto requerida para pedidos.
type UserReader interface {
	GetUserName(ctx context.Context, userID uuid.UUID) (string, error)
	GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

// SSEBroadcaster emite actualizaciones en tiempo real a clientes conectados (FE-005, API-005).
type SSEBroadcaster interface {
	PublishQuoteRequestEvent(id uuid.UUID, eventType string, payload any)
	PublishOrderEvent(id uuid.UUID, eventType string, payload any)
}

// RequestQuotesCmd define los parámetros de creación de una cotización por el tutor.
type RequestQuotesCmd struct {
	PrescriptionID  uuid.UUID
	GuardianID      uuid.UUID
	Items           []domain.RequestedItem
	Mode            domain.FulfillmentMode
	Area            domain.Area
	OrganizationIDs []uuid.UUID
	ExpiresInHours  int
}

// SelectQuoteCmd define la selección de una cotización y la colocación del pedido.
type SelectQuoteCmd struct {
	QuoteRequestID        uuid.UUID
	QuoteID               uuid.UUID
	GuardianID            uuid.UUID
	SubstitutionDecisions []domain.SubstitutionDecision
	DeliveryAddress       *domain.Address
	ContactPhone          string
}

// RecordQuoteCmd define la cotización enviada por una farmacia.
type RecordQuoteCmd struct {
	QuoteRequestID uuid.UUID
	PharmacyOrgID  uuid.UUID
	Lines          []domain.QuoteLine
	DeliveryFee    *float64
	ETA            *string
	Pickup         *domain.PickupRef
	ValidUntil     time.Time
	Notes          *string
}

// DeliverOrderCmd define la finalización de entrega con dispensación.
type DeliverOrderCmd struct {
	OrderID       uuid.UUID
	PharmacyOrgID uuid.UUID
	Lines         []domain.DispensationLine
}
