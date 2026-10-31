package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/templates"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
)

// Store escribe notifications.scheduled y notifications.deliveries. Todas las
// escrituras son idempotentes por dedup_key (NOT-006).
type Store struct{ q Querier }

// NewStore construye el store sobre una tx (PM) o un pool (enqueuer, runners).
func NewStore(q Querier) *Store { return &Store{q: q} }

// Apply ejecuta las acciones del planificador en orden.
func (s *Store) Apply(ctx context.Context, actions []app.Action) error {
	for _, a := range actions {
		var err error
		switch v := a.(type) {
		case app.SendNow:
			err = s.insertDelivery(ctx, newDelivery{
				DedupKey: v.DedupKey(), Category: v.Category, Template: v.Template,
				Recipient: v.Recipient, Vars: v.Vars,
			})
		case app.Schedule:
			err = s.upsertScheduled(ctx, v)
		case app.CancelSubject:
			err = s.cancelSubject(ctx, v)
		case app.CancelPet:
			_, err = s.q.Exec(ctx,
				`UPDATE notifications.scheduled SET status = 'cancelled', updated_at = now()
				  WHERE status = 'pending' AND payload->>'pet_id' = $1`, v.PetID.String())
		default:
			err = fmt.Errorf("notifications: acción desconocida %T", a)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type newDelivery struct {
	DedupKey  string
	Category  domain.Category
	Template  string
	Recipient app.User
	Vars      map[string]string
	Secret    []string
	NotBefore time.Time // cero = ya
}

func (s *Store) insertDelivery(ctx context.Context, d newDelivery) error {
	spec, ok := templates.Catalog[d.Template]
	if !ok {
		return fmt.Errorf("notifications: plantilla %q desconocida", d.Template)
	}
	payload := make(map[string]string, len(d.Vars)+2)
	for k, v := range d.Vars {
		payload[k] = v
	}
	if d.Recipient.ID == notify.GhostRecipientID(d.Recipient.Email) { // sin cuenta: la dirección viaja en el payload
		payload[app.KeyTo] = d.Recipient.Email
	}
	if len(d.Secret) > 0 {
		payload[app.KeySecret] = joinKeys(d.Secret)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	locale := d.Recipient.Locale
	if locale != "en" {
		locale = "es"
	}
	notBefore := d.NotBefore
	if notBefore.IsZero() {
		notBefore = time.Now()
	}
	_, err = s.q.Exec(ctx,
		`INSERT INTO notifications.deliveries
		   (id, dedup_key, channel, category, template, template_version, locale, recipient_id,
		    recipient_address_hash, payload, status, next_attempt_at)
		 VALUES ($1, $2, 'email', $3, $4, $5, $6, $7, $8, $9, 'queued', $10)
		 ON CONFLICT (dedup_key) DO NOTHING`,
		uuid.New(), d.DedupKey, string(d.Category), d.Template, spec.Version, locale, d.Recipient.ID,
		app.AddressHash(d.Recipient.Email), raw, notBefore)
	return err
}

func (s *Store) upsertScheduled(ctx context.Context, v app.Schedule) error {
	raw, err := json.Marshal(v.Vars)
	if err != nil {
		return err
	}
	// Reprogramar (misma clave) mueve due_at y reabre el recordatorio.
	_, err = s.q.Exec(ctx,
		`INSERT INTO notifications.scheduled
		   (id, dedup_key, rule, category, template, recipient_id, subject_type, subject_id, due_at, payload, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending')
		 ON CONFLICT (dedup_key) DO UPDATE
		   SET due_at = EXCLUDED.due_at, payload = EXCLUDED.payload, status = 'pending', updated_at = now()`,
		uuid.New(), v.DedupKey(), v.Rule, string(v.Category), v.Template, v.RecipientID, v.SubjectType, v.SubjectID, v.DueAt, raw)
	return err
}

func (s *Store) cancelSubject(ctx context.Context, v app.CancelSubject) error {
	_, err := s.q.Exec(ctx,
		`UPDATE notifications.scheduled SET status = 'cancelled', updated_at = now()
		  WHERE status = 'pending' AND subject_type = $1 AND subject_id = $2
		    AND (coalesce(cardinality($3::text[]), 0) = 0 OR rule = ANY($3))`,
		v.SubjectType, v.SubjectID, v.Rules)
	return err
}

func joinKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k
	}
	return out
}
