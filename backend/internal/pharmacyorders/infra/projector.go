package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// PharmacyOrdersProjector mantiene de forma síncrona el schema read_pharmacyorders (PP-F4-DAT-002, ARQ-014).
// Es idempotente ante reproyecciones de eventos.
type PharmacyOrdersProjector struct {
	reg *eventstore.Registry
}

var _ projection.Projector = (*PharmacyOrdersProjector)(nil)

// NewPharmacyOrdersProjector construye el proyector de órdenes y cotizaciones.
func NewPharmacyOrdersProjector(reg *eventstore.Registry) *PharmacyOrdersProjector {
	return &PharmacyOrdersProjector{reg: reg}
}

func (*PharmacyOrdersProjector) Name() string { return "pharmacyorders.read_pharmacyorders" }

func (*PharmacyOrdersProjector) Handles() []string {
	return []string{
		"QuoteRequested",
		"QuoteInvitationSent",
		"QuoteInvitationFailed",
		"QuoteReceived",
		"QuoteDeclined",
		"QuoteSelected",
		"QuoteRequestCancelled",
		"QuoteRequestExpired",
		"QuoteRequestClosed",
		"QuoteRequestReopened",
		"OrderPlaced",
		"OrderConfirmed",
		"OrderRejected",
		"OrderPreparing",
		"OrderReady",
		"OrderOutForDelivery",
		"OrderDelivered",
		"OrderCancelled",
	}
}

