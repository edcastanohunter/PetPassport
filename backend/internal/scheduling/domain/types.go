// Package domain es el dominio de Scheduling (PP-F2-DOM-010/011/012,
// docs/phase-2/02 §4): Appointment y ProfessionalAgenda, ambos event-sourced.
// Sin I/O: lo que depende de otros contextos (vínculo, membresía, reserva de
// franja) lo resuelve app y llega a los agregados como valor.
package domain

import (
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AppointmentType es el tipo de cita.
type AppointmentType string

// Tipos de cita (02 §4.1).
const (
	TypeConsultation AppointmentType = "consultation"
	TypeFollowUp     AppointmentType = "follow_up"
	TypeVaccination  AppointmentType = "vaccination"
	TypeSurgery      AppointmentType = "surgery"
	TypeProcedure    AppointmentType = "procedure"
	TypeOther        AppointmentType = "other"
)

// AppointmentTypes lista los tipos válidos.
var AppointmentTypes = []AppointmentType{TypeConsultation, TypeFollowUp, TypeVaccination, TypeSurgery, TypeProcedure, TypeOther}

// NewAppointmentType valida el tipo.
func NewAppointmentType(s string) (AppointmentType, error) {
	for _, t := range AppointmentTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", kernel.NewDomainError("INVALID_TYPE", "tipo de cita inválido")
}

// Status es el estado de una cita.
type Status string

// Estados (A8: solo las transiciones de la tabla).
const (
	StatusRequested Status = "requested"
	StatusProposed  Status = "proposed"
	StatusConfirmed Status = "confirmed"
	StatusCheckedIn Status = "checked_in"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusNoShow    Status = "no_show"
	StatusRejected  Status = "rejected"
)

// Origin es quién originó la cita.
type Origin string

// Orígenes.
const (
	OriginGuardian Origin = "guardian"
	OriginClinic   Origin = "clinic"
)

// CancelledBy es quién cancela.
type CancelledBy string

// Cancelaciones.
const (
	ByGuardian CancelledBy = "guardian"
	ByClinic   CancelledBy = "clinic"
)

// Límites de la cita (A2, A7, 01 PRD-017).
const (
	MinSlotMinutes     = 5
	MaxSlotMinutes     = 480
	MaxPreferences     = 3
	MaxPendingRequests = 3
	MaxReasonRunes     = 300
	// CheckInLead es cuánto antes del inicio se permite el check-in (A6).
	CheckInLead = 2 * time.Hour
	// ProposalTTL es la vigencia de una propuesta de alternativa (48 h).
	ProposalTTL = 48 * time.Hour
	// NoShowAfter es el margen del no-show automático tras el fin (24 h).
	NoShowAfter = 24 * time.Hour
)

// Slot es una franja [Start, End) en UTC.
type Slot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// NewSlot valida A7: End > Start y duración entre 5 y 480 min.
func NewSlot(start, end time.Time) (Slot, error) {
	if !end.After(start) {
		return Slot{}, kernel.NewDomainError("INVALID_SLOT", "la franja debe terminar después de empezar")
	}
	d := end.Sub(start)
	if d < MinSlotMinutes*time.Minute || d > MaxSlotMinutes*time.Minute {
		return Slot{}, kernel.NewDomainError("INVALID_SLOT", "la duración de la franja debe estar entre 5 y 480 minutos")
	}
	return Slot{Start: start.UTC(), End: end.UTC()}, nil
}

// Overlaps indica si dos franjas se solapan (semiabiertas).
func (s Slot) Overlaps(o Slot) bool { return s.Start.Before(o.End) && o.Start.Before(s.End) }

// Preference es una preferencia del tutor: una franja concreta o un rango de
// fechas ("cualquier hora").
type Preference struct {
	Start    *time.Time        `json:"start,omitempty"`
	End      *time.Time        `json:"end,omitempty"`
	DateFrom *kernel.CivilDate `json:"date_from,omitempty"`
	DateTo   *kernel.CivilDate `json:"date_to,omitempty"`
}

// Transition es una entrada del historial de la cita (UI).
type Transition struct {
	Status Status    `json:"status"`
	At     time.Time `json:"at"`
	By     string    `json:"by,omitempty"`
	Note   string    `json:"note,omitempty"`
}

// Cancellation son los datos de una cancelación.
type Cancellation struct {
	By     CancelledBy `json:"by"`
	Reason string      `json:"reason,omitempty"`
	Late   bool        `json:"late"`
}

// Errores de Scheduling (catálogo PP-F2-API-012).
var (
	ErrInvalidTransition   = kernel.NewDomainError("INVALID_TRANSITION", "El estado de la cita no admite esta acción.")
	ErrSlotUnavailable     = kernel.NewDomainError("SLOT_UNAVAILABLE", "La franja no está disponible.")
	ErrOutsideLeadTime     = kernel.NewDomainError("OUTSIDE_LEAD_TIME", "Las preferencias están fuera de la antelación permitida.")
	ErrRequestLimit        = kernel.NewDomainError("REQUEST_LIMIT", "Hay demasiadas solicitudes pendientes para esta mascota y clínica.")
	ErrProposalExpired     = kernel.NewDomainError("PROPOSAL_EXPIRED", "La propuesta de alternativa caducó.")
	ErrCheckInWindow       = kernel.NewDomainError("CHECKIN_WINDOW", "El check-in solo se permite el día de la cita, desde 2 horas antes.")
	ErrNotLinked           = kernel.NewDomainError("PET_NOT_ACCESSIBLE", "La clínica no tiene vínculo activo con la mascota.")
	ErrInvalidProfessional = kernel.NewDomainError("INVALID_PROFESSIONAL", "El profesional debe ser un veterinario activo de la organización.")
)
