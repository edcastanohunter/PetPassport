package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Process managers de Scheduling (PP-F2-DOM-015, docs/phase-2/02 §7). Reaccionan
// a eventos del outbox y NUNCA llaman al bus: programan comandos del sistema
// con clave de deduplicación (reentregas at-least-once no los duplican).

const (
	// ProcessCompleteOnSign: EncounterSigned con appointment_id → CompleteOnSign.
	ProcessCompleteOnSign = "scheduling.complete-on-sign"
	// ProcessAppointmentTimers: programa/cancela los vencimientos de la cita.
	ProcessAppointmentTimers = "scheduling.appointment-timers"
)

// ExpireProposalKey y AutoNoShowKey son las claves de deduplicación de los jobs.
func ExpireProposalKey(appointmentID uuid.UUID) string {
	return "scheduling.expire-proposal:" + appointmentID.String()
}

// AutoNoShowKey es la clave del job de no-show automático.
func AutoNoShowKey(appointmentID uuid.UUID) string {
	return "scheduling.auto-no-show:" + appointmentID.String()
}

func completeOnSignKey(appointmentID uuid.UUID) string {
	return "scheduling.complete-on-sign:" + appointmentID.String()
}

// CompleteOnSign completa la cita enlazada cuando se firma la consulta. El
// payload de EncounterSigned se lee con un struct local: Scheduling no importa
// el dominio de Clinical (ADR-001), solo conoce el contrato del evento.
type CompleteOnSign struct {
	jobs  process.Jobs
	clock kernel.Clock
}

var _ process.ProcessManager = (*CompleteOnSign)(nil)

// NewCompleteOnSign construye el PM.
func NewCompleteOnSign(jobs process.Jobs, clock kernel.Clock) *CompleteOnSign {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &CompleteOnSign{jobs: jobs, clock: clock}
}

// Name implementa ProcessManager.
func (*CompleteOnSign) Name() string { return ProcessCompleteOnSign }

// Handles implementa ProcessManager.
func (*CompleteOnSign) Handles() []string { return []string{"EncounterSigned"} }

// Emits implementa ProcessManager.
func (*CompleteOnSign) Emits() []string { return []string{app.CompleteOnSign{}.CommandName()} }

// Handle implementa ProcessManager.
func (p *CompleteOnSign) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	var ev struct {
		EncounterID   uuid.UUID  `json:"encounter_id"`
		AppointmentID *uuid.UUID `json:"appointment_id"`
	}
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return fmt.Errorf("%s: payload de %s: %w", ProcessCompleteOnSign, e.Type, err)
	}
	if ev.AppointmentID == nil {
		return nil // consulta sin cita
	}
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, p.clock.Now(),
		app.CompleteOnSign{AppointmentID: *ev.AppointmentID, EncounterID: ev.EncounterID},
		completeOnSignKey(*ev.AppointmentID)))
}

// AppointmentTimers programa la caducidad de la propuesta (48 h) y el
// no-show automático (24 h tras el fin), y cancela los jobs cuando la cita
// deja de necesitarlos.
type AppointmentTimers struct{ jobs process.Jobs }

var _ process.ProcessManager = (*AppointmentTimers)(nil)

// NewAppointmentTimers construye el PM.
func NewAppointmentTimers(jobs process.Jobs) *AppointmentTimers {
	return &AppointmentTimers{jobs: jobs}
}

// Name implementa ProcessManager.
func (*AppointmentTimers) Name() string { return ProcessAppointmentTimers }

// Handles implementa ProcessManager.
func (*AppointmentTimers) Handles() []string {
	return []string{
		"AppointmentAlternativeProposed", "AppointmentConfirmed", "AppointmentRescheduled",
		"AppointmentCheckedIn", "AppointmentCancelled", "AppointmentRejected",
		"AppointmentCompleted", "AppointmentNoShow",
	}
}

// Emits implementa ProcessManager.
func (*AppointmentTimers) Emits() []string {
	return []string{app.ExpireProposal{}.CommandName(), app.AutoNoShow{}.CommandName()}
}

// Handle implementa ProcessManager.
func (p *AppointmentTimers) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	switch e.Type {
	case "AppointmentAlternativeProposed":
		var ev domain.AppointmentAlternativeProposed
		if err := json.Unmarshal(e.Payload, &ev); err != nil {
			return fmt.Errorf("%s: payload de %s: %w", ProcessAppointmentTimers, e.Type, err)
		}
		return p.jobs.Schedule(ctx, tx, process.JobFor(e, ev.ExpiresAt,
			app.ExpireProposal{AppointmentID: ev.AppointmentID}, ExpireProposalKey(ev.AppointmentID)))
	case "AppointmentConfirmed", "AppointmentRescheduled":
		id, end, err := confirmedEnd(e)
		if err != nil {
			return err
		}
		// Aceptar la propuesta confirma la cita: su caducidad ya no aplica.
		if err := p.jobs.Cancel(ctx, tx, ExpireProposalKey(id)); err != nil {
			return err
		}
		return p.jobs.Schedule(ctx, tx, process.JobFor(e, end.Add(domain.NoShowAfter),
			app.AutoNoShow{AppointmentID: id}, AutoNoShowKey(id)))
	default: // check-in, cancelación, rechazo, completada, no-show
		var ref domain.ApptRef
		if err := json.Unmarshal(e.Payload, &ref); err != nil {
			return fmt.Errorf("%s: payload de %s: %w", ProcessAppointmentTimers, e.Type, err)
		}
		if err := p.jobs.Cancel(ctx, tx, ExpireProposalKey(ref.AppointmentID)); err != nil {
			return err
		}
		return p.jobs.Cancel(ctx, tx, AutoNoShowKey(ref.AppointmentID))
	}
}

func confirmedEnd(e eventstore.StoredEvent) (uuid.UUID, time.Time, error) {
	var ev struct {
		AppointmentID uuid.UUID   `json:"appointment_id"`
		Slot          domain.Slot `json:"slot"`
	}
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("%s: payload de %s: %w", ProcessAppointmentTimers, e.Type, err)
	}
	return ev.AppointmentID, ev.Slot.End, nil
}
