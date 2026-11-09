package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/api/genpartner"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/egress"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/webhooks"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PartnerHandlers implementa la especificación OpenAPI Partner API v1 (PP-F4-PTN-001, PTN-003, ARQ-005).
// Garantiza aislamiento estricto por partner (un partner solo accede a sus solicitudes, cotizaciones y pedidos).
type PartnerHandlers struct {
	pool       *pgxpool.Pool
	svc        *app.Service
	dispatcher *webhooks.Dispatcher
	logger     *slog.Logger
}

var _ genpartner.StrictServerInterface = (*PartnerHandlers)(nil)

// NewPartnerHandlers inicializa los handlers para la Partner API.
func NewPartnerHandlers(pool *pgxpool.Pool, svc *app.Service, dispatcher *webhooks.Dispatcher, logger *slog.Logger) *PartnerHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &PartnerHandlers{
		pool:       pool,
		svc:        svc,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

func (h *PartnerHandlers) principal(ctx context.Context) (auth.PartnerPrincipal, error) {
	p, ok := auth.PartnerPrincipalFrom(ctx)
	if !ok || p.OrgID == uuid.Nil {
		return auth.PartnerPrincipal{}, &kernel.ForbiddenError{Reason: "autenticación de partner requerida"}
	}
	return p, nil
}

// PartnerListQuoteRequests lista solicitudes donde el partner está invitado (PTN-003, ARQ-005).
func (h *PartnerHandlers) PartnerListQuoteRequests(ctx context.Context, req genpartner.PartnerListQuoteRequestsRequestObject) (genpartner.PartnerListQuoteRequestsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	limit := 20
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}

	var rows pgx.Rows
	if h.pool == nil {
		return genpartner.PartnerListQuoteRequests200JSONResponse{
			Data: []genpartner.PartnerQuoteRequestSummary{},
		}, nil
	}

	query := `
		SELECT qr.id, qr.status, qr.mode, qr.items_count, qr.expires_at, qr.created_at
		FROM read_pharmacyorders.quote_requests qr
		INNER JOIN read_pharmacyorders.quote_request_invitations inv ON inv.quote_request_id = qr.id
		WHERE inv.organization_id = $1`
	args := []any{p.OrgID}

	if req.Params.Status != nil && *req.Params.Status != "" {
		args = append(args, string(*req.Params.Status))
		query += fmt.Sprintf(" AND qr.status = $%d", len(args))
	}
	query += fmt.Sprintf(" ORDER BY qr.created_at DESC LIMIT %d", limit)

	rows, err = h.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("consultar solicitudes: %w", err)
	}
	defer rows.Close()

	items := make([]genpartner.PartnerQuoteRequestSummary, 0)
	for rows.Next() {
		var it genpartner.PartnerQuoteRequestSummary
		var modeStr string
		var statusStr string
		if err := rows.Scan(&it.Id, &statusStr, &modeStr, &it.ItemsCount, &it.ExpiresAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Mode = genpartner.PartnerQuoteRequestSummaryMode(modeStr)
		it.Status = genpartner.PartnerQuoteRequestSummaryStatus(statusStr)
		items = append(items, it)
	}

	return genpartner.PartnerListQuoteRequests200JSONResponse{
		Data: items,
	}, nil
}