func jsonBytes(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

func (p *PharmacyOrdersProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	decoded, err := p.reg.Decode(e)
	if err != nil {
		return fmt.Errorf("pharmacyorders projector decode: %w", err)
	}

	switch ev := decoded.(type) {
	case domain.QuoteRequested:
		itemsJSON, err := jsonBytes(ev.Items)
		if err != nil {
			return err
		}
		areaJSON, err := jsonBytes(ev.Area)
		if err != nil {
			return err
		}

		const upsertQR = `
INSERT INTO read_pharmacyorders.quote_requests (
  id, version, prescription_id, pet_id, guardian_id, country,
  items, mode, area, status, expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
ON CONFLICT (id) DO UPDATE SET
  version = EXCLUDED.version,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at`

		if _, err := tx.Exec(ctx, upsertQR,
			ev.QuoteRequestID, e.StreamVersion, ev.PrescriptionID, ev.PetID, ev.GuardianID, ev.Country,
			itemsJSON, string(ev.Mode), areaJSON, string(domain.QuoteRequestStatusOpen), ev.ExpiresAt, e.OccurredAt,
		); err != nil {
			return fmt.Errorf("project QuoteRequested qr: %w", err)
		}

		const insertInv = `
INSERT INTO read_pharmacyorders.invitations (
  quote_request_id, organization_id, status
) VALUES ($1, $2, $3)
ON CONFLICT (quote_request_id, organization_id) DO NOTHING`

		for _, orgID := range ev.InvitedOrgIDs {
			if _, err := tx.Exec(ctx, insertInv, ev.QuoteRequestID, orgID, string(domain.InvitationPending)); err != nil {
				return fmt.Errorf("project QuoteRequested inv: %w", err)
			}
		}

	case domain.QuoteInvitationSent:
		const updateInvSent = `
UPDATE read_pharmacyorders.invitations
   SET status = $3, channel = $4, sent_at = $5
 WHERE quote_request_id = $1 AND organization_id = $2`

		_, err := tx.Exec(ctx, updateInvSent,
			ev.QuoteRequestID, ev.OrganizationID, string(domain.InvitationSent), ev.Channel, ev.SentAt,
		)
		return err

	case domain.QuoteInvitationFailed:
		const updateInvFailed = `
UPDATE read_pharmacyorders.invitations
   SET status = $3, last_error = $4, attempts = attempts + 1
 WHERE quote_request_id = $1 AND organization_id = $2`

		_, err := tx.Exec(ctx, updateInvFailed,
			ev.QuoteRequestID, ev.OrganizationID, string(domain.InvitationFailed), ev.LastError,
		)
		return err

	case domain.QuoteReceived:
		linesJSON, err := jsonBytes(ev.Quote.Lines)
		if err != nil {
			return err
		}
		pickupJSON, err := jsonBytes(ev.Quote.Pickup)
		if err != nil {
			return err
		}

		// Marcar ofertas anteriores de esta farmacia como superseded
		const supersedeOld = `
UPDATE read_pharmacyorders.quotes
   SET superseded = true
 WHERE quote_request_id = $1 AND organization_id = $2`
		if _, err := tx.Exec(ctx, supersedeOld, ev.QuoteRequestID, ev.OrganizationID); err != nil {
			return err
		}

		const insertQuote = `
INSERT INTO read_pharmacyorders.quotes (
  id, quote_request_id, organization_id, lines, subtotal, delivery_fee,
  total, currency, eta, pickup, valid_until, notes, completeness, received_at, superseded
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, false)
ON CONFLICT (id) DO UPDATE SET
  lines = EXCLUDED.lines,
  total = EXCLUDED.total,
  superseded = EXCLUDED.superseded`

		if _, err := tx.Exec(ctx, insertQuote,
			ev.Quote.ID, ev.QuoteRequestID, ev.OrganizationID, linesJSON, ev.Quote.Subtotal, ev.Quote.DeliveryFee,
			ev.Quote.Total, ev.Quote.Currency, ev.Quote.ETA, pickupJSON, ev.Quote.ValidUntil, ev.Quote.Notes,
			string(ev.Quote.Completeness), ev.Quote.ReceivedAt,
		); err != nil {
			return fmt.Errorf("project QuoteReceived quote: %w", err)
		}

		const updateInvQuoted = `
UPDATE read_pharmacyorders.invitations
   SET status = $3, responded_at = $4
 WHERE quote_request_id = $1 AND organization_id = $2`

		if _, err := tx.Exec(ctx, updateInvQuoted,
			ev.QuoteRequestID, ev.OrganizationID, string(domain.InvitationQuoted), ev.Quote.ReceivedAt,
		); err != nil {
			return fmt.Errorf("project QuoteReceived inv: %w", err)
		}

		// Actualizar SLA por partner
		const upsertSLAQuoted = `
INSERT INTO read_pharmacyorders.partner_sla (organization_id, quoted, updated_at)
VALUES ($1, 1, $2)
ON CONFLICT (organization_id) DO UPDATE SET
  quoted = read_pharmacyorders.partner_sla.quoted + 1,
  updated_at = EXCLUDED.updated_at`
		_, _ = tx.Exec(ctx, upsertSLAQuoted, ev.OrganizationID, e.OccurredAt)

	case domain.QuoteDeclined:
		const updateInvDeclined = `
UPDATE read_pharmacyorders.invitations
   SET status = $3, last_error = $4, responded_at = $5
 WHERE quote_request_id = $1 AND organization_id = $2`

		if _, err := tx.Exec(ctx, updateInvDeclined,
			ev.QuoteRequestID, ev.OrganizationID, string(domain.InvitationDeclined), ev.Reason, ev.At,
		); err != nil {
			return fmt.Errorf("project QuoteDeclined inv: %w", err)
		}

		const upsertSLADeclined = `
INSERT INTO read_pharmacyorders.partner_sla (organization_id, declined, updated_at)
VALUES ($1, 1, $2)
ON CONFLICT (organization_id) DO UPDATE SET
  declined = read_pharmacyorders.partner_sla.declined + 1,
  updated_at = EXCLUDED.updated_at`
		_, _ = tx.Exec(ctx, upsertSLADeclined, ev.OrganizationID, e.OccurredAt)

	case domain.QuoteSelected:
		const updateQRSelected = `
UPDATE read_pharmacyorders.quote_requests
   SET version = $2, status = $3, selected_quote_id = $4, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateQRSelected,
			ev.QuoteRequestID, e.StreamVersion, string(domain.QuoteRequestStatusSelected), ev.QuoteID, e.OccurredAt,
		)
		return err

	case domain.QuoteRequestCancelled:
		const updateQRCancelled = `
UPDATE read_pharmacyorders.quote_requests
   SET version = $2, status = $3, closed_reason = $4, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateQRCancelled,
			ev.QuoteRequestID, e.StreamVersion, string(domain.QuoteRequestStatusClosed), ev.Reason, e.OccurredAt,
		)
		return err

	case domain.QuoteRequestExpired:
		const updateQRExpired = `
UPDATE read_pharmacyorders.quote_requests
   SET version = $2, selectable_until = $3, updated_at = $4
 WHERE id = $1`

		if _, err := tx.Exec(ctx, updateQRExpired,
			ev.QuoteRequestID, e.StreamVersion, ev.SelectableUntil, e.OccurredAt,
		); err != nil {
			return err
		}

		if ev.SelectableUntil == nil {
			const closeQR = `
UPDATE read_pharmacyorders.quote_requests
   SET status = 'closed', closed_reason = 'expired_without_quotes'
 WHERE id = $1`
			_, _ = tx.Exec(ctx, closeQR, ev.QuoteRequestID)
		}

		const expireInvs = `
UPDATE read_pharmacyorders.invitations
   SET status = 'expired'
 WHERE quote_request_id = $1 AND status IN ('pending', 'sent')`
		_, _ = tx.Exec(ctx, expireInvs, ev.QuoteRequestID)

	case domain.QuoteRequestClosed:
		const updateQRClosed = `
UPDATE read_pharmacyorders.quote_requests
   SET version = $2, status = $3, order_id = $4, closed_reason = $5, updated_at = $6
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateQRClosed,
			ev.QuoteRequestID, e.StreamVersion, string(domain.QuoteRequestStatusClosed), ev.OrderID, ev.Reason, e.OccurredAt,
		)
		return err

	case domain.QuoteRequestReopened:
		const updateQRReopen = `
UPDATE read_pharmacyorders.quote_requests
   SET version = $2, status = $3, selected_quote_id = NULL, order_id = NULL, updated_at = $4
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateQRReopen,
			ev.QuoteRequestID, e.StreamVersion, string(domain.QuoteRequestStatusOpen), e.OccurredAt,
		)
		return err

	case domain.OrderPlaced:
		linesJSON, err := jsonBytes(ev.Lines)
		if err != nil {
			return err
		}
		totalsJSON, err := jsonBytes(ev.Totals)
		if err != nil {
			return err
		}
		deliveryJSON, err := jsonBytes(ev.DeliveryAddress)
		if err != nil {
			return err
		}
		initTimeline := []domain.OrderTimelineEntry{
			{Status: domain.OrderStatusPlaced, At: ev.At},
		}
		timelineJSON, err := jsonBytes(initTimeline)
		if err != nil {
			return err
		}

		const insertOrder = `
INSERT INTO read_pharmacyorders.orders (
  id, version, quote_request_id, quote_id, prescription_id, pet_id, guardian_id,
  pharmacy_org_id, mode, delivery_address, contact_phone, lines, totals,
  status, timeline, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
ON CONFLICT (id) DO UPDATE SET
  version = EXCLUDED.version,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at`

		if _, err := tx.Exec(ctx, insertOrder,
			ev.OrderID, e.StreamVersion, ev.QuoteRequestID, ev.QuoteID, ev.PrescriptionID, ev.PetID, ev.GuardianID,
			ev.PharmacyOrgID, string(ev.Mode), deliveryJSON, ev.ContactPhone, linesJSON, totalsJSON,
			string(domain.OrderStatusPlaced), timelineJSON, e.OccurredAt,
		); err != nil {
			return fmt.Errorf("project OrderPlaced: %w", err)
		}

		const upsertSLAOrder = `
INSERT INTO read_pharmacyorders.partner_sla (organization_id, orders_placed, updated_at)
VALUES ($1, 1, $2)
ON CONFLICT (organization_id) DO UPDATE SET
  orders_placed = read_pharmacyorders.partner_sla.orders_placed + 1,
  updated_at = EXCLUDED.updated_at`
		_, _ = tx.Exec(ctx, upsertSLAOrder, ev.PharmacyOrgID, e.OccurredAt)

	case domain.OrderConfirmed:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusConfirmed, At: ev.At}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderConfirmed = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, partner_ref = COALESCE($4, partner_ref),
       timeline = timeline || $5::jsonb, updated_at = $6
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateOrderConfirmed,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusConfirmed), ev.PartnerRef, string(entryJSON), e.OccurredAt,
		)
		return err

	case domain.OrderRejected:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusRejected, At: ev.At, Reason: &ev.Reason}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderRejected = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, timeline = timeline || $4::jsonb, updated_at = $5
 WHERE id = $1`

		if _, err := tx.Exec(ctx, updateOrderRejected,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusRejected), string(entryJSON), e.OccurredAt,
		); err != nil {
			return err
		}

		const upsertSLARejected = `
