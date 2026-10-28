package projection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// AsyncProjector es un proyector ejecutado por el worker mediante una
// suscripción catch-up (PP-F2-ARQ-002, ADR-009 §2). Reset borra sus tablas
// para el rebuild (ARQ-006); se ejecuta en la misma tx que pone el checkpoint
// a 0. Debe ser idempotente: at-least-once ante fallos de commit.
type AsyncProjector interface {
	Projector
	Reset(ctx context.Context, tx pgx.Tx) error
}

// Status es el estado de una suscripción en platform.projection_checkpoints.
type Status string

// Estados (DAT-001).
const (
	StatusRunning    Status = "running"
	StatusPaused     Status = "paused"
	StatusFailed     Status = "failed"
	StatusRebuilding Status = "rebuilding"
)

var allStatuses = []Status{StatusRunning, StatusPaused, StatusFailed, StatusRebuilding}

// DB es lo que las suscripciones necesitan de PostgreSQL (un *pgxpool.Pool sirve).
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Options ajusta las suscripciones; los valores cero toman los de 03 §3 y §8.
type Options struct {
	BatchSize   int             // 500
	IdlePoll    time.Duration   // 250 ms: sin eventos nuevos o suscripción detenida
	BusyPoll    time.Duration   // 10 ms: tras un lote lleno
	Backoff     []time.Duration // 1, 2, 4, 8, 16 s; agotado → fail-stop
	StepTimeout time.Duration   // 30 s: un lote en curso termina aunque llegue SIGTERM
	Logger      *slog.Logger
	Clock       func() time.Time
}

