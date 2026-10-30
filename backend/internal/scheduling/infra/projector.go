package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
)

// AppointmentsProjector proyecta read_scheduling.appointments en la tx del
// comando (DAT-004, síncrona). Idempotente: el insert es ON CONFLICT DO
// NOTHING y todo UPDATE lleva la guarda `version < nueva` (una reentrega o un
// replay no duplica el historial).
type AppointmentsProjector struct{ reg *eventstore.Registry }

var (
	_                 projection.Projector    = (*AppointmentsProjector)(nil)
	_                 projection.SyncResetter = (*AppointmentsProjector)(nil)
	appointmentEvents                         = []string{
		"AppointmentRequested", "AppointmentCreated", "AppointmentConfirmed", "AppointmentAlternativeProposed",
		"AppointmentRejected", "AppointmentRescheduled", "AppointmentCancelled", "AppointmentCheckedIn",
		"AppointmentNoShow", "AppointmentCompleted",
	}
)

// NewAppointmentsProjector construye el proyector.
func NewAppointmentsProjector(reg *eventstore.Registry) *AppointmentsProjector {
	return &AppointmentsProjector{reg: reg}
}

// Name implementa Projector.
func (*AppointmentsProjector) Name() string { return "scheduling.appointments" }

// Handles implementa Projector.
func (*AppointmentsProjector) Handles() []string { return appointmentEvents }

func hist(s domain.Status, e eventstore.StoredEvent, by, note string) string {
	raw, _ := json.Marshal([]domain.Transition{{Status: s, At: e.OccurredAt, By: by, Note: note}})
	return string(raw)
}

func byStr(id uuid.UUID) string {
	if id == uuid.Nil {
		return "system"
	}
	return id.String()
}

// Project implementa Projector.
func (p *AppointmentsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("appointments projector decode: %w", err)
	}
	// Todos los UPDATE comparten guarda de versión y append de historial.
	update := func(id uuid.UUID, status domain.Status, by, note, set string, args ...any) error {
		sql := `UPDATE read_scheduling.appointments SET status = $2, version = $3, updated_at = $4,
			history = history || $5::jsonb` + set + ` WHERE id = $1 AND version < $3`
		base := []any{id, string(status), e.StreamVersion, e.OccurredAt, hist(status, e, by, note)}
		_, err := tx.Exec(ctx, sql, append(base, args...)...)
		return err
	}
	switch ev := decoded.(type) {
	case domain.AppointmentRequested:
		prefs, _ := json.Marshal(ev.Preferences)
		_, err = tx.Exec(ctx, `
			INSERT INTO read_scheduling.appointments (id, version, pet_id, guardian_id, organization_id, type, origin, status,
			  reason, preferences, history, requested_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,'guardian','requested',$7,$8::jsonb,$9::jsonb,$10,$11,$11)
			ON CONFLICT (id) DO NOTHING`,
			ev.AppointmentID, e.StreamVersion, ev.PetID, ev.GuardianID, ev.OrganizationID, string(ev.Type),
			ev.Reason, string(prefs), hist(domain.StatusRequested, e, byStr(ev.RequestedBy), ""), ev.RequestedBy, e.OccurredAt)
		return err
	case domain.AppointmentCreated:
		_, err = tx.Exec(ctx, `
			INSERT INTO read_scheduling.appointments (id, version, pet_id, guardian_id, organization_id, type, origin, status,
			  reason, history, requested_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,'clinic','requested',$7,$8::jsonb,$9,$10,$10)
			ON CONFLICT (id) DO NOTHING`,
			ev.AppointmentID, e.StreamVersion, ev.PetID, ev.GuardianID, ev.OrganizationID, string(ev.Type),
			ev.Reason, hist(domain.StatusRequested, e, byStr(ev.CreatedBy), "created"), ev.CreatedBy, e.OccurredAt)
		return err
	case domain.AppointmentConfirmed:
		return update(ev.AppointmentID, domain.StatusConfirmed, byStr(ev.By), "",
			`, professional_id = $6, slot_start = $7, slot_end = $8, tz = $9, cancellation_policy_hours = $10,
			   proposal_expires_at = NULL, preferences = '[]'::jsonb`,
			ev.ProfessionalID, ev.Slot.Start, ev.Slot.End, ev.TZ, ev.CancellationPolicyHours)
	case domain.AppointmentAlternativeProposed:
		return update(ev.AppointmentID, domain.StatusProposed, byStr(ev.By), "",
			`, professional_id = $6, slot_start = $7, slot_end = $8, tz = $9, cancellation_policy_hours = $10,
			   proposal_expires_at = $11`,
			ev.ProfessionalID, ev.Slot.Start, ev.Slot.End, ev.TZ, ev.CancellationPolicyHours, ev.ExpiresAt)
	case domain.AppointmentRejected:
		return update(ev.AppointmentID, domain.StatusRejected, byStr(ev.By), ev.Reason, `, proposal_expires_at = NULL`)
	case domain.AppointmentRescheduled:
		return update(ev.AppointmentID, domain.StatusConfirmed, byStr(ev.By), "rescheduled",
			`, professional_id = $6, slot_start = $7, slot_end = $8, tz = $9`,
			ev.ProfessionalID, ev.Slot.Start, ev.Slot.End, ev.TZ)
	case domain.AppointmentCancelled:
		c, _ := json.Marshal(domain.Cancellation{By: ev.By, Reason: ev.Reason, Late: ev.Late})
		return update(ev.AppointmentID, domain.StatusCancelled, byStr(ev.ByUser), ev.Reason,
			`, cancellation = $6::jsonb, proposal_expires_at = NULL`, string(c))
	case domain.AppointmentCheckedIn:
		return update(ev.AppointmentID, domain.StatusCheckedIn, byStr(ev.By), "", ``)
	case domain.AppointmentNoShow:
		return update(ev.AppointmentID, domain.StatusNoShow, "system", "", ``)
	case domain.AppointmentCompleted:
		return update(ev.AppointmentID, domain.StatusCompleted, "system", "", `, encounter_id = $6`, ev.EncounterID)
	}
	return nil
}

