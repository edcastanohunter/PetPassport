//go:build integration

// Suscripciones catch-up (PP-F2-ARQ-002), fail-stop (ARQ-007), pausa,
// reanudación y rebuild (ARQ-006, gate QA-002) sobre la primera proyección
// asíncrona clinical.medical_timeline.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"

	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type pgxTx = pgx.Tx

type projectionEnv struct {
	pool  *db.Pool
	store *eventstore.Store
	unit  *db.UnitOfWork
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newProjectionEnv(t *testing.T) *projectionEnv {
	t.Helper()
	url := testkit.Postgres(t)
	pool, err := db.Open(context.Background(), url, 16, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := eventstore.NewRegistry()
	petinfra.RegisterEvents(reg)
	petinfra.RegisterUpcasters(reg)
	orginfra.RegisterEvents(reg)
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	return &projectionEnv{pool: pool, store: eventstore.NewStore(reg, pool, nil), unit: db.NewUnitOfWork(pool)}
}

// subs crea las suscripciones con backoff corto (las pruebas no esperan 31 s).
func (e *projectionEnv) subs(extra ...projection.AsyncProjector) *projection.Subscriptions {
	s := projection.NewSubscriptions(e.pool, e.store, nil, projection.Options{
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond},
		IdlePoll: 5 * time.Millisecond, BatchSize: 100, Logger: quietLogger(),
	})
	s.Register(clinicalinfra.NewTimelineProjector())
	for _, p := range extra {
		s.Register(p)
	}
	return s
}

// appendPet añade eventos de dominio al stream de una mascota.
func (e *projectionEnv) appendPet(t *testing.T, pet uuid.UUID, expected int64, events ...eventstore.Event) {
	t.Helper()
	err := e.unit.Do(context.Background(), func(ctx context.Context) error {
		tx, err := db.TxFrom(ctx)
		if err != nil {
			return err
		}
		evs := make([]eventstore.NewEvent, len(events))
		for i, ev := range events {
			evs[i] = eventstore.NewEvent{ID: kernel.NewID(), Payload: ev, OccurredAt: time.Now().UTC(),
				Metadata: eventstore.Metadata{Source: eventstore.SourceAPI, CorrelationID: "test"}}
		}
		_, err = e.store.Append(ctx, tx, pet, domain.StreamType, expected, evs)
		return err
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
}

func registeredPet(pet uuid.UUID) domain.PetRegistered {
	return domain.PetRegistered{PetID: pet, GuardianID: kernel.NewID(), Name: "Luna", Species: domain.SpeciesDog,
		Sex: domain.SexFemale, Neutered: domain.TristateYes, Alerts: []domain.PetAlert{}}
}

func (e *projectionEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (e *projectionEnv) checkpoint(t *testing.T, name string) projection.Checkpoint {
	t.Helper()
	cps, err := projection.Checkpoints(context.Background(), e.pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cps {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("sin checkpoint %s", name)
	return projection.Checkpoint{}
}

func (e *projectionEnv) head(t *testing.T) int64 {
	t.Helper()
	h, err := e.store.Head(context.Background(), e.pool)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout esperando: %s", what)
}

func jsonHas(raw json.RawMessage, field string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_, ok := m[field]
	return ok
}

// TestProjection_CatchUpTimeline (ARQ-002): la suscripción proyecta solo sus
// tipos, avanza el checkpoint hasta head aunque los últimos eventos no le
// interesen, y proyectar dos veces deja el mismo estado.
func TestProjection_CatchUpTimeline(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	pet, org := kernel.NewID(), kernel.NewID()
	env.appendPet(t, pet, 0, registeredPet(pet),
		domain.CareLinkGranted{PetID: pet, OrganizationID: org, GrantedBy: kernel.NewID()},
		domain.CareLinkRevoked{PetID: pet, OrganizationID: org, RevokedBy: kernel.NewID(), Reason: "texto libre"},
		domain.PetGuardianshipTransferred{PetID: pet, FromGuardianID: kernel.NewID(), ToGuardianID: kernel.NewID()},
		domain.PetMarkedDeceased{PetID: pet, DeceasedAt: "2026-10-01"})

	subs := env.subs()
	if err := subs.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT count(*) FROM read_clinical.medical_timeline WHERE pet_id = $1`, pet); n != 3 {
		t.Fatalf("timeline: %d filas, want 3", n)
	}
	if n := env.count(t, `SELECT count(*) FROM read_clinical.medical_timeline WHERE summary IS NOT NULL`); n != 0 {
		t.Fatal("el motivo de revocación (texto libre) no debe llegar al timeline")
	}
	cp := env.checkpoint(t, clinicalinfra.TimelineProjectorName)
	if cp.LastPosition != env.head(t) || cp.Status != projection.StatusRunning || cp.Mode != "async" {
		t.Fatalf("checkpoint: %+v head %d", cp, env.head(t))
	}

	// Proyectar dos veces = mismo estado (idempotencia por PK).
	events, err := env.store.ReadAll(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	p := clinicalinfra.NewTimelineProjector()
	if err := env.pool.Do(ctx, func(tx pgx.Tx) error {
		for _, e := range events {
			if err := p.Project(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT count(*) FROM read_clinical.medical_timeline`); n != 3 {
		t.Fatalf("reproyectar duplicó: %d", n)
	}
}

// flaky es un proyector que falla (error o panic) en un tipo de evento
// mientras broken sea true; cuenta lo proyectado en una tabla propia.
type flaky struct {
	broken *atomic.Bool
	panics bool
}

func (flaky) Name() string      { return "test.flaky" }
func (flaky) Handles() []string { return []string{"PetRegistered", "PetMarkedDeceased"} }
func (f flaky) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	if e.Type == "PetMarkedDeceased" && f.broken.Load() {
		if f.panics {
			panic("proyector roto")
		}
		return errors.New("proyector roto")
	}
	_, err := tx.Exec(ctx, `INSERT INTO flaky_seen (position) VALUES ($1) ON CONFLICT DO NOTHING`, e.GlobalPosition)
	return err
}
func (flaky) Reset(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DELETE FROM flaky_seen`)
	return err
}

// TestProjection_FailStopAndResume (ARQ-007): un proyector que falla agota el
// backoff, queda en failed con last_error sin confirmar el lote parcial, la
// otra proyección sigue avanzando, y tras `resume` reprocesa y alcanza head.
// Se prueba con error y con panic.
func TestProjection_FailStopAndResume(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%v", panics), func(t *testing.T) {
			env := newProjectionEnv(t)
			ctx := context.Background()
			if _, err := env.pool.Exec(ctx, `CREATE TABLE flaky_seen (position bigint PRIMARY KEY)`); err != nil {
				t.Fatal(err)
			}
			pet, org := kernel.NewID(), kernel.NewID()
			env.appendPet(t, pet, 0, registeredPet(pet),
				domain.CareLinkGranted{PetID: pet, OrganizationID: org, GrantedBy: kernel.NewID()},
				domain.PetMarkedDeceased{PetID: pet, DeceasedAt: "2026-10-01"})

			broken := &atomic.Bool{}
			broken.Store(true)
			subs := env.subs(flaky{broken: broken, panics: panics})
			stop := startRun(ctx, subs)
			defer stop()

			waitFor(t, "fail-stop", func() bool {
				return env.checkpoint(t, "test.flaky").Status == projection.StatusFailed
			})
			cp := env.checkpoint(t, "test.flaky")
			if !strings.Contains(cp.LastError, "proyector roto") || cp.FailedAt == nil {
				t.Fatalf("checkpoint fallido: %+v", cp)
			}
			// Lote parcial nunca confirma: ni checkpoint ni filas del lote.
			if cp.LastPosition != 0 || env.count(t, `SELECT count(*) FROM flaky_seen`) != 0 {
				t.Fatalf("lote parcial confirmado: pos %d filas %d", cp.LastPosition, env.count(t, `SELECT count(*) FROM flaky_seen`))
			}
			// Las demás proyecciones continúan.
			waitFor(t, "timeline al día", func() bool {
				return env.checkpoint(t, clinicalinfra.TimelineProjectorName).LastPosition == env.head(t)
			})

			// Corregido el "código", resume reanuda desde el checkpoint.
			broken.Store(false)
			if err := subs.Resume(ctx, "test.flaky"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "flaky al día tras resume", func() bool {
				c := env.checkpoint(t, "test.flaky")
				return c.Status == projection.StatusRunning && c.LastPosition == env.head(t)
			})
			if cp := env.checkpoint(t, "test.flaky"); cp.LastError != "" || cp.FailedAt != nil {
				t.Fatalf("resume no limpia el error: %+v", cp)
			}
			if n := env.count(t, `SELECT count(*) FROM flaky_seen`); n != 2 {
				t.Fatalf("flaky_seen: %d, want 2", n)
			}
		})
	}
}

// TestProjection_PauseResume (ARQ-006): una proyección pausada no avanza.
func TestProjection_PauseResume(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	subs := env.subs()
	if err := subs.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := subs.Pause(ctx, clinicalinfra.TimelineProjectorName); err != nil {
		t.Fatal(err)
	}
	pet := kernel.NewID()
	env.appendPet(t, pet, 0, registeredPet(pet), domain.CareLinkGranted{PetID: pet, OrganizationID: kernel.NewID(), GrantedBy: kernel.NewID()})
	if err := subs.CatchUp(ctx); err == nil {
		t.Fatal("CatchUp sobre una proyección pausada debe informar del estado")
	}
	if cp := env.checkpoint(t, clinicalinfra.TimelineProjectorName); cp.LastPosition != 0 || cp.Status != projection.StatusPaused {
		t.Fatalf("pausada avanzó: %+v", cp)
	}
	if err := subs.Resume(ctx, clinicalinfra.TimelineProjectorName); err != nil {
		t.Fatal(err)
	}
	if err := subs.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT count(*) FROM read_clinical.medical_timeline`); n != 1 {
		t.Fatalf("timeline tras resume: %d", n)
	}
	if err := subs.Pause(ctx, "no.existe"); err == nil {
		t.Fatal("pausar una proyección desconocida debe fallar")
	}
}

// generateScenario genera eventos sintéticos de un escenario de clínicas y
// mascotas (QA-002): registros, vínculos, revocaciones, transferencias y
// defunciones con reparto pseudoaleatorio reproducible. Encounter, vacunas y
// citas se añaden al generador cuando existan (M2.1–M2.3). Devuelve el número
// de eventos escritos (≥ target).
func generateScenario(t *testing.T, env *projectionEnv, target int, seed uint64) int {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // escenario reproducible por semilla
	orgs := make([]uuid.UUID, 20)
	for i := range orgs {
		orgs[i] = kernel.NewID()
	}
	written := 0
	for written < target {
		pet := kernel.NewID()
		events := []eventstore.Event{registeredPet(pet)}
		linked := map[uuid.UUID]bool{}
		for i, n := 0, 3+rng.IntN(10); i < n; i++ {
			org := orgs[rng.IntN(len(orgs))]
			switch r := rng.IntN(10); {
			case r < 5 && !linked[org]:
				events = append(events, domain.CareLinkGranted{PetID: pet, OrganizationID: org, GrantedBy: kernel.NewID()})
				linked[org] = true
			case r < 8 && linked[org]:
				events = append(events, domain.CareLinkRevoked{PetID: pet, OrganizationID: org, RevokedBy: kernel.NewID()})
				delete(linked, org)
			case r < 9:
				events = append(events, domain.PetGuardianshipTransferred{PetID: pet, FromGuardianID: kernel.NewID(), ToGuardianID: kernel.NewID()})
			default:
				events = append(events, domain.PetProfileUpdated{PetID: pet, Changes: domain.Changes{Breed: ptrTo("Mestizo")}})
			}
		}
		// Streams en varios Appends para intercalar posiciones entre mascotas.
		half := len(events) / 2
		env.appendPet(t, pet, 0, events[:half]...)
		env.appendPet(t, pet, int64(half), events[half:]...)
		written += len(events)
	}
	return written
}

func ptrTo[T any](v T) *T { return &v }

// dumpAsync vuelca todas las proyecciones asíncronas ordenadas, sin
// updated_at (QA-002).
func dumpAsync(t *testing.T, env *projectionEnv) string {
	t.Helper()
	rows, err := env.pool.Query(context.Background(), `
		SELECT pet_id, position, occurred_at, kind, ref_id, organization_id, title, summary, visibility
		  FROM read_clinical.medical_timeline ORDER BY pet_id, position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, vals...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestProjection_QA002_RebuildDumpsEqual es el gate de rebuild: 5.000 eventos
// sintéticos, proyección en vivo, `rebuild --all` con el worker corriendo y
// comparación exacta de dumps; además rebuild parcial --to-position.
func TestProjection_QA002_RebuildDumpsEqual(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	n := generateScenario(t, env, 5000, 42)
	if n < 5000 {
		t.Fatalf("escenario: %d eventos", n)
	}

	subs := env.subs()
	stop := startRun(ctx, subs)
	defer stop()
	head := env.head(t)
	waitFor(t, "proyección en vivo al día", func() bool {
		return env.checkpoint(t, clinicalinfra.TimelineProjectorName).LastPosition == head
	})
	before := dumpAsync(t, env)
	if strings.Count(before, "\n") < 500 {
		t.Fatalf("dump sospechosamente pequeño: %d filas", strings.Count(before, "\n"))
	}

	var batches int
	if err := subs.RebuildAll(ctx, 0, func(string, int64, int64, int) { batches++ }); err != nil {
		t.Fatal(err)
	}
	if batches < 2 {
		t.Fatalf("rebuild en %d lotes: no ejercita el replay por lotes", batches)
	}
	if cp := env.checkpoint(t, clinicalinfra.TimelineProjectorName); cp.Status != projection.StatusRunning || cp.LastPosition != head {
		t.Fatalf("tras rebuild: %+v", cp)
	}
	if after := dumpAsync(t, env); after != before {
		t.Fatal("el dump tras rebuild --all difiere del proyectado en vivo")
	}

	// Rebuild parcial: solo filas con posición ≤ N; luego reanuda hasta head.
	mid := head / 2
	stop()
	if err := subs.Rebuild(ctx, clinicalinfra.TimelineProjectorName, mid, nil); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT count(*) FROM read_clinical.medical_timeline WHERE position > $1`, mid); n != 0 {
		t.Fatalf("rebuild --to-position proyectó %d filas posteriores", n)
	}
	if err := subs.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if after := dumpAsync(t, env); after != before {
		t.Fatal("rebuild parcial + catch-up difiere")
	}
}

// startRun arranca subs.Run y devuelve un stop idempotente que espera a que
// termine el lote en curso.
func startRun(ctx context.Context, subs *projection.Subscriptions) func() {
	if err := subs.Ensure(ctx); err != nil {
		panic(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = subs.Run(runCtx); close(done) }()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}
