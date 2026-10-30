package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/templates"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Roles destinatarios de los avisos de organización.
var (
	orgAdminRoles       = []string{"clinic_admin", "pharmacy_admin"}
	appointmentInboxers = []string{"clinic_admin", "receptionist"}
)

// Nombres de regla (parte `rule` de las claves de idempotencia). Estables.
const (
	RuleOrgStatus             = "org_status"
	RuleAppointmentRequested  = "appointment_requested"
	RuleAppointmentConfirmed  = "appointment_confirmed"
	RuleAppointmentReminder   = "appointment_reminder"
	RuleAppointmentProposed   = "appointment_proposed"
	RuleAppointmentRescheduld = "appointment_rescheduled"
	RuleAppointmentCancelled  = "appointment_cancelled"
	RuleAppointmentRejected   = "appointment_rejected"
	RuleEncounterSigned       = "encounter_signed"
	RuleEncounterAddendum     = "encounter_addendum"
	RuleFollowUpReminder      = "follow_up_reminder"
	RuleVaccineRecorded       = "vaccine_recorded"
	RuleVaccineReminder       = "vaccine_reminder"
	RuleClinicSuspended       = "clinic_suspended_notice"
	RuleTransferDone          = "transfer_done"
	RuleClinicDailyAgenda     = "clinic_daily_agenda"
)

// HandledEvents son los eventos a los que suscribe el PM notifications.rules.
// MemberInvited y GuardianshipTransferInitiated NO están: su correo lleva un
// token de un solo uso que no viaja en el evento (solo su hash); esos envíos
// se encolan desde el comando con notify.Enqueuer (ADR-026 propuesta).
// CareLinkRequested y CareLinkRequest{Approved,Declined} llegan con M2.5.
var HandledEvents = []string{
	"OrganizationVerified", "OrganizationRejected", "OrganizationSuspended",
	"AppointmentRequested", "AppointmentConfirmed", "AppointmentAlternativeProposed",
	"AppointmentRescheduled", "AppointmentCancelled", "AppointmentRejected",
	"AppointmentCheckedIn", "AppointmentNoShow", "AppointmentCompleted",
	"EncounterSigned", "EncounterAddendumAdded",
	"VaccinationEntryRecorded", "PreventiveEntryRecorded", "NextDueAdjusted", "VaccinationEntryVoided",
	"PetMarkedDeceased", "PetGuardianshipTransferred",
}

// Planner traduce un evento del outbox en acciones. Es puro respecto a la
// base de datos: lee por Directory y devuelve las acciones sin ejecutarlas.
type Planner struct {
	dir   Directory
	clock kernel.Clock
}

// NewPlanner construye el planificador.
func NewPlanner(dir Directory, clock kernel.Clock) *Planner {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &Planner{dir: dir, clock: clock}
}

// Plan devuelve las acciones del evento. Un evento sin reglas devuelve nil.
func (p *Planner) Plan(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	switch e.Type {
	case "OrganizationVerified", "OrganizationRejected", "OrganizationSuspended":
		return p.orgStatus(ctx, e)
	case "AppointmentRequested":
		return p.appointmentRequested(ctx, e)
	case "AppointmentConfirmed":
		return p.appointmentConfirmed(ctx, e)
	case "AppointmentAlternativeProposed":
		return p.appointmentProposed(ctx, e)
	case "AppointmentRescheduled":
		return p.appointmentRescheduled(ctx, e)
	case "AppointmentCancelled":
		return p.appointmentCancelled(ctx, e)
	case "AppointmentRejected":
		return p.appointmentRejected(ctx, e)
	case "AppointmentCheckedIn", "AppointmentNoShow", "AppointmentCompleted":
		var ev apptRef
		if err := decode(e, &ev); err != nil {
			return nil, err
		}
		return []Action{CancelSubject{SubjectType: SubjectAppointment, SubjectID: ev.AppointmentID}}, nil
	case "EncounterSigned":
		return p.encounterSigned(ctx, e)
	case "EncounterAddendumAdded":
		return p.encounterAddendum(ctx, e)
	case "VaccinationEntryRecorded", "PreventiveEntryRecorded":
		return p.entryRecorded(ctx, e)
	case "NextDueAdjusted":
		return p.nextDueAdjusted(ctx, e)
	case "VaccinationEntryVoided":
		var ev struct {
			EntryID uuid.UUID `json:"entry_id"`
		}
		if err := decode(e, &ev); err != nil {
			return nil, err
		}
		return []Action{CancelSubject{SubjectType: SubjectCardEntry, SubjectID: ev.EntryID}}, nil
	case "PetMarkedDeceased":
		var ev struct {
			PetID uuid.UUID `json:"pet_id"`
		}
		if err := decode(e, &ev); err != nil {
			return nil, err
		}
		return []Action{CancelPet{PetID: ev.PetID}}, nil
	case "PetGuardianshipTransferred":
		return p.transferDone(ctx, e)
	}
	return nil, nil
}

