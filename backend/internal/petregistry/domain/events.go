package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Eventos Pet Registry v1 (PP-F1-DOM-005). Cada payload lleva tags JSON
// porque es el contrato persistido (excepción de ARQ-002); los campos PII de
// personas viajan en el sub-objeto `pii`. Nunca se renombra un campo ya
// persistido: nueva schema_version + upcaster (ARQ-007).

// BirthDatePayload es la fecha de nacimiento en eventos.
type BirthDatePayload struct {
	Date        string `json:"date"`
	Approximate bool   `json:"approximate"`
}

// PhotoRefPayload es la foto en eventos (nil = sin foto).
type PhotoRefPayload = PhotoRef

// PetRegistered: nace el agregado.
type PetRegistered struct {
	PetID        uuid.UUID         `json:"pet_id"`
	GuardianID   uuid.UUID         `json:"guardian_id"`
	Name         string            `json:"name"`
	Species      Species           `json:"species"`
	SpeciesOther string            `json:"species_other,omitempty"`
	Breed        string            `json:"breed,omitempty"`
	Sex          Sex               `json:"sex"`
	Neutered     Tristate          `json:"neutered"`
	BirthDate    *BirthDatePayload `json:"birth_date,omitempty"`
	Markings     string            `json:"markings,omitempty"`
}

// EventName implementa eventstore.Event.
func (PetRegistered) EventName() string { return "PetRegistered" }

// SchemaVersion implementa eventstore.Event.
func (PetRegistered) SchemaVersion() int { return 1 }

// PetProfileUpdated: solo campos cambiados.
type PetProfileUpdated struct {
	PetID   uuid.UUID `json:"pet_id"`
	Changes Changes   `json:"changes"`
}

// Changes son los campos cambiados del perfil. nil = sin cambio; cadena
// vacía = vaciar el campo opcional (los valores válidos nunca son vacíos).
type Changes struct {
	Name         *string `json:"name,omitempty"`
	Species      *string `json:"species,omitempty"`
	SpeciesOther *string `json:"species_other,omitempty"`
	Breed        *string `json:"breed,omitempty"`
	Sex          *string `json:"sex,omitempty"`
	Neutered     *string `json:"neutered,omitempty"`
	BirthDate    *BirthDatePayload `json:"birth_date,omitempty"`
	Markings     *string `json:"markings,omitempty"`
}

// EventName implementa eventstore.Event.
func (PetProfileUpdated) EventName() string { return "PetProfileUpdated" }

// SchemaVersion implementa eventstore.Event.
func (PetProfileUpdated) SchemaVersion() int { return 1 }

// PetMicrochipAssigned incluye el anterior, si existía.
type PetMicrochipAssigned struct {
	PetID      uuid.UUID `json:"pet_id"`
	Microchip  string    `json:"microchip"`
	Previous   string    `json:"previous,omitempty"`
}

// EventName implementa eventstore.Event.
func (PetMicrochipAssigned) EventName() string { return "PetMicrochipAssigned" }

// SchemaVersion implementa eventstore.Event.
func (PetMicrochipAssigned) SchemaVersion() int { return 1 }

// PetPhotoChanged: Photo nil = quitar la foto.
type PetPhotoChanged struct {
	PetID    uuid.UUID        `json:"pet_id"`
	Photo    *PhotoRefPayload `json:"photo,omitempty"`
	Previous *PhotoRefPayload `json:"previous,omitempty"`
}

// EventName implementa eventstore.Event.
func (PetPhotoChanged) EventName() string { return "PetPhotoChanged" }

// SchemaVersion implementa eventstore.Event.
func (PetPhotoChanged) SchemaVersion() int { return 1 }

// PetMarkedDeceased: fecha civil opcional.
type PetMarkedDeceased struct {
	PetID      uuid.UUID `json:"pet_id"`
	DeceasedAt string    `json:"deceased_at,omitempty"`
}

// EventName implementa eventstore.Event.
func (PetMarkedDeceased) EventName() string { return "PetMarkedDeceased" }

// SchemaVersion implementa eventstore.Event.
func (PetMarkedDeceased) SchemaVersion() int { return 1 }

// PetDeceasedUndone revierte el fallecimiento dentro de la ventana.
type PetDeceasedUndone struct {
	PetID uuid.UUID `json:"pet_id"`
}

// EventName implementa eventstore.Event.
func (PetDeceasedUndone) EventName() string { return "PetDeceasedUndone" }

// SchemaVersion implementa eventstore.Event.
func (PetDeceasedUndone) SchemaVersion() int { return 1 }

// CareLinkGranted: el tutor concede vínculo a una clínica.
type CareLinkGranted struct {
	PetID          uuid.UUID `json:"pet_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	GrantedBy      uuid.UUID `json:"granted_by"`
}

// EventName implementa eventstore.Event.
func (CareLinkGranted) EventName() string { return "CareLinkGranted" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkGranted) SchemaVersion() int { return 1 }

// CareLinkRevoked: el tutor revoca el vínculo.
type CareLinkRevoked struct {
	PetID          uuid.UUID `json:"pet_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	RevokedBy      uuid.UUID `json:"revoked_by"`
	Reason         string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (CareLinkRevoked) EventName() string { return "CareLinkRevoked" }

// SchemaVersion implementa eventstore.Event.
func (CareLinkRevoked) SchemaVersion() int { return 1 }

// TransferPII encapsula la PII de la transferencia (ADR-002: sub-objeto pii).
type TransferPII struct {
	ToEmail kernel.Email `json:"to_email"`
}

// GuardianshipTransferInitiated: el email destino viaja solo en pii.
type GuardianshipTransferInitiated struct {
	PetID          uuid.UUID     `json:"pet_id"`
	FromGuardianID uuid.UUID     `json:"from_guardian_id"`
	ExpiresAt      time.Time     `json:"expires_at"`
	PII            *TransferPII  `json:"pii,omitempty"`
}

// EventName implementa eventstore.Event.
func (GuardianshipTransferInitiated) EventName() string { return "GuardianshipTransferInitiated" }

// SchemaVersion implementa eventstore.Event.
func (GuardianshipTransferInitiated) SchemaVersion() int { return 1 }

// GuardianshipTransferCancelled cancela la transferencia pendiente.
type GuardianshipTransferCancelled struct {
	PetID uuid.UUID `json:"pet_id"`
}

// EventName implementa eventstore.Event.
func (GuardianshipTransferCancelled) EventName() string { return "GuardianshipTransferCancelled" }

// SchemaVersion implementa eventstore.Event.
func (GuardianshipTransferCancelled) SchemaVersion() int { return 1 }

// PetGuardianshipTransferred cambia el tutor; misma PetID, historia intacta.
type PetGuardianshipTransferred struct {
	PetID          uuid.UUID `json:"pet_id"`
	FromGuardianID uuid.UUID `json:"from_guardian_id"`
	ToGuardianID   uuid.UUID `json:"to_guardian_id"`
}

// EventName implementa eventstore.Event.
func (PetGuardianshipTransferred) EventName() string { return "PetGuardianshipTransferred" }

// SchemaVersion implementa eventstore.Event.
func (PetGuardianshipTransferred) SchemaVersion() int { return 1 }
