// Package process es el framework de process managers (PP-F2-ARQ-009,
// docs/phase-2/03 §9) y el consumidor del outbox que los alimenta
// (PP-F2-ARQ-008). Un PM reacciona a eventos de otros contextos dentro de una
// transacción: escribe en sus tablas (notifications.*) o programa/despacha
// comandos por el scheduler (scheduler.command_jobs). Nunca llama al command
// bus directamente: despachar = programar con RunAt = ahora y clave de
// deduplicación, para que reentregas del outbox no dupliquen comandos.
package process

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
)

// ProcessManager reacciona a eventos del outbox. Handle debe ser idempotente
// (at-least-once): mismo evento N veces → mismo resultado (QA-003).
type ProcessManager interface { //nolint:revive // nombre del glosario (03 §9)
	Name() string      // "scheduling.complete-on-sign": prefijo de contexto, estable
	Handles() []string // tipos de evento
	// Emits declara los comandos que el PM puede programar o despachar
	// (SEC-007: system no pasa por authz). Deben estar en AllowedCommands.
	Emits() []string
	Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error
}

// Jobs es el puerto de los PM hacia el scheduler de comandos.
type Jobs interface {
	Schedule(ctx context.Context, tx pgx.Tx, job scheduler.Job) error
	Cancel(ctx context.Context, tx pgx.Tx, dedupKey string) error
}

// Guard limita los comandos que un PM puede programar a los que declara en
// Emits (defensa en profundidad además de AllowedCommands).
func Guard(pm interface {
	Name() string
	Emits() []string
}, jobs Jobs) Jobs {
	return guarded{name: pm.Name(), emits: pm.Emits(), next: jobs}
}

type guarded struct {
	name  string
	emits []string
	next  Jobs
}

func (g guarded) Schedule(ctx context.Context, tx pgx.Tx, job scheduler.Job) error {
	if !slices.Contains(g.emits, job.Command.CommandName()) {
		return fmt.Errorf("%w: %s no declara %s en Emits", scheduler.ErrNotAllowed, g.name, job.Command.CommandName())
	}
	return g.next.Schedule(ctx, tx, job)
}

func (g guarded) Cancel(ctx context.Context, tx pgx.Tx, dedupKey string) error {
	return g.next.Cancel(ctx, tx, dedupKey)
}

// JobFor construye un job causado por e: propaga correlation_id del evento y
// causation_id = event_id (INF-002).
func JobFor(e eventstore.StoredEvent, runAt time.Time, cmd bus.Command, dedupKey string) scheduler.Job {
	return scheduler.Job{
		RunAt: runAt, Command: cmd, DedupKey: dedupKey,
		CorrelationID: e.Metadata.CorrelationID, CausationID: e.EventID.String(),
	}
}

// ValidateEmits comprueba en el arranque que todo comando declarado por los
// PM está en la lista cerrada de comandos programables.
func ValidateEmits(pms []ProcessManager, allowed *scheduler.AllowedCommands) error {
	for _, pm := range pms {
		for _, c := range pm.Emits() {
			if !allowed.IsAllowed(c) {
				return fmt.Errorf("process: %s emite %s, que no está en scheduler.AllowedCommands", pm.Name(), c)
			}
		}
	}
	return nil
}