// ---------------------------------------------------------------- eventos

type apptRef struct {
	AppointmentID  uuid.UUID `json:"appointment_id"`
	PetID          uuid.UUID `json:"pet_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

type slot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func decode(e eventstore.StoredEvent, v any) error {
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("notifications.rules: payload de %s: %w", e.Type, err)
	}
	return nil
}

func (p *Planner) orgStatus(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		OrganizationID uuid.UUID `json:"organization_id"`
		Reason         string    `json:"reason"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	org, ok, err := p.dir.Org(ctx, ev.OrganizationID)
	if err != nil || !ok {
		return nil, err
	}
	status := map[string]string{"OrganizationVerified": "verified", "OrganizationRejected": "rejected", "OrganizationSuspended": "suspended"}[e.Type]
	vars := map[string]string{"org_name": org.Name, "status": status, "reason": strings.TrimSpace(ev.Reason), "link_path": "/app"}

	recipients, err := p.members(ctx, org.ID, orgAdminRoles)
	if err != nil {
		return nil, err
	}
	if len(recipients) == 0 && org.ContactEmail != "" { // sin admins con cuenta: contacto de la org
		recipients = []User{Ghost(org.ContactEmail)}
	}
	var out []Action
	for _, r := range recipients {
		out = append(out, send(RuleOrgStatus, "org_status", domain.Account, r, SubjectOrg, org.ID, e, vars))
	}
	if status == "suspended" {
		ids, err := p.dir.FutureConfirmedGuardians(ctx, org.ID, p.clock.Now())
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			u, ok, err := p.dir.User(ctx, id)
			if err != nil || !ok {
				if err != nil {
					return nil, err
				}
				continue
			}
			out = append(out, send(RuleClinicSuspended, "clinic_suspended_notice", domain.Appointments, u, SubjectOrg, org.ID, e,
				map[string]string{"org_name": org.Name, "link_path": "/app/appointments"}))
		}
	}
	return out, nil
}

func (p *Planner) appointmentRequested(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		Type string `json:"type"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, ok, err := p.dir.Pet(ctx, ev.PetID)
	if err != nil || !ok {
		return nil, err
	}
	recipients, err := p.members(ctx, ev.OrganizationID, appointmentInboxers)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "type": ev.Type, "link_path": "/org/" + ev.OrganizationID.String() + "/agenda"}
	var out []Action
	for _, r := range recipients {
		out = append(out, send(RuleAppointmentRequested, "appointment_requested", domain.Appointments, r, SubjectAppointment, ev.AppointmentID, e, vars))
	}
	return out, nil
}

// guardianCtx reúne mascota, organización y tutor de una cita.
func (p *Planner) guardianCtx(ctx context.Context, r apptRef) (pet Pet, org Org, guardian User, ok bool, err error) {
	pet, ok, err = p.dir.Pet(ctx, r.PetID)
	if err != nil || !ok {
		return pet, org, guardian, false, err
	}
	org, ok, err = p.dir.Org(ctx, r.OrganizationID)
	if err != nil || !ok {
		return pet, org, guardian, false, err
	}
	guardian, ok, err = p.dir.User(ctx, pet.GuardianID)
	return pet, org, guardian, ok, err
}

func (p *Planner) appointmentConfirmed(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		Slot slot   `json:"slot"`
		TZ   string `json:"tz"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, ev.apptRef)
	if err != nil || !ok {
		return nil, err
	}
	vars := apptVars(pet, org, ev.Slot.Start, ev.TZ)
	vars["link_path"] = "/app/appointments"
	// Cancelar primero: una reconfirmación no puede dejar avisos de la franja anterior.
	out := []Action{
		CancelSubject{SubjectType: SubjectAppointment, SubjectID: ev.AppointmentID},
		send(RuleAppointmentConfirmed, "appointment_confirmed", domain.Appointments, guardian, SubjectAppointment, ev.AppointmentID, e, vars),
	}
	return append(out, p.reminders(ev.AppointmentID, guardian.ID, ev.Slot.Start, vars)...), nil
}

