package infra_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process/testkit"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	t0   = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	appt = uuid.MustParse("01930000-0000-7000-8000-00000000f001")
	enc  = uuid.MustParse("01930000-0000-7000-8000-00000000e001")
	ref  = domain.ApptRef{AppointmentID: appt, PetID: uuid.New(), OrganizationID: uuid.New()}
)

// encounterSigned reproduce el contrato del evento de Clinical (el PM no
// importa su dominio, ADR-001).
type encounterSigned struct {
	EncounterID   uuid.UUID  `json:"encounter_id"`
	AppointmentID *uuid.UUID `json:"appointment_id,omitempty"`
}

func (encounterSigned) EventName() string  { return "EncounterSigned" }
func (encounterSigned) SchemaVersion() int { return 1 }

func completeOnSign(jobs process.Jobs, clock kernel.Clock) process.ProcessManager {
	return infra.NewCompleteOnSign(jobs, clock)
}

func timers(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
	return infra.NewAppointmentTimers(jobs)
}

func TestCompleteOnSign_DispatchesForLinkedEncounter(t *testing.T) {
	a := appt
	testkit.Process(t, completeOnSign).At(t0).
		GivenEvents(encounterSigned{EncounterID: enc, AppointmentID: &a}).
		ThenCommands(app.CompleteOnSign{AppointmentID: appt, EncounterID: enc}).
		ThenScheduled()
}

func TestCompleteOnSign_IgnoresEncounterWithoutAppointment(t *testing.T) {
	testkit.Process(t, completeOnSign).At(t0).
		GivenEvents(encounterSigned{EncounterID: enc}).
		ThenNothing()
}

// La propuesta caduca a las 48 h; se programa con la clave de la cita.
func TestTimers_ProposalExpires(t *testing.T) {
	exp := t0.Add(domain.ProposalTTL)
	testkit.Process(t, timers).At(t0).
		GivenEvents(domain.AppointmentAlternativeProposed{ApptRef: ref, Slot: domain.Slot{Start: t0.Add(72 * time.Hour), End: t0.Add(73 * time.Hour)}, ExpiresAt: exp}).
		ThenCommands().
		ThenScheduled(testkit.Scheduled{RunAt: exp, Command: app.ExpireProposal{AppointmentID: appt}, DedupKey: infra.ExpireProposalKey(appt)})
}

// Confirmar programa el no-show a las 24 h del fin y cancela la caducidad.
func TestTimers_ConfirmedSchedulesNoShowAndCancelsExpiry(t *testing.T) {
	end := t0.Add(49 * time.Hour)
	h := testkit.Process(t, timers).At(t0).
		GivenEvents(
			domain.AppointmentAlternativeProposed{ApptRef: ref, Slot: domain.Slot{Start: t0.Add(48 * time.Hour), End: end}, ExpiresAt: t0.Add(domain.ProposalTTL)},
			domain.AppointmentConfirmed{ApptRef: ref, Slot: domain.Slot{Start: t0.Add(48 * time.Hour), End: end}},
		).
		ThenScheduled(testkit.Scheduled{RunAt: end.Add(domain.NoShowAfter), Command: app.AutoNoShow{AppointmentID: appt}, DedupKey: infra.AutoNoShowKey(appt)})
	for _, j := range h.Jobs().All() {
		if j.DedupKey == infra.ExpireProposalKey(appt) && j.Status != "cancelled" {
			t.Fatalf("la caducidad debía cancelarse: %+v", j)
		}
	}
}

// Reprogramar mueve el no-show (misma clave, nuevo RunAt).
func TestTimers_RescheduleMovesNoShow(t *testing.T) {
	s1 := domain.Slot{Start: t0.Add(48 * time.Hour), End: t0.Add(49 * time.Hour)}
	s2 := domain.Slot{Start: t0.Add(96 * time.Hour), End: t0.Add(97 * time.Hour)}
	testkit.Process(t, timers).At(t0).
		GivenEvents(
			domain.AppointmentConfirmed{ApptRef: ref, Slot: s1},
			domain.AppointmentRescheduled{ApptRef: ref, PreviousSlot: s1, Slot: s2},
		).
		ThenScheduled(testkit.Scheduled{RunAt: s2.End.Add(domain.NoShowAfter), Command: app.AutoNoShow{AppointmentID: appt}, DedupKey: infra.AutoNoShowKey(appt)})
}

// Check-in, cancelación y demás estados finales cancelan los vencimientos.
func TestTimers_TerminalEventsCancelJobs(t *testing.T) {
	slot := domain.Slot{Start: t0.Add(48 * time.Hour), End: t0.Add(49 * time.Hour)}
	for name, terminal := range map[string]any{
		"checked_in": domain.AppointmentCheckedIn{ApptRef: ref},
		"cancelled":  domain.AppointmentCancelled{ApptRef: ref, By: domain.ByGuardian},
		"completed":  domain.AppointmentCompleted{ApptRef: ref},
		"no_show":    domain.AppointmentNoShow{ApptRef: ref, Automatic: true},
		"rejected":   domain.AppointmentRejected{ApptRef: ref, Reason: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			testkit.Process(t, timers).At(t0).
				GivenEvents(domain.AppointmentConfirmed{ApptRef: ref, Slot: slot}, terminal).
				ThenNothing()
		})
	}
}
