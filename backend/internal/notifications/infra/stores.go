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
	var email, digest []byte
	err := s.q.QueryRow(ctx,
		`SELECT email, digest FROM notifications.preferences WHERE user_id = $1`, user).Scan(&email, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Preferences{}, false, nil
	}
	if err != nil {
		return domain.Preferences{}, false, err
	}
	p, err := domain.ParsePreferences(email, digest)
	return p, err == nil, err
}

// Put guarda las preferencias (upsert).
func (s *PrefStore) Put(ctx context.Context, user uuid.UUID, p domain.Preferences) error {
	email, err := json.Marshal(p.Email)
	if err != nil {
		return err
	}
	digest, err := json.Marshal(p.Digest)
	if err != nil {
		return err
	}
	_, err = s.q.Exec(ctx,
		`INSERT INTO notifications.preferences (user_id, email, digest, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email, digest = EXCLUDED.digest, updated_at = now()`,
		user, email, digest)
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
