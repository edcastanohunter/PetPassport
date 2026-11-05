package domain

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PushKeys claves del cliente Web Push (P256dh y Auth).
type PushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// PushSubscriptionStatus estado de una suscripción push.
type PushSubscriptionStatus string

const (
	PushStatusActive  PushSubscriptionStatus = "active"
	PushStatusExpired PushSubscriptionStatus = "expired"
)

// PushSubscription entidad de suscripción a notificaciones push (PP-F3-DOM-011).
type PushSubscription struct {
	ID            uuid.UUID              `json:"id"`
	UserID        uuid.UUID              `json:"user_id"`
	Endpoint      string                 `json:"endpoint"`
	EndpointHash  []byte                 `json:"endpoint_hash"`
	Keys          PushKeys               `json:"keys"`
	UserAgentHash []byte                 `json:"user_agent_hash,omitempty"`
	Status        PushSubscriptionStatus `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	LastSeenAt    *time.Time             `json:"last_seen_at,omitempty"`
}

// PushPayload carga útil cifrada enviada en la notificación push (ARQ-015: ≤ 2 KB).
type PushPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Category string `json:"category"`
}

// ValidateEndpoint valida que el endpoint use https (PP-F3-SEC-007).
func ValidateEndpoint(raw string) error {
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("el endpoint debe ser una URL https válida")
	}
	return nil
}

// HashEndpoint calcula sha256 del endpoint.
func HashEndpoint(endpoint string) []byte {
	h := sha256.Sum256([]byte(strings.TrimSpace(endpoint)))
	return h[:]
}
