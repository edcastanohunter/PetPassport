package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// Puertos y vistas del carnet de vacunación (PP-F2-DOM-006/009, API-004/005).

// Nombre estable de la proyección asíncrona de próximas dosis (ARQ-012).
const ProjectionVaccinationDue = "clinical.vaccination_due"

// DefaultCountry es el país de los protocolos cuando no se indica otro (F2
// solo siembra CO).
const DefaultCountry = "CO"

// CardRepository persiste y rehidrata el carnet (stream vax-<PetID>).
type CardRepository interface {
	// Load rehidrata por replay; carnet vacío (versión 0) si no existe.
	Load(ctx context.Context, petID uuid.UUID) (*domain.VaccinationCard, error)
	Save(ctx context.Context, c *domain.VaccinationCard, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
}

// VaccineRef es una vacuna del catálogo ref.vaccines.
type VaccineRef struct {
	ID       uuid.UUID
	Name     string
	Species  []string
	Diseases []string
	Type     string
	Routes   []string
	Active   bool
}

// ProtocolRef es un protocolo de ref.vaccination_protocols.
type ProtocolRef struct {
	ID                    uuid.UUID
	Country               string
	Species               string
	VaccineID             uuid.UUID
	InitialSeries         []domain.SeriesDose
	BoosterIntervalMonths *int
	LegalValidityMonths   *int
	Notes                 string
	Active                bool
}

// Catalog es el acceso a los catálogos de referencia (DOM-009).
type Catalog interface {
	Vaccine(ctx context.Context, id uuid.UUID) (VaccineRef, error)
	Vaccines(ctx context.Context, species, q string) ([]VaccineRef, error)
	// Protocol devuelve el protocolo activo; nil si no hay.
	Protocol(ctx context.Context, country, species string, vaccineID uuid.UUID) (*ProtocolRef, error)
	Protocols(ctx context.Context, country, species string) ([]ProtocolRef, error)
}

// CardEntryView es una entrada del carnet proyectada (card_entries).
type CardEntryView struct {
	ID             uuid.UUID
	Kind           string
	CatalogID      *uuid.UUID
	ProductName    string
	AdministeredAt string
	Lot            *string
	Manufacturer   *string
	ExpiryDate     *string
	Route          *string
	Site           *string
	DoseLabel      *string
	WeightKg       *float64
	Source         string
	Verification   string
	RecordedByOrg  *uuid.UUID
	RecordedByUser *uuid.UUID
	DeclaredBy     *uuid.UUID
	EncounterID    *uuid.UUID
	NextDueDate    *string
	NextDueBasis   *string
	Status         string
	VoidReason     *string
	HasEvidence    bool
	EvidenceKey    *string
	RecordedAt     *time.Time
	CreatedAt      time.Time
}

// NextDueItem es una próxima dosis pendiente (vaccination_due).
type NextDueItem struct {
	EntryID     uuid.UUID
	Kind        string
	ProductName string
	DueDate     string
	Basis       string
	Status      string // upcoming | overdue | satisfied | cancelled
}

// CardView es el carnet completo de una mascota.
type CardView struct {
	PetID   uuid.UUID
	Species string
	Entries []CardEntryView
	NextDue []NextDueItem
}

// CardQueries es el lado de lectura del carnet.
type CardQueries interface {
	Card(ctx context.Context, petID uuid.UUID, includeVoided bool) (CardView, error)
	Entry(ctx context.Context, petID, entryID uuid.UUID) (CardEntryView, error)
}

// ShareView es un enlace compartido sin su token (API-006).
type ShareView struct {
	ID          uuid.UUID
	PetID       uuid.UUID
	Scope       string
	HasPIN      bool
	ExpiresAt   time.Time
	CreatedAt   time.Time
	RevokedAt   *time.Time
	AccessCount int
	LastAccess  *time.Time
}

// ShareRecord es la fila completa para validar un acceso público.
type ShareRecord struct {
	ShareView
	TokenHash   []byte
	CreatedBy   uuid.UUID
	PinHash     string
	FailedPins  int
	LockedUntil *time.Time
}

// Shares es el almacén de enlaces compartidos (record_shares, ARQ-013).
type Shares interface {
	// Create inserta si la mascota tiene < MaxActiveShares activos (SHARE_LIMIT).
	Create(ctx context.Context, s ShareRecord) error
	List(ctx context.Context, petID uuid.UUID) ([]ShareView, error)
	Revoke(ctx context.Context, petID, id uuid.UUID, at time.Time) error
	ByTokenHash(ctx context.Context, hash []byte) (ShareRecord, error)
	// RecordAccess incrementa el contador y la fecha de último acceso.
	RecordAccess(ctx context.Context, id uuid.UUID, at time.Time) error
	// PinFailed suma un intento fallido y bloquea al llegar al límite; PinOK lo reinicia.
	PinFailed(ctx context.Context, id uuid.UUID, lockUntil time.Time, max int) (locked bool, err error)
	PinOK(ctx context.Context, id uuid.UUID) error
}

// Límites de los enlaces compartidos (SEC-002).
const (
	MaxActiveShares = 10
	MinShareDays    = 1
	MaxShareDays    = 90
	MaxPinAttempts  = 5
	PinLockDuration = time.Hour
)
