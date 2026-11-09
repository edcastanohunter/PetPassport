package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// inMemoryUoW implementa UnitOfWork para tests unitarios del app service.
type inMemoryUoW struct{}

func (inMemoryUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// inMemoryQuoteRequestRepo simula almacenamiento en memoria con control optimista de versión.
type inMemoryQuoteRequestRepo struct {
	mu     sync.Mutex
	stores map[uuid.UUID]*domain.QuoteRequest
}

func newInMemoryQuoteRequestRepo() *inMemoryQuoteRequestRepo {
	return &inMemoryQuoteRequestRepo{stores: make(map[uuid.UUID]*domain.QuoteRequest)}
}

func (r *inMemoryQuoteRequestRepo) Load(_ context.Context, id uuid.UUID) (*domain.QuoteRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	qr, ok := r.stores[id]
	if !ok {
		return nil, domain.ErrQuoteNotFound
	}
	// Devolver clon para simular rehidratación
	cloned := domain.NewQuoteRequest()
	cloned.ID = qr.ID
	cloned.Version = qr.Version
	cloned.PrescriptionID = qr.PrescriptionID
	cloned.PetID = qr.PetID
	cloned.GuardianID = qr.GuardianID
	cloned.Country = qr.Country
	cloned.Items = qr.Items
	cloned.Mode = qr.Mode
	cloned.Area = qr.Area
	cloned.Status = qr.Status
	cloned.ExpiresAt = qr.ExpiresAt
	cloned.SelectableUntil = qr.SelectableUntil
	cloned.SelectedQuoteID = qr.SelectedQuoteID
	cloned.OrderID = qr.OrderID
	cloned.ClosedReason = qr.ClosedReason
	for k, v := range qr.Invited {
		cloned.Invited[k] = v
	}
	for k, v := range qr.Quotes {
		cloned.Quotes[k] = v
	}
	return cloned, nil
}

func (r *inMemoryQuoteRequestRepo) Save(_ context.Context, qr *domain.QuoteRequest) ([]eventstore.StoredEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := qr.UncommittedEvents()
	if len(events) == 0 {
		return nil, nil
	}

	expectedVersion := qr.Version - int64(len(events))
	existing, exists := r.stores[qr.ID]
	if exists && existing.Version != expectedVersion {
		return nil, &kernel.ConcurrencyError{Expected: expectedVersion, Actual: existing.Version}
	}

	r.stores[qr.ID] = qr
	qr.MarkCommitted()
	return nil, nil
}

// inMemoryOrderRepo simula almacenamiento de órdenes en memoria.
type inMemoryOrderRepo struct {
	mu     sync.Mutex
	stores map[uuid.UUID]*domain.Order
}

func newInMemoryOrderRepo() *inMemoryOrderRepo {
	return &inMemoryOrderRepo{stores: make(map[uuid.UUID]*domain.Order)}
}

func (r *inMemoryOrderRepo) Load(_ context.Context, id uuid.UUID) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.stores[id]
	if !ok {
		return nil, domain.ErrOrderNotCancellable
	}
	return order, nil
}

func (r *inMemoryOrderRepo) Save(_ context.Context, order *domain.Order) ([]eventstore.StoredEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[order.ID] = order
	order.MarkCommitted()
	return nil, nil
}

// mockGateway simula el PartnerGateway.
type mockGateway struct {
	mu             sync.Mutex
	sentRequests   []domain.QuoteRequestPayload
	notifiedOrders []domain.OrderPayload
}

func (m *mockGateway) SendQuoteRequest(_ context.Context, _ uuid.UUID, req domain.QuoteRequestPayload) (domain.SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentRequests = append(m.sentRequests, req)
	return domain.SendResult{Channel: "portal", SentAt: time.Now()}, nil
}

func (m *mockGateway) NotifyOrder(_ context.Context, _ uuid.UUID, order domain.OrderPayload) (domain.SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifiedOrders = append(m.notifiedOrders, order)
	return domain.SendResult{Channel: "portal", SentAt: time.Now()}, nil
}

func (m *mockGateway) CancelOrder(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) error {
	return nil
}

func (m *mockGateway) Capabilities(_ context.Context, _ uuid.UUID) (domain.PartnerCapabilities, error) {
	return domain.PartnerCapabilities{Pickup: true, Delivery: true, Substitutions: true}, nil
}

// mockPrescriptionReader simula lectura de recetas.
type mockPrescriptionReader struct {
	rx *domain.PrescriptionSnapshot
}

func (m *mockPrescriptionReader) GetPrescription(_ context.Context, _ uuid.UUID) (*domain.PrescriptionSnapshot, error) {
	return m.rx, nil
}

// mockPrescriptionDispenser verifica que se invoque la dispensación sobre la receta.
type mockPrescriptionDispenser struct {
	called bool
	rxID   uuid.UUID
	lines  []domain.DispensationLine
}

