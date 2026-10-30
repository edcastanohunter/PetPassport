package domain

import (
	"time"

	"github.com/google/uuid"
)

// Eventos Encounter v1 (PP-F2-DOM-005, docs/phase-2/02 §3.3). Los payloads
// llevan tags JSON porque son el contrato persistido; nunca se renombra un
// campo ya persistido (nueva schema_version + upcaster, ARQ-005). Ningún
// evento contiene PII del tutor: los textos clínicos son datos de salud del
// animal y los profesionales se referencian por UserID.

// EncounterOpened: nace el agregado en draft.
type EncounterOpened struct {
	EncounterID    uuid.UUID     `json:"encounter_id"`
	PetID          uuid.UUID     `json:"pet_id"`
	OrganizationID uuid.UUID     `json:"organization_id"`
	AppointmentID  *uuid.UUID    `json:"appointment_id,omitempty"`
	Type           EncounterType `json:"type"`
	Reason         string        `json:"reason"`
	StartedAt      time.Time     `json:"started_at"`
	AttendingVetID *uuid.UUID    `json:"attending_vet_id,omitempty"`
	OpenedBy       uuid.UUID     `json:"opened_by"`
}

// EventName implementa eventstore.Event.
func (EncounterOpened) EventName() string { return "EncounterOpened" }

// SchemaVersion implementa eventstore.Event.
func (EncounterOpened) SchemaVersion() int { return 1 }

// EncounterDetailsUpdated: cambio de motivo, tipo o fecha de inicio en
// borrador (PATCH /encounters/{id}, API-001). Solo campos cambiados. No está
// en la tabla de 02 §3.3, que no cubre la edición de started_at (±24 h,
// PRD-001) ni del motivo: decisión registrada en el PR de M2.1.
type EncounterDetailsUpdated struct {
	EncounterID uuid.UUID      `json:"encounter_id"`
	Type        *EncounterType `json:"type,omitempty"`
	Reason      *string        `json:"reason,omitempty"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
}

// EventName implementa eventstore.Event.
func (EncounterDetailsUpdated) EventName() string { return "EncounterDetailsUpdated" }

// SchemaVersion implementa eventstore.Event.
func (EncounterDetailsUpdated) SchemaVersion() int { return 1 }

// VitalsRecorded: reemplazo completo de las constantes.
type VitalsRecorded struct {
	EncounterID uuid.UUID `json:"encounter_id"`
	Vitals      Vitals    `json:"vitals"`
}

// EventName implementa eventstore.Event.
func (VitalsRecorded) EventName() string { return "VitalsRecorded" }

// SchemaVersion implementa eventstore.Event.
func (VitalsRecorded) SchemaVersion() int { return 1 }

// EncounterNarrativeUpdated: solo los campos cambiados (S y/o O completo).
type EncounterNarrativeUpdated struct {
	EncounterID uuid.UUID  `json:"encounter_id"`
	Subjective  *string    `json:"subjective,omitempty"`
	Objective   *Objective `json:"objective,omitempty"`
}

// EventName implementa eventstore.Event.
func (EncounterNarrativeUpdated) EventName() string { return "EncounterNarrativeUpdated" }

// SchemaVersion implementa eventstore.Event.
func (EncounterNarrativeUpdated) SchemaVersion() int { return 1 }

// DiagnosesSet: reemplazo completo de la lista.
type DiagnosesSet struct {
	EncounterID uuid.UUID   `json:"encounter_id"`
	Diagnoses   []Diagnosis `json:"diagnoses"`
}

// EventName implementa eventstore.Event.
func (DiagnosesSet) EventName() string { return "DiagnosesSet" }

// SchemaVersion implementa eventstore.Event.
func (DiagnosesSet) SchemaVersion() int { return 1 }

// PlanSet: reemplazo completo del plan.
type PlanSet struct {
	EncounterID uuid.UUID `json:"encounter_id"`
	Plan        Plan      `json:"plan"`
}

// EventName implementa eventstore.Event.
func (PlanSet) EventName() string { return "PlanSet" }

// SchemaVersion implementa eventstore.Event.
func (PlanSet) SchemaVersion() int { return 1 }

// FollowUpRecommended: control recomendado.
type FollowUpRecommended struct {
	EncounterID uuid.UUID `json:"encounter_id"`
	DueDate     string    `json:"due_date"`
	Reason      string    `json:"reason"`
}

// EventName implementa eventstore.Event.
func (FollowUpRecommended) EventName() string { return "FollowUpRecommended" }

// SchemaVersion implementa eventstore.Event.
func (FollowUpRecommended) SchemaVersion() int { return 1 }

// FollowUpCleared: se retira el control recomendado.
type FollowUpCleared struct {
	EncounterID uuid.UUID `json:"encounter_id"`
}

// EventName implementa eventstore.Event.
func (FollowUpCleared) EventName() string { return "FollowUpCleared" }

// SchemaVersion implementa eventstore.Event.
func (FollowUpCleared) SchemaVersion() int { return 1 }

// AttendingVetAssigned: profesional responsable.
type AttendingVetAssigned struct {
	EncounterID uuid.UUID `json:"encounter_id"`
	UserID      uuid.UUID `json:"user_id"`
}

// EventName implementa eventstore.Event.
func (AttendingVetAssigned) EventName() string { return "AttendingVetAssigned" }

// SchemaVersion implementa eventstore.Event.
func (AttendingVetAssigned) SchemaVersion() int { return 1 }

// AttachmentAdded: adjunto en borrador.
type AttachmentAdded struct {
	EncounterID uuid.UUID  `json:"encounter_id"`
	Attachment  Attachment `json:"attachment"`
}

// EventName implementa eventstore.Event.
func (AttachmentAdded) EventName() string { return "AttachmentAdded" }

// SchemaVersion implementa eventstore.Event.
func (AttachmentAdded) SchemaVersion() int { return 1 }

// AttachmentRemoved: solo en borrador.
type AttachmentRemoved struct {
	EncounterID  uuid.UUID `json:"encounter_id"`
	AttachmentID uuid.UUID `json:"attachment_id"`
}

// EventName implementa eventstore.Event.
func (AttachmentRemoved) EventName() string { return "AttachmentRemoved" }

// SchemaVersion implementa eventstore.Event.
func (AttachmentRemoved) SchemaVersion() int { return 1 }

// AttachmentVisibilityChanged: visibilidad para el tutor (cualquier estado).
type AttachmentVisibilityChanged struct {
	EncounterID  uuid.UUID `json:"encounter_id"`
	AttachmentID uuid.UUID `json:"attachment_id"`
	Visible      bool      `json:"visible"`
	Reason       string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (AttachmentVisibilityChanged) EventName() string { return "AttachmentVisibilityChanged" }

// SchemaVersion implementa eventstore.Event.
func (AttachmentVisibilityChanged) SchemaVersion() int { return 1 }

// EncounterSigned congela el contenido. stream_version es la última versión
// incluida en content_hash (la del propio evento es stream_version + 1).
// Desnormaliza ids para consumidores (complete-on-sign, notificaciones).
type EncounterSigned struct {
	EncounterID    uuid.UUID  `json:"encounter_id"`
	SignedBy       uuid.UUID  `json:"signed_by"`
	SignedAt       time.Time  `json:"signed_at"`
	StreamVersion  int64      `json:"stream_version"`
	ContentHash    string     `json:"content_hash"`
	PetID          uuid.UUID  `json:"pet_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	AppointmentID  *uuid.UUID `json:"appointment_id,omitempty"`
}

