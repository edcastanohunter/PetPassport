package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

type fakeDir struct {
	users    map[uuid.UUID]app.User
	pets     map[uuid.UUID]app.Pet
	orgs     map[uuid.UUID]app.Org
	appts    map[uuid.UUID]app.Appointment
	encs     map[uuid.UUID]app.Encounter
	entries  map[uuid.UUID]app.CardEntry
	members  map[string][]uuid.UUID // org|rol
	guardian []uuid.UUID
}

func newDir() *fakeDir {
	return &fakeDir{
		users: map[uuid.UUID]app.User{}, pets: map[uuid.UUID]app.Pet{}, orgs: map[uuid.UUID]app.Org{},
		appts: map[uuid.UUID]app.Appointment{}, encs: map[uuid.UUID]app.Encounter{}, entries: map[uuid.UUID]app.CardEntry{}, members: map[string][]uuid.UUID{},
	}
}

func (d *fakeDir) User(_ context.Context, id uuid.UUID) (app.User, bool, error) {
	u, ok := d.users[id]
	return u, ok, nil
}
func (d *fakeDir) Pet(_ context.Context, id uuid.UUID) (app.Pet, bool, error) {
	p, ok := d.pets[id]
	return p, ok, nil
}
func (d *fakeDir) Org(_ context.Context, id uuid.UUID) (app.Org, bool, error) {
	o, ok := d.orgs[id]
	return o, ok, nil
}
func (d *fakeDir) Appointment(_ context.Context, id uuid.UUID) (app.Appointment, bool, error) {
	a, ok := d.appts[id]
	return a, ok, nil
}
func (d *fakeDir) Encounter(_ context.Context, id uuid.UUID) (app.Encounter, bool, error) {
	e, ok := d.encs[id]
	return e, ok, nil
}
func (d *fakeDir) CardEntry(_ context.Context, id uuid.UUID) (app.CardEntry, bool, error) {
	c, ok := d.entries[id]
	return c, ok, nil
}
func (d *fakeDir) OrgMembers(_ context.Context, org uuid.UUID, roles ...string) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, r := range roles {
		out = append(out, d.members[org.String()+"|"+r]...)
	}
	return out, nil
}
func (d *fakeDir) FutureConfirmedGuardians(context.Context, uuid.UUID, time.Time) ([]uuid.UUID, error) {
	return d.guardian, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type world struct {
	dir                            *fakeDir
	pl                             *app.Planner
	now                            time.Time
	org, pet, guardian, admin, rec uuid.UUID
}

func newWorld() *world {
	w := &world{dir: newDir(), now: time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC),
		org: uuid.New(), pet: uuid.New(), guardian: uuid.New(), admin: uuid.New(), rec: uuid.New()}
	w.pl = app.NewPlanner(w.dir, fixedClock{w.now})
	w.dir.orgs[w.org] = app.Org{ID: w.org, Name: "Clínica Sur", Country: "CO", ContactEmail: "contacto@sur.co"}
	w.dir.pets[w.pet] = app.Pet{ID: w.pet, Name: "Luna", GuardianID: w.guardian}
	w.dir.users[w.guardian] = app.User{ID: w.guardian, Email: "tutor@example.com", Locale: "es", Country: "CO"}
	w.dir.users[w.admin] = app.User{ID: w.admin, Email: "admin@sur.co", Locale: "es"}
	w.dir.users[w.rec] = app.User{ID: w.rec, Email: "rec@sur.co", Locale: "en"}
	w.dir.members[w.org.String()+"|clinic_admin"] = []uuid.UUID{w.admin}
	w.dir.members[w.org.String()+"|receptionist"] = []uuid.UUID{w.rec}
	return w
}

func event(t string, payload any) eventstore.StoredEvent {
	b, _ := json.Marshal(payload)
	return eventstore.StoredEvent{EventID: uuid.New(), Type: t, Payload: b}
}

