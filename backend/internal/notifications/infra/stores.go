package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
)

// PrefStore implementa app.PrefStore sobre notifications.preferences.
type PrefStore struct{ q Querier }

var _ app.PrefStore = (*PrefStore)(nil)

// NewPrefStore construye el store.
func NewPrefStore(q Querier) *PrefStore { return &PrefStore{q: q} }

// Get lee las preferencias; ok=false si el usuario no tiene fila.
func (s *PrefStore) Get(ctx context.Context, user uuid.UUID) (domain.Preferences, bool, error) {
	var email, push, digest []byte
	err := s.q.QueryRow(ctx,
		`SELECT email, push, digest FROM notifications.preferences WHERE user_id = $1`, user).Scan(&email, &push, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Preferences{}, false, nil
	}
	if err != nil {
		return domain.Preferences{}, false, err
	}
	p, err := domain.ParsePreferences(email, push, digest)
	return p, err == nil, err
}

// Put guarda las preferencias (upsert).
func (s *PrefStore) Put(ctx context.Context, user uuid.UUID, p domain.Preferences) error {
	email, err := json.Marshal(p.Email)
	if err != nil {
		return err
	}
	push, err := json.Marshal(p.Push)
	if err != nil {
		return err
	}
	digest, err := json.Marshal(p.Digest)
	if err != nil {
		return err
	}
	_, err = s.q.Exec(ctx,
		`INSERT INTO notifications.preferences (user_id, email, push, digest, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email, push = EXCLUDED.push, digest = EXCLUDED.digest, updated_at = now()`,
		user, email, push, digest)
	return err
}

// Preferences implementa app.PrefReader (valores por defecto si no hay fila).
func (s *PrefStore) Preferences(ctx context.Context, user uuid.UUID) (domain.Preferences, error) {
	p, ok, err := s.Get(ctx, user)
	if err != nil {
		return domain.Preferences{}, err
	}
	if !ok {
		return domain.DefaultPreferences(), nil
	}
	return p, nil
}

// Suppressions implementa app.SuppressionStore y la limitación por plantilla.
type Suppressions struct{ q Querier }

var (
	_ app.SuppressionStore = (*Suppressions)(nil)
	_ app.ThrottleReader   = (*Suppressions)(nil)
)

// NewSuppressions construye el store.
func NewSuppressions(q Querier) *Suppressions { return &Suppressions{q: q} }

// Suppressed indica si la dirección (por hash) está suprimida.
func (s *Suppressions) Suppressed(ctx context.Context, hash []byte) (bool, error) {
	var one int
	err := s.q.QueryRow(ctx, `SELECT 1 FROM notifications.suppressions WHERE address_hash = $1`, hash).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Suppress registra la dirección como suprimida (idempotente).
func (s *Suppressions) Suppress(ctx context.Context, hash []byte, reason string) error {
	_, err := s.q.Exec(ctx,
		`INSERT INTO notifications.suppressions (address_hash, reason) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hash, reason)
	return err
}

// LastSent devuelve el último envío de la plantilla al destinatario.
func (s *Suppressions) LastSent(ctx context.Context, recipient uuid.UUID, template string) (time.Time, bool, error) {
	var t *time.Time
	err := s.q.QueryRow(ctx,
		`SELECT max(sent_at) FROM notifications.deliveries WHERE recipient_id = $1 AND template = $2 AND status = 'sent'`,
		recipient, template).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, false, err
	}
	return *t, true, nil
}

// PushStore persiste y consulta suscripciones push en notifications.push_subscriptions (PP-F3-DAT-006, PP-F3-DOM-011).
type PushStore struct {
	q Querier
}

// NewPushStore construye el store de suscripciones push.
func NewPushStore(q Querier) *PushStore { return &PushStore{q: q} }

func (s *PushStore) Upsert(ctx context.Context, sub domain.PushSubscription) (uuid.UUID, error) {
	id := sub.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	var outID uuid.UUID
	err := s.q.QueryRow(ctx, `
		INSERT INTO notifications.push_subscriptions
		  (id, user_id, endpoint_hash, endpoint, p256dh, auth, user_agent_hash, status, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now(), now())
		ON CONFLICT (endpoint_hash) DO UPDATE SET
		  user_id = EXCLUDED.user_id,
		  p256dh = EXCLUDED.p256dh,
		  auth = EXCLUDED.auth,
		  user_agent_hash = EXCLUDED.user_agent_hash,
		  status = 'active',
		  last_seen_at = now()
		RETURNING id`,
		id, sub.UserID, sub.EndpointHash, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, sub.UserAgentHash).Scan(&outID)
	return outID, err
}

func (s *PushStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PushSubscription, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, user_id, endpoint_hash, endpoint, p256dh, auth, user_agent_hash, status, created_at, last_seen_at
		FROM notifications.push_subscriptions
		WHERE user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.PushSubscription
	for rows.Next() {
		var sub domain.PushSubscription
		var statusStr string
		var p256dh, authStr string
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.EndpointHash, &sub.Endpoint, &p256dh, &authStr, &sub.UserAgentHash, &statusStr, &sub.CreatedAt, &sub.LastSeenAt); err != nil {
			return nil, err
		}
		sub.Keys.P256dh = p256dh
		sub.Keys.Auth = authStr
		sub.Status = domain.PushSubscriptionStatus(statusStr)
		list = append(list, sub)
	}
	return list, rows.Err()
}

func (s *PushStore) Delete(ctx context.Context, userID, subID uuid.UUID) error {
	_, err := s.q.Exec(ctx, `DELETE FROM notifications.push_subscriptions WHERE user_id = $1 AND id = $2`, userID, subID)
	return err
}

func (s *PushStore) MarkExpired(ctx context.Context, subID uuid.UUID) error {
	_, err := s.q.Exec(ctx, `UPDATE notifications.push_subscriptions SET status = 'expired' WHERE id = $1`, subID)
	return err
}
