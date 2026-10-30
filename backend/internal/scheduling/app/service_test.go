package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Pruebas del servicio con fakes en memoria. El no-solapamiento real lo da la
// restricción EXCLUDE de Postgres (test/integration); aquí se comprueba que el
// servicio traduce el conflicto, libera reservas y respeta la vigencia.

var (
	orgID   = uuid.MustParse("01930000-0000-7000-8000-00000000c001")
	petID   = uuid.MustParse("01930000-0000-7000-8000-00000000b001")
	guardID = uuid.MustParse("01930000-0000-7000-8000-00000000a001")
	vetID   = uuid.MustParse("01930000-0000-7000-8000-00000000d001")
	otherID = uuid.MustParse("01930000-0000-7000-8000-00000000a002")
	now0    = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) // miércoles
)

// ---- fakes

type memAppointments struct{ streams map[uuid.UUID][]any }

func (m *memAppointments) Load(_ context.Context, id uuid.UUID) (*domain.Appointment, error) {
	a := &domain.Appointment{}
	for _, e := range m.streams[id] {
		if err := a.ApplyEvent(e); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (m *memAppointments) Save(_ context.Context, a *domain.Appointment, _ eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := a.UncommittedEvents()
	m.streams[a.ID()] = append(m.streams[a.ID()], pending...)
	a.MarkCommitted()
	return []eventstore.StoredEvent{{StreamVersion: int64(len(m.streams[a.ID()]))}}, nil
}

type memReservations struct {
	clock *kernel.SimClock
	rows  map[uuid.UUID]app.Reservation
}

func (m *memReservations) Reserve(_ context.Context, r app.Reservation) error {
	for id, o := range m.rows {
		if id == r.AppointmentID || o.ProfessionalID != r.ProfessionalID {
			continue
		}
		if o.Provisional && o.ExpiresAt != nil && !o.ExpiresAt.After(m.clock.Now()) {
			delete(m.rows, id) // la provisional vencida se purga (como en Postgres)
			continue
		}
		if r.Slot.Start.Before(o.Slot.End) && o.Slot.Start.Before(r.Slot.End) {
			return domain.ErrSlotUnavailable
		}
	}
	m.rows[r.AppointmentID] = r
	return nil
}

func (m *memReservations) Release(_ context.Context, id uuid.UUID) error {
	delete(m.rows, id)
	return nil
}

func (m *memReservations) Reserved(context.Context, uuid.UUID, time.Time, time.Time) ([]domain.Reservation, error) {
	return nil, nil
}

type fakeAgendas struct{ agenda *domain.ProfessionalAgenda }

func (f fakeAgendas) Agenda(context.Context, uuid.UUID, uuid.UUID) (*domain.ProfessionalAgenda, error) {
	return f.agenda, nil
}

func (f fakeAgendas) Agendas(context.Context, uuid.UUID) ([]*domain.ProfessionalAgenda, error) {
	return []*domain.ProfessionalAgenda{f.agenda}, nil
}

type fakeQueries struct{}

func (fakeQueries) Get(context.Context, uuid.UUID) (app.AppointmentView, error) {
	return app.AppointmentView{}, errors.New("no usado")
}

func (fakeQueries) List(context.Context, app.ListFilter) ([]app.AppointmentView, string, error) {
	return nil, "", nil
}

func (fakeQueries) PendingRequests(context.Context, uuid.UUID, uuid.UUID) (int, error) { return 0, nil }

type fakePets struct{}

func (fakePets) Pet(context.Context, uuid.UUID) (app.PetInfo, error) {
	return app.PetInfo{ID: petID, GuardianID: guardID, Name: "Luna", Species: "dog"}, nil
}
func (fakePets) Linked(context.Context, uuid.UUID, uuid.UUID) (bool, error)         { return true, nil }
func (fakePets) GuardianLinked(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return true, nil }

type fakeMembership struct{}

func (fakeMembership) RoleOf(_ context.Context, u, _ uuid.UUID) (string, bool, error) {
	if u == vetID {
		return "veterinarian", true, nil
	}
	return "", false, nil
}

func (fakeMembership) Org(context.Context, uuid.UUID) (app.OrgInfo, error) {
	return app.OrgInfo{ID: orgID, Country: "CO", Type: "clinic", Status: "active"}, nil
}

func (fakeMembership) Veterinarians(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{vetID}, nil
}

type inlineTx struct{}

func (inlineTx) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type noProjection struct{}

func (noProjection) Apply(context.Context, []eventstore.StoredEvent) error { return nil }

type env struct {
	svc   *app.Service
	clock *kernel.SimClock
	res   *memReservations
	appts *memAppointments
}

func newEnv(t *testing.T) *env {
	t.Helper()
	clock := kernel.NewSimClock(now0)
	cfg := domain.AgendaConfig{
		TimeZone: "UTC", WeeklyTemplate: map[string][]domain.TimeRange{},
		SlotMinutes:  map[domain.AppointmentType]int{domain.TypeConsultation: 30},
		LeadMinHours: 1, LeadMaxDays: 60, CancelPolicyHrs: 24,
	}
	for _, d := range domain.Weekdays {
		cfg.WeeklyTemplate[d] = []domain.TimeRange{{Start: "08:00", End: "18:00"}}
	}
	e := &env{
		clock: clock,
		res:   &memReservations{clock: clock, rows: map[uuid.UUID]app.Reservation{}},
		appts: &memAppointments{streams: map[uuid.UUID][]any{}},
	}
	e.svc = app.NewService(app.Options{
		Appointments: e.appts, AgendaReader: fakeAgendas{agenda: domain.RestoreAgenda(orgID, vetID, 1, true, cfg, nil)},
		Queries: fakeQueries{}, Reservations: e.res, UoW: inlineTx{}, Projects: noProjection{},
		Pets: fakePets{}, Membership: fakeMembership{}, Clock: clock,
	})
	e.svc.RegisterCommands(bus.New())
	return e
}

func as(user uuid.UUID) context.Context {
	return app.WithCommandContext(context.Background(), app.CommandContext{Principal: app.PrincipalInfo{UserID: user}})
}

// system simula el contexto de un comando programado (sin principal).
func system() context.Context { return context.Background() }

func slotAt(day, hour, min int) (time.Time, time.Time) {
	s := time.Date(2026, 10, day, hour, min, 0, 0, time.UTC)
	return s, s.Add(30 * time.Minute)
}

func (e *env) request(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	s, en := slotAt(30, 10, 0)
	_, err := e.svc.Dispatch(as(guardID), app.RequestAppointment{
		AppointmentID: id, PetID: petID, OrganizationID: orgID, Type: "consultation", Reason: "control",
		Preferences: []domain.Preference{{Start: &s, End: &en}},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return id
}

func (e *env) confirm(id uuid.UUID, day, hour, min int) error {
	s, en := slotAt(day, hour, min)
	_, err := e.svc.Dispatch(as(vetID), app.ConfirmAppointment{
		AppointmentID: id, OrganizationID: orgID, ProfessionalID: vetID, Start: s, End: en,
	})
	return err
}

func code(err error) string {
	var de *kernel.DomainError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}

func status(t *testing.T, e *env, id uuid.UUID) domain.Status {
	t.Helper()
	a, err := e.appts.Load(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Status()
}

// ---- pruebas

func TestConfirm_ReservesSlotAndRejectsOverlap(t *testing.T) {
	e := newEnv(t)
	first, second := e.request(t), e.request(t)
	if err := e.confirm(first, 30, 10, 0); err != nil {
		t.Fatalf("confirmar la primera: %v", err)
	}
	if len(e.res.rows) != 1 {
		t.Fatalf("reservas: %d", len(e.res.rows))
	}
	// Misma franja → SLOT_UNAVAILABLE y la segunda sigue solicitada (sin eventos).
	if err := e.confirm(second, 30, 10, 0); code(err) != "SLOT_UNAVAILABLE" {
		t.Fatalf("esperaba SLOT_UNAVAILABLE, obtuvo %v", err)
	}
	if got := status(t, e, second); got != domain.StatusRequested {
		t.Fatalf("la segunda debe seguir solicitada, está %s", got)
	}
	// Solape parcial también conflictúa; una franja contigua no.
	if err := e.confirm(second, 30, 10, 15); code(err) != "SLOT_UNAVAILABLE" {
		t.Fatalf("solape parcial: %v", err)
	}
	if err := e.confirm(second, 30, 10, 30); err != nil {
		t.Fatalf("franja contigua: %v", err)
	}
}

func TestConfirm_OutsideAgendaIsUnavailable(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	if err := e.confirm(id, 30, 19, 0); code(err) != "SLOT_UNAVAILABLE" { // agenda 08–18
		t.Fatalf("fuera del horario: %v", err)
	}
}

func TestConfirm_RequiresClinicMember(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	s, en := slotAt(30, 10, 0)
	_, err := e.svc.Dispatch(as(otherID), app.ConfirmAppointment{
		AppointmentID: id, OrganizationID: orgID, ProfessionalID: vetID, Start: s, End: en,
	})
	var fb *kernel.ForbiddenError
	if !errors.As(err, &fb) {
		t.Fatalf("esperaba Forbidden, obtuvo %v", err)
	}
}

func TestCancel_ReleasesSlot(t *testing.T) {
	e := newEnv(t)
	first, second := e.request(t), e.request(t)
	if err := e.confirm(first, 30, 10, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Dispatch(as(guardID), app.CancelAppointment{AppointmentID: first, Reason: "viaje"}); err != nil {
		t.Fatalf("cancelar: %v", err)
	}
	if len(e.res.rows) != 0 {
		t.Fatalf("la cancelación debe liberar la reserva: %+v", e.res.rows)
	}
	if err := e.confirm(second, 30, 10, 0); err != nil {
		t.Fatalf("la franja liberada debe poder reservarse: %v", err)
	}
}

func TestCancel_OtherGuardianGets404(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	_, err := e.svc.Dispatch(as(otherID), app.CancelAppointment{AppointmentID: id})
	var nf *kernel.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("esperaba NotFound, obtuvo %v", err)
	}
}

func propose(e *env, id uuid.UUID, day, hour, min int) error {
	s, en := slotAt(day, hour, min)
	_, err := e.svc.Dispatch(as(vetID), app.ProposeAlternative{
		AppointmentID: id, OrganizationID: orgID, ProfessionalID: vetID, Start: s, End: en,
	})
	return err
}

func TestPropose_HoldsSlotProvisionallyThenAccept(t *testing.T) {
	e := newEnv(t)
	id, other := e.request(t), e.request(t)
	if err := propose(e, id, 30, 11, 0); err != nil {
		t.Fatalf("proponer: %v", err)
	}
	if r := e.res.rows[id]; !r.Provisional || r.ExpiresAt == nil || !r.ExpiresAt.Equal(now0.Add(domain.ProposalTTL)) {
		t.Fatalf("reserva provisional esperada a 48 h: %+v", r)
	}
	// Mientras la propuesta vive, la franja está retenida.
	if err := e.confirm(other, 30, 11, 0); code(err) != "SLOT_UNAVAILABLE" {
		t.Fatalf("la franja propuesta debe estar retenida: %v", err)
	}
	if _, err := e.svc.Dispatch(as(guardID), app.AcceptProposal{AppointmentID: id}); err != nil {
		t.Fatalf("aceptar: %v", err)
	}
	if r := e.res.rows[id]; r.Provisional {
		t.Fatalf("aceptar debe volver la reserva firme: %+v", r)
	}
	if got := status(t, e, id); got != domain.StatusConfirmed {
		t.Fatalf("estado: %s", got)
	}
}

func TestAccept_AfterExpiryIsGone(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	if err := propose(e, id, 30, 11, 0); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(domain.ProposalTTL + time.Minute)
	_, err := e.svc.Dispatch(as(guardID), app.AcceptProposal{AppointmentID: id})
	if code(err) != "PROPOSAL_EXPIRED" {
		t.Fatalf("esperaba PROPOSAL_EXPIRED, obtuvo %v", err)
	}
}

func TestExpireProposal_ReleasesSlotAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	id, other := e.request(t), e.request(t)
	if err := propose(e, id, 31, 11, 0); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(domain.ProposalTTL)
	for i := 0; i < 2; i++ { // reentrega del job: mismo resultado
		if _, err := e.svc.Dispatch(system(), app.ExpireProposal{AppointmentID: id}); err != nil {
			t.Fatalf("expirar (%d): %v", i+1, err)
		}
	}
	if len(e.res.rows) != 0 {
		t.Fatalf("expirar debe liberar la reserva: %+v", e.res.rows)
	}
	if got := status(t, e, id); got != domain.StatusRejected {
		t.Fatalf("estado tras expirar: %s", got)
	}
	if err := e.confirm(other, 31, 11, 0); err != nil {
		t.Fatalf("la franja liberada debe poder reservarse: %v", err)
	}
}

func TestExpireProposal_IgnoresAcceptedProposal(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	if err := propose(e, id, 30, 11, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Dispatch(as(guardID), app.AcceptProposal{AppointmentID: id}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(domain.ProposalTTL)
	if _, err := e.svc.Dispatch(system(), app.ExpireProposal{AppointmentID: id}); err != nil {
		t.Fatalf("expirar una propuesta ya aceptada no es error: %v", err)
	}
	if got := status(t, e, id); got != domain.StatusConfirmed {
		t.Fatalf("la cita confirmada no debe cambiar: %s", got)
	}
}

func TestReschedule_MovesReservation(t *testing.T) {
	e := newEnv(t)
	id, other := e.request(t), e.request(t)
	if err := e.confirm(id, 30, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.confirm(other, 30, 14, 0); err != nil {
		t.Fatal(err)
	}
	s, en := slotAt(30, 14, 0)
	_, err := e.svc.Dispatch(as(vetID), app.RescheduleAppointment{AppointmentID: id, OrganizationID: orgID, Start: s, End: en})
	if code(err) != "SLOT_UNAVAILABLE" {
		t.Fatalf("mover sobre otra cita: %v", err)
	}
	if r := e.res.rows[id]; !r.Slot.Start.Equal(time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("un fallo no debe mover la reserva: %+v", r)
	}
	s, en = slotAt(30, 15, 0)
	if _, err := e.svc.Dispatch(as(vetID), app.RescheduleAppointment{AppointmentID: id, OrganizationID: orgID, Start: s, End: en}); err != nil {
		t.Fatalf("reprogramar: %v", err)
	}
	if r := e.res.rows[id]; !r.Slot.Start.Equal(s) || len(e.res.rows) != 2 {
		t.Fatalf("la reserva debe moverse (una por cita): %+v", e.res.rows)
	}
}

func TestAutoNoShow_ReleasesAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	id := e.request(t)
	if err := e.confirm(id, 30, 10, 0); err != nil {
		t.Fatal(err)
	}
	e.clock.Set(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	for i := 0; i < 2; i++ {
		if _, err := e.svc.Dispatch(system(), app.AutoNoShow{AppointmentID: id}); err != nil {
			t.Fatalf("no-show (%d): %v", i+1, err)
		}
	}
	if got := status(t, e, id); got != domain.StatusNoShow || len(e.res.rows) != 0 {
		t.Fatalf("estado %s, reservas %d", got, len(e.res.rows))
	}
}