// PartnerGetQuoteRequest obtiene el detalle minimizado de una solicitud de cotización (ARQ-013 etapa 1).
func (h *PartnerHandlers) PartnerGetQuoteRequest(ctx context.Context, req genpartner.PartnerGetQuoteRequestRequestObject) (genpartner.PartnerGetQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool == nil {
		return nil, &kernel.NotFoundError{Resource: "quote_request"}
	}

	var detail genpartner.PartnerQuoteRequestDetail
	var modeStr, statusStr string
	var areaJSON, petJSON []byte

	err = h.pool.QueryRow(ctx, `
		SELECT qr.id, qr.status, qr.mode, qr.area, qr.pet, qr.expires_at, qr.created_at
		FROM read_pharmacyorders.quote_requests qr
		INNER JOIN read_pharmacyorders.quote_request_invitations inv ON inv.quote_request_id = qr.id
		WHERE qr.id = $1 AND inv.organization_id = $2`, req.Id, p.OrgID).Scan(
		&detail.Id, &statusStr, &modeStr, &areaJSON, &petJSON, &detail.ExpiresAt, &detail.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "quote_request"}
	}
	if err != nil {
		return nil, err
	}

	detail.Status = genpartner.PartnerQuoteRequestDetailStatus(statusStr)
	detail.Mode = genpartner.PartnerQuoteRequestDetailMode(modeStr)

	// Items minimizados
	rows, err := h.pool.Query(ctx, `
		SELECT id, catalog_id, name, dose, route, frequency, quantity, substitution_allowed, instructions
		FROM read_pharmacyorders.quote_request_items
		WHERE quote_request_id = $1`, req.Id)
	if err == nil {
		defer rows.Close()
		items := make([]genpartner.PartnerQuoteItem, 0)
		for rows.Next() {
			var it genpartner.PartnerQuoteItem
			var catID *uuid.UUID
			var doseJSON, qtyJSON []byte
			var route, freq, inst *string
			if err := rows.Scan(&it.ItemId, &catID, &it.Name, &doseJSON, &route, &freq, &qtyJSON, &it.SubstitutionAllowed, &inst); err == nil {
				if catID != nil {
					it.CatalogId = catID
				}
				if route != nil {
					it.Route = route
				}
				if freq != nil {
					it.Frequency = freq
				}
				if inst != nil {
					it.Instructions = inst
				}
				items = append(items, it)
			}
		}
		detail.Items = items
	}

	return genpartner.PartnerGetQuoteRequest200JSONResponse(detail), nil
}

// PartnerSubmitQuote envía una oferta de cotización (PTN-003).
func (h *PartnerHandlers) PartnerSubmitQuote(ctx context.Context, req genpartner.PartnerSubmitQuoteRequestObject) (genpartner.PartnerSubmitQuoteResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || len(req.Body.Lines) == 0 {
		return nil, validationErr("lines", "REQUIRED")
	}

	lines := make([]domain.QuoteLine, 0, len(req.Body.Lines))
	for _, l := range req.Body.Lines {
		var sub *domain.QuoteSubstitution
		if l.Substitution != nil {
			var reason string
			if l.Substitution.Reason != nil {
				reason = *l.Substitution.Reason
			}
			var subPrice float64
			if l.Substitution.UnitPrice != nil {
				subPrice = float64(*l.Substitution.UnitPrice)
			}
			var catID uuid.UUID
			if l.Substitution.CatalogId != nil {
				catID = *l.Substitution.CatalogId
			}
			var subName string
			if l.Substitution.Name != nil {
				subName = *l.Substitution.Name
			}
			sub = &domain.QuoteSubstitution{
				CatalogID: catID,
				Name:      subName,
				UnitPrice: subPrice,
				Reason:    reason,
			}
		}

		lines = append(lines, domain.QuoteLine{
			ItemID:       l.ItemId.String(),
			UnitPrice:    float64(l.UnitPrice),
			Qty:          float64(l.Qty),
			Available:    domain.Availability(l.Available),
			Substitution: sub,
		})
	}

	var fee *float64
	if req.Body.DeliveryFee != nil {
		f := float64(*req.Body.DeliveryFee)
		fee = &f
	}

	var pickup *domain.PickupRef
	if req.Body.PickupAddressRef != nil && *req.Body.PickupAddressRef != "" {
		pickup = &domain.PickupRef{
			Address: *req.Body.PickupAddressRef,
		}
	}

	validUntil := req.Body.ValidUntil
	if validUntil.IsZero() {
		validUntil = time.Now().Add(24 * time.Hour)
	}

	quoteID, err := h.svc.RecordQuote(ctx, app.RecordQuoteCmd{
		QuoteRequestID: req.Id,
		PharmacyOrgID:  p.OrgID,
		Lines:          lines,
		DeliveryFee:    fee,
		ETA:            req.Body.Eta,
		Pickup:         pickup,
		ValidUntil:     validUntil,
		Notes:          req.Body.Notes,
	})
	if err != nil {
		return nil, err
	}

	return genpartner.PartnerSubmitQuote201JSONResponse{
		QuoteId: quoteID,
		Status:  "submitted",
	}, nil
}

