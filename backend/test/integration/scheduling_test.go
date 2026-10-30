//go:build integration

// Scheduling (PP-F2-DAT-004, ARQ-004, QA-001): la restricción EXCLUDE de
// slot_reservations es la autoridad de no-solapamiento (ADR-011) y los
// snapshots de la agenda se escriben y se leen en Postgres.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/infra"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type schedulingEnv struct {
	pool  *db.Pool
	unit  *db.UnitOfWork
	store *eventstore.Store
	reg   *eventstore.Registry
	res   *infra.Reservations
}

func newSchedulingEnv(t *testing.T) *schedulingEnv {
	t.Helper()
	pool, err := db.Open(context.Background(), testkit.Postgres(t), 16, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := eventstore.NewRegistry()
	infra.RegisterEvents(reg)
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	return &schedulingEnv{
		pool: pool, unit: db.NewUnitOfWork(pool), store: eventstore.NewStore(reg, pool, nil), reg: reg,
		res: infra.NewReservations(pool.Pool),
	}
}

func (e *schedulingEnv) reserve(res app.Reservation) error {
	return e.unit.Do(context.Background(), func(ctx context.Context) error { return e.res.Reserve(ctx, res) })
}

func slot(start time.Time, minutes int) domain.Slot {
	return domain.Slot{Start: start, End: start.Add(time.Duration(minutes) * time.Minute)}
}

// QA-001: 100 reservas solapadas concurrentes del mismo profesional → exactamente
// una gana; el resto recibe SLOT_UNAVAILABLE.
func TestSlotReservations_ConcurrentOverlapExactlyOneWins(t *testing.T) {
	e := newSchedulingEnv(t)
	prof := uuid.New()
	base := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Hour)
	const n = 100
	var won, unavailable, other atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Todas se solapan: inicios escalonados dentro de la misma hora.
			err := e.reserve(app.Reservation{
				AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(base.Add(time.Duration(i%30)*time.Minute), 45),
			})
			switch {
			case err == nil:
				won.Add(1)
			case errors.Is(err, domain.ErrSlotUnavailable):
				unavailable.Add(1)
			default:
				other.Add(1)
				t.Errorf("error inesperado: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if won.Load() != 1 || unavailable.Load() != n-1 || other.Load() != 0 {
		t.Fatalf("ganadoras=%d no-disponibles=%d otros=%d", won.Load(), unavailable.Load(), other.Load())
	}
}

// Franjas contiguas ([) semiabierto) y de otro profesional no conflictúan.
func TestSlotReservations_AdjacentAndOtherProfessionalAreFree(t *testing.T) {
	e := newSchedulingEnv(t)
	prof, other := uuid.New(), uuid.New()
	start := time.Now().UTC().Add(96 * time.Hour).Truncate(time.Hour)
	for _, r := range []app.Reservation{
		{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start, 30)},
		{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start.Add(30*time.Minute), 30)},
		{AppointmentID: uuid.New(), ProfessionalID: other, Slot: slot(start, 30)},
	} {
		if err := e.reserve(r); err != nil {
			t.Fatalf("reserva %+v: %v", r, err)
		}
	}
	if err := e.reserve(app.Reservation{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start.Add(15*time.Minute), 30)}); !errors.Is(err, domain.ErrSlotUnavailable) {
		t.Fatalf("solape parcial: %v", err)
	}
}

