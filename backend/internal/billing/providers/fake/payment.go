package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// Scenario define el comportamiento simulado del proveedor de pagos en pruebas y CI.
type Scenario string

const (
	ScenarioSuccess  Scenario = "success"
	ScenarioFailure  Scenario = "failure"
	ScenarioPending  Scenario = "pending"
	ScenarioDeclined Scenario = "declined"
)

// FakePaymentProvider simula la pasarela de pagos (PP-F5-DOM-011, ARQ-002, QA-002).
type FakePaymentProvider struct {
	mu            sync.Mutex
	scenario      Scenario
	customers     map[uuid.UUID]string
	charges       map[uuid.UUID]string
	refunds       map[string]float64
	webhookEvents map[string]bool
}

// NewFakePaymentProvider construye un FakePaymentProvider en modo éxito por defecto.
func NewFakePaymentProvider() *FakePaymentProvider {
	return &FakePaymentProvider{
		scenario:      ScenarioSuccess,
		customers:     make(map[uuid.UUID]string),
		charges:       make(map[uuid.UUID]string),
		refunds:       make(map[string]float64),
		webhookEvents: make(map[string]bool),
	}
}

// SetScenario modifica el comportamiento simulado (éxito, fallo, pendiente, rechazado).
func (f *FakePaymentProvider) SetScenario(s Scenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenario = s
}

// CreateCustomer simula el registro del cliente en el proveedor.
func (f *FakePaymentProvider) CreateCustomer(ctx context.Context, accountID uuid.UUID, email, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	custID := fmt.Sprintf("cus_fake_%s", accountID.String()[:8])
	f.customers[accountID] = custID
	return custID, nil
}

// CreateSetupIntent simula la inicialización del widget/tokenización.
func (f *FakePaymentProvider) CreateSetupIntent(ctx context.Context, customerID string) (string, error) {
	return fmt.Sprintf("seti_fake_%s", uuid.New().String()[:8]), nil
}

// Charge ejecuta un intento de cobro idempotente por chargeID.
func (f *FakePaymentProvider) Charge(
	ctx context.Context,
	chargeID uuid.UUID,
	customerID string,
	amount float64,
	currency string,
	token string,
) (paymentRef string, status string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch f.scenario {
	case ScenarioFailure:
		return "", "failed", fmt.Errorf("PROVIDER_UNAVAILABLE")
	case ScenarioDeclined:
		return "", "failed", fmt.Errorf("CARD_DECLINED")
	case ScenarioPending:
		ref := fmt.Sprintf("pay_pend_%s", chargeID.String()[:8])
		f.charges[chargeID] = ref
		return ref, "pending", nil
	default:
		ref := fmt.Sprintf("pay_fake_%s", chargeID.String()[:8])
		f.charges[chargeID] = ref
		return ref, "paid", nil
	}
}

// Refund procesa un reembolso en el proveedor.
func (f *FakePaymentProvider) Refund(ctx context.Context, paymentRef string, amount float64, reason string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := fmt.Sprintf("ref_fake_%s", uuid.New().String()[:8])
	f.refunds[paymentRef] += amount
	return ref, nil
}

// ParseWebhook verifica la firma e interpreta el evento entrante.
func (f *FakePaymentProvider) ParseWebhook(payload []byte, signature string) (eventID string, eventType string, data any, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if signature == "invalid_sig" {
		return "", "", nil, fmt.Errorf("invalid webhook signature")
	}
	evtID := fmt.Sprintf("evt_fake_%s", uuid.New().String()[:8])
	f.webhookEvents[evtID] = true
	return evtID, "charge.paid", payload, nil
}

// GetPayment consulta el estado de un cobro en la pasarela.
func (f *FakePaymentProvider) GetPayment(ctx context.Context, providerRef string) (string, float64, error) {
	return "paid", 100000.0, nil
}
