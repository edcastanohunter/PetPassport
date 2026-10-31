package domain

import (
	"time"

	"github.com/google/uuid"
)

// Eventos Appointment v1 (PP-F2-DOM-012, docs/phase-2/02 §4.3). Contrato
// persistido: cambiar un campo exige schema_version y upcaster (ARQ-005). Sin
// PII del tutor: el motivo de la cita sí es dato de salud y vive en el evento
// (mismo tratamiento que el motivo de consulta).

// ApptRef identifica la cita y su contexto; se embebe en todos los eventos
// (el JSON queda plano) para que los consumidores del outbox no tengan que
// rehidratar el agregado.
type ApptRef struct {
	AppointmentID  uuid.UUID `json:"appointment_id"`
	PetID          uuid.UUID `json:"pet_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

// AppointmentRequested: solicitud del tutor (no bloquea agenda, RN-11).
// GuardianID se añade al contrato del doc para la bandeja del tutor.
type AppointmentRequested struct {
	ApptRef
	GuardianID  uuid.UUID       `json:"guardian_id"`
	Type        AppointmentType `json:"type"`
	Reason      string          `json:"reason"`
	Preferences []Preference    `json:"preferences"`
	RequestedBy uuid.UUID       `json:"requested_by"`
	At          time.Time       `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentRequested) EventName() string { return "AppointmentRequested" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentRequested) SchemaVersion() int { return 1 }

// AppointmentCreated: cita creada directamente por la clínica; siempre va
// seguida de AppointmentConfirmed.
type AppointmentCreated struct {
	ApptRef
	GuardianID uuid.UUID       `json:"guardian_id"`
	Type       AppointmentType `json:"type"`
	Reason     string          `json:"reason"`
	CreatedBy  uuid.UUID       `json:"created_by"`
	At         time.Time       `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentCreated) EventName() string { return "AppointmentCreated" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentCreated) SchemaVersion() int { return 1 }

// AppointmentConfirmed: franja y profesional asignados (reserva obtenida).
type AppointmentConfirmed struct {
	ApptRef
	ProfessionalID          uuid.UUID `json:"professional_id"`
	Slot                    Slot      `json:"slot"`
	TZ                      string    `json:"tz"`
	CancellationPolicyHours int       `json:"cancellation_policy_hours"`
	By                      uuid.UUID `json:"by"`
	At                      time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentConfirmed) EventName() string { return "AppointmentConfirmed" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentConfirmed) SchemaVersion() int { return 1 }

// AppointmentAlternativeProposed: la clínica propone otra franja; reserva
// provisional hasta ExpiresAt (48 h).
type AppointmentAlternativeProposed struct {
	ApptRef
	ProfessionalID          uuid.UUID `json:"professional_id"`
	Slot                    Slot      `json:"slot"`
	TZ                      string    `json:"tz"`
	CancellationPolicyHours int       `json:"cancellation_policy_hours"`
	ExpiresAt               time.Time `json:"expires_at"`
	By                      uuid.UUID `json:"by"`
	At                      time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentAlternativeProposed) EventName() string { return "AppointmentAlternativeProposed" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentAlternativeProposed) SchemaVersion() int { return 1 }

// AppointmentRejected: rechazo de la clínica, o expiración de la propuesta
// (Reason = "PROPOSAL_EXPIRED", By = nil-uuid: lo hace el sistema).
type AppointmentRejected struct {
	ApptRef
	Reason string    `json:"reason"`
	By     uuid.UUID `json:"by"`
	At     time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentRejected) EventName() string { return "AppointmentRejected" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentRejected) SchemaVersion() int { return 1 }

// AppointmentRescheduled: nueva franja (y opcionalmente profesional).
type AppointmentRescheduled struct {
	ApptRef
	PreviousSlot   Slot      `json:"previous_slot"`
	Slot           Slot      `json:"slot"`
	ProfessionalID uuid.UUID `json:"professional_id"`
	TZ             string    `json:"tz"`
	By             uuid.UUID `json:"by"`
	At             time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentRescheduled) EventName() string { return "AppointmentRescheduled" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentRescheduled) SchemaVersion() int { return 1 }

// AppointmentCancelled: cancelación; Late si el tutor cancela dentro de la
// política de la clínica.
type AppointmentCancelled struct {
	ApptRef
	Reason string      `json:"reason,omitempty"`
	By     CancelledBy `json:"by"`
	Late   bool        `json:"late"`
	ByUser uuid.UUID   `json:"by_user"`
	At     time.Time   `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentCancelled) EventName() string { return "AppointmentCancelled" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentCancelled) SchemaVersion() int { return 1 }

// AppointmentCheckedIn: el paciente llegó.
type AppointmentCheckedIn struct {
	ApptRef
	By uuid.UUID `json:"by"`
	At time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentCheckedIn) EventName() string { return "AppointmentCheckedIn" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentCheckedIn) SchemaVersion() int { return 1 }

// AppointmentNoShow: no hubo check-in tras la hora de fin.
type AppointmentNoShow struct {
	ApptRef
	Automatic bool      `json:"automatic"`
	At        time.Time `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentNoShow) EventName() string { return "AppointmentNoShow" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentNoShow) SchemaVersion() int { return 1 }

// AppointmentCompleted: cita cumplida (al firmar la consulta enlazada o a mano).
type AppointmentCompleted struct {
	ApptRef
	EncounterID *uuid.UUID `json:"encounter_id,omitempty"`
	At          time.Time  `json:"at"`
}

// EventName implementa eventstore.Event.
func (AppointmentCompleted) EventName() string { return "AppointmentCompleted" }

// SchemaVersion implementa eventstore.Event.
func (AppointmentCompleted) SchemaVersion() int { return 1 }
