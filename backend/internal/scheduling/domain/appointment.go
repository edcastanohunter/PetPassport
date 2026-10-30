package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AppointmentStreamType es el tipo de stream de la cita (appt-<AppointmentID>).
const AppointmentStreamType = "Appointment"

// Appointment es la raíz de agregado de una cita (PP-F2-DOM-010). Se
// rehidrata por replay (zero value + ApplyEvent). No-solapamiento (A5) NO se
// decide aquí: lo garantiza la restricción de exclusión de
// slot_reservations en la misma transacción (ADR-011); el agregado solo
// valida lo que ve su propio estado.
type Appointment struct {
	id             uuid.UUID
	version        int64
	petID          uuid.UUID
	guardianID     uuid.UUID
	organizationID uuid.UUID
	professionalID *uuid.UUID
	apptType       AppointmentType
	origin         Origin
	status         Status
	reason         string
	preferences    []Preference
	slot           *Slot
	tz             string
	policyHours    int
	proposalExpiry *time.Time
	proposed       *AppointmentAlternativeProposed
	encounterID    *uuid.UUID
	cancellation   *Cancellation
	history        []Transition
	uncommitted    []any
}

// ID devuelve el id de la cita.
func (a *Appointment) ID() uuid.UUID { return a.id }

// Version devuelve la versión del stream.
func (a *Appointment) Version() int64 { return a.version }

// PetID devuelve la mascota.
func (a *Appointment) PetID() uuid.UUID { return a.petID }

// GuardianID devuelve el tutor de la mascota al crear la cita.
func (a *Appointment) GuardianID() uuid.UUID { return a.guardianID }

// OrganizationID devuelve la clínica.
func (a *Appointment) OrganizationID() uuid.UUID { return a.organizationID }

// ProfessionalID devuelve el profesional asignado (nil hasta confirmar).
func (a *Appointment) ProfessionalID() *uuid.UUID { return a.professionalID }

// Status devuelve el estado.
func (a *Appointment) Status() Status { return a.status }

// Type devuelve el tipo de cita.
func (a *Appointment) Type() AppointmentType { return a.apptType }

// Slot devuelve la franja vigente (nil si aún no hay).
func (a *Appointment) Slot() *Slot { return a.slot }

// TZ devuelve la zona horaria de la organización al confirmar.
func (a *Appointment) TZ() string { return a.tz }

// ProposedSlot devuelve la franja propuesta pendiente de aceptar.
func (a *Appointment) ProposedSlot() (AppointmentAlternativeProposed, bool) {
	if a.status != StatusProposed || a.proposed == nil {
		return AppointmentAlternativeProposed{}, false
	}
	return *a.proposed, true
}

// EncounterID devuelve la consulta enlazada.
func (a *Appointment) EncounterID() *uuid.UUID { return a.encounterID }

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (a *Appointment) UncommittedEvents() []any { return a.uncommitted }

// MarkCommitted limpia los eventos tras el Append.
func (a *Appointment) MarkCommitted() { a.uncommitted = nil }

func (a *Appointment) ref() ApptRef {
	return ApptRef{AppointmentID: a.id, PetID: a.petID, OrganizationID: a.organizationID}
}

func (a *Appointment) record(evt any) {
	if err := a.ApplyEvent(evt); err != nil {
		panic("scheduling: aplicar evento de cita recién emitido: " + err.Error())
	}
	a.uncommitted = append(a.uncommitted, evt)
}

func (a *Appointment) push(s Status, at time.Time, by, note string) {
	a.status = s
	a.history = append(a.history, Transition{Status: s, At: at, By: by, Note: note})
}

func userStr(id uuid.UUID) string {
	if id == uuid.Nil {
		return "system"
	}
	return id.String()
}