func (m *mockPrescriptionDispenser) RecordDispensation(_ context.Context, rxID uuid.UUID, _ uuid.UUID, _ uuid.UUID, lines []domain.DispensationLine) error {
	m.called = true
	m.rxID = rxID
	m.lines = lines
	return nil
}

// TestSaga_EndToEndFlow verifica el ciclo completo: cotización -> ofertas -> selección -> pedido -> entrega -> dispensación (QA-001).
func TestSaga_EndToEndFlow(t *testing.T) {
	ctx := context.Background()
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

	qrRepo := newInMemoryQuoteRequestRepo()
	orderRepo := newInMemoryOrderRepo()
	gw := &mockGateway{}
	dispenser := &mockPrescriptionDispenser{}

	svc := NewService(Options{
		UoW:                   inMemoryUoW{},
		QuoteRequests:         qrRepo,
		Orders:                orderRepo,
		Gateway:               gw,
		Prescriptions:         &mockPrescriptionReader{rx: rxSnapshot},
		PrescriptionDispenser: dispenser,
		PublicBaseURL:         "https://petpassport.test",
		Clock:                 func() time.Time { return now },
	})

	// 1. Tutor solicita cotización a farmacia
	qrID, err := svc.RequestQuotes(ctx, RequestQuotesCmd{
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

	// 2. Farmacia cotiza
	deliveryFee := 5000.0
	quoteID, err := svc.RecordQuote(ctx, RecordQuoteCmd{
		QuoteRequestID: qrID,
		PharmacyOrgID:  pharmacyOrgID,
		Lines: []domain.QuoteLine{
			{ItemID: "item-1", UnitPrice: 15000, Qty: 2, Available: domain.AvailabilityFull},
		},
		DeliveryFee: &deliveryFee,
		ValidUntil:  now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("record quote error: %v", err)
	}

	// 3. Tutor selecciona la cotización -> se genera el pedido y se cierra la solicitud
	orderID, err := svc.SelectQuote(ctx, SelectQuoteCmd{
		QuoteRequestID:  qrID,
		QuoteID:         quoteID,
		GuardianID:      guardianID,
		DeliveryAddress: &domain.Address{Street: "Calle 100", City: "Bogota", Country: "CO"},
		ContactPhone:    "+573001234567",
	})
	if err != nil {
		t.Fatalf("select quote error: %v", err)
	}

	qrAfterSelect, _ := qrRepo.Load(ctx, qrID)
	if qrAfterSelect.Status != domain.QuoteRequestStatusClosed {
		t.Fatalf("expected QR closed after order placement, got %s", qrAfterSelect.Status)
	}

	// 4. Ciclo de vida del pedido en farmacia
	if err := svc.ConfirmOrder(ctx, orderID, pharmacyOrgID, nil, nil); err != nil {
		t.Fatalf("confirm order: %v", err)
	}
	if err := svc.PrepareOrder(ctx, orderID, pharmacyOrgID); err != nil {
		t.Fatalf("prepare order: %v", err)
	}
	if err := svc.OutForDeliveryOrder(ctx, orderID, pharmacyOrgID, nil); err != nil {
		t.Fatalf("out for delivery: %v", err)
	}

	// 5. Entregar pedido y registrar dispensación
	err = svc.DeliverOrder(ctx, DeliverOrderCmd{
		OrderID:       orderID,
		PharmacyOrgID: pharmacyOrgID,
		Lines:         []domain.DispensationLine{{ItemID: "item-1", Qty: 2}},
	})
	if err != nil {
		t.Fatalf("deliver order error: %v", err)
	}

	finalOrder, _ := orderRepo.Load(ctx, orderID)
	if finalOrder.Status != domain.OrderStatusDelivered {
		t.Fatalf("expected status delivered, got %s", finalOrder.Status)
	}

	// Verificar propagación de dispensación a la receta (DOM-014, PRD-010)
	if !dispenser.called || dispenser.rxID != rxID || len(dispenser.lines) != 1 {
		t.Fatalf("expected dispensation port called on prescription with matching lines")
	}
}

// TestSaga_SelectQuoteConcurrency prueba que ante dos llamadas concurrentes a SelectQuote solo una tiene éxito (QA-001).
func TestSaga_SelectQuoteConcurrency(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	guardianID := uuid.New()
	rxID := uuid.New()
	pharmacyOrgID := uuid.New()

	rxSnapshot := &domain.PrescriptionSnapshot{
		ID:             rxID,
		GuardianID:     guardianID,
		Country:        "CO",
		Status:         "issued",
		Source:         "issued",
		ValidUntil:     now.Add(48 * time.Hour),
		RefillsAllowed: 1,
		Items: []domain.PrescriptionItemSnapshot{
			{ItemID: "item-1", Name: "Med 1", Quantity: 1, Unit: "pack"},
		},
	}

	qrRepo := newInMemoryQuoteRequestRepo()
	orderRepo := newInMemoryOrderRepo()
	svc := NewService(Options{
		UoW:           inMemoryUoW{},
		QuoteRequests: qrRepo,
		Orders:        orderRepo,
		Gateway:       &mockGateway{},
		Prescriptions: &mockPrescriptionReader{rx: rxSnapshot},
		Clock:         func() time.Time { return now },
	})

	qrID, _ := svc.RequestQuotes(ctx, RequestQuotesCmd{
		PrescriptionID:  rxID,
		GuardianID:      guardianID,
		Items:           []domain.RequestedItem{{ItemID: "item-1"}},
		Mode:            domain.ModePickup,
		Area:            domain.Area{City: "Bogota"},
		OrganizationIDs: []uuid.UUID{pharmacyOrgID},
		ExpiresInHours:  24,
	})

	quoteID, _ := svc.RecordQuote(ctx, RecordQuoteCmd{
		QuoteRequestID: qrID,
		PharmacyOrgID:  pharmacyOrgID,
		Lines:          []domain.QuoteLine{{ItemID: "item-1", UnitPrice: 10000, Qty: 1, Available: domain.AvailabilityFull}},
		ValidUntil:     now.Add(24 * time.Hour),
	})

	// Ejecutar dos SelectQuote concurrentes
	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)

	for i := 0; i < 2; i++ {
		idx := i
		go func() {
			defer wg.Done()
			_, err := svc.SelectQuote(ctx, SelectQuoteCmd{
				QuoteRequestID: qrID,
				QuoteID:        quoteID,
				GuardianID:     guardianID,
				ContactPhone:   "+573001234567",
			})
			results[idx] = err
		}()
	}

	wg.Wait()

	// Exactamente uno debe tener éxito y el otro debe fallar por concurrencia
	successCount := 0
	failureCount := 0
	for _, res := range results {
		if res == nil {
			successCount++
		} else {
			failureCount++
		}
	}

	if successCount != 1 || failureCount != 1 {
		t.Fatalf("expected exactly 1 success and 1 concurrency failure, got %d successes and %d failures (%v)", successCount, failureCount, results)
	}
}

// TestSaga_ReopenOnRejection verifica que si la farmacia rechaza el pedido, la saga reabre la solicitud (O7, DOM-014).
func TestSaga_ReopenOnRejection(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	guardianID := uuid.New()
	rxID := uuid.New()
	pharmacyOrgID := uuid.New()

	rxSnapshot := &domain.PrescriptionSnapshot{
		ID:             rxID,
		GuardianID:     guardianID,
		Country:        "CO",
		Status:         "issued",
		Source:         "issued",
		ValidUntil:     now.Add(48 * time.Hour),
		RefillsAllowed: 1,
		Items: []domain.PrescriptionItemSnapshot{
			{ItemID: "item-1", Name: "Med 1", Quantity: 1, Unit: "pack"},
		},
	}

	qrRepo := newInMemoryQuoteRequestRepo()
	orderRepo := newInMemoryOrderRepo()
	svc := NewService(Options{
		UoW:           inMemoryUoW{},
		QuoteRequests: qrRepo,
		Orders:        orderRepo,
		Gateway:       &mockGateway{},
		Prescriptions: &mockPrescriptionReader{rx: rxSnapshot},
		Clock:         func() time.Time { return now },
	})

	qrID, _ := svc.RequestQuotes(ctx, RequestQuotesCmd{
		PrescriptionID:  rxID,
		GuardianID:      guardianID,
		Items:           []domain.RequestedItem{{ItemID: "item-1"}},
		Mode:            domain.ModePickup,
		Area:            domain.Area{City: "Bogota"},
		OrganizationIDs: []uuid.UUID{pharmacyOrgID},
		ExpiresInHours:  24,
	})

	quoteID, _ := svc.RecordQuote(ctx, RecordQuoteCmd{
		QuoteRequestID: qrID,
		PharmacyOrgID:  pharmacyOrgID,
		Lines:          []domain.QuoteLine{{ItemID: "item-1", UnitPrice: 10000, Qty: 1, Available: domain.AvailabilityFull}},
		ValidUntil:     now.Add(24 * time.Hour),
	})

	orderID, _ := svc.SelectQuote(ctx, SelectQuoteCmd{
		QuoteRequestID: qrID,
		QuoteID:        quoteID,
		GuardianID:     guardianID,
		ContactPhone:   "+573001234567",
	})

	// Farmacia rechaza el pedido
	err := svc.RejectOrder(ctx, orderID, pharmacyOrgID, "Sin transporte hoy")
	if err != nil {
		t.Fatalf("reject order error: %v", err)
	}

	// Verificar que la solicitud de cotización se haya reabierto (DOM-014, O7)
	qr, _ := qrRepo.Load(ctx, qrID)
	if qr.Status != domain.QuoteRequestStatusOpen {
		t.Fatalf("expected QR reopened to open status, got %s", qr.Status)
	}
}
