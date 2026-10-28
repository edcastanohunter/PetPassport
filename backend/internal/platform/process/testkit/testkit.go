// Package testkit es el harness de process managers (PP-F2-QA-003):
//
//	testkit.Process(t, func(jobs process.Jobs, clock kernel.Clock) process.ProcessManager {
//		return scheduling.NewCompleteOnSign(jobs, clock)
//	}).
//		At(t0).
//		GivenEvents(clinical.EncounterSigned{...}).
//		ThenCommands(scheduling.CompleteAppointment{...}).
//		ThenScheduled(testkit.Scheduled{RunAt: t0.Add(48 * time.Hour), Command: ..., DedupKey: "..."})
//
// El harness entrega cada evento al PM con la semántica del scheduler real
// (Schedule actualiza solo si sigue pendiente; Cancel cancela pendientes) y
// comprueba siempre la idempotencia: reentrega todos los eventos dos veces
// más (mismo evento 3 veces) y exige el mismo resultado. Los comandos se
// validan contra Emits del PM (Guard), como en producción.
//
// PMs que escriben en tablas (notifications.*) se prueban en integración; el
// harness pasa tx = nil.
package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Scheduled es un job esperado.
type Scheduled struct {
	RunAt    time.Time
	Command  bus.Command
	DedupKey string
}

// Harness es el escenario de prueba de un PM.
type Harness struct {
	t      testing.TB
	build  func(process.Jobs, kernel.Clock) process.ProcessManager
	clock  *kernel.SimClock
	events []eventstore.StoredEvent
	ran    bool
	jobs   *Recorder
	err    error
	pos    int64
}

// Process crea el harness. build recibe los Jobs (ya limitados por Guard) y
// el reloj simulado.
func Process(t testing.TB, build func(jobs process.Jobs, clock kernel.Clock) process.ProcessManager) *Harness {
	t.Helper()
	return &Harness{t: t, build: build, clock: kernel.NewSimClock(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))}
}

// At fija el instante del reloj simulado antes de entregar eventos.
func (h *Harness) At(now time.Time) *Harness {
	h.clock.Set(now)
	return h
}

// Clock expone el reloj simulado.
func (h *Harness) Clock() *kernel.SimClock { return h.clock }

// GivenEvents añade eventos a entregar, en orden. Acepta eventstore.StoredEvent
// o eventos de dominio (eventstore.Event), que se serializan como en el store.
func (h *Harness) GivenEvents(events ...any) *Harness {
	h.t.Helper()
	for _, ev := range events {
		switch e := ev.(type) {
		case eventstore.StoredEvent:
			h.events = append(h.events, e)
		case eventstore.Event:
			raw, err := json.Marshal(e)
			if err != nil {
				h.t.Fatalf("testkit: serializar %T: %v", e, err)
			}
			h.pos++
			h.events = append(h.events, eventstore.StoredEvent{
				GlobalPosition: h.pos, EventID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%d", h.pos))),
				StreamID: uuid.Nil, Type: e.EventName(), SchemaVersion: e.SchemaVersion(), Payload: raw,
				Metadata:   eventstore.Metadata{CorrelationID: "corr-test", Source: eventstore.SourceAPI},
				OccurredAt: h.clock.Now(), RecordedAt: h.clock.Now(),
			})
		default:
			h.t.Fatalf("testkit: %T no es un evento", ev)
		}
	}
	return h
}

func (h *Harness) run() {
	h.t.Helper()
	if h.ran {
		return
	}
	h.ran = true
	deliver := func() (*Recorder, error) {
		rec := NewRecorder()
		probe := h.build(nil, h.clock) // solo para leer Name/Emits del Guard
		pm := h.build(process.Guard(probe, rec), h.clock)
		for round := 0; round < 3; round++ { // idempotencia: cada evento 3 veces
			for _, e := range h.events {
				if !handles(pm, e.Type) {
					continue
				}
				if err := pm.Handle(context.Background(), e, nil); err != nil {
					return rec, fmt.Errorf("%s@%d (entrega %d): %w", e.Type, e.GlobalPosition, round+1, err)
				}
			}
			if round == 0 {
				rec.snapshot()
			}
		}
		return rec, nil
	}
	h.jobs, h.err = deliver()
	if h.err == nil && !h.jobs.sameAsSnapshot() {
		h.t.Fatalf("testkit: el PM no es idempotente: reentregar los eventos cambió el resultado\nprimera: %v\ntres:    %v",
			h.jobs.first, h.jobs.pendingList())
	}
}

func handles(pm process.ProcessManager, t string) bool {
	for _, h := range pm.Handles() {
		if h == t {
			return true
		}
	}
	return false
}

// ThenError exige que la entrega falle con un error que contenga msg.
func (h *Harness) ThenError(msg string) {
	h.t.Helper()
	h.run()
	if h.err == nil {
		h.t.Fatalf("testkit: se esperaba error %q", msg)
	}
	if !strings.Contains(h.err.Error(), msg) {
		h.t.Fatalf("testkit: error %q no contiene %q", h.err, msg)
	}
}

