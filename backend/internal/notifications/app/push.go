package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PushStore almacena y recupera suscripciones push (PP-F3-DOM-011).
type PushStore interface {
	Upsert(ctx context.Context, sub domain.PushSubscription) (uuid.UUID, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PushSubscription, error)
	Delete(ctx context.Context, userID, subID uuid.UUID) error
	MarkExpired(ctx context.Context, subID uuid.UUID) error
}

// VAPIDConfig configuración VAPID por entorno (PP-F3-SEC-007, PP-F3-ARQ-015).
type VAPIDConfig struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

// PushService gestiona suscripciones Web Push y envíos (PP-F3-DOM-011, PP-F3-API-008).
type PushService struct {
	store PushStore
	vapid VAPIDConfig
	clock kernel.Clock
}

// NewPushService construye el servicio de Web Push.
func NewPushService(store PushStore, vapid VAPIDConfig, clock kernel.Clock) *PushService {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	if vapid.PublicKey == "" || vapid.PrivateKey == "" {
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err == nil {
			vapid.PublicKey = pub
			vapid.PrivateKey = priv
		}
	}
	if vapid.Subject == "" {
		vapid.Subject = "mailto:soporte@petpassport.app"
	}
	return &PushService{store: store, vapid: vapid, clock: clock}
}

// VAPIDPublicKey devuelve la clave pública para el frontend (GET /push/vapid-public-key).
func (s *PushService) VAPIDPublicKey() string {
	return s.vapid.PublicKey
}

// PushSubscriptionInput datos de entrada al registrar una suscripción.
type PushSubscriptionInput struct {
	Endpoint  string          `json:"endpoint"`
	Keys      domain.PushKeys `json:"keys"`
	UserAgent string          `json:"user_agent,omitempty"`
}

// PushSubscriptionView vista de una suscripción push para el usuario.
type PushSubscriptionView struct {
	ID        uuid.UUID  `json:"id"`
	Endpoint  string     `json:"endpoint"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// RegisterSubscription registra o actualiza una suscripción Web Push (PP-F3-API-008).
func (s *PushService) RegisterSubscription(ctx context.Context, userID uuid.UUID, input PushSubscriptionInput) (uuid.UUID, error) {
	if err := domain.ValidateEndpoint(input.Endpoint); err != nil {
		return uuid.Nil, kernel.NewDomainError("INVALID_ENDPOINT", err.Error())
	}
	if strings.TrimSpace(input.Keys.P256dh) == "" || strings.TrimSpace(input.Keys.Auth) == "" {
		return uuid.Nil, kernel.NewDomainError("INVALID_KEYS", "p256dh y auth son requeridos")
	}

	var uaHash []byte
	if input.UserAgent != "" {
		h := sha256.Sum256([]byte(input.UserAgent))
		uaHash = h[:]
	}

	sub := domain.PushSubscription{
		ID:            uuid.New(),
		UserID:        userID,
		Endpoint:      input.Endpoint,
		EndpointHash:  domain.HashEndpoint(input.Endpoint),
		Keys:          input.Keys,
		UserAgentHash: uaHash,
		Status:        domain.PushStatusActive,
		CreatedAt:     s.clock.Now(),
	}

	return s.store.Upsert(ctx, sub)
}

// ListSubscriptions devuelve las suscripciones de un usuario (PP-F3-API-008).
func (s *PushService) ListSubscriptions(ctx context.Context, userID uuid.UUID) ([]PushSubscriptionView, error) {
	subs, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PushSubscriptionView, len(subs))
	for i, sub := range subs {
		out[i] = PushSubscriptionView{
			ID:         sub.ID,
			Endpoint:   sub.Endpoint,
			Status:     string(sub.Status),
			CreatedAt:  sub.CreatedAt,
			LastSeenAt: sub.LastSeenAt,
		}
	}
	return out, nil
}

// DeleteSubscription revoca una suscripción push (PP-F3-API-008).
func (s *PushService) DeleteSubscription(ctx context.Context, userID, subID uuid.UUID) error {
	return s.store.Delete(ctx, userID, subID)
}

// SendPushNotification envía una notificación push cifrada vía VAPID (PP-F3-ARQ-015, PP-F3-SEC-007).
func (s *PushService) SendPushNotification(ctx context.Context, sub domain.PushSubscription, payload domain.PushPayload) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}
	if len(payloadBytes) > 2048 {
		return errors.New("payload push excede el límite de 2 KB (ARQ-015)")
	}

	subData := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.Keys.P256dh,
			Auth:   sub.Keys.Auth,
		},
	}

	res, err := webpush.SendNotification(payloadBytes, subData, &webpush.Options{
		Subscriber:      s.vapid.Subject,
		VAPIDPublicKey:  s.vapid.PublicKey,
		VAPIDPrivateKey: s.vapid.PrivateKey,
		TTL:             86400,
	})
	if err != nil {
		return fmt.Errorf("webpush send: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusGone || res.StatusCode == http.StatusNotFound {
		_ = s.store.MarkExpired(ctx, sub.ID)
	}

	return nil
}