func plan(t *testing.T, w *world, e eventstore.StoredEvent) []app.Action {
	t.Helper()
	acts, err := w.pl.Plan(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func kinds(acts []app.Action) (sends []app.SendNow, sched []app.Schedule, cancels []app.CancelSubject) {
	for _, a := range acts {
		switch v := a.(type) {
		case app.SendNow:
			sends = append(sends, v)
		case app.Schedule:
			sched = append(sched, v)
		case app.CancelSubject:
			cancels = append(cancels, v)
		}
	}
	return
}

func ref(w *world, appt uuid.UUID) map[string]any {
	return map[string]any{"appointment_id": appt, "pet_id": w.pet, "organization_id": w.org}
}

func TestConfirmedSendsAndSchedulesReminders(t *testing.T) {
	w := newWorld()
	appt := uuid.New()
	start := w.now.Add(30 * time.Hour)
	p := ref(w, appt)
	p["slot"] = map[string]any{"start": start, "end": start.Add(30 * time.Minute)}
	p["tz"] = "America/Bogota"
	acts := plan(t, w, event("AppointmentConfirmed", p))
	sends, sched, cancels := kinds(acts)

	if len(cancels) != 1 || cancels[0].SubjectID != appt {
		t.Fatalf("debe cancelar avisos previos: %+v", cancels)
	}
	if len(sends) != 1 || sends[0].Template != "appointment_confirmed" || sends[0].Recipient.ID != w.guardian || sends[0].Category != domain.Appointments {
		t.Fatalf("sends: %+v", sends)
	}
	if sends[0].Vars["pet_name"] != "Luna" || sends[0].Vars["org_name"] != "Clínica Sur" || sends[0].Vars["tz"] != "America/Bogota" {
		t.Fatalf("vars: %+v", sends[0].Vars)
	}
	if len(sched) != 2 || sched[0].Offset != "T-24h" || sched[1].Offset != "T-2h" {
		t.Fatalf("programados: %+v", sched)
	}
	if !sched[0].DueAt.Equal(start.Add(-24*time.Hour)) || !sched[1].DueAt.Equal(start.Add(-2*time.Hour)) {
		t.Fatalf("due_at: %v %v", sched[0].DueAt, sched[1].DueAt)
	}
	// Claves estables ante reentrega del mismo evento.
	if sched[0].DedupKey() == sched[1].DedupKey() {
		t.Fatal("las claves distinguen el offset")
	}
}

func TestSendNowDedupKeyUsesEventID(t *testing.T) {
	w := newWorld()
	appt := uuid.New()
	p := ref(w, appt)
	p["slot"] = map[string]any{"start": w.now.Add(48 * time.Hour), "end": w.now.Add(49 * time.Hour)}
	p["tz"] = "UTC"
	e := event("AppointmentConfirmed", p)
	a, _, _ := kinds(plan(t, w, e))
	b, _, _ := kinds(plan(t, w, e)) // mismo evento reentregado
	if a[0].DedupKey() != b[0].DedupKey() {
		t.Fatal("reentregar el mismo evento debe dar la misma clave")
	}
	other, _, _ := kinds(plan(t, w, event("AppointmentConfirmed", p)))
	if a[0].DedupKey() == other[0].DedupKey() {
		t.Fatal("otro evento (otro event_id) debe dar otra clave")
	}
}

func TestRescheduledSoonOnlyKeepsLaterReminder(t *testing.T) {
	w := newWorld()
	p := ref(w, uuid.New())
	start := w.now.Add(3 * time.Hour)
	p["slot"] = map[string]any{"start": start, "end": start.Add(time.Hour)}
	p["tz"] = "UTC"
	sends, sched, cancels := kinds(plan(t, w, event("AppointmentRescheduled", p)))
	if len(cancels) != 1 || len(sends) != 1 || sends[0].Template != "appointment_rescheduled" {
		t.Fatalf("sends=%+v cancels=%+v", sends, cancels)
	}
	if len(sched) != 1 || sched[0].Offset != "T-2h" {
		t.Fatalf("a 3 h solo queda T-2h: %+v", sched)
	}
}

func TestCancelledByGuardianNotifiesClinicInbox(t *testing.T) {
	w := newWorld()
	appt := uuid.New()
	start := w.now.Add(48 * time.Hour)
	w.dir.appts[appt] = app.Appointment{ID: appt, Start: &start, TZ: "America/Bogota"}
	p := ref(w, appt)
	p["by"] = "guardian"
	sends, _, cancels := kinds(plan(t, w, event("AppointmentCancelled", p)))
	if len(cancels) != 1 {
		t.Fatalf("cancelaciones: %+v", cancels)
	}
	got := map[uuid.UUID]bool{}
	for _, s := range sends {
		got[s.Recipient.ID] = true
		if s.Template != "appointment_cancelled" || s.Vars["by"] != "guardian" {
			t.Fatalf("send: %+v", s)
		}
	}
	if len(sends) != 2 || !got[w.admin] || !got[w.rec] || got[w.guardian] {
		t.Fatalf("la clínica (admin y recepción) se entera, no el tutor: %+v", sends)
	}
}

func TestCancelledByClinicNotifiesGuardian(t *testing.T) {
	w := newWorld()
	p := ref(w, uuid.New())
	p["by"] = "clinic"
	sends, _, _ := kinds(plan(t, w, event("AppointmentCancelled", p)))
	if len(sends) != 1 || sends[0].Recipient.ID != w.guardian {
		t.Fatalf("sends: %+v", sends)
	}
}

func TestRejectedBySystemOnlyCancels(t *testing.T) {
	w := newWorld()
	p := ref(w, uuid.New())
	p["reason"] = "PROPOSAL_EXPIRED"
	p["by"] = uuid.Nil
	sends, _, cancels := kinds(plan(t, w, event("AppointmentRejected", p)))
	if len(sends) != 0 || len(cancels) != 1 {
		t.Fatalf("el vencimiento no envía correo: %+v %+v", sends, cancels)
	}
	p["by"] = w.admin
	sends, _, _ = kinds(plan(t, w, event("AppointmentRejected", p)))
	if len(sends) != 1 || sends[0].Template != "appointment_rejected" {
		t.Fatalf("un rechazo de la clínica sí avisa: %+v", sends)
	}
}

func TestTerminalAppointmentEventsCancelReminders(t *testing.T) {
	w := newWorld()
	for _, typ := range []string{"AppointmentCheckedIn", "AppointmentNoShow", "AppointmentCompleted"} {
		sends, sched, cancels := kinds(plan(t, w, event(typ, ref(w, uuid.New()))))
		if len(sends) != 0 || len(sched) != 0 || len(cancels) != 1 || cancels[0].SubjectType != app.SubjectAppointment {
			t.Fatalf("%s: %+v %+v %+v", typ, sends, sched, cancels)
		}
	}
}

func TestRequestedNotifiesAdminsAndReception(t *testing.T) {
	w := newWorld()
	p := ref(w, uuid.New())
	p["type"] = "consultation"
	sends, _, _ := kinds(plan(t, w, event("AppointmentRequested", p)))
	if len(sends) != 2 {
		t.Fatalf("sends: %+v", sends)
	}
	for _, s := range sends {
		if s.Template != "appointment_requested" || s.Vars["link_path"] != "/org/"+w.org.String()+"/agenda" {
			t.Fatalf("send: %+v", s)
		}
	}
}

func TestEncounterSignedWithFollowUpSchedulesAtNineLocal(t *testing.T) {
	w := newWorld()
	enc := uuid.New()
	w.dir.encs[enc] = app.Encounter{ID: enc, PetID: w.pet, OrgID: w.org, Status: "signed", FollowUp: &app.FollowUp{DueDate: "2026-11-20"}}
	e := event("EncounterSigned", map[string]any{"encounter_id": enc, "pet_id": w.pet, "organization_id": w.org})
	sends, sched, cancels := kinds(plan(t, w, e))
	if len(sends) != 1 || sends[0].Template != "encounter_signed" || sends[0].Category != domain.Clinical {
		t.Fatalf("sends: %+v", sends)
	}
	if len(cancels) != 1 || len(cancels[0].Rules) != 1 || cancels[0].Rules[0] != app.RuleFollowUpReminder {
		t.Fatalf("cancels: %+v", cancels)
	}
	if len(sched) != 2 || sched[0].Offset != "T-3d" || sched[1].Offset != "T-0" {
		t.Fatalf("sched: %+v", sched)
	}
	if want := time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC); !sched[0].DueAt.Equal(want) {
		t.Fatalf("T-3d = 09:00 de Bogotá: %v", sched[0].DueAt)
	}
	if sched[0].Category != domain.PreventiveCare {
		t.Fatalf("categoría: %v", sched[0].Category)
	}
	// Sin control recomendado: solo el aviso de consulta.
	w.dir.encs[enc] = app.Encounter{ID: enc, PetID: w.pet, OrgID: w.org}
	_, sched, _ = kinds(plan(t, w, e))
	if len(sched) != 0 {
		t.Fatalf("sin follow-up no hay recordatorios: %+v", sched)
	}
}

func TestVaccineRecordedAndNextDueAdjusted(t *testing.T) {
	w := newWorld()
	entry := uuid.New()
	rec := map[string]any{
		"pet_id": w.pet, "entry_id": entry,
		"recorded_by": map[string]any{"user_id": uuid.New(), "organization_id": w.org},
		"next_due":    map[string]any{"date": "2026-12-01", "basis": "protocol"},
	}
	sends, sched, _ := kinds(plan(t, w, event("VaccinationEntryRecorded", rec)))
	if len(sends) != 1 || sends[0].Template != "vaccine_recorded" || len(sched) != 2 {
		t.Fatalf("sends=%+v sched=%+v", sends, sched)
	}
	if sched[0].Offset != "T-14d" || sched[0].SubjectType != app.SubjectCardEntry || sched[0].SubjectID != entry || sched[0].Template != "vaccine_reminder" {
		t.Fatalf("sched: %+v", sched[0])
	}
	// Sin próxima dosis: solo el aviso inmediato.
	delete(rec, "next_due")
	_, sched, _ = kinds(plan(t, w, event("PreventiveEntryRecorded", rec)))
	if len(sched) != 0 {
		t.Fatalf("sched: %+v", sched)
	}

	adj := map[string]any{"pet_id": w.pet, "entry_id": entry, "date": "2027-01-10", "basis": "manual"}
	_, sched, cancels := kinds(plan(t, w, event("NextDueAdjusted", adj)))
	if len(cancels) != 1 || cancels[0].SubjectID != entry || cancels[0].Rules[0] != app.RuleVaccineReminder {
		t.Fatalf("cancels: %+v", cancels)
	}
	if len(sched) != 2 || !sched[1].DueAt.Equal(time.Date(2027, 1, 10, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("sched: %+v", sched)
	}
}

func TestVoidAndDeceasedCancel(t *testing.T) {
	w := newWorld()
	entry := uuid.New()
	acts := plan(t, w, event("VaccinationEntryVoided", map[string]any{"pet_id": w.pet, "entry_id": entry}))
	if c, ok := acts[0].(app.CancelSubject); !ok || c.SubjectID != entry || c.SubjectType != app.SubjectCardEntry {
		t.Fatalf("acts: %+v", acts)
	}
	acts = plan(t, w, event("PetMarkedDeceased", map[string]any{"pet_id": w.pet}))
	if c, ok := acts[0].(app.CancelPet); !ok || c.PetID != w.pet {
		t.Fatalf("acts: %+v", acts)
	}
}

func TestOrgStatusFallsBackToContactAndSuspensionNotifiesGuardians(t *testing.T) {
	w := newWorld()
	// Sin admins con cuenta → correo al contacto de la organización (sin cuenta).
	delete(w.dir.members, w.org.String()+"|clinic_admin")
	sends, _, _ := kinds(plan(t, w, event("OrganizationVerified", map[string]any{"organization_id": w.org})))
	if len(sends) != 1 || sends[0].Recipient.Email != "contacto@sur.co" || sends[0].Vars["status"] != "verified" {
		t.Fatalf("sends: %+v", sends)
	}
	w.dir.members[w.org.String()+"|clinic_admin"] = []uuid.UUID{w.admin}
	sends, _, _ = kinds(plan(t, w, event("OrganizationRejected", map[string]any{"organization_id": w.org, "reason": "RUT vencido"})))
	if len(sends) != 1 || sends[0].Recipient.ID != w.admin || sends[0].Vars["reason"] != "RUT vencido" || sends[0].Category != domain.Account {
		t.Fatalf("sends: %+v", sends)
	}
	w.dir.guardian = []uuid.UUID{w.guardian}
	sends, _, _ = kinds(plan(t, w, event("OrganizationSuspended", map[string]any{"organization_id": w.org, "reason": "x"})))
	templates := map[string]uuid.UUID{}
	for _, s := range sends {
		templates[s.Template] = s.Recipient.ID
	}
	if templates["org_status"] != w.admin || templates["clinic_suspended_notice"] != w.guardian {
		t.Fatalf("suspensión: %+v", templates)
	}
}

func TestTransferDoneNotifiesBoth(t *testing.T) {
	w := newWorld()
	to := uuid.New()
	w.dir.users[to] = app.User{ID: to, Email: "nuevo@example.com", Locale: "en", DisplayName: "Nuevo Tutor"}
	e := event("PetGuardianshipTransferred", map[string]any{"pet_id": w.pet, "from_guardian_id": w.guardian, "to_guardian_id": to})
	sends, _, _ := kinds(plan(t, w, e))
	if len(sends) != 2 || sends[0].Vars["to_name"] != "Nuevo Tutor" || sends[0].Category != domain.Account {
		t.Fatalf("sends: %+v", sends)
	}
}

func TestUnknownEventAndMissingDataProduceNothing(t *testing.T) {
	w := newWorld()
	if acts := plan(t, w, event("SomethingElse", map[string]any{})); len(acts) != 0 {
		t.Fatalf("acts: %+v", acts)
	}
	// Mascota inexistente: sin acciones ni error (no bloquea el outbox).
	p := ref(w, uuid.New())
	p["pet_id"] = uuid.New()
	p["slot"] = map[string]any{"start": w.now.Add(48 * time.Hour), "end": w.now.Add(49 * time.Hour)}
	if acts := plan(t, w, event("AppointmentConfirmed", p)); len(acts) != 0 {
		t.Fatalf("acts: %+v", acts)
	}
	// Payload corrupto: error (el outbox reintenta y termina en dead-letter).
	if _, err := w.pl.Plan(context.Background(), eventstore.StoredEvent{Type: "AppointmentConfirmed", Payload: []byte("{")}); err == nil {
		t.Fatal("un payload ilegible debe devolver error")
	}
}

func TestEveryRuleTemplateExists(t *testing.T) {
	if err := app.Validate(); err != nil {
		t.Fatal(err)
	}
}
