package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PrefStore persiste las preferencias (notifications.preferences).
type PrefStore interface {
	// Get devuelve las preferencias o ok=false si el usuario no tiene fila.
	Get(ctx context.Context, user uuid.UUID) (domain.Preferences, bool, error)
	Put(ctx context.Context, user uuid.UUID, p domain.Preferences) error
}

// Preferences atiende la API de preferencias y la baja por enlace (API-010).
type Preferences struct {
	store  PrefStore
	secret []byte
	clock  kernel.Clock
}

// NewPreferences construye el servicio.
func NewPreferences(store PrefStore, unsubscribeSecret []byte, clock kernel.Clock) *Preferences {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &Preferences{store: store, secret: unsubscribeSecret, clock: clock}
}

// Get devuelve las preferencias del usuario (valores por defecto si no hay fila).
func (s *Preferences) Get(ctx context.Context, user uuid.UUID) (domain.Preferences, error) {
	p, ok, err := s.store.Get(ctx, user)
	if err != nil {
		return domain.Preferences{}, err
	}
	if !ok {
		return domain.DefaultPreferences(), nil
	}
	return p.Normalize(), nil
}

// Preferences implementa PrefReader para el sender.
func (s *Preferences) Preferences(ctx context.Context, user uuid.UUID) (domain.Preferences, error) {
	return s.Get(ctx, user)
}

// Put reemplaza las preferencias; security se ignora (siempre activa).
func (s *Preferences) Put(ctx context.Context, user uuid.UUID, p domain.Preferences) (domain.Preferences, error) {
	p = p.Normalize()
	if err := s.store.Put(ctx, user, p); err != nil {
		return domain.Preferences{}, err
	}
	return p, nil
}

// Unsubscribe da de baja la categoría del token sin sesión (NOT-004). Token
// inválido, firmado de otro modo o caducado → domain.ErrTokenInvalid.
func (s *Preferences) Unsubscribe(ctx context.Context, token string) (domain.Category, error) {
	user, cat, err := domain.VerifyUnsubscribe(s.secret, token, s.clock.Now())
	if err != nil {
		return "", err
	}
	p, err := s.Get(ctx, user)
	if err != nil {
		return "", err
	}
	if !p.Email[cat] { // idempotente: ya estaba de baja
		return cat, nil
	}
	p.Email[cat] = false
	if err := s.store.Put(ctx, user, p); err != nil {
		return "", err
	}
	return cat, nil
}
