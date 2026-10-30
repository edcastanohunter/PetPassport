package app

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
)

// Comandos del bus de Scheduling. Los de la API los construye el handler
// http; los tres últimos de cada bloque los programa el scheduler o un
// process manager (Principal = system) y por eso son serializables y están en
// scheduler.AllowedCommands. Un comando toca un solo agregado.

// RequestAppointment: el tutor solicita una cita.
type RequestAppointment struct {
	AppointmentID  uuid.UUID
	PetID          uuid.UUID
	OrganizationID uuid.UUID
	Type           string
	Reason         string
	Preferences    []domain.Preference
}

// CommandName implementa bus.Command.
func (RequestAppointment) CommandName() string { return "scheduling.RequestAppointment" }

// CreateAppointment: la clínica crea una cita directamente confirmada.
type CreateAppointment struct {
	AppointmentID  uuid.UUID
	PetID          uuid.UUID
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Type           string
	Reason         string
	Start, End     time.Time
}

// CommandName implementa bus.Command.
func (CreateAppointment) CommandName() string { return "scheduling.CreateAppointment" }

// ConfirmAppointment: la clínica confirma una solicitud.
type ConfirmAppointment struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Start, End     time.Time
}

// CommandName implementa bus.Command.
func (ConfirmAppointment) CommandName() string { return "scheduling.ConfirmAppointment" }

// ProposeAlternative: la clínica propone otra franja.
type ProposeAlternative struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Start, End     time.Time
}

// CommandName implementa bus.Command.
func (ProposeAlternative) CommandName() string { return "scheduling.ProposeAlternative" }

// AcceptProposal: el tutor acepta la alternativa.
type AcceptProposal struct{ AppointmentID uuid.UUID }

// CommandName implementa bus.Command.
func (AcceptProposal) CommandName() string { return "scheduling.AcceptProposal" }

// RejectRequest: la clínica rechaza una solicitud o propuesta.
type RejectRequest struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (RejectRequest) CommandName() string { return "scheduling.RejectRequest" }

// RescheduleAppointment: la clínica mueve una cita confirmada.
type RescheduleAppointment struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
	ProfessionalID *uuid.UUID
	Start, End     time.Time
}

// CommandName implementa bus.Command.
func (RescheduleAppointment) CommandName() string { return "scheduling.RescheduleAppointment" }

// CancelAppointment: cancela el tutor (OrganizationID nil) o la clínica.
type CancelAppointment struct {
	AppointmentID  uuid.UUID
	OrganizationID *uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (CancelAppointment) CommandName() string { return "scheduling.CancelAppointment" }

// CheckInAppointment registra la llegada.
type CheckInAppointment struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
}

// CommandName implementa bus.Command.
func (CheckInAppointment) CommandName() string { return "scheduling.CheckInAppointment" }

// MarkAppointmentNoShow marca la ausencia (manual).
type MarkAppointmentNoShow struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
}

// CommandName implementa bus.Command.
func (MarkAppointmentNoShow) CommandName() string { return "scheduling.MarkAppointmentNoShow" }

// CompleteAppointment completa la cita a mano.
type CompleteAppointment struct {
	AppointmentID  uuid.UUID
	OrganizationID uuid.UUID
}

// CommandName implementa bus.Command.
func (CompleteAppointment) CommandName() string { return "scheduling.CompleteAppointment" }

// --- comandos del sistema (programables: JSON estable) ------------------

// ExpireProposal caduca una propuesta a las 48 h y libera la reserva
// provisional. Idempotente.
type ExpireProposal struct {
	AppointmentID uuid.UUID `json:"appointment_id"`
}

// CommandName implementa bus.Command.
func (ExpireProposal) CommandName() string { return "scheduling.ExpireProposal" }

// AutoNoShow marca no-show a las 24 h del fin si no hubo check-in. Idempotente.
type AutoNoShow struct {
	AppointmentID uuid.UUID `json:"appointment_id"`
}

// CommandName implementa bus.Command.
func (AutoNoShow) CommandName() string { return "scheduling.AutoNoShow" }

// CompleteOnSign completa la cita al firmarse la consulta enlazada
// (process manager scheduling.complete-on-sign, DOM-015). Idempotente.
type CompleteOnSign struct {
	AppointmentID uuid.UUID `json:"appointment_id"`
	EncounterID   uuid.UUID `json:"encounter_id"`
}

// CommandName implementa bus.Command.
func (CompleteOnSign) CommandName() string { return "scheduling.CompleteOnSign" }

// --- agendas ------------------------------------------------------------

// SaveAgenda crea la agenda o actualiza lo que cambió (PUT de la API).
type SaveAgenda struct {
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Config         domain.AgendaConfig
	Active         bool
}

// CommandName implementa bus.Command.
func (SaveAgenda) CommandName() string { return "scheduling.SaveAgenda" }

// AddAgendaException añade o reemplaza la excepción de un día.
type AddAgendaException struct {
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Date           string
	Exception      domain.DayException
}

// CommandName implementa bus.Command.
func (AddAgendaException) CommandName() string { return "scheduling.AddAgendaException" }

// RemoveAgendaException elimina la excepción de un día.
type RemoveAgendaException struct {
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Date           string
}

// CommandName implementa bus.Command.
func (RemoveAgendaException) CommandName() string { return "scheduling.RemoveAgendaException" }
