// Package scheduler implementa el scheduler de comandos (PP-F2-ARQ-009,
// DAT-006, docs/phase-2/03 §9): los process managers programan comandos en
// scheduler.command_jobs dentro de su transacción (Schedule/Cancel, con clave
// de deduplicación) y el runner `command-scheduler` del worker ejecuta los
// vencidos con FOR UPDATE SKIP LOCKED, despachándolos por el command bus con
// Principal = system.
//
// Solo los comandos de AllowedCommands se pueden programar: system no pasa
// por authz, así que la lista cerrada es el control (SEC-007, 09 §3).
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ErrNotAllowed: el comando no está en AllowedCommands.
var ErrNotAllowed = errors.New("scheduler: comando no programable")

// AllowedCommands es la lista cerrada de comandos programables. Cada módulo
// añade los suyos en el arranque (Allow); fuera de ella no se puede
// serializar ni despachar ningún comando.
type AllowedCommands struct {
	mu        sync.RWMutex
	factories map[string]func() bus.Command
}

// NewAllowedCommands crea la lista vacía.
func NewAllowedCommands() *AllowedCommands {
	return &AllowedCommands{factories: map[string]func() bus.Command{}}
}

// Allow añade un comando programable. El factory devuelve un VALOR del
// comando (como el Registry de eventos). Pánico si se repite.
func (a *AllowedCommands) Allow(factory func() bus.Command) {
	name := factory().CommandName()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, dup := a.factories[name]; dup {
		panic("scheduler: comando ya permitido: " + name)
	}
	a.factories[name] = factory
}

// Names devuelve los comandos permitidos, ordenados.
func (a *AllowedCommands) Names() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.factories))
	for n := range a.factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsAllowed indica si el comando es programable.
func (a *AllowedCommands) IsAllowed(name string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.factories[name]
	return ok
}

func (a *AllowedCommands) encode(cmd bus.Command) (string, json.RawMessage, error) {
	name := cmd.CommandName()
	if !a.IsAllowed(name) {
		return "", nil, fmt.Errorf("%w: %s", ErrNotAllowed, name)
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return "", nil, fmt.Errorf("scheduler: serializar %s: %w", name, err)
	}
	return name, raw, nil
}

func (a *AllowedCommands) decode(name string, raw json.RawMessage) (bus.Command, error) {
	a.mu.RLock()
	factory, ok := a.factories[name]
	a.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotAllowed, name)
	}
	target := reflect.New(reflect.TypeOf(factory()))
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return nil, fmt.Errorf("scheduler: deserializar %s: %w", name, err)
	}
	cmd, ok := target.Elem().Interface().(bus.Command)
	if !ok {
		return nil, fmt.Errorf("scheduler: %s no implementa bus.Command por valor", name)
	}
	return cmd, nil
}

// Job es un comando a ejecutar en RunAt. DedupKey identifica la ocurrencia
// (p. ej. "expire-care-link-request:<request_id>"): programar dos veces la
// misma clave no duplica (idempotencia de los PM ante reentregas del outbox).
type Job struct {
	RunAt         time.Time
	Command       bus.Command
	DedupKey      string
	CorrelationID string // del evento origen
	CausationID   string // event_id del evento origen
}

// envelope es lo que se guarda en command_jobs.command.
type envelope struct {
	Payload       json.RawMessage `json:"payload"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
}

// Scheduler escribe en scheduler.command_jobs dentro de la tx del llamante
// (la del process manager). Implementa process.Jobs.
type Scheduler struct {
	allowed *AllowedCommands
	ids     kernel.IDGenerator
}

// New construye el scheduler. ids nil = UUIDv7 real.
func New(allowed *AllowedCommands, ids kernel.IDGenerator) *Scheduler {
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	return &Scheduler{allowed: allowed, ids: ids}
}

// Allowed devuelve la lista cerrada de comandos programables.
func (s *Scheduler) Allowed() *AllowedCommands { return s.allowed }

// Schedule programa el job. Si la clave ya existe y sigue pendiente, se
// actualizan run_at y el comando (reprogramación); si ya se ejecutó o se
// canceló, no se toca (para reprogramar tras cancelar, nueva ocurrencia).
func (s *Scheduler) Schedule(ctx context.Context, tx pgx.Tx, job Job) error {
	if job.DedupKey == "" {
		return errors.New("scheduler: dedup_key obligatoria")
	}
	name, payload, err := s.allowed.encode(job.Command)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope{Payload: payload, CorrelationID: job.CorrelationID, CausationID: job.CausationID})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO scheduler.command_jobs (id, dedup_key, run_at, command_type, command, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT (dedup_key) DO UPDATE
		   SET run_at = EXCLUDED.run_at, command_type = EXCLUDED.command_type, command = EXCLUDED.command, updated_at = now()
		 WHERE scheduler.command_jobs.status = 'pending'`,
		s.ids.NewID(), job.DedupKey, job.RunAt.UTC(), name, raw)
	if err != nil {
		return fmt.Errorf("scheduler: programar %s: %w", job.DedupKey, err)
	}
	return nil
}

// Cancel cancela el job pendiente con esa clave (no-op si no existe o ya corrió).
func (s *Scheduler) Cancel(ctx context.Context, tx pgx.Tx, dedupKey string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE scheduler.command_jobs SET status = 'cancelled', updated_at = now()
		 WHERE dedup_key = $1 AND status = 'pending'`, dedupKey); err != nil {
		return fmt.Errorf("scheduler: cancelar %s: %w", dedupKey, err)
	}
	return nil
}

// JobID es el id de un job (consultas de prueba y admin).
type JobID = uuid.UUID
