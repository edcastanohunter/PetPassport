package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Eventos VaccinationCard v1 (PP-F2-DOM-008, docs/phase-2/02 §3.6). Mismas
// reglas que los de Encounter: contrato persistido, sin PII del tutor.

// RecordedFields es el cuerpo común de las entradas registradas por una
// organización (embebido en los eventos: el JSON queda plano).
type RecordedFields struct {
	PetID          uuid.UUID         `json:"pet_id"`
	EntryID        uuid.UUID         `json:"entry_id"`
	Product        ProductRef        `json:"product"`
	AdministeredAt kernel.CivilDate  `json:"administered_at"`
	Lot            *string           `json:"lot,omitempty"`
	Manufacturer   *string           `json:"manufacturer,omitempty"`
	ExpiryDate     *kernel.CivilDate `json:"expiry_date,omitempty"`
	Route          *string           `json:"route,omitempty"`
	Site           *string           `json:"site,omitempty"`
	DoseLabel      *string           `json:"dose_label,omitempty"`
	WeightKg       *float64          `json:"weight_kg,omitempty"`
	RecordedBy     EntryActor        `json:"recorded_by"`
	RecordedAt     time.Time         `json:"recorded_at"`
	EncounterID    *uuid.UUID        `json:"encounter_id,omitempty"`
	NextDue        *NextDue          `json:"next_due,omitempty"`
	Source         EntrySource       `json:"source"`
}

// VaccinationEntryRecorded: vacuna registrada por una organización.
type VaccinationEntryRecorded struct {
	RecordedFields
}

// EventName implementa eventstore.Event.
func (VaccinationEntryRecorded) EventName() string { return "VaccinationEntryRecorded" }

// SchemaVersion implementa eventstore.Event.
func (VaccinationEntryRecorded) SchemaVersion() int { return 1 }

// PreventiveEntryRecorded: desparasitación o antiparasitario externo.
type PreventiveEntryRecorded struct {
	RecordedFields
	Kind EntryKind `json:"kind"`
}

// EventName implementa eventstore.Event.
func (PreventiveEntryRecorded) EventName() string { return "PreventiveEntryRecorded" }

// SchemaVersion implementa eventstore.Event.
func (PreventiveEntryRecorded) SchemaVersion() int { return 1 }

// VaccinationEntryDeclared: entrada declarada por el tutor (no verificada).
type VaccinationEntryDeclared struct {
	PetID          uuid.UUID        `json:"pet_id"`
	EntryID        uuid.UUID        `json:"entry_id"`
	Kind           EntryKind        `json:"kind"`
	Product        ProductRef       `json:"product"`
	AdministeredAt kernel.CivilDate `json:"administered_at"`
	Lot            *string          `json:"lot,omitempty"`
	DoseLabel      *string          `json:"dose_label,omitempty"`
	NextDue        *NextDue         `json:"next_due,omitempty"`
	DeclaredBy     uuid.UUID        `json:"declared_by"`
	EvidenceKey    *string          `json:"evidence_object_key,omitempty"`
}

// EventName implementa eventstore.Event.
func (VaccinationEntryDeclared) EventName() string { return "VaccinationEntryDeclared" }

// SchemaVersion implementa eventstore.Event.
func (VaccinationEntryDeclared) SchemaVersion() int { return 1 }

// VaccinationEntryVerified: una clínica con acceso confirma una declarada.
type VaccinationEntryVerified struct {
	PetID   uuid.UUID  `json:"pet_id"`
	EntryID uuid.UUID  `json:"entry_id"`
	By      EntryActor `json:"by"`
	Note    *string    `json:"note,omitempty"`
}

// EventName implementa eventstore.Event.
func (VaccinationEntryVerified) EventName() string { return "VaccinationEntryVerified" }

// SchemaVersion implementa eventstore.Event.
func (VaccinationEntryVerified) SchemaVersion() int { return 1 }

// VaccinationEntryDisputed: la clínica disputa una declarada.
type VaccinationEntryDisputed struct {
	PetID   uuid.UUID  `json:"pet_id"`
	EntryID uuid.UUID  `json:"entry_id"`
	By      EntryActor `json:"by"`
	Note    *string    `json:"note,omitempty"`
}

// EventName implementa eventstore.Event.
func (VaccinationEntryDisputed) EventName() string { return "VaccinationEntryDisputed" }

// SchemaVersion implementa eventstore.Event.
func (VaccinationEntryDisputed) SchemaVersion() int { return 1 }

// VaccinationEntryVoided: anulación con motivo. By.OrganizationID es el
// nil-uuid si anuló el tutor (entrada declarada) o un super_admin sin
// organización.
type VaccinationEntryVoided struct {
	PetID   uuid.UUID  `json:"pet_id"`
	EntryID uuid.UUID  `json:"entry_id"`
	Reason  string     `json:"reason"`
	By      EntryActor `json:"by"`
}

// EventName implementa eventstore.Event.
func (VaccinationEntryVoided) EventName() string { return "VaccinationEntryVoided" }

// SchemaVersion implementa eventstore.Event.
func (VaccinationEntryVoided) SchemaVersion() int { return 1 }

// NextDueAdjusted: la clínica fija la próxima dosis (basis clinic).
type NextDueAdjusted struct {
	PetID   uuid.UUID        `json:"pet_id"`
	EntryID uuid.UUID        `json:"entry_id"`
	Date    kernel.CivilDate `json:"date"`
	Basis   DueBasis         `json:"basis"`
	By      EntryActor       `json:"by"`
}

// EventName implementa eventstore.Event.
func (NextDueAdjusted) EventName() string { return "NextDueAdjusted" }

// SchemaVersion implementa eventstore.Event.
func (NextDueAdjusted) SchemaVersion() int { return 1 }

// CardSpeciesSynced: especie de la mascota copiada al carnet. Se emite al
// crearlo (primera entrada) y cuando cambia en Pet.
type CardSpeciesSynced struct {
	PetID   uuid.UUID `json:"pet_id"`
	Species string    `json:"species"`
}

// EventName implementa eventstore.Event.
func (CardSpeciesSynced) EventName() string { return "CardSpeciesSynced" }

// SchemaVersion implementa eventstore.Event.
func (CardSpeciesSynced) SchemaVersion() int { return 1 }
