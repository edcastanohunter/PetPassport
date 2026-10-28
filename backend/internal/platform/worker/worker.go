// Package worker supervisa el proceso `cmd/worker` (PP-F2-ARQ-001,
// docs/phase-2/03 §2): lock de instancia por grupo, runners de larga duración
// con apagado ordenado y estado para /readyz.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// InstanceLock es el lock de instancia `pg_advisory_lock(worker.<group>)`:
// garantiza un solo consumidor por grupo (una réplica en F2, ADR-009). Es de
// sesión, así que vive en una conexión dedicada mientras dura el proceso.
type InstanceLock struct {
	pool       *pgxpool.Pool
	group      string
	instanceID string
	conn       *pgxpool.Conn
	held       atomic.Bool
	gauge      *prometheus.GaugeVec
	logger     *slog.Logger
}

// NewInstanceLock prepara el lock del grupo. reg nil = métrica sin registrar.
func NewInstanceLock(pool *pgxpool.Pool, group string, reg prometheus.Registerer, logger *slog.Logger) *InstanceLock {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "worker_instance_lock",
		Help: "1 si esta instancia del worker tiene el lock de su grupo (alerta WorkerDown).",
	}, []string{"group"})
	if reg != nil {
		reg.MustRegister(g)
	}
	g.WithLabelValues(group).Set(0)
	return &InstanceLock{pool: pool, group: group, instanceID: kernel.NewID().String(), gauge: g, logger: logger}
}

func (l *InstanceLock) key() string { return "worker." + l.group }

// Acquire espera hasta obtener el lock (reintenta cada retry). Si otra
// instancia lo tiene, esta queda en espera y no consume nada.
func (l *InstanceLock) Acquire(ctx context.Context, retry time.Duration) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("worker: conexión del lock: %w", err)
	}
	for {
		var ok bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, l.key()).Scan(&ok); err != nil {
			conn.Release()
			return fmt.Errorf("worker: lock de instancia: %w", err)
		}
		if ok {
			break
		}
		l.logger.Info("worker instance lock held by another instance, waiting", "group", l.group)
		select {
		case <-ctx.Done():
			conn.Release()
			return ctx.Err()
		case <-time.After(retry):
		}
	}
	l.conn = conn
	host, _ := os.Hostname()
	if _, err := conn.Exec(ctx, `
		INSERT INTO platform.worker_instances (group_name, instance_id, hostname, started_at, heartbeat_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (group_name) DO UPDATE
		   SET instance_id = EXCLUDED.instance_id, hostname = EXCLUDED.hostname,
		       started_at = EXCLUDED.started_at, heartbeat_at = EXCLUDED.heartbeat_at`,
		l.group, l.instanceID, host); err != nil {
		l.Release()
		return fmt.Errorf("worker: registrar instancia: %w", err)
	}
	l.held.Store(true)
	l.gauge.WithLabelValues(l.group).Set(1)
	l.logger.Info("worker instance lock acquired", "group", l.group, "instance_id", l.instanceID)
	return nil
}

// Heartbeat actualiza worker_instances.heartbeat_at por la conexión del lock
// cada interval. Si la conexión se pierde, el lock se ha perdido con ella:
// devuelve error para que el proceso termine (systemd lo reinicia).
func (l *InstanceLock) Heartbeat(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		hbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := l.conn.Exec(hbCtx, `UPDATE platform.worker_instances SET heartbeat_at = now() WHERE group_name = $1 AND instance_id = $2`, l.group, l.instanceID)
		cancel()
		if err != nil && ctx.Err() == nil {
			l.held.Store(false)
			l.gauge.WithLabelValues(l.group).Set(0)
			return fmt.Errorf("worker: heartbeat (lock de instancia perdido): %w", err)
		}
	}
}

// Held indica si el lock está en poder de esta instancia.
func (l *InstanceLock) Held() bool { return l.held.Load() }

// Release libera el lock y devuelve la conexión.
func (l *InstanceLock) Release() {
	if l.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = l.conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, l.key())
	l.conn.Release()
	l.conn = nil
	l.held.Store(false)
	l.gauge.WithLabelValues(l.group).Set(0)
}

// Runner es un bucle de larga duración del worker (projections, outbox,
// command-scheduler…). Debe volver cuando ctx se cancela, tras terminar su
// unidad de trabajo en curso.
type Runner struct {
	Name string
	Run  func(ctx context.Context) error
}

// Supervisor ejecuta los runners y expone su estado para /readyz. Si un
// runner termina antes del apagado (error o panic), cancela el resto: el
// proceso sale con error y systemd lo reinicia (mejor que un worker a medias).
type Supervisor struct {
	logger *slog.Logger
	mu     sync.RWMutex
	alive  map[string]bool
}

// NewSupervisor crea el supervisor.
func NewSupervisor(logger *slog.Logger) *Supervisor {
	return &Supervisor{logger: logger, alive: map[string]bool{}}
}

// Run arranca los runners y espera a que terminen todos. Devuelve el primer
// error de un runner que terminó antes de tiempo.
func (s *Supervisor) Run(ctx context.Context, runners ...Runner) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, len(runners))
	for _, r := range runners {
		s.set(r.Name, true)
		wg.Add(1)
		go func(r Runner) {
			defer wg.Done()
			err := runSafely(ctx, r)
			s.set(r.Name, false)
			if ctx.Err() == nil { // terminó sin apagado: fallo del runner
				if err == nil {
					err = errors.New("terminó inesperadamente")
				}
				s.logger.Error("worker runner stopped", "runner", r.Name, "error", err.Error())
				errs <- fmt.Errorf("runner %s: %w", r.Name, err)
				cancel()
			}
		}(r)
		s.logger.Info("worker runner started", "runner", r.Name)
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func runSafely(ctx context.Context, r Runner) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	return r.Run(ctx)
}

func (s *Supervisor) set(name string, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alive[name] = v
}

// Check es el checker de /readyz: todos los runners vivos.
func (s *Supervisor) Check(context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.alive) == 0 {
		return errors.New("sin runners")
	}
	for name, ok := range s.alive {
		if !ok {
			return fmt.Errorf("runner %s detenido", name)
		}
	}
	return nil
}
