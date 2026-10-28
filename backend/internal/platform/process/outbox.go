package process

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// DB es lo que el consumidor necesita (un *pgxpool.Pool sirve).
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConsumerOptions ajusta el consumidor; los valores cero toman los de 03 §9.
type ConsumerOptions struct {
	BatchSize   int           // 200
	MaxAttempts int           // 10: superados → dead_letter
	BaseBackoff time.Duration // 1 s × 2^intento, tope MaxBackoff
	MaxBackoff  time.Duration // 5 min
	IdlePoll    time.Duration // 250 ms
	BusyPoll    time.Duration // 10 ms
	Clock       kernel.Clock
	Logger      *slog.Logger
}

// Consumer es el runner `outbox` del worker (PP-F2-ARQ-008).
//
// Orden: los eventos se toman por global_position. Dentro de un stream, un
// evento fallido pendiente bloquea a los posteriores de ese stream hasta que
// se publique (o pase a dead-letter); los demás streams siguen. Dead-letter
// no bloquea: un email imposible no debe frenar el resto (ADR-009 §4).
type Consumer struct {
	db    DB
	store *eventstore.Store
	pms   []ProcessManager
	byTyp map[string][]ProcessManager
	opts  ConsumerOptions

	processed  *prometheus.CounterVec
	deadLetter prometheus.Counter
}

// NewConsumer construye el consumidor. reg nil = métricas sin registrar.
func NewConsumer(db DB, store *eventstore.Store, reg prometheus.Registerer, opts ConsumerOptions) *Consumer {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 200
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 10
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Minute
	}
	if opts.IdlePoll <= 0 {
		opts.IdlePoll = 250 * time.Millisecond
	}
	if opts.BusyPoll <= 0 {
		opts.BusyPoll = 10 * time.Millisecond
	}
	if opts.Clock == nil {
		opts.Clock = kernel.RealClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	c := &Consumer{
		db: db, store: store, byTyp: map[string][]ProcessManager{}, opts: opts,
		processed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_processed_total",
			Help: "Eventos del outbox procesados, por resultado (published | retry | dead_letter).",
		}, []string{"result"}),
		deadLetter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_dead_letter_total",
			Help: "Eventos del outbox que agotaron reintentos y pasaron a dead-letter.",
		}),
	}
	if reg != nil {
		reg.MustRegister(c.processed, c.deadLetter)
	}
	return c
}

// Register suscribe un process manager. Pánico si el nombre se repite.
func (c *Consumer) Register(pm ProcessManager) {
	for _, p := range c.pms {
		if p.Name() == pm.Name() {
			panic("process: process manager ya registrado: " + pm.Name())
		}
	}
	c.pms = append(c.pms, pm)
	for _, t := range pm.Handles() {
		c.byTyp[t] = append(c.byTyp[t], pm)
	}
}

// ProcessManagers devuelve los PM registrados.
func (c *Consumer) ProcessManagers() []ProcessManager { return c.pms }

// Run consume hasta que ctx se cancela; el lote en curso termina (≤ 30 s).
func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		n, err := c.Tick(stepCtx)
		cancel()
		wait := c.opts.IdlePoll
		switch {
		case err != nil:
			c.opts.Logger.Error("outbox batch failed", "error", err.Error())
		case n == c.opts.BatchSize:
			wait = c.opts.BusyPoll
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	return nil
}

