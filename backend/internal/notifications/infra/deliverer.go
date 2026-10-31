package infra

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// leaseFor es el tiempo que una entrega 'sending' queda reservada a un worker:
// si el proceso muere a mitad, otro la retoma al vencer (at-least-once).
const leaseFor = 5 * time.Minute

// Deliverer es el runner `notifications-sender` (NOT-005): reclama entregas
// con SKIP LOCKED, las envía por el canal fuera de la transacción de reclamo y
// registra el resultado (enviada, suprimida, diferida, reintento o dead-letter).
type Deliverer struct {
	db      Beginner
	sender  *app.Sender
	clock   kernel.Clock
	metrics *Metrics
	logger  *slog.Logger
	batch   int
	every   time.Duration
}

// NewDeliverer construye el runner.
func NewDeliverer(db Beginner, sender *app.Sender, m *Metrics, logger *slog.Logger, clock kernel.Clock) *Deliverer {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Deliverer{db: db, sender: sender, clock: clock, metrics: m, logger: logger, batch: 20, every: time.Second}
}

// Run itera hasta que se cancela el contexto.
func (d *Deliverer) Run(ctx context.Context) error {
	t := time.NewTicker(d.every)
	defer t.Stop()
	for {
		n, err := d.Tick(ctx)
		if err != nil && ctx.Err() == nil {
			d.logger.ErrorContext(ctx, "notifications-sender: lote falló", "error", err.Error())
		}
		if n >= d.batch {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

type claimed struct {
	app.Delivery
	created time.Time
}

// Tick reclama y procesa un lote; devuelve cuántas entregas tomó.
func (d *Deliverer) Tick(ctx context.Context) (int, error) {
	now := d.clock.Now()
	batch, err := d.claim(ctx, now)
	if err != nil {
		return 0, err
	}
	for _, c := range batch {
		out, err := d.sender.Deliver(ctx, c.Delivery)
		if err != nil {
			// Fallo de infraestructura (BD): la entrega queda 'sending' y se
			// retoma al vencer el lease. El texto puede incluir SQL, nunca PII.
			d.logger.ErrorContext(ctx, "notifications-sender: entrega no procesada", "delivery_id", c.ID.String(), "error", err.Error())
			continue
		}
		if err := d.record(ctx, c, out, now); err != nil {
			d.logger.ErrorContext(ctx, "notifications-sender: no se pudo registrar el resultado", "delivery_id", c.ID.String(), "error", err.Error())
		}
	}
	return len(batch), nil
}

func (d *Deliverer) claim(ctx context.Context, now time.Time) ([]claimed, error) {
	rows, err := d.db.Query(ctx,
		`WITH c AS (
		   SELECT id FROM notifications.deliveries
		    WHERE status IN ('queued','failed','sending') AND NOT dead_letter AND next_attempt_at <= $1
		    ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
		 UPDATE notifications.deliveries d
		    SET status = 'sending', attempts = d.attempts + 1, next_attempt_at = $1::timestamptz + $3::interval
		   FROM c WHERE d.id = c.id
		 RETURNING d.id, d.dedup_key, d.category, d.template, d.locale, d.recipient_id, d.payload, d.attempts, d.created_at`,
		now, d.batch, leaseFor.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claimed
	for rows.Next() {
		var c claimed
		var cat string
		var raw []byte
		if err := rows.Scan(&c.ID, &c.DedupKey, &cat, &c.Template, &c.Locale, &c.RecipientID, &raw, &c.Attempts, &c.created); err != nil {
			return nil, err
		}
		c.Category = domain.Category(cat)
		c.CreatedAt = c.created
		if err := json.Unmarshal(raw, &c.Payload); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// record guarda el resultado de la entrega.
func (d *Deliverer) record(ctx context.Context, c claimed, out app.Outcome, now time.Time) error {
	switch {
	case out.Deferred: // limitación: vuelve a la cola sin consumir el intento
		_, err := d.db.Exec(ctx,
			`UPDATE notifications.deliveries SET status = 'queued', attempts = attempts - 1, next_attempt_at = $2 WHERE id = $1`,
			c.ID, now.Add(out.Wait))
		return err
	case out.Status == domain.StatusSent:
		d.metrics.Deliveries.WithLabelValues(c.Template, domain.StatusSent).Inc()
		d.metrics.Latency.Observe(now.Sub(c.created).Seconds())
		return d.finish(ctx, c, domain.StatusSent, "", out.MessageID, now)
	case out.Status == domain.StatusSuppressed:
		d.metrics.Deliveries.WithLabelValues(c.Template, domain.StatusSuppressed).Inc()
		return d.finish(ctx, c, domain.StatusSuppressed, out.Reason, "", now)
	case out.Dead:
		d.metrics.Deliveries.WithLabelValues(c.Template, domain.StatusFailed).Inc()
		d.metrics.DeadLetter.Inc()
		d.logger.WarnContext(ctx, "notifications: entrega fallida definitivamente",
			"delivery_id", c.ID.String(), "template", c.Template, "attempts", c.Attempts, "reason", out.Reason)
		return d.finishDead(ctx, c, out.Reason)
	default: // reintento con backoff
		_, err := d.db.Exec(ctx,
			`UPDATE notifications.deliveries SET status = 'failed', last_error = $2, next_attempt_at = $3 WHERE id = $1`,
			c.ID, out.Reason, now.Add(out.Wait))
		return err
	}
}

func (d *Deliverer) finish(ctx context.Context, c claimed, status, reason, providerID string, now time.Time) error {
	payload, err := scrub(c.Payload)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(ctx,
		`UPDATE notifications.deliveries
		    SET status = $2, last_error = NULLIF($3, ''), provider_message_id = NULLIF($4, ''),
		        sent_at = CASE WHEN $2 = 'sent' THEN $5 ELSE sent_at END, payload = $6, next_attempt_at = NULL
		  WHERE id = $1`, c.ID, status, reason, providerID, now, payload)
	return err
}

func (d *Deliverer) finishDead(ctx context.Context, c claimed, reason string) error {
	payload, err := scrub(c.Payload)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(ctx,
		`UPDATE notifications.deliveries
		    SET status = 'failed', dead_letter = true, last_error = $2, payload = $3, next_attempt_at = NULL
		  WHERE id = $1`, c.ID, reason, payload)
	return err
}

// scrub elimina del payload los secretos de un solo uso y la dirección de
// destinatarios sin cuenta: tras un estado terminal ya no se necesitan.
func scrub(p map[string]string) ([]byte, error) {
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = v
	}
	for _, k := range strings.Split(p[app.KeySecret], ",") {
		delete(out, k)
	}
	delete(out, app.KeySecret)
	delete(out, app.KeyTo)
	return json.Marshal(out)
}