func (p *Planner) reminders(appointmentID, guardianID uuid.UUID, start time.Time, vars map[string]string) []Action {
	var out []Action
	for _, d := range domain.AppointmentDue(start, p.clock.Now()) {
		out = append(out, Schedule{
			Rule: RuleAppointmentReminder, Template: "appointment_reminder", Category: domain.Appointments,
			RecipientID: guardianID, SubjectType: SubjectAppointment, SubjectID: appointmentID,
			Offset: d.Offset, DueAt: d.At, Vars: vars,
		})
	}
	return out
}

func (p *Planner) appointmentProposed(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		Slot      slot      `json:"slot"`
		TZ        string    `json:"tz"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, ev.apptRef)
	if err != nil || !ok {
		return nil, err
	}
	vars := apptVars(pet, org, ev.Slot.Start, ev.TZ)
	vars["expires@time"] = ev.ExpiresAt.UTC().Format(time.RFC3339)
	vars["link_path"] = "/app/appointments"
	return []Action{send(RuleAppointmentProposed, "appointment_proposed", domain.Appointments, guardian, SubjectAppointment, ev.AppointmentID, e, vars)}, nil
}

func (p *Planner) appointmentRescheduled(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		Slot slot   `json:"slot"`
		TZ   string `json:"tz"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, ev.apptRef)
	if err != nil || !ok {
		return nil, err
	}
	vars := apptVars(pet, org, ev.Slot.Start, ev.TZ)
	vars["link_path"] = "/app/appointments"
	out := []Action{
		CancelSubject{SubjectType: SubjectAppointment, SubjectID: ev.AppointmentID},
		send(RuleAppointmentRescheduld, "appointment_rescheduled", domain.Appointments, guardian, SubjectAppointment, ev.AppointmentID, e, vars),
	}
	return append(out, p.reminders(ev.AppointmentID, guardian.ID, ev.Slot.Start, vars)...), nil
}

func (p *Planner) appointmentCancelled(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		By string `json:"by"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	out := []Action{CancelSubject{SubjectType: SubjectAppointment, SubjectID: ev.AppointmentID}}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, ev.apptRef)
	if err != nil || !ok {
		return out, err
	}
	appt, found, err := p.dir.Appointment(ctx, ev.AppointmentID)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "org_name": org.Name, "by": ev.By, "when": ""}
	if found && appt.Start != nil {
		vars = apptVars(pet, org, *appt.Start, appt.TZ)
		vars["by"] = ev.By
	}
	if ev.By == "guardian" { // la clínica es quien se entera
		recipients, err := p.members(ctx, ev.OrganizationID, appointmentInboxers)
		if err != nil {
			return nil, err
		}
		vars["link_path"] = "/org/" + ev.OrganizationID.String() + "/agenda"
		for _, r := range recipients {
			out = append(out, send(RuleAppointmentCancelled, "appointment_cancelled", domain.Appointments, r, SubjectAppointment, ev.AppointmentID, e, vars))
		}
		return out, nil
	}
	vars["link_path"] = "/app/appointments"
	return append(out, send(RuleAppointmentCancelled, "appointment_cancelled", domain.Appointments, guardian, SubjectAppointment, ev.AppointmentID, e, vars)), nil
}

func (p *Planner) appointmentRejected(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		apptRef
		Reason string    `json:"reason"`
		By     uuid.UUID `json:"by"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	out := []Action{CancelSubject{SubjectType: SubjectAppointment, SubjectID: ev.AppointmentID}}
	if ev.By == uuid.Nil { // vencimiento de la propuesta, lo hace el sistema: sin correo
		return out, nil
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, ev.apptRef)
	if err != nil || !ok {
		return out, err
	}
	vars := map[string]string{"pet_name": pet.Name, "org_name": org.Name, "link_path": "/app/appointments"}
	return append(out, send(RuleAppointmentRejected, "appointment_rejected", domain.Appointments, guardian, SubjectAppointment, ev.AppointmentID, e, vars)), nil
}

