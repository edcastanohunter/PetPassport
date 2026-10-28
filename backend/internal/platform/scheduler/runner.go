package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Dispatcher despacha un comando (el command bus).
type Dispatcher interface {
	Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error)
}

// DB es lo que el runner necesita (un *pgxpool.Pool sirve).
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// RunnerOptions ajusta el runner; los valores cero toman los por defecto.
type RunnerOptions struct {
	MaxAttempts int           // 5; agotados → failed + command_jobs_failed_total
	IdlePoll    time.Duration // 1 s
	BaseBackoff time.Duration // 30 s × 2^(intento−1), tope 1 h
	Clock       kernel.Clock  // reloj simulado en pruebas (QA-003)
	Logger      *slog.Logger
}

// Runner es el runner `command-scheduler` del worker.
type Runner struct {
	db      DB
	allowed *AllowedCommands
	bus     Dispatcher
	opts    RunnerOptions

	executed *prometheus.CounterVec
	failed   prometheus.Counter
}

// NewRunner construye el runner. reg nil = métricas sin registrar.
func NewRunner(db DB, allowed *AllowedCommands, d Dispatcher, reg prometheus.Registerer, opts RunnerOptions) *Runner {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.IdlePoll <= 0 {
		opts.IdlePoll = time.Second
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 30 * time.Second
	}
	if opts.Clock == nil {
		opts.Clock = kernel.RealClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	r := &Runner{
		db: db, allowed: allowed, bus: d, opts: opts,
		executed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "command_jobs_executed_total",
			Help: "Comandos programados ejecutados, por tipo y resultado (ok | retry | failed).",
		}, []string{"command", "result"}),
		failed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "command_jobs_failed_total",
			Help: "Comandos programados que agotaron reintentos (status = failed).",
		}),
	}
	if reg != nil {
		reg.MustRegister(r.executed, r.failed, prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "command_jobs_pending",
			Help: "Comandos programados pendientes (vencidos o no).",
		}, func() float64 {
			var n int64
			if err := db.QueryRow(context.Background(), `SELECT count(*) FROM scheduler.command_jobs WHERE status = 'pending'`).Scan(&n); err != nil {
				return -1
			}
			return float64(n)
		}))
	}
	return r
}

// Run ejecuta jobs vencidos hasta que ctx se cancela. El job en curso termina
// (contexto sin cancelación, ≤ 30 s) antes de salir.
func (r *Runner) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		ran, err := r.RunOne(stepCtx)
		cancel()
		if err != nil {
			r.opts.Logger.Error("command scheduler step failed", "error", err.Error())
		}
		if !ran || err != nil {
			t := time.NewTimer(r.opts.IdlePoll)
			select {
			case <-ctx.Done():
			case <-t.C:
			}
			t.Stop()
		}
	}
	return nil
}

// RunDue ejecuta todos los jobs vencidos según el reloj (pruebas con reloj
// simulado). Devuelve cuántos ejecutó (con éxito o no).
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	n := 0
	for {
		ran, err := r.RunOne(ctx)
		if err != nil {
			return n, err
		}
		if !ran {
			return n, nil
		}
		n++
	}
}

// RunOne toma un job vencido (FOR UPDATE SKIP LOCKED), lo despacha con
// Principal = system y registra el resultado en la misma tx. El comando corre
// en su propia UoW: si el commit del job fallase tras un comando correcto, el
// job se repite (at-least-once; los comandos programables son idempotentes).
func (r *Runner) RunOne(ctx context.Context) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	now := r.opts.Clock.Now().UTC()
	var (
		id       uuid.UUID
		name     string
		raw      []byte
		attempts int
	)
	err = tx.QueryRow(ctx, `
		SELECT id, command_type, command, attempts FROM scheduler.command_jobs
		 WHERE status = 'pending' AND run_at <= $1
		 ORDER BY run_at, id LIMIT 1
		 FOR UPDATE SKIP LOCKED`, now).Scan(&id, &name, &raw, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scheduler: tomar job: %w", err)
	}
	log := r.opts.Logger.With("job_id", id.String(), "command", name)

	runErr := r.dispatch(ctx, id, name, raw)
	switch {
	case runErr == nil:
		_, err = tx.Exec(ctx, `UPDATE scheduler.command_jobs SET status = 'done', attempts = attempts + 1, last_error = NULL, updated_at = now() WHERE id = $1`, id)
		r.executed.WithLabelValues(name, "ok").Inc()
	case attempts+1 >= r.opts.MaxAttempts || errors.Is(runErr, ErrNotAllowed):
		_, err = tx.Exec(ctx, `UPDATE scheduler.command_jobs SET status = 'failed', attempts = attempts + 1, last_error = $2, updated_at = now() WHERE id = $1`, id, truncate(runErr.Error()))
		r.executed.WithLabelValues(name, "failed").Inc()
		r.failed.Inc()
		log.Error("scheduled command failed permanently", "attempts", attempts+1, "error", runErr.Error())
	default:
		backoff := r.opts.BaseBackoff << attempts
		if backoff > time.Hour || backoff <= 0 {
			backoff = time.Hour
		}
		_, err = tx.Exec(ctx, `UPDATE scheduler.command_jobs SET attempts = attempts + 1, last_error = $2, run_at = $3, updated_at = now() WHERE id = $1`,
			id, truncate(runErr.Error()), now.Add(backoff))
		r.executed.WithLabelValues(name, "retry").Inc()
		log.Warn("scheduled command failed, retrying", "attempts", attempts+1, "backoff", backoff.String(), "error", runErr.Error())
	}
	if err != nil {
		return true, fmt.Errorf("scheduler: registrar resultado: %w", err)
	}
	return true, tx.Commit(ctx)
}

func (r *Runner) dispatch(ctx context.Context, id uuid.UUID, name string, raw []byte) error {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("scheduler: sobre inválido: %w", err)
	}
	cmd, err := r.allowed.decode(name, env.Payload)
	if err != nil {
		return err
	}
	ctx = bus.WithSystemOrigin(ctx, bus.SystemOrigin{CorrelationID: env.CorrelationID, CausationID: env.CausationID, JobID: id})
	_, err = r.bus.Dispatch(ctx, cmd)
	return err
}

func truncate(s string) string {
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}
