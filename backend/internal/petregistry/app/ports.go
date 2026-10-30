// Package app expone los casos de uso de Pet Registry (ARQ-003): cada comando
// ejecuta authz (en http) → idempotency (en http, sobre la clave del request)
// → uow.Do {load → handle → reserve → append → project(sync) → outbox}.
// Los puertos viven aquí; los adaptadores en infra.
package app

import (
	"context"
	"errors"
	"time"

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
	// Contact devuelve los datos de contacto del tutor (RN-05: solo para G, L
	// o super_admin, tras pet:guardian_contact:read).
	Contact(ctx context.Context, id uuid.UUID) (GuardianContact, bool)
}

// GuardianContact son los datos de contacto del tutor visibles para clínicas
// con vínculo activo (PRD-016).
type GuardianContact struct {
	DisplayName string
	Email       string
	Phone       string
}

// OrganizationLookup es el puerto hacia Partner Network (DOM-010).
type OrganizationLookup interface {
	// ActiveClinic devuelve ErrOrganizationNotEligible si la org no existe, no
	// es clínica o no está activa (I5); otros errores son de infraestructura.
	ActiveClinic(ctx context.Context, orgID uuid.UUID) (OrgSummary, error)
}

// ErrOrganizationNotEligible lo devuelve OrganizationLookup (I5).
var ErrOrganizationNotEligible = errors.New("organization not eligible")

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
	// ListPatients lista las mascotas con vínculo activo de una clínica
	// (PRD-016); excluye vínculos de org suspendida. q busca por nombre de
	// mascota o de tutor (ILIKE, trigram).
	ListPatients(ctx context.Context, orgID uuid.UUID, q, status string, cursor PatientCursor, limit int) ([]PatientRow, *PatientCursor, error)
	// TransferByTokenHash resuelve una transferencia pendiente y vigente;
	// otra cosa → *kernel.NotFoundError (404 sin distinguir).
	TransferByTokenHash(ctx context.Context, hash []byte, now time.Time) (TransferView, error)
}

// ------------------------------------------------- hash de token en contexto

type transferHashKey struct{}

// WithTransferTokenHash adjunta el hash del token de la transferencia que el
// comando va a iniciar; el proyector de pet_transfers lo lee en la misma tx.
func WithTransferTokenHash(ctx context.Context, petID uuid.UUID, hash []byte) context.Context {
	return context.WithValue(ctx, transferHashKey{}, map[uuid.UUID][]byte{petID: hash})
}

// TransferTokenHashFrom devuelve el hash adjunto; nil en replay/rebuild (el
// proyector invalida el token con un hash aleatorio).
func TransferTokenHashFrom(ctx context.Context, petID uuid.UUID) []byte {
	m, _ := ctx.Value(transferHashKey{}).(map[uuid.UUID][]byte)
	return m[petID]
}

// ErrMicrochipTaken lo devuelve Reserve cuando el chip pertenece a otra mascota.
var ErrMicrochipTaken = errMicrochipTaken{}

type errMicrochipTaken struct{}

func (errMicrochipTaken) Error() string { return "microchip taken by another pet" }

// ClockNeeded marca los puertos de tiempo/identidad (kernel), inyectables.
type ClockNeeded = kernel.Clock