// ApplyEvent rehidrata el agregado.
func (a *Appointment) ApplyEvent(evt any) error {
	switch ev := evt.(type) {
	case AppointmentRequested:
		a.initFrom(ev.ApptRef, ev.GuardianID, ev.Type, OriginGuardian, ev.Reason)
		a.preferences = ev.Preferences
		a.push(StatusRequested, ev.At, userStr(ev.RequestedBy), "")
	case AppointmentCreated:
		a.initFrom(ev.ApptRef, ev.GuardianID, ev.Type, OriginClinic, ev.Reason)
		a.history = append(a.history, Transition{Status: StatusRequested, At: ev.At, By: userStr(ev.CreatedBy), Note: "created"})
	case AppointmentConfirmed:
		prof, slot := ev.ProfessionalID, ev.Slot
		a.professionalID, a.slot, a.tz, a.policyHours = &prof, &slot, ev.TZ, ev.CancellationPolicyHours
		a.preferences, a.proposalExpiry, a.proposed = nil, nil, nil
		a.push(StatusConfirmed, ev.At, userStr(ev.By), "")
	case AppointmentAlternativeProposed:
		prof, slot, exp := ev.ProfessionalID, ev.Slot, ev.ExpiresAt
		a.professionalID, a.slot, a.tz, a.policyHours = &prof, &slot, ev.TZ, ev.CancellationPolicyHours
		a.proposalExpiry = &exp
		cp := ev
		a.proposed = &cp
		a.push(StatusProposed, ev.At, userStr(ev.By), "")
	case AppointmentRejected:
		a.proposalExpiry, a.proposed = nil, nil
		a.push(StatusRejected, ev.At, userStr(ev.By), ev.Reason)
	case AppointmentRescheduled:
		prof, slot := ev.ProfessionalID, ev.Slot
		a.professionalID, a.slot, a.tz = &prof, &slot, ev.TZ
		a.push(StatusConfirmed, ev.At, userStr(ev.By), "rescheduled")
	case AppointmentCancelled:
		a.cancellation = &Cancellation{By: ev.By, Reason: ev.Reason, Late: ev.Late}
		a.proposalExpiry, a.proposed = nil, nil
		a.push(StatusCancelled, ev.At, userStr(ev.ByUser), ev.Reason)
	case AppointmentCheckedIn:
		a.push(StatusCheckedIn, ev.At, userStr(ev.By), "")
	case AppointmentNoShow:
		a.push(StatusNoShow, ev.At, "", "")
	case AppointmentCompleted:
		a.encounterID = ev.EncounterID
		a.push(StatusCompleted, ev.At, "", "")
	default:
		return kernel.NewDomainError("UNKNOWN_EVENT", "evento desconocido para Appointment")
	}
	a.version++
	return nil
}

func (a *Appointment) initFrom(r ApptRef, guardian uuid.UUID, t AppointmentType, o Origin, reason string) {
	a.id, a.petID, a.organizationID = r.AppointmentID, r.PetID, r.OrganizationID
	a.guardianID, a.apptType, a.origin, a.reason = guardian, t, o, reason
}

// ---------------------------------------------------------------- comandos

// RequestAppointment es la solicitud del tutor (A1, A2).
type RequestAppointment struct {
	AppointmentID  uuid.UUID
	PetID          uuid.UUID
	GuardianID     uuid.UUID
	OrganizationID uuid.UUID
	Type           string
	Reason         string
	Preferences    []Preference
	Linked         bool          // A1: vínculo activo con la organización activa
	Pending        int           // A2: solicitudes requested de la mascota en la organización
	LeadMin        time.Duration // antelación mínima de las agendas de la organización
	LeadMax        time.Duration
	RequestedBy    uuid.UUID
	Now            time.Time
}

// CreateAppointment es la cita directa de la clínica (nace confirmada).
type CreateAppointment struct {
	AppointmentID  uuid.UUID
	PetID          uuid.UUID
	GuardianID     uuid.UUID
	OrganizationID uuid.UUID
	ProfessionalID uuid.UUID
	Type           string
	Reason         string
	Start, End     time.Time
	TZ             string
	PolicyHours    int
	Linked         bool // A3
	ProfessionalOK bool // A4
	CreatedBy      uuid.UUID
	Now            time.Time
}

// ConfirmAppointment confirma una solicitud asignando profesional y franja.
type ConfirmAppointment struct {
	ProfessionalID uuid.UUID
	Start, End     time.Time
	TZ             string
	PolicyHours    int
	ProfessionalOK bool
	By             uuid.UUID
	Now            time.Time
}

// ProposeAlternative propone otra franja (reserva provisional 48 h).
type ProposeAlternative struct {
	ProfessionalID uuid.UUID
	Start, End     time.Time
	TZ             string
	PolicyHours    int
	ProfessionalOK bool
	By             uuid.UUID
	Now            time.Time
}

// AcceptProposal es la aceptación del tutor.
type AcceptProposal struct {
	By  uuid.UUID
	Now time.Time
}

// ExpireProposal lo dispara el scheduler a las 48 h; idempotente.
type ExpireProposal struct{ Now time.Time }

// RejectRequest rechaza una solicitud o propuesta.
type RejectRequest struct {
	Reason string
	By     uuid.UUID
	Now    time.Time
}