// PartnerDeclineQuoteRequest declina la solicitud de cotización (PTN-003).
func (h *PartnerHandlers) PartnerDeclineQuoteRequest(ctx context.Context, req genpartner.PartnerDeclineQuoteRequestRequestObject) (genpartner.PartnerDeclineQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	reason := "declined"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.DeclineQuote(ctx, req.Id, p.OrgID, reason); err != nil {
		return nil, err
	}

	return genpartner.PartnerDeclineQuoteRequest200JSONResponse{
		Success: true,
	}, nil
}

// PartnerListOrders lista pedidos adjudicados al partner (PTN-003).
func (h *PartnerHandlers) PartnerListOrders(ctx context.Context, req genpartner.PartnerListOrdersRequestObject) (genpartner.PartnerListOrdersResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	limit := 20
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}

	if h.pool == nil {
		return genpartner.PartnerListOrders200JSONResponse{Data: []genpartner.PartnerOrderSummary{}}, nil
	}

	query := `
		SELECT id, quote_request_id, status, mode, total_cents, partner_ref, created_at
		FROM read_pharmacyorders.orders
		WHERE pharmacy_org_id = $1`
	args := []any{p.OrgID}

	if req.Params.Status != nil && *req.Params.Status != "" {
		args = append(args, string(*req.Params.Status))
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]genpartner.PartnerOrderSummary, 0)
	for rows.Next() {
		var it genpartner.PartnerOrderSummary
		var modeStr, statusStr string
		var totalCents int64
		var partnerRef *string
		if err := rows.Scan(&it.Id, &it.QuoteRequestId, &statusStr, &modeStr, &totalCents, &partnerRef, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Status = genpartner.PartnerOrderSummaryStatus(statusStr)
		it.Mode = genpartner.PartnerOrderSummaryMode(modeStr)
		it.Total = float32(totalCents) / 100
		if partnerRef != nil {
			it.PartnerRef = partnerRef
		}
		items = append(items, it)
	}

	return genpartner.PartnerListOrders200JSONResponse{Data: items}, nil
}

