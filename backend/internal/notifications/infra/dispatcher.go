package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Beginner abre transacciones (*pgxpool.Pool).
type Beginner interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Dispatcher es el runner `notifications-dispatch` (NOT-002): cada 5 s toma
// los recordatorios vencidos (SKIP LOCKED, lote 100), los re-valida contra el
// estado actual del sujeto y crea la entrega. También emite el resumen diario
// de agenda a los clinic_admin que lo activaron.
type Dispatcher struct {
	db       Beginner
	clock    kernel.Clock
	metrics  *Metrics
	logger   *slog.Logger
	batch    int
	every    time.Duration
	lastDig  time.Time
	digestIn time.Duration
}

// NewDispatcher construye el runner.
func NewDispatcher(db Beginner, m *Metrics, logger *slog.Logger, clock kernel.Clock) *Dispatcher {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{db: db, clock: clock, metrics: m, logger: logger, batch: 100, every: 5 * time.Second, digestIn: time.Minute}
}

// Run itera hasta que se cancela el contexto.
func (d *Dispatcher) Run(ctx context.Context) error {
	t := time.NewTicker(d.every)
	defer t.Stop()
	for {
		n, err := d.Tick(ctx)
		if err != nil && ctx.Err() == nil {
			d.logger.ErrorContext(ctx, "notifications-dispatch: lote falló", "error", err.Error())
		}
		if n >= d.batch { // cola larga: seguir sin esperar
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

type dueRow struct {
	app.Scheduled
	payload []byte
}

// Tick procesa un lote y devuelve cuántos recordatorios tomó.
func (d *Dispatcher) Tick(ctx context.Context) (int, error) {
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := d.clock.Now()
	rows, err := tx.Query(ctx,
		`SELECT id, dedup_key, rule, category, template, recipient_id, subject_type, subject_id, due_at, payload
		   FROM notifications.scheduled
		  WHERE status = 'pending' AND due_at <= $1
		  ORDER BY due_at LIMIT $2 FOR UPDATE SKIP LOCKED`, now, d.batch)
	if err != nil {
		return 0, err
	}
	var due []dueRow
	for rows.Next() {
		var r dueRow
		if err := rows.Scan(&r.ID, &r.DedupKey, &r.Rule, &r.Category, &r.Template, &r.RecipientID,
			&r.SubjectType, &r.SubjectID, &r.DueAt, &r.payload); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	dir, store := NewDirectory(tx), NewStore(tx)
	for _, r := range due {
		if err := json.Unmarshal(r.payload, &r.Payload); err != nil {
			return 0, fmt.Errorf("notifications-dispatch: payload de %s: %w", r.ID, err)
		}
		status := "dispatched"
		valid, err := app.StillValid(ctx, dir, r.Scheduled)
		if err != nil {
			return 0, err
		}
		var user app.User
		if valid {
			var ok bool
			if user, ok, err = dir.User(ctx, r.RecipientID); err != nil {
				return 0, err
			}
			valid = ok
		}
		if valid {
			// La clave de la entrega incluye due_at: reprogramar la misma cita
			// genera una entrega nueva; reprocesar la misma no duplica (NOT-006).
			err = store.insertDelivery(ctx, newDelivery{
				DedupKey: fmt.Sprintf("%s|%d", r.DedupKey, r.DueAt.Unix()), Category: domain.Category(r.Category),
				Template: r.Template, Recipient: user, Vars: r.Payload,
			})
			if err != nil {
				return 0, err
			}
			d.metrics.ReminderDelay.Observe(now.Sub(r.DueAt).Seconds())
		} else {
			status = "cancelled"
		}
		if _, err := tx.Exec(ctx,
			`UPDATE notifications.scheduled SET status = $2, updated_at = $3 WHERE id = $1`, r.ID, status, now); err != nil {
			return 0, err
		}
	}
	if err := d.digest(ctx, tx, now); err != nil {
		return 0, err
	}
	return len(due), tx.Commit(ctx)
}

// digest emite el resumen diario (07 §1, última fila): a las 07:00 hora de la
// organización, a los clinic_admin con digest.clinic_daily_agenda, solo si hay
// citas. Se evalúa como mucho una vez por minuto.
func (d *Dispatcher) digest(ctx context.Context, tx pgx.Tx, now time.Time) error {
	if now.Sub(d.lastDig) < d.digestIn {
		return nil
	}
	d.lastDig = now
	type orgRow struct {
		id            uuid.UUID
		name, country string
		tz            string
	}
	rows, err := tx.Query(ctx,
		`SELECT o.id, o.display_name, o.country,
		        coalesce((SELECT a.tz FROM read_scheduling.agendas a WHERE a.organization_id = o.id ORDER BY a.professional_id LIMIT 1), '')
		   FROM read_partnernetwork.organizations o
		  WHERE o.type = 'clinic' AND o.status = 'active'`)
	if err != nil {
		return err
	}
	var orgs []orgRow
	for rows.Next() {
		var o orgRow
		if err := rows.Scan(&o.id, &o.name, &o.country, &o.tz); err != nil {
			rows.Close()
			return err
		}
		orgs = append(orgs, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range orgs {
		tz := o.tz
		if tz == "" {
			tz = domain.TimeZoneFor(o.country)
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			continue
		}
		local := now.In(loc)
		if local.Hour() < 7 {
			continue
		}
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		if err := d.digestOrg(ctx, tx, o.id, o.name, start); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) digestOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID, name string, dayStart time.Time) error {
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM read_scheduling.appointments
		  WHERE organization_id = $1 AND status IN ('confirmed','checked_in') AND slot_start >= $2 AND slot_start < $3`,
		org, dayStart, dayStart.AddDate(0, 0, 1)).Scan(&count); err != nil || count == 0 {
		return err
	}
	rows, err := tx.Query(ctx,
		`SELECT m.user_id FROM read_partnernetwork.org_members m
		   JOIN notifications.preferences p ON p.user_id = m.user_id
		  WHERE m.organization_id = $1 AND m.status = 'active' AND m.role = 'clinic_admin'
		    AND coalesce((p.digest->>'clinic_daily_agenda')::boolean, false)`, org)
	if err != nil {
		return err
	}
	var admins []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		admins = append(admins, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	dir, store := NewDirectory(tx), NewStore(tx)
	date := dayStart.Format("2006-01-02")
	for _, id := range admins {
		u, ok, err := dir.User(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		err = store.insertDelivery(ctx, newDelivery{
			DedupKey: domain.DedupKey(app.RuleClinicDailyAgenda, org.String(), id.String(), date),
			Category: domain.Appointments, Template: "clinic_daily_agenda", Recipient: u,
			Vars: map[string]string{
				"org_name": name, "date": date, "count": fmt.Sprint(count), "link_path": "/org/" + org.String() + "/agenda",
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}