// Tick procesa un lote y devuelve cuántas filas tomó. Una sola tx por lote;
// cada evento se entrega a sus PM dentro de un savepoint: un fallo revierte
// solo los efectos de ese evento, registra el intento y bloquea su stream
// durante el resto del lote.
func (c *Consumer) Tick(ctx context.Context) (int, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	now := c.opts.Clock.Now().UTC()

	// Candidatos: pendientes, sin dead-letter, vencido su backoff, y sin un
	// evento anterior del mismo stream aún en backoff (orden por stream).
	rows, err := tx.Query(ctx, `
		SELECT o.global_position, o.attempts FROM es.outbox o
		 WHERE o.published_at IS NULL AND NOT o.dead_letter
		   AND (o.next_attempt_at IS NULL OR o.next_attempt_at <= $1)
		   AND NOT EXISTS (
		         SELECT 1 FROM es.outbox p
		          WHERE p.stream_id = o.stream_id AND p.published_at IS NULL AND NOT p.dead_letter
		            AND p.global_position < o.global_position AND p.next_attempt_at > $1)
		 ORDER BY o.global_position
		 LIMIT $2
		 FOR UPDATE OF o SKIP LOCKED`, now, c.opts.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("outbox: candidatos: %w", err)
	}
	attempts := map[int64]int{}
	var positions []int64
	for rows.Next() {
		var pos int64
		var n int
		if err := rows.Scan(&pos, &n); err != nil {
			rows.Close()
			return 0, err
		}
		attempts[pos] = n
		positions = append(positions, pos)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(positions) == 0 {
		return 0, tx.Commit(ctx)
	}
	events, err := c.store.ReadPositions(ctx, tx, positions)
	if err != nil {
		return 0, err
	}

	blocked := map[uuid.UUID]bool{}
	var published []int64
	for _, e := range events {
		if blocked[e.StreamID] {
			continue
		}
		pms := c.byTyp[e.Type]
		if len(pms) == 0 {
			published = append(published, e.GlobalPosition)
			continue
		}
		if herr := c.deliver(ctx, tx, e, pms); herr != nil {
			dead, err := c.recordFailure(ctx, tx, e, attempts[e.GlobalPosition]+1, herr, now)
			if err != nil {
				return 0, err
			}
			if !dead {
				blocked[e.StreamID] = true
			}
			continue
		}
		published = append(published, e.GlobalPosition)
	}
	if len(published) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE es.outbox SET published_at = $2, last_error = NULL, next_attempt_at = NULL
			 WHERE global_position = ANY($1)`, published, now); err != nil {
			return 0, fmt.Errorf("outbox: marcar publicados: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("outbox: commit: %w", err)
	}
	c.processed.WithLabelValues("published").Add(float64(len(published)))
	return len(positions), nil
}

// deliver entrega el evento a sus PM dentro de un savepoint.
func (c *Consumer) deliver(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, pms []ProcessManager) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	for _, pm := range pms {
		if err := handleSafely(ctx, sp, pm, e); err != nil {
			_ = sp.Rollback(ctx)
			return fmt.Errorf("%s: %w", pm.Name(), err)
		}
	}
	return sp.Commit(ctx)
}

func handleSafely(ctx context.Context, tx pgx.Tx, pm ProcessManager, e eventstore.StoredEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return pm.Handle(ctx, e, tx)
}

func (c *Consumer) recordFailure(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, attempt int, cause error, now time.Time) (bool, error) {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	dead := attempt > c.opts.MaxAttempts
	backoff := c.opts.BaseBackoff << min(attempt, 20)
	if backoff > c.opts.MaxBackoff || backoff <= 0 {
		backoff = c.opts.MaxBackoff
	}
	if _, err := tx.Exec(ctx, `
		UPDATE es.outbox SET attempts = $2, last_error = $3, next_attempt_at = $4, dead_letter = $5
		 WHERE global_position = $1`, e.GlobalPosition, attempt, msg, now.Add(backoff), dead); err != nil {
		return false, fmt.Errorf("outbox: registrar fallo: %w", err)
	}
	log := c.opts.Logger.With("global_position", e.GlobalPosition, "event_type", e.Type, "stream_id", e.StreamID.String())
	if dead {
		c.deadLetter.Inc()
		c.processed.WithLabelValues("dead_letter").Inc()
		log.Error("outbox event dead-lettered", "alert", "OutboxDeadLetter", "attempts", attempt, "error", msg)
		return true, nil
	}
	c.processed.WithLabelValues("retry").Inc()
	log.Warn("outbox delivery failed, retrying", "attempts", attempt, "backoff", backoff.String(), "error", msg)
	return false, nil
}
