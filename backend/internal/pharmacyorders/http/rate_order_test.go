package http

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

type stubQueries struct {
	order *infra.OrderDetailView
}

func (s stubQueries) GetQuoteRequest(_ context.Context, _ uuid.UUID) (*infra.QuoteRequestDetailView, error) {
	return nil, nil
}
func (s stubQueries) ListGuardianQuoteRequests(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]infra.QuoteRequestDetailView, error) {
	return nil, nil
}
func (s stubQueries) GetOrder(_ context.Context, _ uuid.UUID) (*infra.OrderDetailView, error) {
	return s.order, nil
}
func (s stubQueries) ListGuardianOrders(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]infra.OrderDetailView, error) {
	return nil, nil
}
func (s stubQueries) ListPharmacyInbox(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]infra.PharmacyInboxItem, error) {
	return nil, nil
}
func (s stubQueries) ListPharmacyOrders(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]infra.OrderDetailView, error) {
	return nil, nil
}
func (s stubQueries) GetPrescriptionQuoteOptions(_ context.Context, _ uuid.UUID) (*infra.QuoteOptionsView, error) {
	return nil, nil
}
func (s stubQueries) GetIntegrationSettings(_ context.Context, _ uuid.UUID) (*infra.PharmacyIntegrationSettingsView, error) {
	return nil, nil
}
func (s stubQueries) UpdateIntegrationSettings(_ context.Context, _ uuid.UUID, _ string, _ *string, _ bool, _ []infra.DeliveryAreaView) error {
	return nil
}
func (s stubQueries) ListProductMappings(_ context.Context, _ uuid.UUID, _, _ int) ([]infra.ProductMappingItemView, int, error) {
	return nil, 0, nil
}
func (s stubQueries) UpsertProductMapping(_ context.Context, _, _ uuid.UUID, _, _ string, _ int, _ bool) error {
	return nil
}
func (s stubQueries) DeleteProductMapping(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func TestPharmacyHandlers_RateOrder_ValidationAndFlow(t *testing.T) {
	tutorID := uuid.New()
	orgID := uuid.New()
	orderID := uuid.New()

	order := domain.NewOrder()
	order.ID = orderID
	order.GuardianID = tutorID
	order.PharmacyOrgID = orgID
	order.Status = domain.OrderStatusDelivered

	orderRepo := newInMemOrderRepo()
	orderRepo.stores[orderID] = order

	orderView := &infra.OrderDetailView{
		ID:            orderID,
		GuardianID:    tutorID,
		PharmacyOrgID: orgID,
		Status:        domain.OrderStatusDelivered,
	}

	svc := app.NewService(app.Options{
		Orders: orderRepo,
		UoW:    dummyUoW{},
	})

	handlers := NewPharmacyHandlers(svc, stubQueries{order: orderView}, nil)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		UserID:      tutorID,
		GlobalRoles: []string{"pet_owner"},
	})

	// 1. Validar cuerpo nulo
	_, err := handlers.RateOrder(ctx, gen.RateOrderRequestObject{
		OrderId: orderID,
		Body:    nil,
	})
	if err == nil {
		t.Fatalf("expected error for nil body")
	}

	// 2. Score inválido
	_, err = handlers.RateOrder(ctx, gen.RateOrderRequestObject{
		OrderId: orderID,
		Body: &gen.RateOrderJSONRequestBody{
			Score: 6,
		},
	})
	if err == nil {
		t.Fatalf("expected error for score 6")
	}

	// 3. Valoración exitosa
	comment := "Muy buen despacho y empaque"
	resp, err := handlers.RateOrder(ctx, gen.RateOrderRequestObject{
		OrderId: orderID,
		Body: &gen.RateOrderJSONRequestBody{
			Score:   5,
			Comment: &comment,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	okResp, ok := resp.(gen.RateOrder200JSONResponse)
	if !ok {
		t.Fatalf("expected RateOrder200JSONResponse, got %T", resp)
	}
	if okResp.Score != 5 {
		t.Errorf("expected score 5, got %d", okResp.Score)
	}
	if okResp.OrderId != orderID {
		t.Errorf("expected order ID %s, got %s", orderID, okResp.OrderId)
	}

	// 4. Intento de re-valorar falla
	_, err = handlers.RateOrder(ctx, gen.RateOrderRequestObject{
		OrderId: orderID,
		Body: &gen.RateOrderJSONRequestBody{
			Score: 4,
		},
	})
	if err == nil {
		t.Fatalf("expected error for already rated order")
	}
}

func TestPharmacyHandlers_GetPharmacyStats_Defaults(t *testing.T) {
	orgID := uuid.New()
	handlers := NewPharmacyHandlers(nil, nil, nil)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		UserID:      uuid.New(),
		GlobalRoles: []string{"pharmacy_admin"},
		Memberships: []auth.Membership{
			{
				OrganizationID: orgID,
				OrgType:        "pharmacy",
				OrgStatus:      "active",
				Role:           "pharmacy_admin",
			},
		},
	})

	resp, err := handlers.GetPharmacyStats(ctx, gen.GetPharmacyStatsRequestObject{
		OrgId: orgID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	okResp, ok := resp.(gen.GetPharmacyStats200JSONResponse)
	if !ok {
		t.Fatalf("expected GetPharmacyStats200JSONResponse, got %T", resp)
	}
	if okResp.QuotesReceived != 0 {
		t.Errorf("expected quotes received 0, got %d", okResp.QuotesReceived)
	}
	if okResp.RatingAvg == nil || *okResp.RatingAvg != 5.0 {
		t.Errorf("expected default rating avg 5.0, got %v", okResp.RatingAvg)
	}
}

// silence unused warning
var _ = time.Now
