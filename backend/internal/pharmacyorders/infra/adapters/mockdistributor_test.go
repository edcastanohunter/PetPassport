package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
)

func TestMockDistributorAdapter_Contract(t *testing.T) {
	adapter := NewMockDistributorAdapter("https://distributor.example.com/api/v1")
	ctx := context.Background()
	orgID := uuid.New()

	// 1. Capabilities
	caps, err := adapter.Capabilities(ctx, orgID)
	if err != nil {
		t.Fatalf("Capabilities() error: %v", err)
	}
	if caps.Pickup || !caps.Delivery {
		t.Errorf("unexpected caps: %+v", caps)
	}

	// 2. SendQuoteRequest
	res, err := adapter.SendQuoteRequest(ctx, orgID, domain.QuoteRequestPayload{
		QuoteRequestID: uuid.New(),
		ExpiresAt:      time.Now().Add(2 * time.Hour),
		Mode:           domain.ModeDelivery,
	})
	if err != nil {
		t.Fatalf("SendQuoteRequest() error: %v", err)
	}
	if res.Channel != "adapter:mock-distributor" {
		t.Errorf("expected channel adapter:mock-distributor, got %s", res.Channel)
	}

	// 3. NotifyOrder
	orderRes, err := adapter.NotifyOrder(ctx, orgID, domain.OrderPayload{
		OrderID:      uuid.New(),
		GuardianName: "Ana Silva",
	})
	if err != nil {
		t.Fatalf("NotifyOrder() error: %v", err)
	}
	if orderRes.Channel != "adapter:mock-distributor" {
		t.Errorf("expected channel adapter:mock-distributor, got %s", orderRes.Channel)
	}

	// 4. CancelOrder
	if err := adapter.CancelOrder(ctx, orgID, uuid.New(), "customer requested"); err != nil {
		t.Fatalf("CancelOrder() error: %v", err)
	}
}
