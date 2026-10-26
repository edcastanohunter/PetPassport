package domain

import (
	"time"

	"github.com/google/uuid"
)

// Comandos del agregado Pet (docs/phase-1/02 §3.1). Cada comando toca un solo
// agregado (ARQ-003). El actor se valida en authz (I6); los comandos traen
// los datos que necesitan (timestamps, hashes de token) para que el dominio
// sea puro y determinista en pruebas.

// BirthDateInput es la fecha de nacimiento entrante (string ISO + flag).
type BirthDateInput struct {
	Date        string
	Approximate bool
}

// RegisterPet crea la mascota.
type RegisterPet struct {
	PetID        uuid.UUID
	GuardianID   uuid.UUID
	Name         string
	Species      string
	SpeciesOther string
	Breed        *string
	Sex          string
	Neutered     string
	BirthDate    *BirthDateInput
	Markings     *string
	Now          time.Time
}

// CommandName implementa bus.Command.
func (RegisterPet) CommandName() string { return "petregistry.RegisterPet" }

// UpdatePetProfile edita campos opcionales. Puntero nil = sin cambio;
// cadena vacía en SpeciesOther/Breed/Markings = vaciar; BirthDate.Date
// vacío = vaciar.
type UpdatePetProfile struct {
	PetID        uuid.UUID
	Name         *string
	Species      *string
	SpeciesOther *string
	Breed        *string
	Sex          *string
	Neutered     *string
	BirthDate    *BirthDateInput
	Markings     *string
	Now          time.Time
}

// CommandName implementa bus.Command.
func (UpdatePetProfile) CommandName() string { return "petregistry.UpdatePetProfile" }

// AssignMicrochip asigna o corrige el microchip (I2 formato, I7 correcciones).
type AssignMicrochip struct {
	PetID     uuid.UUID
	Microchip string
}

// CommandName implementa bus.Command.
func (AssignMicrochip) CommandName() string { return "petregistry.AssignMicrochip" }

// ChangePetPhoto cambia (Photo nil = quita) la foto. Permitido en deceased (I3).
type ChangePetPhoto struct {
	PetID uuid.UUID
	Photo *PhotoRef
}

// CommandName implementa bus.Command.
func (ChangePetPhoto) CommandName() string { return "petregistry.ChangePetPhoto" }

// MarkPetDeceased marca la mascota fallecida.
type MarkPetDeceased struct {
	PetID      uuid.UUID
	DeceasedAt *string // fecha civil opcional
	Now        time.Time
}

// CommandName implementa bus.Command.
func (MarkPetDeceased) CommandName() string { return "petregistry.MarkPetDeceased" }

// UndoDeceased deshace el fallecimiento. La ventana de 24 h la computa app
// con deceased_marked_at del read model (DAT-005); el dominio decide con las
// banderas.
type UndoDeceased struct {
	PetID             uuid.UUID
	WithinUndoWindow  bool
	ActorIsSuperAdmin bool
}

// CommandName implementa bus.Command.
func (UndoDeceased) CommandName() string { return "petregistry.UndoDeceased" }

// GrantCareLink concede vínculo a una clínica (I5 se valida en app con el
// puerto OrganizationLookup; I8 y la idempotencia viven aquí).
type GrantCareLink struct {
	PetID          uuid.UUID
	OrganizationID uuid.UUID
	ActorID        uuid.UUID
}

// CommandName implementa bus.Command.
func (GrantCareLink) CommandName() string { return "petregistry.GrantCareLink" }

// RevokeCareLink revoca un vínculo activo.
type RevokeCareLink struct {
	PetID          uuid.UUID
	OrganizationID uuid.UUID
	ActorID        uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (RevokeCareLink) CommandName() string { return "petregistry.RevokeCareLink" }

// InitiateGuardianshipTransfer inicia la transferencia de tutela.
type InitiateGuardianshipTransfer struct {
	PetID                uuid.UUID
	ToEmail              string // normalizado por app
	CurrentGuardianEmail string // I4
	TokenHash            []byte // SHA-256 del token crudo (TokenGen en app)
	ExpiresAt            time.Time
	Now                  time.Time
}

// CommandName implementa bus.Command.
func (InitiateGuardianshipTransfer) CommandName() string {
	return "petregistry.InitiateGuardianshipTransfer"
}

// CancelGuardianshipTransfer cancela la transferencia pendiente.
type CancelGuardianshipTransfer struct {
	PetID uuid.UUID
	Now   time.Time
}

// CommandName implementa bus.Command.
func (CancelGuardianshipTransfer) CommandName() string {
	return "petregistry.CancelGuardianshipTransfer"
}

// AcceptGuardianshipTransfer acepta la transferencia. La validez del token
// se resuelve por consulta sobre pet_transfers en app (404 si no coincide o
// caducó, sin distinguir); el dominio valida vigencia, destinatario y tutor.
type AcceptGuardianshipTransfer struct {
	PetID       uuid.UUID
	ActorEmail  string
	ActorUserID uuid.UUID
	Now         time.Time
}

// CommandName implementa bus.Command.
func (AcceptGuardianshipTransfer) CommandName() string {
	return "petregistry.AcceptGuardianshipTransfer"
}
