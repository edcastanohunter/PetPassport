package http

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/genpartner"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/egress"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type mockDNSResolver struct{}

func (mockDNSResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if host == "127.0.0.1" || host == "localhost" {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	if host == "10.0.0.1" {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}
	if host == "169.254.169.254" {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

func TestMain(m *testing.M) {
	orig := egress.DefaultResolver
	egress.DefaultResolver = mockDNSResolver{}
	code := m.Run()
	egress.DefaultResolver = orig
	os.Exit(code)
}

type dummyUoW struct{}

func (dummyUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type inMemQRRepo struct {
	mu     sync.Mutex
	stores map[uuid.UUID]*domain.QuoteRequest
}

func newInMemQRRepo() *inMemQRRepo {
	return &inMemQRRepo{stores: make(map[uuid.UUID]*domain.QuoteRequest)}
}

func (r *inMemQRRepo) Load(_ context.Context, id uuid.UUID) (*domain.QuoteRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	qr, ok := r.stores[id]
	if !ok {
		return nil, domain.ErrQuoteNotFound
	}
	return qr, nil
}

func (r *inMemQRRepo) Save(_ context.Context, qr *domain.QuoteRequest) ([]eventstore.StoredEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[qr.ID] = qr
	qr.MarkCommitted()
	return nil, nil
}

type inMemOrderRepo struct {
	mu     sync.Mutex
	stores map[uuid.UUID]*domain.Order
}

func newInMemOrderRepo() *inMemOrderRepo {
	return &inMemOrderRepo{stores: make(map[uuid.UUID]*domain.Order)}
}

func (r *inMemOrderRepo) Load(_ context.Context, id uuid.UUID) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.stores[id]
	if !ok {
		return nil, domain.ErrOrderNotCancellable
	}
	return o, nil
}

func (r *inMemOrderRepo) Save(_ context.Context, o *domain.Order) ([]eventstore.StoredEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[o.ID] = o
	o.MarkCommitted()
	return nil, nil
}

type dummyRxReader struct {
	rx *domain.PrescriptionSnapshot
}

func (d *dummyRxReader) GetPrescription(_ context.Context, id uuid.UUID) (*domain.PrescriptionSnapshot, error) {
	if d.rx != nil && d.rx.ID == id {
		return d.rx, nil
	}
	return nil, errors.New("prescription not found")
}

type dummyGateway struct{}

func (dummyGateway) SendQuoteRequest(ctx context.Context, orgID uuid.UUID, payload domain.QuoteRequestPayload) (domain.SendResult, error) {
	return domain.SendResult{Channel: "api", SentAt: time.Now()}, nil
}

func (dummyGateway) NotifyOrder(ctx context.Context, orgID uuid.UUID, payload domain.OrderPayload) (domain.SendResult, error) {
	return domain.SendResult{Channel: "api", SentAt: time.Now()}, nil
}

func (dummyGateway) CancelOrder(ctx context.Context, orgID uuid.UUID, orderID uuid.UUID, reason string) error {
	return nil
}

func (dummyGateway) Capabilities(ctx context.Context, orgID uuid.UUID) (domain.PartnerCapabilities, error) {
	return domain.PartnerCapabilities{Pickup: true, Delivery: true, Substitutions: true}, nil
}

func withPartnerCtx(ctx context.Context, orgID uuid.UUID) context.Context {
	p := auth.PartnerPrincipal{
		OrgID:    orgID,
		ClientID: "partner-test-client",
		Scopes:   []string{"partner:read", "partner:write"},
	}
	return auth.WithPartnerPrincipal(ctx, p)
}

func TestPartnerHandlers_Unauthenticated(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	ctx := context.Background()

	_, err := handlers.PartnerGetMe(ctx, genpartner.PartnerGetMeRequestObject{})
	if err == nil {
		t.Fatalf("se esperaba error de autenticación")
	}

	var fb *kernel.ForbiddenError
	if !errors.As(err, &fb) {
		t.Fatalf("se esperaba kernel.ForbiddenError, se obtuvo %T: %v", err, err)
	}
}

func TestPartnerHandlers_SubmitQuote_Validation(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	orgID := uuid.New()
	ctx := withPartnerCtx(context.Background(), orgID)

	// Body nil
	_, err := handlers.PartnerSubmitQuote(ctx, genpartner.PartnerSubmitQuoteRequestObject{
		Id:   uuid.New(),
		Body: nil,
	})
	if err == nil {
		t.Fatalf("se esperaba error de validación con body nil")
	}

	// Empty lines
	_, err = handlers.PartnerSubmitQuote(ctx, genpartner.PartnerSubmitQuoteRequestObject{
		Id: uuid.New(),
		Body: &genpartner.PartnerSubmitQuotePayload{
			Lines: []genpartner.PartnerQuoteLinePayload{},
		},
	})
	if err == nil {
		t.Fatalf("se esperaba error de validación con líneas vacías")
	}
	var ve *kernel.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("se esperaba kernel.ValidationError, se obtuvo: %T", err)
	}
}

func TestPartnerHandlers_SubmitQuote_Success(t *testing.T) {
	now := time.Now()
	guardianID := uuid.New()
	rxID := uuid.New()
	catID := uuid.New()
	pharmacyOrgID := uuid.New()

	rxSnapshot := &domain.PrescriptionSnapshot{
		ID:             rxID,
		PetID:          uuid.New(),
		GuardianID:     guardianID,
		Country:        "CO",
		Status:         "issued",
		Source:         "issued",
		ValidUntil:     now.Add(48 * time.Hour),
		RefillsAllowed: 1,
		RefillsUsed:    0,
		Items: []domain.PrescriptionItemSnapshot{
			{
				ItemID:              "item-1",
				CatalogID:           &catID,
				Name:                "Amoxicilina 500mg",
				Quantity:            2,
				Unit:                "pack",
				SubstitutionAllowed: true,
			},
		},
	}

	qrRepo := newInMemQRRepo()
	orderRepo := newInMemOrderRepo()
	svc := app.NewService(app.Options{
		UoW:           dummyUoW{},
		QuoteRequests: qrRepo,
		Orders:        orderRepo,
		Gateway:       dummyGateway{},
		Prescriptions: &dummyRxReader{rx: rxSnapshot},
		Clock:         func() time.Time { return now },
	})

	ctx := withPartnerCtx(context.Background(), pharmacyOrgID)

	qrID, err := svc.RequestQuotes(ctx, app.RequestQuotesCmd{
		PrescriptionID:  rxID,
		GuardianID:      guardianID,
		Items:           []domain.RequestedItem{{ItemID: "item-1"}},
		Mode:            domain.ModeDelivery,
		Area:            domain.Area{City: "Bogota"},
		OrganizationIDs: []uuid.UUID{pharmacyOrgID},
		ExpiresInHours:  24,
	})
	if err != nil {
		t.Fatalf("request quotes error: %v", err)
	}

	handlers := NewPartnerHandlers(nil, svc, nil, nil)

	subCatID := uuid.New()
	subName := "Genérico 500mg"
	subReason := "principio activo idéntico"
	subPrice := float32(12.50)

	resp, err := handlers.PartnerSubmitQuote(ctx, genpartner.PartnerSubmitQuoteRequestObject{
		Id: qrID,
		Body: &genpartner.PartnerSubmitQuotePayload{
			Lines: []genpartner.PartnerQuoteLinePayload{
				{
					ItemId:    catID,
					UnitPrice: 15.00,
					Qty:       2,
					Available: genpartner.Full,
					Substitution: &struct {
						CatalogId *uuid.UUID `json:"catalog_id,omitempty"`
						Name      *string    `json:"name,omitempty"`
						Reason    *string    `json:"reason,omitempty"`
						UnitPrice *float32   `json:"unit_price,omitempty"`
					}{
						CatalogId: &subCatID,
						Name:      &subName,
						Reason:    &subReason,
						UnitPrice: &subPrice,
					},
				},
			},
			Eta: stringPtr("30 minutos"),
		},
	})
	if err != nil {
		t.Fatalf("PartnerSubmitQuote falló inesperadamente: %v", err)
	}

	okResp, ok := resp.(genpartner.PartnerSubmitQuote201JSONResponse)
	if !ok {
		t.Fatalf("se esperaba PartnerSubmitQuote201JSONResponse, se obtuvo %T", resp)
	}
	if okResp.QuoteId == uuid.Nil {
		t.Fatalf("quote ID generado no debe ser nil")
	}
	if okResp.Status != "submitted" {
		t.Fatalf("status inesperado: %s", okResp.Status)
	}
}

func TestPartnerHandlers_WebhookConfig_AntiSSRF(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	orgID := uuid.New()
	ctx := withPartnerCtx(context.Background(), orgID)

	badURLs := []string{
		"http://127.0.0.1/wh",
		"http://localhost:8080/wh",
		"https://10.0.0.1/wh",
		"https://169.254.169.254/latest/meta-data",
		"ftp://farmacia.com/wh",
		"https://admin:pass@farmacia.com/wh",
	}

	for _, u := range badURLs {
		_, err := handlers.PartnerConfigureWebhook(ctx, genpartner.PartnerConfigureWebhookRequestObject{
			Body: &genpartner.PartnerConfigureWebhookPayload{
				Url:    u,
				Events: []string{"quote_request.created"},
			},
		})
		if err == nil {
			t.Errorf("se esperaba que la URL SSRF %s fuese rechazada", u)
		}
		var ve *kernel.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("se esperaba kernel.ValidationError para URL %s, se obtuvo: %T", u, err)
		}
	}
}

func TestPartnerHandlers_WebhookConfig_ValidGeneratesSecret(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	orgID := uuid.New()
	ctx := withPartnerCtx(context.Background(), orgID)

	resp, err := handlers.PartnerConfigureWebhook(ctx, genpartner.PartnerConfigureWebhookRequestObject{
		Body: &genpartner.PartnerConfigureWebhookPayload{
			Url:    "https://api.farmacia-ejemplo.com/webhooks/petpassport",
			Events: []string{"quote_request.created"},
		},
	})
	if err != nil {
		t.Fatalf("configuración de webhook válida falló: %v", err)
	}

	okResp, ok := resp.(genpartner.PartnerConfigureWebhook200JSONResponse)
	if !ok {
		t.Fatalf("se esperaba PartnerConfigureWebhook200JSONResponse, se obtuvo %T", resp)
	}
	if okResp.Secret == nil || *okResp.Secret == "" {
		t.Fatalf("se esperaba que se generase un secreto criptográfico automáticamente")
	}
	if len(*okResp.Secret) < 32 {
		t.Fatalf("el secreto generado es demasiado corto: %s", *okResp.Secret)
	}
}

func TestPartnerHandlers_BulkProductMappings_Validation(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	orgID := uuid.New()
	ctx := withPartnerCtx(context.Background(), orgID)

	_, err := handlers.PartnerBulkProductMappings(ctx, genpartner.PartnerBulkProductMappingsRequestObject{
		Body: nil,
	})
	if err == nil {
		t.Fatalf("se esperaba error con body nil")
	}

	_, err = handlers.PartnerBulkProductMappings(ctx, genpartner.PartnerBulkProductMappingsRequestObject{
		Body: &genpartner.PartnerBulkMappingsPayload{
			Mappings: nil,
		},
	})
	if err == nil {
		t.Fatalf("se esperaba error con mappings vacíos")
	}
}

func stringPtr(s string) *string { return &s }

func TestPartnerHandlers_PartnerGetStats(t *testing.T) {
	handlers := NewPartnerHandlers(nil, nil, nil, nil)
	orgID := uuid.New()
	ctx := withPartnerCtx(context.Background(), orgID)

	resp, err := handlers.PartnerGetStats(ctx, genpartner.PartnerGetStatsRequestObject{})
	if err != nil {
		t.Fatalf("unexpected error calling PartnerGetStats: %v", err)
	}

	okResp, ok := resp.(genpartner.PartnerGetStats200JSONResponse)
	if !ok {
		t.Fatalf("expected PartnerGetStats200JSONResponse, got %T", resp)
	}

	if okResp.OrganizationId != orgID {
		t.Errorf("expected org ID %s, got %s", orgID, okResp.OrganizationId)
	}
	if okResp.WindowDays != 90 {
		t.Errorf("expected window days 90, got %d", okResp.WindowDays)
	}
	if !okResp.SlaCompliant {
		t.Errorf("expected default SLA compliance true")
	}
	if okResp.Thresholds == nil || okResp.Thresholds.MinQuoteRatePercent != 70.0 || okResp.Thresholds.MaxAvgResponseHours != 6.0 {
		t.Errorf("invalid SLA thresholds: %+v", okResp.Thresholds)
	}
}