// ResetForRebuild implementa projection.SyncResetter. NO toca
// slot_reservations: la restricción de exclusión no se reconstruye por replay.
func (*AppointmentsProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	_, err := tx.Exec(ctx, `DELETE FROM read_scheduling.appointments`)
	return nil, err
}

// AgendasProjector proyecta read_scheduling.agendas (síncrona).
type AgendasProjector struct{ reg *eventstore.Registry }

var (
	_            projection.Projector    = (*AgendasProjector)(nil)
	_            projection.SyncResetter = (*AgendasProjector)(nil)
	agendaEvents                         = []string{
		"AgendaDefined", "AgendaWeeklyTemplateUpdated", "AgendaSlotDurationsSet", "AgendaExceptionAdded",
		"AgendaExceptionRemoved", "AgendaPoliciesSet", "AgendaDeactivated", "AgendaActivated",
	}
)

// NewAgendasProjector construye el proyector.
func NewAgendasProjector(reg *eventstore.Registry) *AgendasProjector {
	return &AgendasProjector{reg: reg}
}

// Name implementa Projector.
func (*AgendasProjector) Name() string { return "scheduling.agendas" }

// Handles implementa Projector.
func (*AgendasProjector) Handles() []string { return agendaEvents }

// Project implementa Projector.
func (p *AgendasProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("agendas projector decode: %w", err)
	}
	upd := func(ref domain.AgendaRef, set string, args ...any) error {
		sql := `UPDATE read_scheduling.agendas SET version = $3, updated_at = $4` + set +
			` WHERE organization_id = $1 AND professional_id = $2 AND version < $3`
		_, err := tx.Exec(ctx, sql, append([]any{ref.OrganizationID, ref.ProfessionalID, e.StreamVersion, e.OccurredAt}, args...)...)
		return err
	}
	switch ev := decoded.(type) {
	case domain.AgendaDefined:
		tpl, _ := json.Marshal(ev.Config.WeeklyTemplate)
		dur, _ := json.Marshal(ev.Config.SlotMinutes)
		_, err = tx.Exec(ctx, `
			INSERT INTO read_scheduling.agendas (organization_id, professional_id, version, tz, weekly_template, slot_minutes,
			  lead_time_min_hours, lead_time_max_days, cancellation_policy_hours, active, updated_at)
			VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9,true,$10)
			ON CONFLICT (organization_id, professional_id) DO NOTHING`,
			ev.OrganizationID, ev.ProfessionalID, e.StreamVersion, ev.Config.TimeZone, string(tpl), string(dur),
			ev.Config.LeadMinHours, ev.Config.LeadMaxDays, ev.Config.CancelPolicyHrs, e.OccurredAt)
		return err
	case domain.AgendaWeeklyTemplateUpdated:
		tpl, _ := json.Marshal(ev.WeeklyTemplate)
		return upd(ev.AgendaRef, `, weekly_template = $5::jsonb`, string(tpl))
	case domain.AgendaSlotDurationsSet:
		dur, _ := json.Marshal(ev.SlotMinutes)
		return upd(ev.AgendaRef, `, slot_minutes = $5::jsonb`, string(dur))
	case domain.AgendaExceptionAdded:
		raw, _ := json.Marshal(ev.Exception)
		return upd(ev.AgendaRef, `, exceptions = jsonb_set(exceptions, ARRAY[$5::text], $6::jsonb, true)`, ev.Date.String(), string(raw))
	case domain.AgendaExceptionRemoved:
		return upd(ev.AgendaRef, `, exceptions = exceptions - $5::text`, ev.Date.String())
	case domain.AgendaPoliciesSet:
		return upd(ev.AgendaRef, `, lead_time_min_hours = $5, lead_time_max_days = $6, cancellation_policy_hours = $7`,
			ev.LeadMinHours, ev.LeadMaxDays, ev.CancelHours)
	case domain.AgendaDeactivated:
		return upd(ev.AgendaRef, `, active = false`)
	case domain.AgendaActivated:
		return upd(ev.AgendaRef, `, active = true`)
	}
	return nil
}

// ResetForRebuild implementa projection.SyncResetter.
func (*AgendasProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	_, err := tx.Exec(ctx, `DELETE FROM read_scheduling.agendas`)
	return nil, err
}

// SyncProjector adapta el Runner a app.ProjectionRunner.
type SyncProjector struct{ runner *projection.Runner }

var _ app.ProjectionRunner = (*SyncProjector)(nil)

// NewSyncProjector construye el adaptador.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply aplica los proyectores en la tx del contexto.
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return a.runner.Apply(ctx, tx, events)
}

// RegisterSyncProjectors registra las proyecciones síncronas de Scheduling.
func RegisterSyncProjectors(runner *projection.Runner, reg *eventstore.Registry) {
	runner.Register(NewAppointmentsProjector(reg))
	runner.Register(NewAgendasProjector(reg))
}
