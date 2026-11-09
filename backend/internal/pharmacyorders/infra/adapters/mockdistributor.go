package adapters

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
)

// MockDistributorAdapter implementa un adaptador saliente nivel 3 para un distribuidor mayorista (ARQ-003, ADR-021).
type MockDistributorAdapter struct {
	Endpoint string
}

var _ domain.PartnerGateway = (*MockDistributorAdapter)(nil)

// NewMockDistributorAdapter construye el adaptador para pruebas y homologación.
func NewMockDistributorAdapter(endpoint string) *MockDistributorAdapter {
	return &MockDistributorAdapter{Endpoint: endpoint}
}

// SendQuoteRequest traduce el modelo de dominio al formato propietario del distribuidor (ACL).
func (a *MockDistributorAdapter) SendQuoteRequest(ctx context.Context, orgID uuid.UUID, req domain.QuoteRequestPayload) (domain.SendResult, error) {
	return domain.SendResult{
		Channel: "adapter:mock-distributor",
		SentAt:  time.Now(),
	}, nil
}

// NotifyOrder envía la confirmación de pedido al sistema ERP del distribuidor.
func (a *MockDistributorAdapter) NotifyOrder(ctx context.Context, orgID uuid.UUID, order domain.OrderPayload) (domain.SendResult, error) {
	return domain.SendResult{
		Channel: "adapter:mock-distributor",
		SentAt:  time.Now(),
	}, nil
}

// CancelOrder emite la cancelación al API del distribuidor.
func (a *MockDistributorAdapter) CancelOrder(ctx context.Context, orgID uuid.UUID, orderID uuid.UUID, reason string) error {
	return nil
}

// Capabilities devuelve las capacidades del mayorista.
func (a *MockDistributorAdapter) Capabilities(ctx context.Context, orgID uuid.UUID) (domain.PartnerCapabilities, error) {
	return domain.PartnerCapabilities{
		Pickup:        false,
		Delivery:      true,
		Substitutions: true,
	}, nil
}