// EventName implementa eventstore.Event.
func (EncounterSigned) EventName() string { return "EncounterSigned" }

// SchemaVersion implementa eventstore.Event.
func (EncounterSigned) SchemaVersion() int { return 1 }

// EncounterAddendumAdded: adenda firmada sobre un encuentro firmado.
type EncounterAddendumAdded struct {
	EncounterID uuid.UUID      `json:"encounter_id"`
	Seq         int            `json:"seq"`
	Text        string         `json:"text"`
	Reason      AddendumReason `json:"reason"`
	SignedBy    uuid.UUID      `json:"signed_by"`
	SignedAt    time.Time      `json:"signed_at"`
	Attachments []Attachment   `json:"attachments"`
}

// EventName implementa eventstore.Event.
func (EncounterAddendumAdded) EventName() string { return "EncounterAddendumAdded" }

// SchemaVersion implementa eventstore.Event.
func (EncounterAddendumAdded) SchemaVersion() int { return 1 }

// EncounterDiscarded: borrador descartado con motivo.
type EncounterDiscarded struct {
	EncounterID uuid.UUID `json:"encounter_id"`
	Reason      string    `json:"reason"`
	By          uuid.UUID `json:"by"`
}

// EventName implementa eventstore.Event.
func (EncounterDiscarded) EventName() string { return "EncounterDiscarded" }

// SchemaVersion implementa eventstore.Event.
func (EncounterDiscarded) SchemaVersion() int { return 1 }
