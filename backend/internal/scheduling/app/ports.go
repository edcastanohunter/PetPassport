package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
)

// PetInfo son los datos mínimos de la mascota que Scheduling necesita.
type PetInfo struct {
	ID         uuid.UUID
	GuardianID uuid.UUID
	Name       string
	Species    string
	Deceased   bool
}

// PetAccess es la única puerta de Scheduling a Pet Registry (02 §6, SEC-007).
type PetAccess interface {
	// Pet devuelve la mascota (NotFoundError si no existe).
	Pet(ctx context.Context, petID uuid.UUID) (PetInfo, error)
	// Linked indica vínculo activo mascota↔organización con la organización activa.
	Linked(ctx context.Context, petID, orgID uuid.UUID) (bool, error)
	// GuardianLinked indica si el usuario es tutor de alguna mascota con
	// vínculo activo a la organización (ve la disponibilidad publicada).
	GuardianLinked(ctx context.Context, guardianID, orgID uuid.UUID) (bool, error)
}

// OrgInfo son los datos mínimos de la organización.
type OrgInfo struct {
	ID      uuid.UUID
	Name    string
	Country string
	Type    string // clinic | pharmacy
	Status  string // active | …
}

// Membership es el puerto hacia Partner Network.
type Membership interface {
	// RoleOf devuelve el rol del usuario y si la membresía y la organización
	// están activas.
	RoleOf(ctx context.Context, userID, orgID uuid.UUID) (role string, active bool, err error)
	Org(ctx context.Context, orgID uuid.UUID) (OrgInfo, error)
	// Veterinarians devuelve los veterinarios activos de la organización.
	Veterinarians(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
}

// Reservation es una fila de slot_reservations.
type Reservation struct {
	AppointmentID  uuid.UUID
	ProfessionalID uuid.UUID
	Slot           domain.Slot
	Provisional    bool
	ExpiresAt      *time.Time
}

// Reservations es el puerto SlotReservations (DAT-004, ADR-011). Reserve y
// Release usan la transacción del contexto: van en la misma tx que el Append.
type Reservations interface {
	// Reserve crea o reemplaza la reserva de la cita; ErrSlotUnavailable si
	// se solapa con otra del profesional.
	Reserve(ctx context.Context, r Reservation) error
	Release(ctx context.Context, appointmentID uuid.UUID) error
	// Reserved devuelve las reservas vigentes del profesional que tocan [from, to).
	Reserved(ctx context.Context, professionalID uuid.UUID, from, to time.Time) ([]domain.Reservation, error)
}

// AppointmentRepository persiste y rehidrata Appointment desde el Event Store.
type AppointmentRepository interface {
	// Load rehidrata por replay; agregado vacío (versión 0) si el stream no existe.
	Load(ctx context.Context, id uuid.UUID) (*domain.Appointment, error)
	Save(ctx context.Context, a *domain.Appointment, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
}

// AgendaRepository persiste y rehidrata ProfessionalAgenda (con snapshots).
type AgendaRepository interface {
	Load(ctx context.Context, orgID, professionalID uuid.UUID) (*domain.ProfessionalAgenda, error)
	Save(ctx context.Context, a *domain.ProfessionalAgenda, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
}

// AgendaReader es el lado de lectura de las agendas (read model).
type AgendaReader interface {
	// Agenda devuelve la agenda proyectada (NotFoundError si no existe).
	Agenda(ctx context.Context, orgID, professionalID uuid.UUID) (*domain.ProfessionalAgenda, error)
	// Agendas devuelve todas las agendas de la organización.
	Agendas(ctx context.Context, orgID uuid.UUID) ([]*domain.ProfessionalAgenda, error)
}

// TxRunner ejecuta fn dentro de una transacción (db.UnitOfWork).
type TxRunner interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica los proyectores síncronos en la misma tx.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// AppointmentView es la cita proyectada con los datos denormalizados que la
// UI necesita (nombres de mascota, clínica y profesional).
type AppointmentView struct {
	ID                uuid.UUID
	Version           int64
	PetID             uuid.UUID
	PetName           string
	PetSpecies        string
	GuardianID        uuid.UUID
	OrganizationID    uuid.UUID
	OrganizationName  string
	ProfessionalID    *uuid.UUID
	ProfessionalName  string
	Type              string
	Origin            string
	Status            string
	Reason            string
	Preferences       []domain.Preference
	Slot              *domain.Slot
	TZ                string
	ProposalExpiresAt *time.Time
	EncounterID       *uuid.UUID
	Cancellation      *domain.Cancellation
	PolicyHours       int
	History           []domain.Transition
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ListFilter filtra las listas de citas. Con From/To la lista es una agenda
// (orden ascendente por franja, sin cursor); sin ellos, una bandeja
// (descendente, con cursor).
type ListFilter struct {
	OrganizationID *uuid.UUID
	GuardianID     *uuid.UUID
	PetID          *uuid.UUID
	ProfessionalID *uuid.UUID
	Statuses       []string
	From, To       *time.Time
	Cursor         string
	Limit          int
}

// AppointmentQueries es el lado de lectura de las citas.
type AppointmentQueries interface {
	Get(ctx context.Context, id uuid.UUID) (AppointmentView, error)
	List(ctx context.Context, f ListFilter) (items []AppointmentView, next string, err error)
	// PendingRequests cuenta las solicitudes `requested` de la mascota en la organización (A2).
	PendingRequests(ctx context.Context, petID, orgID uuid.UUID) (int, error)
}