// PartnerGetOrder obtiene el detalle de un pedido adjudicado con datos de tutor (ARQ-013 etapa 2).
func (h *PartnerHandlers) PartnerGetOrder(ctx context.Context, req genpartner.PartnerGetOrderRequestObject) (genpartner.PartnerGetOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool == nil {
		return nil, &kernel.NotFoundError{Resource: "order"}
	}

	var detail genpartner.PartnerOrderDetail
	var modeStr, statusStr string
	var totalCents, subtotalCents, delFeeCents int64
	var guardianName, guardianPhone string
	var deliveryAddress *string
	var partnerRef, eta, notes *string

	err = h.pool.QueryRow(ctx, `
		SELECT id, quote_request_id, status, mode, total_cents, subtotal_cents, delivery_fee_cents,
		       guardian_name, guardian_phone, delivery_address, partner_ref, eta, notes, created_at, updated_at
		FROM read_pharmacyorders.orders
		WHERE id = $1 AND pharmacy_org_id = $2`, req.Id, p.OrgID).Scan(
		&detail.Id, &detail.QuoteRequestId, &statusStr, &modeStr, &totalCents, &subtotalCents, &delFeeCents,
		&guardianName, &guardianPhone, &deliveryAddress, &partnerRef, &eta, &notes, &detail.CreatedAt, &detail.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "order"}
	}
	if err != nil {
		return nil, err
	}

	detail.Status = genpartner.PartnerOrderDetailStatus(statusStr)
	detail.Mode = genpartner.PartnerOrderDetailMode(modeStr)
	detail.Total = float32(totalCents) / 100
	st := float32(subtotalCents) / 100
	detail.Subtotal = &st
	df := float32(delFeeCents) / 100
	detail.DeliveryFee = &df
	detail.Guardian = struct {
		DeliveryAddress *string `json:"delivery_address,omitempty"`
		Name            string  `json:"name"`
		Phone           string  `json:"phone"`
	}{
		Name:            guardianName,
		Phone:           guardianPhone,
		DeliveryAddress: deliveryAddress,
	}
	if partnerRef != nil {
		detail.PartnerRef = partnerRef
	}
	if eta != nil {
		detail.Eta = eta
	}
	if notes != nil {
		detail.Notes = notes
	}

	return genpartner.PartnerGetOrder200JSONResponse(detail), nil
}

// PartnerUpdateOrderStatus actualiza el estado del pedido y gestiona la dispensación (PTN-003, O1–O8).
func (h *PartnerHandlers) PartnerUpdateOrderStatus(ctx context.Context, req genpartner.PartnerUpdateOrderStatusRequestObject) (genpartner.PartnerUpdateOrderStatusResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if req.Body == nil {
		return nil, validationErr("body", "REQUIRED")
	}

	switch req.Body.Status {
	case genpartner.PartnerUpdateOrderStatusPayloadStatusConfirmed:
		if err := h.svc.ConfirmOrder(ctx, req.Id, p.OrgID, req.Body.PartnerRef, req.Body.Eta); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusPreparing:
		if err := h.svc.PrepareOrder(ctx, req.Id, p.OrgID); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusReady:
		if err := h.svc.ReadyOrder(ctx, req.Id, p.OrgID); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusOutForDelivery:
		if err := h.svc.OutForDeliveryOrder(ctx, req.Id, p.OrgID, req.Body.Eta); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusDelivered:
		// Registrar dispensación si se incluyen líneas
		var lines []domain.DispensationLine
		if req.Body.Dispensation != nil && req.Body.Dispensation.Lines != nil {
			for _, dl := range *req.Body.Dispensation.Lines {
				lines = append(lines, domain.DispensationLine{
					ItemID: dl.ItemId.String(),
					Qty:    float64(dl.Qty),
				})
			}
		}
		if err := h.svc.DeliverOrder(ctx, app.DeliverOrderCmd{
			OrderID:       req.Id,
			PharmacyOrgID: p.OrgID,
			Lines:         lines,
		}); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusCancelled:
		reason := "cancelado por farmacia"
		if req.Body.Reason != nil && *req.Body.Reason != "" {
			reason = *req.Body.Reason
		}
		if err := h.svc.CancelOrder(ctx, req.Id, "pharmacy", reason); err != nil {
			return nil, err
		}
	case genpartner.PartnerUpdateOrderStatusPayloadStatusRejected:
		reason := "rechazado por farmacia"
		if req.Body.Reason != nil && *req.Body.Reason != "" {
			reason = *req.Body.Reason
		}
		if err := h.svc.RejectOrder(ctx, req.Id, p.OrgID, reason); err != nil {
			return nil, err
		}
	default:
		return nil, domain.ErrInvalidTransition
	}

	// Consultar y devolver el detalle actualizado
	getResp, err := h.PartnerGetOrder(ctx, genpartner.PartnerGetOrderRequestObject{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if okResp, ok := getResp.(genpartner.PartnerGetOrder200JSONResponse); ok {
		return genpartner.PartnerUpdateOrderStatus200JSONResponse(okResp), nil
	}
	return nil, errors.New("error al recuperar estado del pedido")
}

// PartnerListMedicationsCatalog lista medicamentos del catálogo PetPassport (PTN-003).
func (h *PartnerHandlers) PartnerListMedicationsCatalog(ctx context.Context, req genpartner.PartnerListMedicationsCatalogRequestObject) (genpartner.PartnerListMedicationsCatalogResponseObject, error) {
	_, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	limit := 50
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}

	if h.pool == nil {
		return genpartner.PartnerListMedicationsCatalog200JSONResponse{Data: []genpartner.PartnerCatalogMedicationItem{}}, nil
	}

	query := `SELECT id, name, active_ingredient, concentration, pharmaceutical_form, species, controlled FROM ref.medications`
	args := []any{}
	if req.Params.Q != nil && *req.Params.Q != "" {
		args = append(args, "%"+*req.Params.Q+"%")
		query += " WHERE name ILIKE $1 OR active_ingredient ILIKE $1"
	}
	query += fmt.Sprintf(" ORDER BY name ASC LIMIT %d", limit)

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]genpartner.PartnerCatalogMedicationItem, 0)
	for rows.Next() {
		var it genpartner.PartnerCatalogMedicationItem
		var act, conc, form *string
		var species []string
		if err := rows.Scan(&it.Id, &it.Name, &act, &conc, &form, &species, &it.Controlled); err != nil {
			return nil, err
		}
		if act != nil {
			it.ActiveIngredient = act
		}
		if conc != nil {
			it.Concentration = conc
		}
		if form != nil {
			it.PharmaceuticalForm = form
		}
		it.Species = species
		items = append(items, it)
	}

	return genpartner.PartnerListMedicationsCatalog200JSONResponse{Data: items}, nil
}