// Reschedule mueve una cita confirmada.
type Reschedule struct {
	ProfessionalID *uuid.UUID // nil = mismo profesional
	Start, End     time.Time
	TZ             string
	ProfessionalOK bool
	By             uuid.UUID
	Now            time.Time
}

// Cancel cancela una cita no iniciada.
type Cancel struct {
	Reason string
	By     CancelledBy
	ByUser uuid.UUID
	Now    time.Time
}

// CheckIn registra la llegada (A6).
type CheckIn struct {
	By  uuid.UUID
	Now time.Time
}

// MarkNoShow marca la ausencia. Automatic lo envía el scheduler: si la cita
// ya no está confirmada o aún no pasó el margen, no hace nada.
type MarkNoShow struct {
	Automatic bool
	Now       time.Time
}

// Complete completa la cita; idempotente si ya está completada.
type Complete struct {
	EncounterID *uuid.UUID
	Now         time.Time
}

// HandleCommand ejecuta un comando y devuelve los eventos emitidos (ya
// aplicados). Un comando que no cambia nada devuelve cero eventos.
func (a *Appointment) HandleCommand(cmd any) ([]any, error) {
	before := len(a.uncommitted)
	var err error
	switch x := cmd.(type) {
	case RequestAppointment:
		err = a.handleRequest(x)
	case CreateAppointment:
		err = a.handleCreate(x)
	case ConfirmAppointment:
		err = a.handleConfirm(x)
	case ProposeAlternative:
		err = a.handlePropose(x)
	case AcceptProposal:
		err = a.handleAccept(x)
	case ExpireProposal:
		err = a.handleExpire(x)
	case RejectRequest:
		err = a.handleReject(x)
	case Reschedule:
		err = a.handleReschedule(x)
	case Cancel:
		err = a.handleCancel(x)
	case CheckIn:
		err = a.handleCheckIn(x)
	case MarkNoShow:
		err = a.handleNoShow(x)
	case Complete:
		err = a.handleComplete(x)
	default:
		err = kernel.NewDomainError("UNKNOWN_COMMAND", "comando desconocido para Appointment")
	}
	if err != nil {
		return nil, err
	}
	return a.uncommitted[before:], nil
}

func validReason(reason string, ve *kernel.ValidationError) string {
	r := strings.TrimSpace(reason)
	if r == "" || utf8.RuneCountInString(r) > MaxReasonRunes {
		ve.Add("reason", "INVALID")
	}
	return r
}