func (o *Options) defaults() {
	if o.BatchSize <= 0 {
		o.BatchSize = 500
	}
	if o.IdlePoll <= 0 {
		o.IdlePoll = 250 * time.Millisecond
	}
	if o.BusyPoll <= 0 {
		o.BusyPoll = 10 * time.Millisecond
	}
	if o.Backoff == nil {
		o.Backoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	}
	if o.StepTimeout <= 0 {
		o.StepTimeout = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
}

// Subscriptions agrupa las suscripciones catch-up del worker: una por
// proyección asíncrona, cada una con su checkpoint y avance independiente.
type Subscriptions struct {
	db       DB
	store    *eventstore.Store
	byName   map[string]AsyncProjector
	ordered  []AsyncProjector
	opts     Options
	m        *subMetrics
	stepHook func(name string) // pruebas: se invoca antes de cada lote
}

// NewSubscriptions construye el conjunto. reg nil = métricas sin registrar.
func NewSubscriptions(db DB, store *eventstore.Store, reg prometheus.Registerer, opts Options) *Subscriptions {
	opts.defaults()
	return &Subscriptions{db: db, store: store, byName: map[string]AsyncProjector{}, opts: opts, m: newSubMetrics(reg)}
}

// Register añade una proyección asíncrona. Pánico si el nombre se repite (el
// nombre es la clave del checkpoint: renombrar = rebuild).
func (s *Subscriptions) Register(p AsyncProjector) {
	if _, dup := s.byName[p.Name()]; dup {
		panic("projection: proyección asíncrona ya registrada: " + p.Name())
	}
	s.byName[p.Name()] = p
	s.ordered = append(s.ordered, p)
}

// Names devuelve las proyecciones registradas en orden de registro (también
// el orden de dependencia del rebuild --all).
func (s *Subscriptions) Names() []string {
	out := make([]string, len(s.ordered))
	for i, p := range s.ordered {
		out[i] = p.Name()
	}
	return out
}

// Ensure crea las filas de checkpoint que falten (posición 0, running).
func (s *Subscriptions) Ensure(ctx context.Context) error {
	for _, p := range s.ordered {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO platform.projection_checkpoints (projection_name, last_position, mode)
			VALUES ($1, 0, 'async') ON CONFLICT (projection_name) DO NOTHING`, p.Name()); err != nil {
			return fmt.Errorf("projection: checkpoint %s: %w", p.Name(), err)
		}
	}
	return nil
}

// Run ejecuta todas las suscripciones hasta que ctx se cancela. Cada una
// corre en su goroutine; un fallo en una no detiene las demás (fail-stop por
// proyección, ARQ-007). Devuelve cuando todas han terminado su lote en curso.
func (s *Subscriptions) Run(ctx context.Context) error {
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	done := make(chan struct{}, len(s.ordered))
	for _, p := range s.ordered {
		go func(p AsyncProjector) {
			defer func() { done <- struct{}{} }()
			s.loop(ctx, p)
		}(p)
	}
	for range s.ordered {
		<-done
	}
	return nil
}

// errNotRunning: la suscripción no está en un estado que permita avanzar.
type errNotRunning struct{ status Status }

func (e errNotRunning) Error() string {
	return "projection: suscripción en estado " + string(e.status)
}

func (s *Subscriptions) loop(ctx context.Context, p AsyncProjector) {
	log := s.opts.Logger.With("projection", p.Name())
	attempt := 0
	for ctx.Err() == nil {
		stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.StepTimeout)
		res, err := s.step(stepCtx, p, StatusRunning, 0)
		cancel()

		var nr errNotRunning
		switch {
		case errors.As(err, &nr):
			s.m.setStatus(p.Name(), nr.status)
			attempt = 0
			sleep(ctx, s.opts.IdlePoll)
		case err != nil:
			attempt++
			if attempt > len(s.opts.Backoff) {
				s.fail(context.WithoutCancel(ctx), p.Name(), err, log)
				attempt = 0
				continue
			}
			wait := s.opts.Backoff[attempt-1]
			log.Warn("projection batch failed, retrying", "attempt", attempt, "backoff", wait.String(), "error", err.Error())
			sleep(ctx, wait)
		default:
			attempt = 0
			s.m.setStatus(p.Name(), StatusRunning)
			if res.caughtUp {
				sleep(ctx, s.opts.IdlePoll)
			} else {
				sleep(ctx, s.opts.BusyPoll)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

type stepResult struct {
	projected int
	position  int64
	head      int64
	caughtUp  bool
}

// step proyecta un lote en UNA transacción: checkpoint FOR UPDATE → head →
// eventos filtrados por tipo en (last, head] → Project en orden → checkpoint.
// Proyección y checkpoint confirman juntos (exactly-once efectivo sobre el
// read model, ARQ-002); un lote parcial nunca confirma (ARQ-007). El FOR
// UPDATE serializa el lote con pause/rebuild lanzados desde la CLI.
func (s *Subscriptions) step(ctx context.Context, p AsyncProjector, allowed Status, toPosition int64) (res stepResult, err error) {
	if s.stepHook != nil {
		s.stepHook(p.Name())
	}
	ctx, span := otel.Tracer("projection").Start(ctx, "projection.batch")
	span.SetAttributes(attribute.String("projection.name", p.Name()))
	defer func() {
		if err != nil {
			var nr errNotRunning
			if !errors.As(err, &nr) {
				span.RecordError(err)
				span.SetStatus(codes.Error, "batch failed")
			}
		}
		span.End()
	}()
	start := s.opts.Clock()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("projection %s: begin: %w", p.Name(), err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var last int64
	var status Status
	if err := tx.QueryRow(ctx, `
		SELECT last_position, status FROM platform.projection_checkpoints
		 WHERE projection_name = $1 FOR UPDATE`, p.Name()).Scan(&last, &status); err != nil {
		return res, fmt.Errorf("projection %s: checkpoint: %w", p.Name(), err)
	}
	if status != allowed {
		return res, errNotRunning{status: status}
	}
	head, err := s.store.Head(ctx, tx)
	if err != nil {
		return res, err
	}
	if toPosition > 0 && toPosition < head {
		head = toPosition
	}
	res.head, res.position = head, last
	if last >= head {
		res.caughtUp = true
		s.m.observeLag(p.Name(), 0, 0)
		return res, tx.Commit(ctx)
	}

	events, err := s.store.ReadRange(ctx, tx, last, head, p.Handles(), s.opts.BatchSize)
	if err != nil {
		return res, err
	}
	for _, e := range events {
		if err := projectSafely(ctx, tx, p, e); err != nil {
			return res, fmt.Errorf("projection %s: posición %d %s@%d: %w", p.Name(), e.GlobalPosition, e.Type, e.SchemaVersion, err)
		}
	}
	// Lote incompleto = no hay más eventos de interés hasta head: el
	// checkpoint salta a head (con orden total, todo ≤ head ya es visible).
	next := head
	if len(events) == s.opts.BatchSize {
		next = events[len(events)-1].GlobalPosition
	}
	if _, err := tx.Exec(ctx, `
		UPDATE platform.projection_checkpoints SET last_position = $2, updated_at = now()
		 WHERE projection_name = $1`, p.Name(), next); err != nil {
		return res, fmt.Errorf("projection %s: avanzar checkpoint: %w", p.Name(), err)
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("projection %s: commit: %w", p.Name(), err)
	}

	res.projected, res.position, res.caughtUp = len(events), next, next >= head
	span.SetAttributes(attribute.Int("batch.size", len(events)), attribute.Int64("from.position", last), attribute.Int64("to.position", next))
	s.m.batch.WithLabelValues(p.Name()).Observe(s.opts.Clock().Sub(start).Seconds())
	lagSeconds := 0.0
	if len(events) > 0 && !res.caughtUp {
		lagSeconds = s.opts.Clock().Sub(events[len(events)-1].RecordedAt).Seconds()
	}
	s.m.observeLag(p.Name(), float64(head-next), lagSeconds)
	if len(events) > 0 {
		s.opts.Logger.Debug("projection batch", "projection", p.Name(), "events", len(events),
			"from_position", last, "global_position", next)
	}
	return res, nil
}

// projectSafely convierte un panic del proyector en error (ARQ-007).
func projectSafely(ctx context.Context, tx pgx.Tx, p AsyncProjector, e eventstore.StoredEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return p.Project(ctx, tx, e)
}

// fail aplica fail-stop: status=failed con el error; la suscripción deja de
// avanzar hasta `cmd/worker resume`. Las demás continúan.
func (s *Subscriptions) fail(ctx context.Context, name string, cause error, log *slog.Logger) {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE platform.projection_checkpoints
		   SET status = 'failed', last_error = $2, failed_at = now(), updated_at = now()
		 WHERE projection_name = $1`, name, msg); err != nil {
		log.Error("projection fail-stop could not be persisted", "error", err.Error())
	}
	s.m.setStatus(name, StatusFailed)
	log.Error("projection failed: fail-stop", "alert", "ProjectionFailed", "error", msg)
}

// Checkpoint es el estado persistido de una suscripción.
type Checkpoint struct {
	Name         string
	LastPosition int64
	Status       Status
	Mode         string
	LastError    string
	FailedAt     *time.Time
	UpdatedAt    time.Time
}

// Checkpoints lista el estado de todas las proyecciones registradas en BD.
func Checkpoints(ctx context.Context, db DB) ([]Checkpoint, error) {
	rows, err := db.Query(ctx, `
		SELECT projection_name, last_position, status, mode, coalesce(last_error, ''), failed_at, updated_at
		  FROM platform.projection_checkpoints ORDER BY projection_name`)
	if err != nil {
		return nil, fmt.Errorf("projection: checkpoints: %w", err)
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		if err := rows.Scan(&c.Name, &c.LastPosition, &c.Status, &c.Mode, &c.LastError, &c.FailedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Pause detiene la suscripción (status=paused) tras su lote en curso.
func (s *Subscriptions) Pause(ctx context.Context, name string) error {
	return s.setStatus(ctx, name, StatusPaused, StatusRunning, StatusFailed)
}

// Resume reanuda una suscripción pausada o fallida (fail-stop, ARQ-007):
// status=running y se limpia el error. La siguiente pasada reintenta el lote.
func (s *Subscriptions) Resume(ctx context.Context, name string) error {
	return s.setStatus(ctx, name, StatusRunning, StatusPaused, StatusFailed)
}

func (s *Subscriptions) setStatus(ctx context.Context, name string, to Status, from ...Status) error {
	if _, ok := s.byName[name]; !ok {
		return fmt.Errorf("projection: %q no es una proyección asíncrona registrada (%v)", name, s.Names())
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	fromText := make([]string, len(from))
	for i, f := range from {
		fromText[i] = string(f)
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE platform.projection_checkpoints
		   SET status = $2, last_error = CASE WHEN $2 = 'running' THEN NULL ELSE last_error END,
		       failed_at = CASE WHEN $2 = 'running' THEN NULL ELSE failed_at END, updated_at = now()
		 WHERE projection_name = $1 AND status = ANY($3)`, name, string(to), fromText)
	if err != nil {
		return fmt.Errorf("projection: %s → %s: %w", name, to, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("projection: %s no está en %v", name, from)
	}
	s.m.setStatus(name, to)
	return nil
}

// RebuildProgress informa del avance del rebuild.
type RebuildProgress func(name string, position, head int64, projected int)

// Rebuild reconstruye una proyección asíncrona (ARQ-006): pausa
// (status=rebuilding, espera al lote en curso por el FOR UPDATE), Reset +
// checkpoint 0 en una tx, replay por lotes hasta head (o toPosition) y
// reanuda. No toca es.events. Un error deja la proyección en failed.
func (s *Subscriptions) Rebuild(ctx context.Context, name string, toPosition int64, progress RebuildProgress) error {
	p, ok := s.byName[name]
	if !ok {
		return fmt.Errorf("projection: %q no es una proyección asíncrona registrada (%v)", name, s.Names())
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	log := s.opts.Logger.With("projection", name)
	if _, err := s.db.Exec(ctx, `
		UPDATE platform.projection_checkpoints SET status = 'rebuilding', updated_at = now()
		 WHERE projection_name = $1`, name); err != nil {
		return fmt.Errorf("projection: pausar %s: %w", name, err)
	}
	s.m.setStatus(name, StatusRebuilding)

	if err := s.reset(ctx, p); err != nil {
		s.fail(ctx, name, err, log)
		return err
	}
	log.Info("projection rebuild started", "to_position", toPosition)
	for {
		res, err := s.step(ctx, p, StatusRebuilding, toPosition)
		if err != nil {
			s.fail(ctx, name, err, log)
			return err
		}
		if res.head > 0 {
			s.m.rebuild.WithLabelValues(name).Set(float64(res.position) / float64(res.head))
		}
		if progress != nil {
			progress(name, res.position, res.head, res.projected)
		}
		if res.caughtUp {
			break
		}
	}
	s.m.rebuild.WithLabelValues(name).Set(1)
	if err := s.setStatus(ctx, name, StatusRunning, StatusRebuilding); err != nil {
		return err
	}
	log.Info("projection rebuild finished")
	return nil
}

func (s *Subscriptions) reset(ctx context.Context, p AsyncProjector) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM platform.projection_checkpoints WHERE projection_name = $1 FOR UPDATE`, p.Name()); err != nil {
		return err
	}
	if err := p.Reset(ctx, tx); err != nil {
		return fmt.Errorf("projection %s: reset: %w", p.Name(), err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE platform.projection_checkpoints SET last_position = 0, last_error = NULL, failed_at = NULL, updated_at = now()
		 WHERE projection_name = $1`, p.Name()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RebuildAll reconstruye todas las asíncronas en orden de registro (ninguna
// depende de otra en F2).
func (s *Subscriptions) RebuildAll(ctx context.Context, toPosition int64, progress RebuildProgress) error {
	for _, p := range s.ordered {
		if err := s.Rebuild(ctx, p.Name(), toPosition, progress); err != nil {
			return err
		}
	}
	return nil
}

// CatchUp avanza todas las suscripciones running hasta head una vez (pruebas
// y herramientas; el worker usa Run).
func (s *Subscriptions) CatchUp(ctx context.Context) error {
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	for _, p := range s.ordered {
		for {
			res, err := s.step(ctx, p, StatusRunning, 0)
			if err != nil {
				return err
			}
			if res.caughtUp {
				break
			}
		}
	}
	return nil
}

type subMetrics struct {
	lagEvents  *prometheus.GaugeVec
	lagSeconds *prometheus.GaugeVec
	batch      *prometheus.HistogramVec
	status     *prometheus.GaugeVec
	rebuild    *prometheus.GaugeVec
}

func newSubMetrics(reg prometheus.Registerer) *subMetrics {
	m := &subMetrics{
		lagEvents: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "projection_lag_events",
			Help: "Eventos por detrás de la cabeza del Event Store (head − checkpoint), por proyección asíncrona.",
		}, []string{"name"}),
		lagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "projection_lag_seconds",
			Help: "Segundos entre ahora y recorded_at del último evento proyectado mientras hay retraso (0 al día).",
		}, []string{"name"}),
		batch: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "projection_batch_duration_seconds",
			Help:    "Duración de cada lote de proyección asíncrona.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"name"}),
		status: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "projection_status",
			Help: "1 en el estado actual de la suscripción (running|paused|failed|rebuilding), 0 en el resto.",
		}, []string{"name", "status"}),
		rebuild: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "projection_rebuild_progress",
			Help: "Fracción (0–1) del replay del último rebuild.",
		}, []string{"name"}),
	}
	if reg != nil {
		reg.MustRegister(m.lagEvents, m.lagSeconds, m.batch, m.status, m.rebuild)
	}
	return m
}

func (m *subMetrics) setStatus(name string, st Status) {
	for _, s := range allStatuses {
		v := 0.0
		if s == st {
			v = 1
		}
		m.status.WithLabelValues(name, string(s)).Set(v)
	}
}

func (m *subMetrics) observeLag(name string, events, seconds float64) {
	m.lagEvents.WithLabelValues(name).Set(events)
	m.lagSeconds.WithLabelValues(name).Set(seconds)
}