// PartnerGetCatalogMedication obtiene un medicamento por su Catalog ID.
func (h *PartnerHandlers) PartnerGetCatalogMedication(ctx context.Context, req genpartner.PartnerGetCatalogMedicationRequestObject) (genpartner.PartnerGetCatalogMedicationResponseObject, error) {
	_, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool == nil {
		return nil, &kernel.NotFoundError{Resource: "medication"}
	}

	var it genpartner.PartnerCatalogMedicationItem
	var act, conc, form *string
	var species []string

	err = h.pool.QueryRow(ctx, `
		SELECT id, name, active_ingredient, concentration, pharmaceutical_form, species, controlled
		FROM ref.medications WHERE id = $1`, req.CatalogId).Scan(
		&it.Id, &it.Name, &act, &conc, &form, &species, &it.Controlled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "medication"}
	}
	if err != nil {
		return nil, err
	}

	if act != nil {
		it.ActiveIngredient = act
	}
	if conc != nil {
		it.Concentration = conc
	}
	if form != nil {
		it.PharmaceuticalForm = form
	}
	it.Species = species

	return genpartner.PartnerGetCatalogMedication200JSONResponse(it), nil
}

// PartnerListProductMappings consulta los mapeos de catálogo del partner (PTN-003).
func (h *PartnerHandlers) PartnerListProductMappings(ctx context.Context, req genpartner.PartnerListProductMappingsRequestObject) (genpartner.PartnerListProductMappingsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	limit := 50
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}

	if h.pool == nil {
		return genpartner.PartnerListProductMappings200JSONResponse{Data: []genpartner.PartnerProductMappingItem{}}, nil
	}

	rows, err := h.pool.Query(ctx, `
		SELECT catalog_id, partner_sku, partner_name, base_price, active, updated_at
		FROM read_partnernetwork.product_mappings
		WHERE organization_id = $1
		ORDER BY updated_at DESC LIMIT $2`, p.OrgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]genpartner.PartnerProductMappingItem, 0)
	for rows.Next() {
		var it genpartner.PartnerProductMappingItem
		var name *string
		var basePrice *float64
		var upAt time.Time
		if err := rows.Scan(&it.CatalogId, &it.Sku, &name, &basePrice, &it.Active, &upAt); err != nil {
			return nil, err
		}
		if name != nil {
			it.Name = name
		}
		if basePrice != nil {
			bp := float32(*basePrice)
			it.BasePrice = &bp
		}
		it.UpdatedAt = &upAt
		items = append(items, it)
	}

	return genpartner.PartnerListProductMappings200JSONResponse{Data: items}, nil
}