func (a *Appointment) handleRequest(c RequestAppointment) error {
	if a.version != 0 {
		return ErrInvalidTransition
	}
	ve := &kernel.ValidationError{}
	t, err := NewAppointmentType(c.Type)
	if err != nil {
		ve.Add("type", "INVALID")
	}
	reason := validReason(c.Reason, ve)
	if len(c.Preferences) == 0 || len(c.Preferences) > MaxPreferences {
		ve.Add("preferences", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	if !c.Linked {
		return ErrNotLinked
	}
	if c.Pending >= MaxPendingRequests {
		return ErrRequestLimit
	}
	min, max := c.Now.Add(c.LeadMin), c.Now.Add(c.LeadMax)
	for _, p := range c.Preferences {
		if err := checkPreference(p, min, max); err != nil {
			if errors.Is(err, ErrOutsideLeadTime) {
				return err
			}
			ve.Add("preferences", "INVALID")
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	a.record(AppointmentRequested{
		ApptRef:    ApptRef{AppointmentID: c.AppointmentID, PetID: c.PetID, OrganizationID: c.OrganizationID},
		GuardianID: c.GuardianID, Type: t, Reason: reason, Preferences: c.Preferences,
		RequestedBy: c.RequestedBy, At: c.Now,
	})
	return nil
}

// checkPreference valida una preferencia contra la ventana de antelación.
func checkPreference(p Preference, min, max time.Time) error {
	switch {
	case p.Start != nil && p.End != nil && p.DateFrom == nil && p.DateTo == nil:
		if _, err := NewSlot(*p.Start, *p.End); err != nil {
			return err
		}
		if p.Start.Before(min) || p.Start.After(max) {
			return ErrOutsideLeadTime
		}
	case p.DateFrom != nil && p.DateTo != nil && p.Start == nil && p.End == nil:
		if p.DateTo.Before(*p.DateFrom) || p.DateTo.Time().Sub(p.DateFrom.Time()) > 60*24*time.Hour {
			return kernel.NewDomainError("INVALID_PREFERENCE", "rango de fechas inválido")
		}
		if p.DateTo.Time().Add(24*time.Hour).Before(min) || p.DateFrom.Time().After(max) {
			return ErrOutsideLeadTime
		}
	default:
		return kernel.NewDomainError("INVALID_PREFERENCE", "preferencia inválida")
	}
	return nil
}

func (a *Appointment) handleCreate(c CreateAppointment) error {
	if a.version != 0 {
		return ErrInvalidTransition
	}
	ve := &kernel.ValidationError{}
	t, err := NewAppointmentType(c.Type)
	if err != nil {
		ve.Add("type", "INVALID")
	}
	reason := validReason(c.Reason, ve)
	slot, err := NewSlot(c.Start, c.End)
	if err != nil {
		ve.Add("slot", "INVALID")
	} else if !slot.End.After(c.Now) {
		ve.Add("slot", "IN_THE_PAST")
	}
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	if !c.Linked {
		return ErrNotLinked
	}
	if !c.ProfessionalOK {
		return ErrInvalidProfessional
	}
	ref := ApptRef{AppointmentID: c.AppointmentID, PetID: c.PetID, OrganizationID: c.OrganizationID}
	a.record(AppointmentCreated{ApptRef: ref, GuardianID: c.GuardianID, Type: t, Reason: reason, CreatedBy: c.CreatedBy, At: c.Now})
	a.record(AppointmentConfirmed{
		ApptRef: ref, ProfessionalID: c.ProfessionalID, Slot: slot, TZ: c.TZ,
		CancellationPolicyHours: c.PolicyHours, By: c.CreatedBy, At: c.Now,
	})
	return nil
}

func (a *Appointment) handleConfirm(c ConfirmAppointment) error {
	if a.status != StatusRequested {
		return ErrInvalidTransition
	}
	slot, err := a.futureSlot(c.Start, c.End, c.Now)
	if err != nil {
		return err
	}
	if !c.ProfessionalOK {
		return ErrInvalidProfessional
	}
	a.record(AppointmentConfirmed{
		ApptRef: a.ref(), ProfessionalID: c.ProfessionalID, Slot: slot, TZ: c.TZ,
		CancellationPolicyHours: c.PolicyHours, By: c.By, At: c.Now,
	})
	return nil
}

func (a *Appointment) futureSlot(start, end, now time.Time) (Slot, error) {
	slot, err := NewSlot(start, end)
	if err != nil {
		ve := &kernel.ValidationError{}
		ve.Add("slot", "INVALID")
		return Slot{}, ve
	}
	if !slot.End.After(now) {
		ve := &kernel.ValidationError{}
		ve.Add("slot", "IN_THE_PAST")
		return Slot{}, ve
	}
	return slot, nil
}

func (a *Appointment) handlePropose(c ProposeAlternative) error {
	if a.status != StatusRequested {
		return ErrInvalidTransition
	}
	slot, err := a.futureSlot(c.Start, c.End, c.Now)
	if err != nil {
		return err
	}
	if !c.ProfessionalOK {
		return ErrInvalidProfessional
	}
	a.record(AppointmentAlternativeProposed{
		ApptRef: a.ref(), ProfessionalID: c.ProfessionalID, Slot: slot, TZ: c.TZ,
		CancellationPolicyHours: c.PolicyHours, ExpiresAt: c.Now.Add(ProposalTTL), By: c.By, At: c.Now,
	})
	return nil
}

func (a *Appointment) handleAccept(c AcceptProposal) error {
	if a.status != StatusProposed || a.proposed == nil {
		return ErrInvalidTransition
	}
	if !c.Now.Before(*a.proposalExpiry) {
		return ErrProposalExpired
	}
	p := a.proposed
	a.record(AppointmentConfirmed{
		ApptRef: a.ref(), ProfessionalID: p.ProfessionalID, Slot: p.Slot, TZ: p.TZ,
		CancellationPolicyHours: p.CancellationPolicyHours, By: c.By, At: c.Now,
	})
	return nil
}

func (a *Appointment) handleExpire(c ExpireProposal) error {
	if a.status != StatusProposed || a.proposalExpiry == nil || c.Now.Before(*a.proposalExpiry) {
		return nil // idempotente: nada que expirar
	}
	a.record(AppointmentRejected{ApptRef: a.ref(), Reason: "PROPOSAL_EXPIRED", At: c.Now})
	return nil
}

func (a *Appointment) handleReject(c RejectRequest) error {
	if a.status != StatusRequested && a.status != StatusProposed {
		return ErrInvalidTransition
	}
	ve := &kernel.ValidationError{}
	reason := validReason(c.Reason, ve)
	if err := ve.ErrOrNil(); err != nil {
		return err
	}
	a.record(AppointmentRejected{ApptRef: a.ref(), Reason: reason, By: c.By, At: c.Now})
	return nil
}

func (a *Appointment) handleReschedule(c Reschedule) error {
	if a.status != StatusConfirmed || a.slot == nil || a.professionalID == nil {
		return ErrInvalidTransition
	}
	slot, err := a.futureSlot(c.Start, c.End, c.Now)
	if err != nil {
		return err
	}
	if !c.ProfessionalOK {
		return ErrInvalidProfessional
	}
	prof := *a.professionalID
	if c.ProfessionalID != nil {
		prof = *c.ProfessionalID
	}
	a.record(AppointmentRescheduled{
		ApptRef: a.ref(), PreviousSlot: *a.slot, Slot: slot, ProfessionalID: prof, TZ: c.TZ, By: c.By, At: c.Now,
	})
	return nil
}

func (a *Appointment) handleCancel(c Cancel) error {
	switch a.status {
	case StatusRequested, StatusProposed, StatusConfirmed:
	default:
		return ErrInvalidTransition
	}
	if c.By != ByGuardian && c.By != ByClinic {
		ve := &kernel.ValidationError{}
		ve.Add("by", "INVALID")
		return ve
	}
	reason := strings.TrimSpace(c.Reason)
	if utf8.RuneCountInString(reason) > MaxReasonRunes || (c.By == ByClinic && reason == "") {
		ve := &kernel.ValidationError{}
		ve.Add("reason", "INVALID")
		return ve
	}
	late := c.By == ByGuardian && a.status == StatusConfirmed && a.slot != nil &&
		a.slot.Start.Sub(c.Now) < time.Duration(a.policyHours)*time.Hour
	a.record(AppointmentCancelled{ApptRef: a.ref(), Reason: reason, By: c.By, Late: late, ByUser: c.ByUser, At: c.Now})
	return nil
}

func (a *Appointment) handleCheckIn(c CheckIn) error {
	if a.status != StatusConfirmed || a.slot == nil {
		return ErrInvalidTransition
	}
	if c.Now.Before(a.slot.Start.Add(-CheckInLead)) || !sameLocalDay(c.Now, a.slot.Start, a.tz) {
		return ErrCheckInWindow
	}
	a.record(AppointmentCheckedIn{ApptRef: a.ref(), By: c.By, At: c.Now})
	return nil
}

func sameLocalDay(a, b time.Time, tz string) bool {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

func (a *Appointment) handleNoShow(c MarkNoShow) error {
	if a.status != StatusConfirmed || a.slot == nil {
		if c.Automatic {
			return nil
		}
		return ErrInvalidTransition
	}
	limit := a.slot.End
	if c.Automatic {
		limit = limit.Add(NoShowAfter)
	}
	if !c.Now.After(limit) {
		if c.Automatic {
			return nil
		}
		return ErrInvalidTransition
	}
	a.record(AppointmentNoShow{ApptRef: a.ref(), Automatic: c.Automatic, At: c.Now})
	return nil
}

func (a *Appointment) handleComplete(c Complete) error {
	switch a.status {
	case StatusCompleted:
		return nil // idempotente (reentrega del PM)
	case StatusCheckedIn:
	case StatusConfirmed:
		if c.EncounterID == nil {
			return ErrInvalidTransition
		}
	default:
		return ErrInvalidTransition
	}
	a.record(AppointmentCompleted{ApptRef: a.ref(), EncounterID: c.EncounterID, At: c.Now})
	return nil
}

// Preferences devuelve las preferencias del tutor (solo en requested).
func (a *Appointment) Preferences() []Preference { return a.preferences }

// Reason devuelve el motivo.
func (a *Appointment) Reason() string { return a.reason }

// History devuelve el historial de transiciones.
func (a *Appointment) History() []Transition { return a.history }

// Cancellation devuelve los datos de cancelación, si los hay.
func (a *Appointment) Cancellation() *Cancellation { return a.cancellation }

// PolicyHours devuelve la política de cancelación copiada al confirmar.
func (a *Appointment) PolicyHours() int { return a.policyHours }

// ProposalExpiresAt devuelve el vencimiento de la propuesta vigente.
func (a *Appointment) ProposalExpiresAt() *time.Time { return a.proposalExpiry }

// Origin devuelve quién originó la cita.
func (a *Appointment) Origin() Origin { return a.origin }
