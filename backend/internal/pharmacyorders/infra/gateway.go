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

// CompositePartnerGateway enruta las operaciones hacia el partner según su modo configurado:
// portal (nivel 1), api con webhooks (nivel 2) o adaptador saliente (nivel 3) (ARQ-003, ADR-021).
type CompositePartnerGateway struct {
	pool       *pgxpool.Pool
	portal     *PortalPartnerGateway
	dispatcher WebhookDispatcher
	adapters   map[string]domain.PartnerGateway
}

// WebhookDispatcher abstrae el despacho de eventos de webhooks para el gateway.
type WebhookDispatcher interface {
	Deliver(ctx context.Context, target WebhookTarget, event string, payload any) error
}

// WebhookTarget describe el destino de un webhook.
type WebhookTarget struct {
	OrganizationID uuid.UUID
	URL            string
	Secrets        [][]byte
}

var _ domain.PartnerGateway = (*CompositePartnerGateway)(nil)

// NewCompositePartnerGateway construye el despachador multicanal.
func NewCompositePartnerGateway(pool *pgxpool.Pool, dispatcher WebhookDispatcher) *CompositePartnerGateway {
	return &CompositePartnerGateway{
		pool:       pool,
		portal:     NewPortalPartnerGateway(pool),
		dispatcher: dispatcher,
		adapters:   make(map[string]domain.PartnerGateway),
	}
}

// RegisterAdapter registra un adaptador saliente nivel 3.
func (c *CompositePartnerGateway) RegisterAdapter(slug string, adapter domain.PartnerGateway) {
	c.adapters[slug] = adapter
}

func (c *CompositePartnerGateway) partnerMode(ctx context.Context, orgID uuid.UUID) (string, string, WebhookTarget, error) {
	var target WebhookTarget
	target.OrganizationID = orgID
	if c.pool == nil {
		return "portal", "", target, nil
	}

	var mode string
	var adapterSlug *string
	var webhookJSON []byte

	err := c.pool.QueryRow(ctx, `
		SELECT mode, adapter_slug, webhook
		FROM read_partnernetwork.partner_integrations
		WHERE organization_id = $1`, orgID).Scan(&mode, &adapterSlug, &webhookJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return "portal", "", target, nil
	}
	if err != nil {
		return "portal", "", target, err
	}

	slug := ""
	if adapterSlug != nil {
		slug = *adapterSlug
	}

	if len(webhookJSON) > 0 {
		var wh struct {
			URL    string `json:"url"`
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(webhookJSON, &wh); err == nil && wh.URL != "" {
			target.URL = wh.URL
			if wh.Secret != "" {
				target.Secrets = [][]byte{[]byte(wh.Secret)}
			}
		}
	}

	return mode, slug, target, nil
}

// SendQuoteRequest despacha la solicitud según el nivel de integración.
func (c *CompositePartnerGateway) SendQuoteRequest(ctx context.Context, orgID uuid.UUID, req domain.QuoteRequestPayload) (domain.SendResult, error) {
	mode, slug, target, err := c.partnerMode(ctx, orgID)
	if err != nil {
		return domain.SendResult{}, err
	}

	switch mode {
	case "adapter":
		if ad, ok := c.adapters[slug]; ok {
			return ad.SendQuoteRequest(ctx, orgID, req)
		}
		return c.portal.SendQuoteRequest(ctx, orgID, req)

	case "api":
		if c.dispatcher != nil && target.URL != "" {
			summary := map[string]any{
				"items_count": len(req.Items),
				"mode":        string(req.Mode),
				"area":        req.Area.City,
			}
			payload := map[string]any{
				"quote_request_id": req.QuoteRequestID,
				"expires_at":       req.ExpiresAt,
				"summary":          summary,
			}
			_ = c.dispatcher.Deliver(ctx, target, "quote_request.created", payload)
		}
		return domain.SendResult{
			Channel: "api",
			SentAt:  time.Now(),
		}, nil

	default: // "portal"
		return c.portal.SendQuoteRequest(ctx, orgID, req)
	}
}

// NotifyOrder notifica la adjudicación de pedido según el nivel de integración.
func (c *CompositePartnerGateway) NotifyOrder(ctx context.Context, orgID uuid.UUID, order domain.OrderPayload) (domain.SendResult, error) {
	mode, slug, target, err := c.partnerMode(ctx, orgID)
	if err != nil {
		return domain.SendResult{}, err
	}

	switch mode {
	case "adapter":
		if ad, ok := c.adapters[slug]; ok {
			return ad.NotifyOrder(ctx, orgID, order)
		}
		return c.portal.NotifyOrder(ctx, orgID, order)

	case "api":
		if c.dispatcher != nil && target.URL != "" {
			payload := map[string]any{
				"order_id": order.OrderID,
			}
			_ = c.dispatcher.Deliver(ctx, target, "order.placed", payload)
		}
		return domain.SendResult{
			Channel: "api",
			SentAt:  time.Now(),
		}, nil

	default:
		return c.portal.NotifyOrder(ctx, orgID, order)
	}
}

// CancelOrder notifica cancelación de pedido según el nivel de integración.
func (c *CompositePartnerGateway) CancelOrder(ctx context.Context, orgID uuid.UUID, orderID uuid.UUID, reason string) error {
	mode, slug, target, err := c.partnerMode(ctx, orgID)
	if err != nil {
		return err
	}

	switch mode {
	case "adapter":
		if ad, ok := c.adapters[slug]; ok {
			return ad.CancelOrder(ctx, orgID, orderID, reason)
		}
		return c.portal.CancelOrder(ctx, orgID, orderID, reason)

	case "api":
		if c.dispatcher != nil && target.URL != "" {
			payload := map[string]any{
				"order_id": orderID,
				"reason":   reason,
			}
			_ = c.dispatcher.Deliver(ctx, target, "order.cancelled", payload)
		}
		return nil

	default:
		return c.portal.CancelOrder(ctx, orgID, orderID, reason)
	}
}

// Capabilities delega la consulta de capacidades.
func (c *CompositePartnerGateway) Capabilities(ctx context.Context, orgID uuid.UUID) (domain.PartnerCapabilities, error) {
	return c.portal.Capabilities(ctx, orgID)
}