// Mover una cita (misma appointment_id) reemplaza su reserva sin chocar consigo
// misma; liberar deja la franja libre.
func TestSlotReservations_ReplaceAndRelease(t *testing.T) {
	e := newSchedulingEnv(t)
	prof, appt := uuid.New(), uuid.New()
	start := time.Now().UTC().Add(120 * time.Hour).Truncate(time.Hour)
	if err := e.reserve(app.Reservation{AppointmentID: appt, ProfessionalID: prof, Slot: slot(start, 30)}); err != nil {
		t.Fatal(err)
	}
	if err := e.reserve(app.Reservation{AppointmentID: appt, ProfessionalID: prof, Slot: slot(start.Add(15*time.Minute), 30)}); err != nil {
		t.Fatalf("reprogramar sobre su propia franja: %v", err)
	}
	var count int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM read_scheduling.slot_reservations WHERE appointment_id = $1`, appt).Scan(&count); err != nil || count != 1 {
		t.Fatalf("una reserva por cita: %d (%v)", count, err)
	}
	if err := e.unit.Do(context.Background(), func(ctx context.Context) error { return e.res.Release(ctx, appt) }); err != nil {
		t.Fatal(err)
	}
	if err := e.reserve(app.Reservation{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start, 30)}); err != nil {
		t.Fatalf("franja liberada: %v", err)
	}
}

// Una reserva provisional vencida (propuesta de 48 h) no bloquea y se purga al
// reservar; una vigente sí bloquea.
func TestSlotReservations_ExpiredProvisionalIsPurged(t *testing.T) {
	e := newSchedulingEnv(t)
	prof := uuid.New()
	start := time.Now().UTC().Add(144 * time.Hour).Truncate(time.Hour)
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	if err := e.reserve(app.Reservation{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start, 30), Provisional: true, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if err := e.reserve(app.Reservation{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start, 30), Provisional: true, ExpiresAt: &future}); err != nil {
		t.Fatalf("la provisional vencida no debe bloquear: %v", err)
	}
	if err := e.reserve(app.Reservation{AppointmentID: uuid.New(), ProfessionalID: prof, Slot: slot(start, 30)}); !errors.Is(err, domain.ErrSlotUnavailable) {
		t.Fatalf("la provisional vigente debe bloquear: %v", err)
	}
	got, err := e.res.Reserved(context.Background(), prof, start.Add(-time.Hour), start.Add(time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("Reserved: %v (%v)", got, err)
	}
}

// ARQ-004: el snapshot de la agenda se escribe al cruzar 100 eventos y Load
// (snapshot + posteriores) reproduce el mismo estado que el replay completo.
func TestAgendaSnapshot_WrittenAtThresholdAndEquivalentToReplay(t *testing.T) {
	e := newSchedulingEnv(t)
	repo := infra.NewAgendaRepository(e.store, e.reg, nil, nil)
	org, prof := uuid.New(), uuid.New()
	ctx := context.Background()
	meta := eventstore.Metadata{Source: eventstore.SourceAPI}
	now := time.Now().UTC()

	run := func(cmd any) {
		t.Helper()
		err := e.unit.Do(ctx, func(ctx context.Context) error {
			a, err := repo.Load(ctx, org, prof)
			if err != nil {
				return err
			}
			if _, err := a.HandleCommand(cmd); err != nil {
				return err
			}
			_, err = repo.Save(ctx, a, meta)
			return err
		})
		if err != nil {
			t.Fatalf("%T: %v", cmd, err)
		}
	}
	cfg := domain.AgendaConfig{
		TimeZone: "America/Bogota", WeeklyTemplate: map[string][]domain.TimeRange{"mon": {{Start: "08:00", End: "12:00"}}},
		SlotMinutes:  map[domain.AppointmentType]int{domain.TypeConsultation: 30},
		LeadMinHours: 1, LeadMaxDays: 60, CancelPolicyHrs: 24,
	}
	run(domain.DefineAgenda{OrganizationID: org, ProfessionalID: prof, Config: cfg, ProfessionalOK: true, By: uuid.New(), Now: now})
	day := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 104; i++ { // 1 + 104 = 105 eventos
		run(domain.AddException{Date: day.AddDate(0, 0, i).Format("2006-01-02"), Exception: domain.DayException{Closed: true}, By: uuid.New(), Now: now})
	}

	stream := infra.AgendaStreamID(org, prof)
	var snapVersion int64
	if err := e.pool.QueryRow(ctx, `SELECT stream_version FROM es.snapshots WHERE stream_id = $1`, stream).Scan(&snapVersion); err != nil {
		t.Fatalf("snapshot ausente: %v", err)
	}
	if snapVersion != domain.SnapshotThreshold {
		t.Fatalf("snapshot en versión %d, esperaba %d", snapVersion, domain.SnapshotThreshold)
	}

	// Load con snapshot.
	var withSnap *domain.ProfessionalAgenda
	if err := e.unit.Do(ctx, func(ctx context.Context) error {
		var err error
		withSnap, err = repo.Load(ctx, org, prof)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Replay completo sin snapshot.
	full := &domain.ProfessionalAgenda{}
	if err := e.unit.Do(ctx, func(ctx context.Context) error {
		tx, err := db.TxFrom(ctx)
		if err != nil {
			return err
		}
		events, err := e.store.Load(ctx, tx, stream, 0)
		if err != nil {
			return err
		}
		for _, ev := range events {
			decoded, err := e.reg.Decode(ev)
			if err != nil {
				return err
			}
			if err := full.ApplyEvent(decoded); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if withSnap.Version() != 105 || full.Version() != 105 {
		t.Fatalf("versiones: snapshot=%d replay=%d", withSnap.Version(), full.Version())
	}
	a, _ := json.Marshal(struct {
		C domain.AgendaConfig
		E map[string]domain.DayException
		A bool
	}{withSnap.Config(), withSnap.Exceptions(), withSnap.Active()})
	b, _ := json.Marshal(struct {
		C domain.AgendaConfig
		E map[string]domain.DayException
		A bool
	}{full.Config(), full.Exceptions(), full.Active()})
	if string(a) != string(b) {
		t.Fatalf("snapshot y replay difieren:\n%s\n%s", a, b)
	}
	if len(withSnap.Exceptions()) != 104 {
		t.Fatalf("excepciones: %d", len(withSnap.Exceptions()))
	}
}