// PartnerUpsertProductMapping guarda un mapeo de SKU (PTN-003).
func (h *PartnerHandlers) PartnerUpsertProductMapping(ctx context.Context, req genpartner.PartnerUpsertProductMappingRequestObject) (genpartner.PartnerUpsertProductMappingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || req.Body.Sku == "" {
		return nil, validationErr("sku", "REQUIRED")
	}

	active := true
	if req.Body.Active != nil {
		active = *req.Body.Active
	}

	now := time.Now()
	var basePrice *float64
	if req.Body.BasePrice != nil {
		bp := float64(*req.Body.BasePrice)
		basePrice = &bp
	}

	if h.pool != nil {
		_, err = h.pool.Exec(ctx, `
			INSERT INTO read_partnernetwork.product_mappings (
				organization_id, catalog_id, partner_sku, partner_name, base_price, active, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (organization_id, catalog_id) DO UPDATE
			SET partner_sku = EXCLUDED.partner_sku,
			    partner_name = EXCLUDED.partner_name,
			    base_price = EXCLUDED.base_price,
			    active = EXCLUDED.active,
			    updated_at = EXCLUDED.updated_at`,
			p.OrgID, req.CatalogId, req.Body.Sku, req.Body.Name, basePrice, active, now,
		)
		if err != nil {
			return nil, err
		}
	}

	resp := genpartner.PartnerProductMappingItem{
		CatalogId: req.CatalogId,
		Sku:       req.Body.Sku,
		Name:      req.Body.Name,
		BasePrice: req.Body.BasePrice,
		Active:    active,
		UpdatedAt: &now,
	}

	return genpartner.PartnerUpsertProductMapping200JSONResponse(resp), nil
}

// PartnerDeleteProductMapping elimina un mapeo de SKU.
func (h *PartnerHandlers) PartnerDeleteProductMapping(ctx context.Context, req genpartner.PartnerDeleteProductMappingRequestObject) (genpartner.PartnerDeleteProductMappingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool != nil {
		_, err = h.pool.Exec(ctx, `
			DELETE FROM read_partnernetwork.product_mappings
			WHERE organization_id = $1 AND catalog_id = $2`, p.OrgID, req.CatalogId)
		if err != nil {
			return nil, err
		}
	}

	return genpartner.PartnerDeleteProductMapping204Response{}, nil
}

// PartnerBulkProductMappings permite carga masiva de equivalencias SKU (PTN-003).
func (h *PartnerHandlers) PartnerBulkProductMappings(ctx context.Context, req genpartner.PartnerBulkProductMappingsRequestObject) (genpartner.PartnerBulkProductMappingsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || len(req.Body.Mappings) == 0 {
		return nil, validationErr("mappings", "REQUIRED")
	}

	processed := len(req.Body.Mappings)
	now := time.Now()

	if h.pool != nil {
		for _, m := range req.Body.Mappings {
			var bp *float64
			if m.BasePrice != nil {
				b := float64(*m.BasePrice)
				bp = &b
			}
			active := true
			if m.Active != nil {
				active = *m.Active
			}
			_, _ = h.pool.Exec(ctx, `
				INSERT INTO read_partnernetwork.product_mappings (
					organization_id, catalog_id, partner_sku, partner_name, base_price, active, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (organization_id, catalog_id) DO UPDATE
				SET partner_sku = EXCLUDED.partner_sku,
				    partner_name = EXCLUDED.partner_name,
				    base_price = EXCLUDED.base_price,
				    active = EXCLUDED.active,
				    updated_at = EXCLUDED.updated_at`,
				p.OrgID, m.CatalogId, m.Sku, m.Name, bp, active, now,
			)
		}
	}

	return genpartner.PartnerBulkProductMappings200JSONResponse{
		Processed: processed,
		Created:   processed,
		Updated:   0,
	}, nil
}