func (p *Planner) encounterSigned(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		EncounterID    uuid.UUID `json:"encounter_id"`
		PetID          uuid.UUID `json:"pet_id"`
		OrganizationID uuid.UUID `json:"organization_id"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, apptRef{PetID: ev.PetID, OrganizationID: ev.OrganizationID})
	if err != nil || !ok {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "org_name": org.Name, "pet_id": pet.ID.String(), "link_path": "/app/pets/" + pet.ID.String()}
	out := []Action{send(RuleEncounterSigned, "encounter_signed", domain.Clinical, guardian, SubjectEncounter, ev.EncounterID, e, vars)}

	// Control recomendado (PRD-007): solo con la consulta firmada.
	enc, found, err := p.dir.Encounter(ctx, ev.EncounterID)
	if err != nil {
		return nil, err
	}
	if found && enc.FollowUp != nil {
		fvars := map[string]string{
			"pet_name": pet.Name, "org_name": org.Name, "pet_id": pet.ID.String(),
			"due_date": enc.FollowUp.DueDate, "link_path": "/app/appointments",
		}
		due, err := domain.DateDue(kernel.CivilDate(enc.FollowUp.DueDate), p.guardianTZ(guardian), domain.FollowUpOffsets, p.clock.Now())
		if err != nil {
			return nil, err
		}
		out = append(out, CancelSubject{SubjectType: SubjectEncounter, SubjectID: ev.EncounterID, Rules: []string{RuleFollowUpReminder}})
		for _, d := range due {
			out = append(out, Schedule{
				Rule: RuleFollowUpReminder, Template: "follow_up_reminder", Category: domain.PreventiveCare,
				RecipientID: guardian.ID, SubjectType: SubjectEncounter, SubjectID: ev.EncounterID,
				Offset: d.Offset, DueAt: d.At, Vars: fvars,
			})
		}
	}
	return out, nil
}

func (p *Planner) encounterAddendum(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		EncounterID uuid.UUID `json:"encounter_id"`
		Seq         int       `json:"seq"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	enc, found, err := p.dir.Encounter(ctx, ev.EncounterID)
	if err != nil || !found {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, apptRef{PetID: enc.PetID, OrganizationID: enc.OrgID})
	if err != nil || !ok {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "org_name": org.Name, "pet_id": pet.ID.String(), "link_path": "/app/pets/" + pet.ID.String()}
	return []Action{send(RuleEncounterAddendum, "encounter_addendum", domain.Clinical, guardian, SubjectEncounter, ev.EncounterID, e, vars)}, nil
}

type entryEvent struct {
	PetID      uuid.UUID `json:"pet_id"`
	EntryID    uuid.UUID `json:"entry_id"`
	RecordedBy struct {
		OrganizationID uuid.UUID `json:"organization_id"`
	} `json:"recorded_by"`
	NextDue *struct {
		Date string `json:"date"`
	} `json:"next_due"`
}

