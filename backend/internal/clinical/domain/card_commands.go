package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Comandos de VaccinationCard (docs/phase-2/02 §3.4). Igual que Encounter, lo
// que depende de otros contextos (vínculo, nacimiento, propuesta del
// protocolo) lo resuelve app y llega aquí como valor.

// CardActor es quien ejecuta el comando sobre el carnet.
type CardActor struct {
	UserID         uuid.UUID
	OrganizationID uuid.UUID // nil-uuid para tutor o super_admin sin organización
	SuperAdmin     bool
	Guardian       bool // tutor de la mascota (PetAccess)
}

func (a CardActor) entryActor() EntryActor {
	return EntryActor{UserID: a.UserID, OrganizationID: a.OrganizationID}
}

// EntryInput agrupa los campos comunes a RecordEntry y DeclareEntry, en bruto
// (se validan al ejecutar el comando).
type EntryInput struct {
	Kind           string
	Product        ProductRef
	AdministeredAt string
	Lot            *string
	DoseLabel      *string
	NextDueDate    *string // dado por el actor; si es nil se usa Proposed
}

// RecordEntry registra una vacuna o preventivo (V1–V3, V6, V7). Species es la
// especie de la mascota (puerto PetAccess) y se usa al crear el carnet.
type RecordEntry struct {
	PetID     uuid.UUID
	EntryID   uuid.UUID
	Species   string
	BirthDate *kernel.CivilDate
	PetLinked bool // V1
	EntryInput
	Manufacturer *string
	ExpiryDate   *string
	Route        *string
	Site         *string
	WeightKg     *float64
	EncounterID  *uuid.UUID
	Proposed     *NextDue // NextDueCalculator.Propose; solo si NextDueDate es nil
	Actor        CardActor
	Now          time.Time
}

// DeclareEntry es la declaración del tutor (Source declared, Unverified).
type DeclareEntry struct {
	PetID       uuid.UUID
	EntryID     uuid.UUID
	Species     string
	BirthDate   *kernel.CivilDate
	EvidenceKey *string
	EntryInput
	Proposed *NextDue
	Actor    CardActor
	Now      time.Time
}

// VerifyEntry confirma una entrada declarada (V1).
type VerifyEntry struct {
	EntryID   uuid.UUID
	PetLinked bool
	Note      *string
	Actor     CardActor
}

// DisputeEntry disputa una entrada declarada (V1).
type DisputeEntry struct {
	EntryID   uuid.UUID
	PetLinked bool
	Note      *string
	Actor     CardActor
}

// VoidEntry anula una entrada activa (V4, V5). PetLinked solo importa para
// actores de organización.
type VoidEntry struct {
	EntryID   uuid.UUID
	Reason    string
	PetLinked bool
	Actor     CardActor
	Now       time.Time
}

// AdjustNextDue fija la próxima dosis (basis clinic). V1 y V7.
type AdjustNextDue struct {
	EntryID   uuid.UUID
	Date      string
	PetLinked bool
	Actor     CardActor
}

// SyncSpecies refleja un cambio de especie de Pet (reacción, sin actor).
type SyncSpecies struct {
	PetID   uuid.UUID
	Species string
}
