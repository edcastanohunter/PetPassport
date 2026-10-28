package testkit_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process/testkit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Un PM de ejemplo con la forma de los de F2 (02 §7): una solicitud crea un
// job de expiración a 14 días; su resolución lo cancela; una aprobación
// despacha un comando inmediato.
type requested struct {
	RequestID uuid.UUID `json:"request_id"`
}

func (requested) EventName() string  { return "TestRequested" }
func (requested) SchemaVersion() int { return 1 }

type resolved struct {
	RequestID uuid.UUID `json:"request_id"`
	Approved  bool      `json:"approved"`
}

func (resolved) EventName() string  { return "TestResolved" }
func (resolved) SchemaVersion() int { return 1 }

type expireRequest struct {
	RequestID uuid.UUID `json:"request_id"`
}

func (expireRequest) CommandName() string { return "ExpireTestRequest" }

type notifyApproved struct {
	RequestID uuid.UUID `json:"request_id"`
}

func (notifyApproved) CommandName() string { return "NotifyTestApproved" }

type samplePM struct {
	jobs  process.Jobs
	clock kernel.Clock
	emits []string
}

func (p samplePM) Name() string      { return "test.sample" }
func (p samplePM) Handles() []string { return []string{"TestRequested", "TestResolved"} }
func (p samplePM) Emits() []string {
	if p.emits != nil {
		return p.emits
	}
	return []string{"ExpireTestRequest", "NotifyTestApproved"}
}

func (p samplePM) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	switch e.Type {
	case "TestRequested":
		var r requested
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return err
		}
		return p.jobs.Schedule(ctx, tx, process.JobFor(e, e.OccurredAt.Add(14*24*time.Hour), expireRequest(r), "expire:"+r.RequestID.String()))
	case "TestResolved":
		var r resolved
		if err := json.Unmarshal(e.Payload, &r); err != nil {
			return err
		}
		if err := p.jobs.Cancel(ctx, tx, "expire:"+r.RequestID.String()); err != nil {
			return err
		}
		if r.Approved {
			return p.jobs.Schedule(ctx, tx, process.JobFor(e, p.clock.Now(), notifyApproved{RequestID: r.RequestID}, "approved:"+r.RequestID.String()))
		}
	}
	return nil
}

var (
	t0  = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	req = uuid.MustParse("0192f3a1-0000-7000-8000-00000000aa01")
)

func build(emits []string) func(process.Jobs, kernel.Clock) process.ProcessManager {
	return func(j process.Jobs, c kernel.Clock) process.ProcessManager {
		return samplePM{jobs: j, clock: c, emits: emits}
	}
}

// TestHarness_ThenScheduled (QA-003): la solicitud programa la expiración a
// 14 días y nada inmediato; la idempotencia (3 entregas) se comprueba sola.
func TestHarness_ThenScheduled(t *testing.T) {
	testkit.Process(t, build(nil)).At(t0).
		GivenEvents(requested{RequestID: req}).
		ThenCommands().
		ThenScheduled(testkit.Scheduled{RunAt: t0.Add(14 * 24 * time.Hour), Command: expireRequest{RequestID: req}, DedupKey: "expire:" + req.String()})
}

// TestHarness_ThenCommands: aprobar cancela la expiración y despacha.
func TestHarness_ThenCommands(t *testing.T) {
	h := testkit.Process(t, build(nil)).At(t0).
		GivenEvents(requested{RequestID: req}, resolved{RequestID: req, Approved: true}).
		ThenCommands(notifyApproved{RequestID: req}).
		ThenScheduled()
	all := h.Jobs().All()
	if len(all) != 2 || all[0].Status != "cancelled" {
		t.Fatalf("jobs: %+v", all)
	}
	// Propagación de correlación (INF-002): causation = event_id del origen.
	if all[1].CorrelationID != "corr-test" || all[1].CausationID == "" {
		t.Fatalf("correlación: %+v", all[1].Job)
	}
}

// TestHarness_GuardRejectsUndeclared (SEC-007): un PM no puede emitir un
// comando que no declara en Emits.
func TestHarness_GuardRejectsUndeclared(t *testing.T) {
	testkit.Process(t, build([]string{"NotifyTestApproved"})).At(t0).
		GivenEvents(requested{RequestID: req}).
		ThenError("no declara ExpireTestRequest")
}

// TestValidateEmits: los comandos declarados deben estar en AllowedCommands.
func TestValidateEmits(t *testing.T) {
	allowed := scheduler.NewAllowedCommands()
	allowed.Allow(func() bus.Command { return expireRequest{} })
	pm := samplePM{}
	if err := process.ValidateEmits([]process.ProcessManager{pm}, allowed); err == nil {
		t.Fatal("NotifyTestApproved no está permitido: debe fallar")
	}
	allowed.Allow(func() bus.Command { return notifyApproved{} })
	if err := process.ValidateEmits([]process.ProcessManager{pm}, allowed); err != nil {
		t.Fatal(err)
	}
}