// PartnerVerifyPrescription verifica autenticidad de una receta médica para el partner (PTN-003).
func (h *PartnerHandlers) PartnerVerifyPrescription(ctx context.Context, req genpartner.PartnerVerifyPrescriptionRequestObject) (genpartner.PartnerVerifyPrescriptionResponseObject, error) {
	_, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool == nil {
		return genpartner.PartnerVerifyPrescription200JSONResponse{
			Valid:  true,
			Status: "issued",
		}, nil
	}

	var status, hash string
	var issuedAt, validUntil time.Time
	err = h.pool.QueryRow(ctx, `
		SELECT status, document_hash, issued_at, valid_until
		FROM read_prescriptions.prescriptions
		WHERE id = $1`, req.Id).Scan(&status, &hash, &issuedAt, &validUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "prescription"}
	}
	if err != nil {
		return nil, err
	}

	valid := status == "issued" || status == "confirmed"
	if req.Params.H != nil && *req.Params.H != "" && *req.Params.H != hash {
		valid = false
	}

	return genpartner.PartnerVerifyPrescription200JSONResponse{
		Valid:      valid,
		Status:     status,
		IssuedAt:   &issuedAt,
		ValidUntil: &validUntil,
	}, nil
}

// PartnerGetMe devuelve perfil y límites del partner (PTN-003, ARQ-005).
func (h *PartnerHandlers) PartnerGetMe(ctx context.Context, req genpartner.PartnerGetMeRequestObject) (genpartner.PartnerGetMeResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	displayName := "Farmacia Partner"
	mode := "api"
	webhookConfigured := false
	var webhookURL *string
	webhookEvents := []string{}

	if h.pool != nil {
		var name string
		var m string
		var whJSON []byte
		err := h.pool.QueryRow(ctx, `
			SELECT o.display_name, i.mode, i.webhook
			FROM read_partnernetwork.organizations o
			INNER JOIN read_partnernetwork.partner_integrations i ON i.organization_id = o.id
			WHERE o.id = $1`, p.OrgID).Scan(&name, &m, &whJSON)
		if err == nil {
			displayName = name
			mode = m
			if len(whJSON) > 0 {
				var wh struct {
					URL    string   `json:"url"`
					Events []string `json:"events"`
				}
				if json.Unmarshal(whJSON, &wh) == nil && wh.URL != "" {
					webhookConfigured = true
					webhookURL = &wh.URL
					webhookEvents = wh.Events
				}
			}
		}
	}

	readLim := 600
	writeLim := 120

	return genpartner.PartnerGetMe200JSONResponse{
		OrganizationId: p.OrgID,
		DisplayName:    displayName,
		Mode:           genpartner.PartnerProfileResponseMode(mode),
		Capabilities: struct {
			Delivery      *bool `json:"delivery,omitempty"`
			Pickup        *bool `json:"pickup,omitempty"`
			Substitutions *bool `json:"substitutions,omitempty"`
		}{
			Pickup:        boolPtr(true),
			Delivery:      boolPtr(true),
			Substitutions: boolPtr(true),
		},
		Webhook: struct {
			Configured *bool     `json:"configured,omitempty"`
			Events     *[]string `json:"events,omitempty"`
			Url        *string   `json:"url,omitempty"`
		}{
			Configured: &webhookConfigured,
			Url:        webhookURL,
			Events:     &webhookEvents,
		},
		RateLimits: struct {
			ReadPerMinute  *int `json:"read_per_minute,omitempty"`
			WritePerMinute *int `json:"write_per_minute,omitempty"`
		}{
			ReadPerMinute:  &readLim,
			WritePerMinute: &writeLim,
		},
	}, nil
}

