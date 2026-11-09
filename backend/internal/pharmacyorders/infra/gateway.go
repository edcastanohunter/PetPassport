package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
)

// PortalPartnerGateway implementa domain.PartnerGateway para el nivel 1 (portal web sin API externa) (ARQ-003, ADR-021).
type PortalPartnerGateway struct {
	pool *pgxpool.Pool
}

var _ domain.PartnerGateway = (*PortalPartnerGateway)(nil)

// NewPortalPartnerGateway construye la implementación de gateway nivel portal.
func NewPortalPartnerGateway(pool *pgxpool.Pool) *PortalPartnerGateway {
	return &PortalPartnerGateway{pool: pool}
}

// SendQuoteRequest procesa la solicitud para el portal (la solicitud queda disponible en la bandeja y se genera notificación).
func (g *PortalPartnerGateway) SendQuoteRequest(ctx context.Context, orgID uuid.UUID, req domain.QuoteRequestPayload) (domain.SendResult, error) {
	// Nivel 1: no realiza llamadas externas HTTP; la proyección síncrona ya alimenta pharmacy_inbox
	return domain.SendResult{
		Channel: "portal",
		SentAt:  time.Now(),
	}, nil
}

// NotifyOrder notifica a la farmacia el nuevo pedido confirmado en el portal.
func (g *PortalPartnerGateway) NotifyOrder(ctx context.Context, orgID uuid.UUID, order domain.OrderPayload) (domain.SendResult, error) {
	// Nivel 1: no realiza llamadas salientes HTTP
	return domain.SendResult{
		Channel: "portal",
		SentAt:  time.Now(),
	}, nil
}

// CancelOrder notifica a la farmacia de la cancelación de un pedido.
func (g *PortalPartnerGateway) CancelOrder(ctx context.Context, orgID uuid.UUID, orderID uuid.UUID, reason string) error {
	// Nivel 1: la cancelación se refleja en el estado del pedido en base de datos
	return nil
}

// Capabilities consulta las capacidades operativas configuradas para la organización de farmacia.
func (g *PortalPartnerGateway) Capabilities(ctx context.Context, orgID uuid.UUID) (domain.PartnerCapabilities, error) {
	defaultCaps := domain.PartnerCapabilities{
		Pickup:        true,
		Delivery:      false,
		Substitutions: true,
	}

	if g.pool == nil {
		return defaultCaps, nil
	}

	var capsJSON []byte
	err := g.pool.QueryRow(ctx, `
		SELECT capabilities
		FROM read_partnernetwork.partner_integrations
		WHERE organization_id = $1`, orgID).Scan(&capsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultCaps, nil
	}
	if err != nil {
		return defaultCaps, err
	}

	var caps domain.PartnerCapabilities
	if err := json.Unmarshal(capsJSON, &caps); err != nil {
		return defaultCaps, nil
	}
	return caps, nil
}
