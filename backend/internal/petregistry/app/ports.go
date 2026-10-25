// Package app expone los casos de uso de Pet Registry (ARQ-003): cada comando
// ejecuta authz (en http) → idempotency (en http, sobre la clave del request)
// → uow.Do {load → handle → reserve → append → project(sync) → outbox}.
// Los puertos viven aquí; los adaptadores en infra.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Repository persiste y rehidrata el agregado Pet desde el event store.
// La transacción viaja en el contexto (UnitOfWork); el adaptador la extrae.
type Repository interface {
	// Load rehidrata la mascota; (*domain.Pet zero-value, nil) si el stream no existe.
	Load(ctx context.Context, id uuid.UUID) (*PetAggregate, error)
	// Save hace Append de los eventos no confirmados con la versión esperada y
	// devuelve los eventos persistidos (para el runner de proyecciones).
	Save(ctx context.Context, p *PetAggregate, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
}

// TxRunner ejecuta una función dentro de la transacción del UoW (db.UnitOfWork).
type TxRunner interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica los proyectores síncronos dentro de la misma tx.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// GuardianLookup es el puerto hacia Identity (DOM-010): solo lo que Pet
// Registry necesita saber de un tutor.
type GuardianLookup interface {
	Exists(ctx context.Context, id uuid.UUID) bool
	EmailOf(ctx context.Context, id uuid.UUID) (string, bool)
	DisplayName(ctx context.Context, id uuid.UUID) string
}

// OrganizationLookup es el puerto hacia Partner Network (DOM-010). El
// adaptador llega con M1.3; los handlers de vínculo lo usan para I5.
type OrganizationLookup interface {
	// ActiveClinic devuelve error si la org no existe, no es clínica o no está
	// activa (→ ORGANIZATION_NOT_ELIGIBLE).
	ActiveClinic(ctx context.Context, orgID uuid.UUID) (OrgSummary, error)
}

// OrgSummary es lo que Pet Registry sabe de una organización.
type OrgSummary struct {
	ID          uuid.UUID
	Type        string // clinic | pharmacy
	DisplayName string
	City        string
	Status      string
}

// MicrochipReservations gestiona la unicidad global por reserva transaccional
// (DAT-005, ADR-002). Todo ocurre en la tx del comando, antes del Append.
type MicrochipReservations interface {
	// Reserve inserta la reserva. Si el chip ya está tomado por OTRA mascota
	// devuelve ErrMicrochipTaken; si es de la misma mascota, es idempotente.
	Reserve(ctx context.Context, microchip string, petID uuid.UUID) error
	// Release borra la reserva de un chip que pertenece a la mascota dada.
	Release(ctx context.Context, microchip string, petID uuid.UUID) error
}

// PetQueries es el lado de consulta (ARQ-009): siempre filtra por principal;
// sin filtro de principal no compila sin // authz: public.
type PetQueries interface {
	// ListPets lista las mascotas del tutor con paginación por cursor.
	ListPets(ctx context.Context, guardianID uuid.UUID, status string, cursor PetCursor, limit int) ([]PetListItem, *PetCursor, error)
	// GetPet devuelve la mascota (vista completa con vínculos).
	GetPet(ctx context.Context, petID uuid.UUID) (PetView, error)
}

// ErrMicrochipTaken lo devuelve Reserve cuando el chip pertenece a otra mascota.
var ErrMicrochipTaken = errMicrochipTaken{}

type errMicrochipTaken struct{}

func (errMicrochipTaken) Error() string { return "microchip taken by another pet" }

// ClockNeeded marca los puertos de tiempo/identidad (kernel), inyectables.
type ClockNeeded = kernel.Clock