// PartnerConfigureWebhook configura endpoint y secretos de webhook (PTN-004, SEC-003).
func (h *PartnerHandlers) PartnerConfigureWebhook(ctx context.Context, req genpartner.PartnerConfigureWebhookRequestObject) (genpartner.PartnerConfigureWebhookResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || req.Body.Url == "" {
		return nil, validationErr("url", "REQUIRED")
	}

	// Validación estricta anti-SSRF (ARQ-006, SEC-004)
	if err := egress.ValidateWebhookURL(req.Body.Url); err != nil {
		return nil, validationErr("url", err.Error())
	}

	secret := ""
	if req.Body.Secret != nil && *req.Body.Secret != "" {
		secret = *req.Body.Secret
	} else {
		// Generar secreto criptográfico seguro de 32 bytes (64 hex chars)
		raw := make([]byte, 32)
		_, _ = rand.Read(raw)
		secret = "whsec_" + hex.EncodeToString(raw)
	}

	if h.pool != nil {
		whObj := map[string]any{
			"url":    req.Body.Url,
			"events": req.Body.Events,
			"secret": secret,
			"active": true,
		}
		whBytes, _ := json.Marshal(whObj)
		_, err = h.pool.Exec(ctx, `
			UPDATE read_partnernetwork.partner_integrations
			SET webhook = $1, updated_at = now()
			WHERE organization_id = $2`, whBytes, p.OrgID)
		if err != nil {
			return nil, err
		}
	}

	return genpartner.PartnerConfigureWebhook200JSONResponse{
		Url:    req.Body.Url,
		Events: req.Body.Events,
		Secret: &secret,
	}, nil
}

// PartnerTestWebhook envía un evento ping de prueba al webhook configurado (PTN-004, ARQ-004).
func (h *PartnerHandlers) PartnerTestWebhook(ctx context.Context, req genpartner.PartnerTestWebhookRequestObject) (genpartner.PartnerTestWebhookResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	if h.pool == nil || h.dispatcher == nil {
		return nil, kernel.NewDomainError("WEBHOOK_UNAVAILABLE", "despachador de webhooks no disponible")
	}

	var whJSON []byte
	err = h.pool.QueryRow(ctx, `
		SELECT webhook
		FROM read_partnernetwork.partner_integrations
		WHERE organization_id = $1`, p.OrgID).Scan(&whJSON)
	if err != nil || len(whJSON) == 0 {
		return nil, validationErr("webhook", "webhook no configurado para este partner")
	}

	var wh struct {
		URL    string `json:"url"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(whJSON, &wh); err != nil || wh.URL == "" {
		return nil, validationErr("webhook", "configuración de webhook inválida")
	}

	target := webhooks.DeliveryTarget{
		OrganizationID: p.OrgID,
		URL:            wh.URL,
		Secrets:        [][]byte{[]byte(wh.Secret)},
	}

	payload := webhooks.PingPayload{
		Message:   "Ping de prueba PetPassport Partner API",
		Timestamp: time.Now(),
	}

	result, err := h.dispatcher.Deliver(ctx, target, webhooks.EventPing, payload)
	if err != nil {
		return nil, err
	}

	status := "queued"
	if result.Success {
		status = "sent"
	}

	return genpartner.PartnerTestWebhook200JSONResponse{
		DeliveryId: result.DeliveryID,
		Status:     status,
		StatusCode: &result.StatusCode,
		Error:      &result.Error,
	}, nil
}

func boolPtr(b bool) *bool { return &b }

func validationErr(field, code string) *kernel.ValidationError {
	ve := &kernel.ValidationError{}
	ve.Add(field, code)
	return ve
}
