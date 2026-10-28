//go:build integration

// Consumidor de outbox con orden por stream y dead-letter (PP-F2-ARQ-008),
// scheduler de comandos con reloj simulado (ARQ-009, QA-003) y lock de
// instancia del worker (ARQ-001).
package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// recordingPM registra entregas (en memoria y en una tabla, para comprobar
// que el savepoint revierte los efectos de un intento fallido) y falla las
// primeras failures[pos] veces (−1 = siempre).
type recordingPM struct {
	mu        sync.Mutex
	delivered []int64
	failures  map[int64]int
}

func (*recordingPM) Name() string      { return "test.recording" }
func (*recordingPM) Handles() []string { return []string{"CareLinkGranted", "PetMarkedDeceased"} }
func (*recordingPM) Emits() []string   { return nil }
func (p *recordingPM) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `INSERT INTO pm_seen (position) VALUES ($1) ON CONFLICT DO NOTHING`, e.GlobalPosition); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := p.failures[e.GlobalPosition]; n != 0 {
		if n > 0 {
			p.failures[e.GlobalPosition] = n - 1
		}
		return errors.New("smtp caído")
	}
	p.delivered = append(p.delivered, e.GlobalPosition)
	return nil
}

func positionOf(t *testing.T, env *projectionEnv, stream uuid.UUID, version int64) int64 {
	t.Helper()
	var pos int64
	if err := env.pool.QueryRow(context.Background(), `SELECT global_position FROM es.events WHERE stream_id = $1 AND stream_version = $2`, stream, version).Scan(&pos); err != nil {
		t.Fatal(err)
	}
	return pos
}

type outboxRow struct {
	published  bool
	attempts   int
	deadLetter bool
}