// ThenCommands exige que los comandos despachados (jobs pendientes con
// RunAt ≤ ahora) sean exactamente cmds, en cualquier orden.
func (h *Harness) ThenCommands(cmds ...bus.Command) *Harness {
	h.t.Helper()
	h.run()
	h.noErr()
	var got []bus.Command
	for _, j := range h.jobs.pending() {
		if !j.RunAt.After(h.clock.Now()) {
			got = append(got, j.Command)
		}
	}
	if !sameCommands(got, cmds) {
		h.t.Fatalf("testkit: comandos despachados\ngot:  %v\nwant: %v", got, cmds)
	}
	return h
}

// ThenScheduled exige que los jobs pendientes con RunAt > ahora sean
// exactamente want (comparando RunAt, comando y clave).
func (h *Harness) ThenScheduled(want ...Scheduled) *Harness {
	h.t.Helper()
	h.run()
	h.noErr()
	var got []Scheduled
	for _, j := range h.jobs.pending() {
		if j.RunAt.After(h.clock.Now()) {
			got = append(got, Scheduled{RunAt: j.RunAt, Command: j.Command, DedupKey: j.DedupKey})
		}
	}
	sortScheduled(got)
	sortScheduled(want)
	if len(got) != len(want) {
		h.t.Fatalf("testkit: jobs programados\ngot:  %v\nwant: %v", got, want)
	}
	for i := range got {
		if !got[i].RunAt.Equal(want[i].RunAt) || got[i].DedupKey != want[i].DedupKey || !reflect.DeepEqual(got[i].Command, want[i].Command) {
			h.t.Fatalf("testkit: job %d\ngot:  %+v\nwant: %+v", i, got[i], want[i])
		}
	}
	return h
}

// ThenNothing exige que no haya comandos ni jobs pendientes.
func (h *Harness) ThenNothing() {
	h.t.Helper()
	h.run()
	h.noErr()
	if p := h.jobs.pending(); len(p) > 0 {
		h.t.Fatalf("testkit: se esperaba ningún job y hay %v", p)
	}
}

// Jobs devuelve el registro (aserciones a medida: cancelados, propagación de
// correlation/causation).
func (h *Harness) Jobs() *Recorder {
	h.run()
	h.noErr()
	return h.jobs
}

func (h *Harness) noErr() {
	h.t.Helper()
	if h.err != nil {
		h.t.Fatalf("testkit: el PM falló: %v", h.err)
	}
}

func sameCommands(a, b []bus.Command) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
outer:
	for _, x := range a {
		for i, y := range b {
			if !used[i] && reflect.DeepEqual(x, y) {
				used[i] = true
				continue outer
			}
		}
		return false
	}
	return true
}

func sortScheduled(s []Scheduled) {
	sort.Slice(s, func(i, j int) bool {
		if !s[i].RunAt.Equal(s[j].RunAt) {
			return s[i].RunAt.Before(s[j].RunAt)
		}
		return s[i].DedupKey < s[j].DedupKey
	})
}

// Job es un job registrado con su estado.
type Job struct {
	scheduler.Job
	Status string // pending | cancelled
}

// Recorder implementa process.Jobs en memoria con la semántica de
// scheduler.Scheduler.
type Recorder struct {
	byKey map[string]*Job
	order []string
	first []Job
}

// NewRecorder crea un registro vacío.
func NewRecorder() *Recorder { return &Recorder{byKey: map[string]*Job{}} }

var _ process.Jobs = (*Recorder)(nil)

// Schedule implementa process.Jobs.
func (r *Recorder) Schedule(_ context.Context, _ pgx.Tx, job scheduler.Job) error {
	if job.DedupKey == "" {
		return fmt.Errorf("scheduler: dedup_key obligatoria")
	}
	if j, ok := r.byKey[job.DedupKey]; ok {
		if j.Status == "pending" {
			j.Job = job
		}
		return nil
	}
	r.byKey[job.DedupKey] = &Job{Job: job, Status: "pending"}
	r.order = append(r.order, job.DedupKey)
	return nil
}

// Cancel implementa process.Jobs.
func (r *Recorder) Cancel(_ context.Context, _ pgx.Tx, dedupKey string) error {
	if j, ok := r.byKey[dedupKey]; ok && j.Status == "pending" {
		j.Status = "cancelled"
	}
	return nil
}

// All devuelve todos los jobs en orden de primera programación.
func (r *Recorder) All() []Job {
	out := make([]Job, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, *r.byKey[k])
	}
	return out
}

func (r *Recorder) pending() []scheduler.Job {
	var out []scheduler.Job
	for _, j := range r.All() {
		if j.Status == "pending" {
			out = append(out, j.Job)
		}
	}
	return out
}

func (r *Recorder) pendingList() []Job { return r.All() }

func (r *Recorder) snapshot() { r.first = r.All() }

func (r *Recorder) sameAsSnapshot() bool { return reflect.DeepEqual(r.first, r.All()) }