UPDATE read_pharmacyorders.partner_sla
   SET orders_rejected = orders_rejected + 1, updated_at = $2
 WHERE organization_id = (SELECT pharmacy_org_id FROM read_pharmacyorders.orders WHERE id = $1)`
		_, _ = tx.Exec(ctx, upsertSLARejected, ev.OrderID, e.OccurredAt)

	case domain.OrderPreparing:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusPreparing, At: ev.At}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderPreparing = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, timeline = timeline || $4::jsonb, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateOrderPreparing,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusPreparing), string(entryJSON), e.OccurredAt,
		)
		return err

	case domain.OrderReady:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusReady, At: ev.At}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderReady = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, timeline = timeline || $4::jsonb, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateOrderReady,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusReady), string(entryJSON), e.OccurredAt,
		)
		return err

	case domain.OrderOutForDelivery:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusOutForDelivery, At: ev.At}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderOut = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, timeline = timeline || $4::jsonb, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateOrderOut,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusOutForDelivery), string(entryJSON), e.OccurredAt,
		)
		return err

	case domain.OrderDelivered:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusDelivered, At: ev.At}
		entryJSON, _ := jsonBytes(timelineEntry)
		dispJSON, err := jsonBytes(ev.Dispensation)
		if err != nil {
			return err
		}

		const updateOrderDelivered = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, dispensation = $4,
       timeline = timeline || $5::jsonb, updated_at = $6
 WHERE id = $1`

		if _, err := tx.Exec(ctx, updateOrderDelivered,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusDelivered), dispJSON, string(entryJSON), e.OccurredAt,
		); err != nil {
			return err
		}

		const upsertSLACompleted = `
UPDATE read_pharmacyorders.partner_sla
   SET orders_completed = orders_completed + 1, updated_at = $2
 WHERE organization_id = (SELECT pharmacy_org_id FROM read_pharmacyorders.orders WHERE id = $1)`
		_, _ = tx.Exec(ctx, upsertSLACompleted, ev.OrderID, e.OccurredAt)

	case domain.OrderCancelled:
		timelineEntry := domain.OrderTimelineEntry{Status: domain.OrderStatusCancelled, At: ev.At, Reason: &ev.Reason}
		entryJSON, _ := jsonBytes(timelineEntry)

		const updateOrderCancelled = `
UPDATE read_pharmacyorders.orders
   SET version = $2, status = $3, timeline = timeline || $4::jsonb, updated_at = $5
 WHERE id = $1`

		_, err := tx.Exec(ctx, updateOrderCancelled,
			ev.OrderID, e.StreamVersion, string(domain.OrderStatusCancelled), string(entryJSON), e.OccurredAt,
		)
		return err
	}

	return nil
}

// RegisterSyncProjectors registra las proyecciones síncronas de cotización y pedidos en el Runner (ARQ-014).
func RegisterSyncProjectors(runner *projection.Runner, reg *eventstore.Registry) {
	runner.Register(NewPharmacyOrdersProjector(reg))
}

// SyncProjector adapta el runner para ejecución síncrona dentro del Unit of Work.
type SyncProjector struct {
	runner *projection.Runner
}

// NewSyncProjector construye el adaptador del runner.
func NewSyncProjector(runner *projection.Runner) *SyncProjector {
	return &SyncProjector{runner: runner}
}

// Apply ejecuta las proyecciones registradas sobre la transacción actual.
func (a *SyncProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	return a.runner.Apply(ctx, tx, events)
}