func outboxState(t *testing.T, env *projectionEnv, pos int64) outboxRow {
	t.Helper()
	var r outboxRow
	if err := env.pool.QueryRow(context.Background(), `SELECT published_at IS NOT NULL, attempts, dead_letter FROM es.outbox WHERE global_position = $1`, pos).
		Scan(&r.published, &r.attempts, &r.deadLetter); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestOutbox_StreamOrderRetriesAndDeadLetter (ARQ-008).
func TestOutbox_StreamOrderRetriesAndDeadLetter(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `CREATE TABLE pm_seen (position bigint PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	petA, petB, petC := kernel.NewID(), kernel.NewID(), kernel.NewID()
	env.appendPet(t, petA, 0, registeredPet(petA), domain.CareLinkGranted{PetID: petA, OrganizationID: kernel.NewID(), GrantedBy: kernel.NewID()},
		domain.PetMarkedDeceased{PetID: petA, DeceasedAt: "2026-10-01"})
	env.appendPet(t, petB, 0, registeredPet(petB), domain.CareLinkGranted{PetID: petB, OrganizationID: kernel.NewID(), GrantedBy: kernel.NewID()})
	env.appendPet(t, petC, 0, registeredPet(petC), domain.CareLinkGranted{PetID: petC, OrganizationID: kernel.NewID(), GrantedBy: kernel.NewID()},
		domain.PetMarkedDeceased{PetID: petC, DeceasedAt: "2026-10-01"})
	a1, a2 := positionOf(t, env, petA, 2), positionOf(t, env, petA, 3)
	b1 := positionOf(t, env, petB, 2)
	c1, c2 := positionOf(t, env, petC, 2), positionOf(t, env, petC, 3)

	pm := &recordingPM{failures: map[int64]int{a1: 2, c1: -1}}
	clock := kernel.NewSimClock(time.Now())
	consumer := process.NewConsumer(env.pool, env.store, nil, process.ConsumerOptions{
		MaxAttempts: 3, BaseBackoff: time.Second, MaxBackoff: time.Minute, Clock: clock, Logger: quietLogger(),
	})
	consumer.Register(pm)
	tick := func() {
		t.Helper()
		if _, err := consumer.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// 1.ª pasada: a1 falla y bloquea a2 (mismo stream); b1 y los eventos sin
	// PM se publican; el efecto del intento fallido se revierte (savepoint).
	tick()
	if s := outboxState(t, env, a1); s.published || s.attempts != 1 {
		t.Fatalf("a1: %+v", s)
	}
	if outboxState(t, env, a2).published || !outboxState(t, env, b1).published {
		t.Fatal("a2 debe esperar a a1; b1 no debe esperar")
	}
	if !outboxState(t, env, positionOf(t, env, petA, 1)).published {
		t.Fatal("eventos sin PM suscrito se marcan publicados")
	}
	if env.count(t, `SELECT count(*) FROM pm_seen WHERE position IN ($1, $2)`, a1, c1) != 0 {
		t.Fatal("los efectos de un intento fallido deben revertirse")
	}

	// Durante el backoff, ni a1 ni a2 se reintentan.
	tick()
	if s := outboxState(t, env, a1); s.attempts != 1 || outboxState(t, env, a2).published {
		t.Fatalf("reintento antes del backoff: %+v", s)
	}

	// Pasado el backoff: 2.º fallo; luego éxito y a2 detrás, en orden.
	for i := 0; i < 2; i++ {
		clock.Advance(time.Minute)
		tick()
	}
	if !outboxState(t, env, a1).published || !outboxState(t, env, a2).published {
		t.Fatal("stream A no se completó")
	}
	pm.mu.Lock()
	order := map[int64]int{}
	for i, p := range pm.delivered {
		order[p] = i
	}
	pm.mu.Unlock()
	if order[a1] > order[a2] {
		t.Fatalf("orden por stream roto: %v", pm.delivered)
	}

	// c1 falla siempre: tras MaxAttempts pasa a dead-letter y no bloquea c2.
	for i := 0; i < 4; i++ {
		clock.Advance(time.Minute)
		tick()
	}
	if s := outboxState(t, env, c1); !s.deadLetter || s.published || s.attempts != 4 {
		t.Fatalf("c1 debe estar en dead-letter: %+v", s)
	}
	clock.Advance(time.Minute)
	tick()
	if !outboxState(t, env, c2).published {
		t.Fatal("dead-letter no debe bloquear el resto del stream")
	}
	if n := env.count(t, `SELECT count(*) FROM es.outbox WHERE published_at IS NULL AND NOT dead_letter`); n != 0 {
		t.Fatalf("quedan %d pendientes", n)
	}
}

type expireThing struct {
	ThingID uuid.UUID `json:"thing_id"`
}

func (expireThing) CommandName() string { return "ExpireThing" }

type notProgrammable struct{}

func (notProgrammable) CommandName() string { return "DeleteEverything" }

// TestScheduler_SimulatedClock (ARQ-009, QA-003): dedup, reprogramación,
// cancelación, ejecución con Principal = system al vencer, reintento con
// backoff, fallo permanente y lista cerrada de comandos.
func TestScheduler_SimulatedClock(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	clock := kernel.NewSimClock(t0)

	allowed := scheduler.NewAllowedCommands()
	allowed.Allow(func() bus.Command { return expireThing{} })
	sched := scheduler.New(allowed, nil)

	var mu sync.Mutex
	var origins []bus.SystemOrigin
	failNext := 1
	commands := bus.New()
	commands.Register(expireThing{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		o, ok := bus.SystemOriginFrom(ctx)
		if !ok {
			return bus.Result{}, errors.New("sin origen de sistema")
		}
		mu.Lock()
		defer mu.Unlock()
		if cmd.(expireThing).ThingID == uuid.Nil {
			return bus.Result{}, errors.New("siempre falla")
		}
		if failNext > 0 {
			failNext--
			return bus.Result{}, errors.New("transitorio")
		}
		origins = append(origins, o)
		return bus.Result{}, nil
	})
	runner := scheduler.NewRunner(env.pool, allowed, commands, nil, scheduler.RunnerOptions{
		MaxAttempts: 3, BaseBackoff: time.Minute, Clock: clock, Logger: quietLogger(),
	})

	thing := kernel.NewID()
	schedule := func(job scheduler.Job) error {
		return env.pool.Do(ctx, func(tx pgx.Tx) error { return sched.Schedule(ctx, tx, job) })
	}
	job := scheduler.Job{RunAt: t0.Add(48 * time.Hour), Command: expireThing{ThingID: thing}, DedupKey: "expire:" + thing.String(),
		CorrelationID: "corr-1", CausationID: "evt-1"}
	// El mismo evento entregado 3 veces → un job (idempotencia del PM).
	for i := 0; i < 3; i++ {
		if err := schedule(job); err != nil {
			t.Fatal(err)
		}
	}
	// Reprogramar con la misma clave mueve run_at.
	job.RunAt = t0.Add(49 * time.Hour)
	if err := schedule(job); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT count(*) FROM scheduler.command_jobs`); n != 1 {
		t.Fatalf("dedup: %d jobs", n)
	}
	if err := schedule(scheduler.Job{RunAt: t0, Command: notProgrammable{}, DedupKey: "x"}); !errors.Is(err, scheduler.ErrNotAllowed) {
		t.Fatalf("comando fuera de AllowedCommands: %v", err)
	}
	cancelled := kernel.NewID()
	if err := schedule(scheduler.Job{RunAt: t0.Add(time.Hour), Command: expireThing{ThingID: cancelled}, DedupKey: "cancel-me"}); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.Do(ctx, func(tx pgx.Tx) error { return sched.Cancel(ctx, tx, "cancel-me") }); err != nil {
		t.Fatal(err)
	}

	runDue := func() int {
		t.Helper()
		n, err := runner.RunDue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := runDue(); n != 0 {
		t.Fatalf("t0: ejecutó %d jobs no vencidos", n)
	}
	clock.Advance(48 * time.Hour)
	if n := runDue(); n != 0 {
		t.Fatal("ejecutó antes del run_at reprogramado")
	}
	clock.Advance(time.Hour)
	if n := runDue(); n != 1 { // falla transitoria → reprogramado con backoff
		t.Fatalf("t0+49h: %d", n)
	}
	if n := runDue(); n != 0 {
		t.Fatal("reintentó antes del backoff")
	}
	clock.Advance(time.Minute)
	if n := runDue(); n != 1 {
		t.Fatal("no reintentó tras el backoff")
	}
	var status string
	var attempts int
	if err := env.pool.QueryRow(ctx, `SELECT status, attempts FROM scheduler.command_jobs WHERE dedup_key = $1`, job.DedupKey).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "done" || attempts != 2 {
		t.Fatalf("job: %s/%d", status, attempts)
	}
	mu.Lock()
	if len(origins) != 1 || origins[0].CorrelationID != "corr-1" || origins[0].CausationID != "evt-1" {
		t.Fatalf("origen de sistema: %+v", origins)
	}
	mu.Unlock()
	if err := env.pool.QueryRow(ctx, `SELECT status FROM scheduler.command_jobs WHERE dedup_key = 'cancel-me'`).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("cancelado: %s %v", status, err)
	}

	// Fallo permanente tras MaxAttempts.
	if err := schedule(scheduler.Job{RunAt: clock.Now(), Command: expireThing{}, DedupKey: "always-fails"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		runDue()
		clock.Advance(time.Hour)
	}
	if err := env.pool.QueryRow(ctx, `SELECT status FROM scheduler.command_jobs WHERE dedup_key = 'always-fails'`).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("fallo permanente: %s %v", status, err)
	}
}

// TestWorker_InstanceLock (ARQ-001): un solo worker por grupo; el segundo
// espera hasta que el primero libera.
func TestWorker_InstanceLock(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	first := worker.NewInstanceLock(env.pool.Pool, "default", nil, quietLogger())
	if err := first.Acquire(ctx, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	second := worker.NewInstanceLock(env.pool.Pool, "default", nil, quietLogger())
	waitCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := second.Acquire(waitCtx, 50*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("el segundo worker obtuvo el lock: %v", err)
	}
	// Otro grupo no compite.
	other := worker.NewInstanceLock(env.pool.Pool, "reports", nil, quietLogger())
	if err := other.Acquire(ctx, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	defer other.Release()

	first.Release()
	if err := second.Acquire(ctx, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if !second.Held() || first.Held() {
		t.Fatal("estado del lock")
	}
	if n := env.count(t, `SELECT count(*) FROM platform.worker_instances WHERE group_name = 'default'`); n != 1 {
		t.Fatalf("worker_instances: %d", n)
	}
}
