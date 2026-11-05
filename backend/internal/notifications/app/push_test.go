package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type memPushStore struct {
	subs    map[uuid.UUID]domain.PushSubscription
	expired map[uuid.UUID]bool
}

func newMemPushStore() *memPushStore {
	return &memPushStore{
		subs:    make(map[uuid.UUID]domain.PushSubscription),
		expired: make(map[uuid.UUID]bool),
	}
}

func (m *memPushStore) Upsert(_ context.Context, sub domain.PushSubscription) (uuid.UUID, error) {
	m.subs[sub.ID] = sub
	return sub.ID, nil
}

func (m *memPushStore) ListByUser(_ context.Context, userID uuid.UUID) ([]domain.PushSubscription, error) {
	var out []domain.PushSubscription
	for _, s := range m.subs {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memPushStore) Delete(_ context.Context, userID, subID uuid.UUID) error {
	if s, ok := m.subs[subID]; ok && s.UserID == userID {
		delete(m.subs, subID)
	}
	return nil
}

func (m *memPushStore) MarkExpired(_ context.Context, subID uuid.UUID) error {
	m.expired[subID] = true
	if s, ok := m.subs[subID]; ok {
		s.Status = domain.PushStatusExpired
		m.subs[subID] = s
	}
	return nil
}

func TestPushService_RegisterAndManage(t *testing.T) {
	store := newMemPushStore()
	svc := app.NewPushService(store, app.VAPIDConfig{}, kernel.RealClock{})

	pubKey := svc.VAPIDPublicKey()
	if pubKey == "" {
		t.Fatal("esperaba clave VAPID pública generada automáticamente")
	}

	userID := uuid.New()

	// Endpoint inválido (no https)
	_, err := svc.RegisterSubscription(context.Background(), userID, app.PushSubscriptionInput{
		Endpoint: "http://example.com/push",
		Keys: domain.PushKeys{
			P256dh: "key-p256dh",
			Auth:   "key-auth",
		},
	})
	if err == nil {
		t.Fatal("endpoint no-https debe fallar (SEC-007)")
	}

	// Registro exitoso
	subID, err := svc.RegisterSubscription(context.Background(), userID, app.PushSubscriptionInput{
		Endpoint:  "https://updates.push.services.mozilla.com/wpush/v2/gAAAAAB...",
		Keys:      domain.PushKeys{P256dh: "valid-p256dh", Auth: "valid-auth"},
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	})
	if err != nil {
		t.Fatalf("RegisterSubscription falló: %v", err)
	}

	// Listar suscripciones
	list, err := svc.ListSubscriptions(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListSubscriptions falló: %v", err)
	}
	if len(list) != 1 || list[0].ID != subID {
		t.Fatalf("esperaba 1 suscripción con ID %s, got %v", subID, list)
	}

	// Payload > 2 KB debe fallar (ARQ-015)
	hugePayload := domain.PushPayload{
		Title: "Alerta",
		Body:  strings.Repeat("A", 2048),
	}
	err = svc.SendPushNotification(context.Background(), domain.PushSubscription{
		ID:       subID,
		Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/gAAAAAB...",
		Keys:     domain.PushKeys{P256dh: "p256dh", Auth: "auth"},
	}, hugePayload)
	if err == nil {
		t.Fatal("payload > 2 KB debe fallar (ARQ-015)")
	}

	// Eliminar suscripción
	if err := svc.DeleteSubscription(context.Background(), userID, subID); err != nil {
		t.Fatalf("DeleteSubscription falló: %v", err)
	}
	listAfter, _ := svc.ListSubscriptions(context.Background(), userID)
	if len(listAfter) != 0 {
		t.Fatalf("esperaba 0 suscripciones tras borrado, got %d", len(listAfter))
	}
}
