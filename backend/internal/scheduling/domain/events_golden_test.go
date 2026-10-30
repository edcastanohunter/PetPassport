package domain

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var update = flag.Bool("update", false, "regenera los golden files")

// TestEvents_Golden (ARQ-007): el JSON persistido de cada evento v1 es un
// contrato estable; cambiarlo exige schema_version+1 y un upcaster. Regenerar
// la primera vez: `go test ./internal/scheduling/domain -run Golden -update`.
func TestEvents_Golden(t *testing.T) {
	ref := ApptRef{AppointmentID: apptID, PetID: petID, OrganizationID: orgID}
	slot := Slot{Start: slotStart, End: slotEnd}
	from, to := kernel.CivilDate("2026-11-05"), kernel.CivilDate("2026-11-09")
	enc := petID
	agenda := AgendaRef{OrganizationID: orgID, ProfessionalID: vetID}
	events := []any{
		AppointmentRequested{ApptRef: ref, GuardianID: guardID, Type: TypeConsultation, Reason: "Cojera",
			Preferences: []Preference{{Start: &slotStart, End: &slotEnd}, {DateFrom: &from, DateTo: &to}}, RequestedBy: guardID, At: now},
		AppointmentCreated{ApptRef: ref, GuardianID: guardID, Type: TypeVaccination, Reason: "Refuerzo", CreatedBy: recepID, At: now},
		AppointmentConfirmed{ApptRef: ref, ProfessionalID: vetID, Slot: slot, TZ: "America/Bogota", CancellationPolicyHours: 4, By: recepID, At: now},
		AppointmentAlternativeProposed{ApptRef: ref, ProfessionalID: vetID, Slot: slot, TZ: "America/Bogota", CancellationPolicyHours: 4, ExpiresAt: now.Add(ProposalTTL), By: recepID, At: now},
		AppointmentRejected{ApptRef: ref, Reason: "Sin cupo", By: recepID, At: now},
		AppointmentRescheduled{ApptRef: ref, PreviousSlot: slot, Slot: Slot{Start: slotStart.Add(time.Hour), End: slotEnd.Add(time.Hour)}, ProfessionalID: vetID, TZ: "America/Bogota", By: recepID, At: now},
		AppointmentCancelled{ApptRef: ref, Reason: "Viaje", By: ByGuardian, Late: true, ByUser: guardID, At: now},
		AppointmentCheckedIn{ApptRef: ref, By: recepID, At: now},
		AppointmentNoShow{ApptRef: ref, Automatic: true, At: now},
		AppointmentCompleted{ApptRef: ref, EncounterID: &enc, At: now},
		AgendaDefined{AgendaRef: agenda, Config: AgendaConfig{TimeZone: "America/Bogota",
			WeeklyTemplate: map[string][]TimeRange{"mon": {{Start: "08:00", End: "12:00"}}},
			SlotMinutes:    map[AppointmentType]int{TypeConsultation: 30}, LeadMinHours: 2, LeadMaxDays: 60, CancelPolicyHrs: 4}, By: recepID, At: now},
		AgendaWeeklyTemplateUpdated{AgendaRef: agenda, WeeklyTemplate: map[string][]TimeRange{"tue": {{Start: "09:00", End: "13:00"}}}, By: recepID, At: now},
		AgendaSlotDurationsSet{AgendaRef: agenda, SlotMinutes: map[AppointmentType]int{TypeSurgery: 90}, By: recepID, At: now},
		AgendaExceptionAdded{AgendaRef: agenda, Date: "2026-11-09", Exception: DayException{Closed: true}, By: recepID, At: now},
		AgendaExceptionRemoved{AgendaRef: agenda, Date: "2026-11-09", By: recepID, At: now},
		AgendaPoliciesSet{AgendaRef: agenda, LeadMinHours: 4, LeadMaxDays: 30, CancelHours: 12, By: recepID, At: now},
		AgendaDeactivated{AgendaRef: agenda, By: recepID, At: now},
		AgendaActivated{AgendaRef: agenda, By: recepID, At: now},
	}
	for _, e := range events {
		ev, ok := e.(interface {
			EventName() string
			SchemaVersion() int
		})
		if !ok {
			t.Fatalf("%T no implementa eventstore.Event", e)
		}
		if ev.SchemaVersion() != 1 {
			t.Errorf("%s: schema_version %d, want 1", ev.EventName(), ev.SchemaVersion())
		}
		t.Run(ev.EventName(), func(t *testing.T) {
			raw, err := json.MarshalIndent(e, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", ev.EventName()+".json")
			if *update {
				if err := os.WriteFile(golden, raw, 0o600); err != nil { //nolint:gosec // golden de prueba
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden) //nolint:gosec // ruta fija en testdata
			if err != nil {
				t.Fatalf("lee golden (¿ejecuta con -update la primera vez?): %v", err)
			}
			if string(raw) != string(want) {
				t.Errorf("golden %s cambió:\ngot:\n%s\nwant:\n%s\nSi es intencional: schema_version+1 y upcaster (ARQ-007).", golden, raw, want)
			}
		})
	}
}