func (p *Planner) entryRecorded(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev entryEvent
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, org, guardian, ok, err := p.guardianCtx(ctx, apptRef{PetID: ev.PetID, OrganizationID: ev.RecordedBy.OrganizationID})
	if err != nil || !ok {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "org_name": org.Name, "pet_id": pet.ID.String(), "link_path": "/app/pets/" + pet.ID.String()}
	out := []Action{send(RuleVaccineRecorded, "vaccine_recorded", domain.PreventiveCare, guardian, SubjectCardEntry, ev.EntryID, e, vars)}
	if ev.NextDue != nil {
		more, err := p.dueReminders(guardian, pet, ev.EntryID, ev.NextDue.Date)
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	return out, nil
}

func (p *Planner) nextDueAdjusted(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		PetID   uuid.UUID `json:"pet_id"`
		EntryID uuid.UUID `json:"entry_id"`
		Date    string    `json:"date"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	out := []Action{CancelSubject{SubjectType: SubjectCardEntry, SubjectID: ev.EntryID, Rules: []string{RuleVaccineReminder}}}
	pet, ok, err := p.dir.Pet(ctx, ev.PetID)
	if err != nil || !ok {
		return out, err
	}
	guardian, ok, err := p.dir.User(ctx, pet.GuardianID)
	if err != nil || !ok {
		return out, err
	}
	more, err := p.dueReminders(guardian, pet, ev.EntryID, ev.Date)
	if err != nil {
		return nil, err
	}
	return append(out, more...), nil
}

func (p *Planner) dueReminders(guardian User, pet Pet, entryID uuid.UUID, date string) ([]Action, error) {
	due, err := domain.DateDue(kernel.CivilDate(date), p.guardianTZ(guardian), domain.VaccineOffsets, p.clock.Now())
	if err != nil {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "pet_id": pet.ID.String(), "due_date": date, "link_path": "/app/appointments"}
	var out []Action
	for _, d := range due {
		out = append(out, Schedule{
			Rule: RuleVaccineReminder, Template: "vaccine_reminder", Category: domain.PreventiveCare,
			RecipientID: guardian.ID, SubjectType: SubjectCardEntry, SubjectID: entryID,
			Offset: d.Offset, DueAt: d.At, Vars: vars,
		})
	}
	return out, nil
}

func (p *Planner) transferDone(ctx context.Context, e eventstore.StoredEvent) ([]Action, error) {
	var ev struct {
		PetID          uuid.UUID `json:"pet_id"`
		FromGuardianID uuid.UUID `json:"from_guardian_id"`
		ToGuardianID   uuid.UUID `json:"to_guardian_id"`
	}
	if err := decode(e, &ev); err != nil {
		return nil, err
	}
	pet, ok, err := p.dir.Pet(ctx, ev.PetID)
	if err != nil || !ok {
		return nil, err
	}
	to, _, err := p.dir.User(ctx, ev.ToGuardianID)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{"pet_name": pet.Name, "to_name": to.DisplayName, "link_path": "/app"}
	var out []Action
	for _, id := range []uuid.UUID{ev.FromGuardianID, ev.ToGuardianID} {
		u, found, err := p.dir.User(ctx, id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, send(RuleTransferDone, "transfer_done", domain.Account, u, SubjectPet, ev.PetID, e, vars))
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- utilidades

// Subject types adicionales para entregas inmediatas.
const (
	SubjectOrg = "organization"
	SubjectPet = "pet"
)

// Ghost es el destinatario sin cuenta de una dirección (invitados, contacto de org).
func Ghost(address string) User {
	return User{ID: notify.GhostRecipientID(address), Email: address, Locale: "es"}
}

func (p *Planner) members(ctx context.Context, orgID uuid.UUID, roles []string) ([]User, error) {
	ids, err := p.dir.OrgMembers(ctx, orgID, roles...)
	if err != nil {
		return nil, err
	}
	var out []User
	for _, id := range ids {
		u, ok, err := p.dir.User(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, u)
		}
	}
	return out, nil
}

// guardianTZ: el usuario no tiene zona propia; se deduce del país (09:00 local).
func (p *Planner) guardianTZ(u User) string { return domain.TimeZoneFor(u.Country) }

func send(rule, template string, cat domain.Category, to User, subjectType string, subject uuid.UUID, e eventstore.StoredEvent, vars map[string]string) SendNow {
	return SendNow{
		Rule: rule, Template: template, Category: cat, Recipient: to,
		SubjectType: subjectType, SubjectID: subject, Occurrence: e.EventID.String(), Vars: vars,
	}
}

// apptVars arma las variables comunes de una cita; la hora se formatea al
// enviar, con el idioma del destinatario (clave `when@time` + `tz`).
func apptVars(pet Pet, org Org, start time.Time, tz string) map[string]string {
	return map[string]string{
		"pet_name": pet.Name, "pet_id": pet.ID.String(), "org_name": org.Name,
		"when@time": start.UTC().Format(time.RFC3339), "tz": tz,
	}
}

// Validate comprueba en el arranque que las plantillas que usan las reglas existen.
func Validate() error {
	for _, t := range []string{
		"org_status", "appointment_requested", "appointment_confirmed", "appointment_reminder",
		"appointment_proposed", "appointment_rescheduled", "appointment_cancelled", "appointment_rejected",
		"encounter_signed", "encounter_addendum", "follow_up_reminder", "vaccine_recorded", "vaccine_reminder",
		"clinic_suspended_notice", "transfer_done", "clinic_daily_agenda",
	} {
		if _, ok := templates.Catalog[t]; !ok {
			return fmt.Errorf("notifications: la regla usa la plantilla %q, que no está en el manifiesto", t)
		}
	}
	return nil
}
