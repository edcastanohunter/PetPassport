package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/webhooks"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// QueriesProvider define el puerto de consultas para los handlers de farmacia.
type QueriesProvider interface {
	GetQuoteRequest(ctx context.Context, id uuid.UUID) (*infra.QuoteRequestDetailView, error)
	ListGuardianQuoteRequests(ctx context.Context, guardianID uuid.UUID, status string, limit, offset int) ([]infra.QuoteRequestDetailView, error)
	GetOrder(ctx context.Context, id uuid.UUID) (*infra.OrderDetailView, error)
	ListGuardianOrders(ctx context.Context, guardianID uuid.UUID, status string, limit, offset int) ([]infra.OrderDetailView, error)
	ListPharmacyInbox(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]infra.PharmacyInboxItem, error)
	ListPharmacyOrders(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]infra.OrderDetailView, error)
	GetPrescriptionQuoteOptions(ctx context.Context, prescriptionID uuid.UUID) (*infra.QuoteOptionsView, error)
	GetIntegrationSettings(ctx context.Context, orgID uuid.UUID) (*infra.PharmacyIntegrationSettingsView, error)
	UpdateIntegrationSettings(ctx context.Context, orgID uuid.UUID, level string, webhookURL *string, catalogMapping bool, deliveryAreas []infra.DeliveryAreaView) error
	ListProductMappings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]infra.ProductMappingItemView, int, error)
	UpsertProductMapping(ctx context.Context, orgID, catalogID uuid.UUID, sku, name string, priceCents int, inStock bool) error
	DeleteProductMapping(ctx context.Context, orgID, catalogID uuid.UUID) error
}

// PharmacyHandlers implementa los endpoints de cotizaciones y pedidos de farmacia (API-005, API-006, API-007, API-008).
type PharmacyHandlers struct {
	svc        *app.Service
	queries    QueriesProvider
	logger     *slog.Logger
	pool       *pgxpool.Pool
	dispatcher *webhooks.Dispatcher
}

// NewPharmacyHandlers crea una nueva instancia de PharmacyHandlers.
func NewPharmacyHandlers(svc *app.Service, queries QueriesProvider, logger *slog.Logger) *PharmacyHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &PharmacyHandlers{
		svc:     svc,
		queries: queries,
		logger:  logger,
	}
}

func (h *PharmacyHandlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "unauthenticated"}
	}
	return p, nil
}

// -----------------------------------------------------------------------------
// Tutor Quote Requests & Options (API-005)
// -----------------------------------------------------------------------------

// GetPrescriptionQuoteOptions obtiene opciones para cotizar receta.
func (h *PharmacyHandlers) GetPrescriptionQuoteOptions(ctx context.Context, req gen.GetPrescriptionQuoteOptionsRequestObject) (gen.GetPrescriptionQuoteOptionsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	opts, err := h.queries.GetPrescriptionQuoteOptions(ctx, req.PrescriptionId)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		return nil, &kernel.NotFoundError{Resource: "Prescription " + req.PrescriptionId.String()}
	}

	if d := authz.Can(p, authz.QuoteRead, authz.QuoteRequestResource{GuardianID: opts.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	items := make([]struct {
		Dose               string    `json:"dose"`
		IsControlled       bool      `json:"is_controlled"`
		MedicationName     string    `json:"medication_name"`
		PrescriptionItemId uuid.UUID `json:"prescription_item_id"`
		Quantity           float32   `json:"quantity"`
		Unit               string    `json:"unit"`
	}, 0, len(opts.Items))

	for _, it := range opts.Items {
		items = append(items, struct {
			Dose               string    `json:"dose"`
			IsControlled       bool      `json:"is_controlled"`
			MedicationName     string    `json:"medication_name"`
			PrescriptionItemId uuid.UUID `json:"prescription_item_id"`
			Quantity           float32   `json:"quantity"`
			Unit               string    `json:"unit"`
		}{
			Dose:               it.Dose,
			IsControlled:       it.IsControlled,
			MedicationName:     it.MedicationName,
			PrescriptionItemId: it.PrescriptionItemID,
			Quantity:           float32(it.Quantity),
			Unit:               it.Unit,
		})
	}

	pharmacies := make([]struct {
		Active                  bool      `json:"active"`
		Address                 string    `json:"address"`
		AverageQuoteTimeSeconds int       `json:"average_quote_time_seconds"`
		FulfillmentTypes        []string  `json:"fulfillment_types"`
		Name                    string    `json:"name"`
		OnTimeDeliveryRate      float32   `json:"on_time_delivery_rate"`
		PharmacyId              uuid.UUID `json:"pharmacy_id"`
		Rating                  float32   `json:"rating"`
	}, 0, len(opts.Pharmacies))

	for _, ph := range opts.Pharmacies {
		pharmacies = append(pharmacies, struct {
			Active                  bool      `json:"active"`
			Address                 string    `json:"address"`
			AverageQuoteTimeSeconds int       `json:"average_quote_time_seconds"`
			FulfillmentTypes        []string  `json:"fulfillment_types"`
			Name                    string    `json:"name"`
			OnTimeDeliveryRate      float32   `json:"on_time_delivery_rate"`
			PharmacyId              uuid.UUID `json:"pharmacy_id"`
			Rating                  float32   `json:"rating"`
		}{
			Active:                  ph.Active,
			Address:                 ph.Address,
			AverageQuoteTimeSeconds: ph.AverageQuoteTimeSeconds,
			FulfillmentTypes:        ph.FulfillmentTypes,
			Name:                    ph.Name,
			OnTimeDeliveryRate:      float32(ph.OnTimeDeliveryRate),
			PharmacyId:              ph.PharmacyID,
			Rating:                  float32(ph.Rating),
		})
	}

	return gen.GetPrescriptionQuoteOptions200JSONResponse{
		PrescriptionId: req.PrescriptionId,
		Items:          items,
		Pharmacies:     pharmacies,
	}, nil
}

// CreateQuoteRequest crea una solicitud de cotización.
func (h *PharmacyHandlers) CreateQuoteRequest(ctx context.Context, req gen.CreateQuoteRequestRequestObject) (gen.CreateQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	opts, err := h.queries.GetPrescriptionQuoteOptions(ctx, b.PrescriptionId)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		return nil, &kernel.NotFoundError{Resource: "Prescription " + b.PrescriptionId.String()}
	}

	if d := authz.Can(p, authz.QuoteRequest, authz.QuoteRequestResource{GuardianID: opts.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	var reqItems []domain.RequestedItem
	for _, it := range b.Items {
		reqItems = append(reqItems, domain.RequestedItem{
			ItemID:              it.PrescriptionItemId.String(),
			CatalogID:           it.CatalogItemId,
			Quantity:            float64(it.Quantity),
			Unit:                it.Unit,
			SubstitutionAllowed: it.SubstitutionAllowed,
		})
	}

	var area domain.Area
	if b.DeliveryAddress != nil {
		area = domain.Area{
			City: valOrEmpty(b.DeliveryAddress.City),
		}
	}

	var orgIDs []uuid.UUID
	if b.PharmacyIds != nil {
		orgIDs = *b.PharmacyIds
	}

	cmd := app.RequestQuotesCmd{
		PrescriptionID:  b.PrescriptionId,
		GuardianID:      p.UserID,
		Items:           reqItems,
		Mode:            domain.FulfillmentMode(b.FulfillmentType),
		Area:            area,
		OrganizationIDs: orgIDs,
		ExpiresInHours:  24,
	}

	qrID, err := h.svc.RequestQuotes(ctx, cmd)
	if err != nil {
		return nil, err
	}

	detail, err := h.queries.GetQuoteRequest(ctx, qrID)
	if err != nil || detail == nil {
		return gen.CreateQuoteRequest201JSONResponse{
			Id:              qrID,
			PrescriptionId:  b.PrescriptionId,
			GuardianId:      &p.UserID,
			Status:          "open",
			FulfillmentType: string(b.FulfillmentType),
			CreatedAt:       time.Now(),
			ExpiresAt:       time.Now().Add(24 * time.Hour),
		}, nil
	}

	return gen.CreateQuoteRequest201JSONResponse(h.detailViewToGen(detail)), nil
}

// ListQuoteRequests lista solicitudes del tutor.
func (h *PharmacyHandlers) ListQuoteRequests(ctx context.Context, req gen.ListQuoteRequestsRequestObject) (gen.ListQuoteRequestsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.QuoteRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = *req.Params.Status
	}
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	items, err := h.queries.ListGuardianQuoteRequests(ctx, p.UserID, status, limit, offset)
	if err != nil {
		return nil, err
	}

	summaries := make([]gen.QuoteRequestSummary, 0, len(items))
	for _, it := range items {
		summaries = append(summaries, gen.QuoteRequestSummary{
			Id:              it.ID,
			PrescriptionId:  it.PrescriptionID,
			Status:          string(it.Status),
			FulfillmentType: string(it.Mode),
			ItemsCount:      len(it.Items),
			QuotesCount:     len(it.Quotes),
			CreatedAt:       it.CreatedAt,
			ExpiresAt:       it.ExpiresAt,
		})
	}

	return gen.ListQuoteRequests200JSONResponse{
		Items: summaries,
		Total: len(summaries),
	}, nil
}

// GetQuoteRequest consulta el detalle de una solicitud.
func (h *PharmacyHandlers) GetQuoteRequest(ctx context.Context, req gen.GetQuoteRequestRequestObject) (gen.GetQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detail, err := h.queries.GetQuoteRequest(ctx, req.QuoteRequestId)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, &kernel.NotFoundError{Resource: "QuoteRequest " + req.QuoteRequestId.String()}
	}

	if d := authz.Can(p, authz.QuoteRead, authz.QuoteRequestResource{ID: detail.ID, GuardianID: detail.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.GetQuoteRequest200JSONResponse(h.detailViewToGen(detail)), nil
}

// SelectQuote selecciona una cotización y genera un pedido.
func (h *PharmacyHandlers) SelectQuote(ctx context.Context, req gen.SelectQuoteRequestObject) (gen.SelectQuoteResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	detail, err := h.queries.GetQuoteRequest(ctx, req.QuoteRequestId)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, &kernel.NotFoundError{Resource: "QuoteRequest " + req.QuoteRequestId.String()}
	}

	if d := authz.Can(p, authz.QuoteRequest, authz.QuoteRequestResource{ID: detail.ID, GuardianID: detail.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	var subs []domain.SubstitutionDecision
	if b.AcceptedSubstitutions != nil {
		for _, subID := range *b.AcceptedSubstitutions {
			subs = append(subs, domain.SubstitutionDecision{
				ItemID: subID.String(),
				Accept: true,
			})
		}
	}

	cmd := app.SelectQuoteCmd{
		QuoteRequestID:        req.QuoteRequestId,
		QuoteID:               b.QuoteId,
		GuardianID:            p.UserID,
		SubstitutionDecisions: subs,
	}

	orderID, err := h.svc.SelectQuote(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return gen.SelectQuote200JSONResponse{
		QuoteRequestId: req.QuoteRequestId,
		OrderId:        orderID,
		Status:         "placed",
	}, nil
}

// CancelQuoteRequest cancela una solicitud de cotización.
func (h *PharmacyHandlers) CancelQuoteRequest(ctx context.Context, req gen.CancelQuoteRequestRequestObject) (gen.CancelQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detail, err := h.queries.GetQuoteRequest(ctx, req.QuoteRequestId)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, &kernel.NotFoundError{Resource: "QuoteRequest " + req.QuoteRequestId.String()}
	}

	if d := authz.Can(p, authz.QuoteRequest, authz.QuoteRequestResource{ID: detail.ID, GuardianID: detail.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := "user_cancelled"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.CancelQuoteRequest(ctx, req.QuoteRequestId, p.UserID, reason); err != nil {
		return nil, err
	}

	updated, err := h.queries.GetQuoteRequest(ctx, req.QuoteRequestId)
	if err != nil || updated == nil {
		return gen.CancelQuoteRequest200JSONResponse(h.detailViewToGen(detail)), nil
	}

	return gen.CancelQuoteRequest200JSONResponse(h.detailViewToGen(updated)), nil
}

// StreamQuoteRequestEvents provee el stream SSE de eventos para la solicitud de cotización.
func (h *PharmacyHandlers) StreamQuoteRequestEvents(ctx context.Context, req gen.StreamQuoteRequestEventsRequestObject) (gen.StreamQuoteRequestEventsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	detail, err := h.queries.GetQuoteRequest(ctx, req.QuoteRequestId)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, &kernel.NotFoundError{Resource: "QuoteRequest " + req.QuoteRequestId.String()}
	}

	if d := authz.Can(p, authz.QuoteRead, authz.QuoteRequestResource{ID: detail.ID, GuardianID: detail.GuardianID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = fmt.Fprintf(pw, "event: ping\ndata: {\"status\":\"connected\",\"quote_request_id\":\"%s\"}\n\n", req.QuoteRequestId)
		<-ctx.Done()
	}()

	return gen.StreamQuoteRequestEvents200TexteventStreamResponse{
		Body: pr,
	}, nil
}

// -----------------------------------------------------------------------------
// Tutor Orders (API-006)
// -----------------------------------------------------------------------------

// ListOrders lista pedidos del tutor.
func (h *PharmacyHandlers) ListOrders(ctx context.Context, req gen.ListOrdersRequestObject) (gen.ListOrdersResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.OrderRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = *req.Params.Status
	}
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	orders, err := h.queries.ListGuardianOrders(ctx, p.UserID, status, limit, offset)
	if err != nil {
		return nil, err
	}

	summaries := make([]gen.OrderSummary, 0, len(orders))
	for _, o := range orders {
		modeStr := string(o.Mode)
		summaries = append(summaries, gen.OrderSummary{
			Id:              o.ID,
			QuoteRequestId:  o.QuoteRequestID,
			PrescriptionId:  o.PrescriptionID,
			PharmacyId:      o.PharmacyOrgID,
			PharmacyName:    o.PharmacyName,
			Status:          string(o.Status),
			FulfillmentType: &modeStr,
			TotalCents:      int(o.Totals.Total * 100),
			CreatedAt:       o.CreatedAt,
		})
	}

	return gen.ListOrders200JSONResponse{
		Items: summaries,
		Total: len(summaries),
	}, nil
}

// GetOrder consulta detalle de pedido para el tutor.
func (h *PharmacyHandlers) GetOrder(ctx context.Context, req gen.GetOrderRequestObject) (gen.GetOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, &kernel.NotFoundError{Resource: "Order " + req.OrderId.String()}
	}

	if d := authz.Can(p, authz.OrderRead, authz.OrderResource{ID: order.ID, GuardianID: order.GuardianID, PharmacyOrgID: order.PharmacyOrgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.GetOrder200JSONResponse(h.orderDetailViewToGen(order)), nil
}

// CancelOrder cancela pedido por el tutor.
func (h *PharmacyHandlers) CancelOrder(ctx context.Context, req gen.CancelOrderRequestObject) (gen.CancelOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, &kernel.NotFoundError{Resource: "Order " + req.OrderId.String()}
	}

	if d := authz.Can(p, authz.OrderCancel, authz.OrderResource{ID: order.ID, GuardianID: order.GuardianID, PharmacyOrgID: order.PharmacyOrgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := "tutor_cancellation"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.CancelOrder(ctx, req.OrderId, "tutor", reason); err != nil {
		return nil, err
	}

	updated, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || updated == nil {
		return gen.CancelOrder200JSONResponse(h.orderDetailViewToGen(order)), nil
	}

	return gen.CancelOrder200JSONResponse(h.orderDetailViewToGen(updated)), nil
}

// StreamOrderEvents provee stream SSE de eventos de pedido.
func (h *PharmacyHandlers) StreamOrderEvents(ctx context.Context, req gen.StreamOrderEventsRequestObject) (gen.StreamOrderEventsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, &kernel.NotFoundError{Resource: "Order " + req.OrderId.String()}
	}

	if d := authz.Can(p, authz.OrderRead, authz.OrderResource{ID: order.ID, GuardianID: order.GuardianID, PharmacyOrgID: order.PharmacyOrgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = fmt.Fprintf(pw, "event: ping\ndata: {\"status\":\"connected\",\"order_id\":\"%s\"}\n\n", req.OrderId)
		<-ctx.Done()
	}()

	return gen.StreamOrderEvents200TexteventStreamResponse{
		Body: pr,
	}, nil
}

// -----------------------------------------------------------------------------
// Pharmacy Portal (API-007)
// -----------------------------------------------------------------------------

// GetPharmacyInbox obtiene solicitudes de cotización para la farmacia.
func (h *PharmacyHandlers) GetPharmacyInbox(ctx context.Context, req gen.GetPharmacyInboxRequestObject) (gen.GetPharmacyInboxResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := "pending"
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	inbox, err := h.queries.ListPharmacyInbox(ctx, req.OrgId, status, limit, offset)
	if err != nil {
		return nil, err
	}

	items := make([]gen.PharmacyInboxItem, 0, len(inbox))
	for _, it := range inbox {
		delCity := it.Area.City
		items = append(items, gen.PharmacyInboxItem{
			QuoteRequestId:  it.QuoteRequestID,
			PrescriptionId:  it.Prescription.ID,
			PetName:         "Patient",
			PetSpecies:      it.Pet.Species,
			Status:          string(it.InvitationStatus),
			FulfillmentType: string(it.Mode),
			DeliveryCity:    &delCity,
			ItemsCount:      len(it.Items),
			InvitedAt:       it.ReceivedAt,
			ExpiresAt:       it.ExpiresAt,
		})
	}

	return gen.GetPharmacyInbox200JSONResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// SubmitPharmacyQuote envía una cotización.
func (h *PharmacyHandlers) SubmitPharmacyQuote(ctx context.Context, req gen.SubmitPharmacyQuoteRequestObject) (gen.SubmitPharmacyQuoteResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyQuote, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	lines := make([]domain.QuoteLine, 0, len(b.Lines))
	for _, l := range b.Lines {
		var sub *domain.QuoteSubstitution
		avail := domain.AvailabilityFull
		if l.Substitution && l.SubstitutedItemId != nil {
			avail = domain.AvailabilityPartial
			sub = &domain.QuoteSubstitution{
				CatalogID: *l.SubstitutedItemId,
				Name:      l.ProductName,
				UnitPrice: float64(l.UnitPriceCents) / 100.0,
				Reason:    "equivalent_substitution",
			}
		}
		lines = append(lines, domain.QuoteLine{
			ItemID:       l.ItemId.String(),
			UnitPrice:    float64(l.UnitPriceCents) / 100.0,
			Qty:          float64(l.Quantity),
			Available:    avail,
			Substitution: sub,
		})
	}

	var fee *float64
	if b.DeliveryFeeCents != 0 {
		f := float64(b.DeliveryFeeCents) / 100.0
		fee = &f
	}

	var etaStr *string
	if b.EstimatedReadyAt != nil {
		s := b.EstimatedReadyAt.Format(time.RFC3339)
		etaStr = &s
	}

	cmd := app.RecordQuoteCmd{
		QuoteRequestID: req.QuoteRequestId,
		PharmacyOrgID:  req.OrgId,
		Lines:          lines,
		DeliveryFee:    fee,
		ETA:            etaStr,
		ValidUntil:     time.Now().Add(24 * time.Hour),
	}

	quoteID, err := h.svc.RecordQuote(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return gen.SubmitPharmacyQuote201JSONResponse{
		QuoteId:        quoteID,
		QuoteRequestId: req.QuoteRequestId,
		CreatedAt:      time.Now(),
	}, nil
}

// DeclinePharmacyQuoteRequest rechaza cotizar una solicitud.
func (h *PharmacyHandlers) DeclinePharmacyQuoteRequest(ctx context.Context, req gen.DeclinePharmacyQuoteRequestRequestObject) (gen.DeclinePharmacyQuoteRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyQuote, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := "out_of_stock"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.DeclineQuote(ctx, req.QuoteRequestId, req.OrgId, reason); err != nil {
		return nil, err
	}

	return gen.DeclinePharmacyQuoteRequest200JSONResponse{
		Success: true,
	}, nil
}

// ValidatePharmacyPrescription valida una receta externa por el farmacéutico.
func (h *PharmacyHandlers) ValidatePharmacyPrescription(ctx context.Context, req gen.ValidatePharmacyPrescriptionRequestObject) (gen.ValidatePharmacyPrescriptionResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyValidate, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	return gen.ValidatePharmacyPrescription200JSONResponse{
		Success: true,
	}, nil
}

// ListPharmacyOrders lista pedidos de una farmacia.
func (h *PharmacyHandlers) ListPharmacyOrders(ctx context.Context, req gen.ListPharmacyOrdersRequestObject) (gen.ListPharmacyOrdersResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = *req.Params.Status
	}
	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	orders, err := h.queries.ListPharmacyOrders(ctx, req.OrgId, status, limit, offset)
	if err != nil {
		return nil, err
	}

	items := make([]gen.PharmacyOrderDetail, 0, len(orders))
	for _, o := range orders {
		items = append(items, h.orderDetailViewToPharmacyGen(&o))
	}

	return gen.ListPharmacyOrders200JSONResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// GetPharmacyOrder consulta detalle de pedido para la farmacia.
func (h *PharmacyHandlers) GetPharmacyOrder(ctx context.Context, req gen.GetPharmacyOrderRequestObject) (gen.GetPharmacyOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	o, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, &kernel.NotFoundError{Resource: "Order " + req.OrderId.String()}
	}

	return gen.GetPharmacyOrder200JSONResponse(h.orderDetailViewToPharmacyGen(o)), nil
}

// ConfirmPharmacyOrder confirma aceptación de pedido por la farmacia.
func (h *PharmacyHandlers) ConfirmPharmacyOrder(ctx context.Context, req gen.ConfirmPharmacyOrderRequestObject) (gen.ConfirmPharmacyOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	var etaStr *string
	if req.Body != nil && req.Body.EstimatedReadyAt != nil {
		s := req.Body.EstimatedReadyAt.Format(time.RFC3339)
		etaStr = &s
	}

	if err := h.svc.ConfirmOrder(ctx, req.OrderId, req.OrgId, nil, etaStr); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.ConfirmPharmacyOrder200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "confirmed",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.ConfirmPharmacyOrder200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// RejectPharmacyOrder rechaza pedido por la farmacia y reabre la saga de cotización.
func (h *PharmacyHandlers) RejectPharmacyOrder(ctx context.Context, req gen.RejectPharmacyOrderRequestObject) (gen.RejectPharmacyOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := "pharmacy_rejected"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.RejectOrder(ctx, req.OrderId, req.OrgId, reason); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.RejectPharmacyOrder200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "rejected",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.RejectPharmacyOrder200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// MarkPharmacyOrderPreparing marca el pedido en preparación.
func (h *PharmacyHandlers) MarkPharmacyOrderPreparing(ctx context.Context, req gen.MarkPharmacyOrderPreparingRequestObject) (gen.MarkPharmacyOrderPreparingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if err := h.svc.PrepareOrder(ctx, req.OrderId, req.OrgId); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.MarkPharmacyOrderPreparing200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "preparing",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.MarkPharmacyOrderPreparing200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// MarkPharmacyOrderReady marca el pedido listo para entrega o despacho.
func (h *PharmacyHandlers) MarkPharmacyOrderReady(ctx context.Context, req gen.MarkPharmacyOrderReadyRequestObject) (gen.MarkPharmacyOrderReadyResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if err := h.svc.ReadyOrder(ctx, req.OrderId, req.OrgId); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.MarkPharmacyOrderReady200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "ready",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.MarkPharmacyOrderReady200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// MarkPharmacyOrderOutForDelivery marca el pedido en camino.
func (h *PharmacyHandlers) MarkPharmacyOrderOutForDelivery(ctx context.Context, req gen.MarkPharmacyOrderOutForDeliveryRequestObject) (gen.MarkPharmacyOrderOutForDeliveryResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	var eta *string
	if err := h.svc.OutForDeliveryOrder(ctx, req.OrderId, req.OrgId, eta); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.MarkPharmacyOrderOutForDelivery200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "out_for_delivery",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.MarkPharmacyOrderOutForDelivery200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// DeliverPharmacyOrder entrega el pedido y propaga la dispensación hacia la receta.
func (h *PharmacyHandlers) DeliverPharmacyOrder(ctx context.Context, req gen.DeliverPharmacyOrderRequestObject) (gen.DeliverPharmacyOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	var lines []domain.DispensationLine
	for _, dr := range b.DispensationRecords {
		lines = append(lines, domain.DispensationLine{
			ItemID: dr.PrescriptionItemId.String(),
			Qty:    float64(dr.QuantityDispensed),
		})
	}

	cmd := app.DeliverOrderCmd{
		OrderID:       req.OrderId,
		PharmacyOrgID: req.OrgId,
		Lines:         lines,
	}

	if err := h.svc.DeliverOrder(ctx, cmd); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.DeliverPharmacyOrder200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "delivered",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.DeliverPharmacyOrder200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// CancelPharmacyOrder cancela pedido por parte de la farmacia.
func (h *PharmacyHandlers) CancelPharmacyOrder(ctx context.Context, req gen.CancelPharmacyOrderRequestObject) (gen.CancelPharmacyOrderResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyOrders, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := "pharmacy_cancelled"
	if req.Body != nil && req.Body.Reason != "" {
		reason = req.Body.Reason
	}

	if err := h.svc.CancelOrder(ctx, req.OrderId, "pharmacy", reason); err != nil {
		return nil, err
	}

	order, err := h.queries.GetOrder(ctx, req.OrderId)
	if err != nil || order == nil {
		return gen.CancelPharmacyOrder200JSONResponse{
			Id:              req.OrderId,
			QuoteRequestId:  uuid.Nil,
			PrescriptionId:  uuid.Nil,
			Status:          "cancelled",
			FulfillmentType: "delivery",
			TotalCents:      0,
			CreatedAt:       time.Now(),
		}, nil
	}

	return gen.CancelPharmacyOrder200JSONResponse(h.orderDetailViewToPharmacyGen(order)), nil
}

// GetPharmacyIntegration consulta configuración de integración.
func (h *PharmacyHandlers) GetPharmacyIntegration(ctx context.Context, req gen.GetPharmacyIntegrationRequestObject) (gen.GetPharmacyIntegrationResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyAdmin, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	settings, err := h.queries.GetIntegrationSettings(ctx, req.OrgId)
	if err != nil {
		return nil, err
	}

	areas := make([]struct {
		City        string   `json:"city"`
		PostalCodes []string `json:"postal_codes"`
	}, 0, len(settings.DeliveryAreas))

	for _, a := range settings.DeliveryAreas {
		areas = append(areas, struct {
			City        string   `json:"city"`
			PostalCodes []string `json:"postal_codes"`
		}{
			City:        a.City,
			PostalCodes: a.PostalCodes,
		})
	}

	return gen.GetPharmacyIntegration200JSONResponse{
		Level:                 gen.PharmacyIntegrationSettingsLevel(settings.Level),
		WebhookUrl:            settings.WebhookURL,
		CatalogMappingEnabled: settings.CatalogMappingEnabled,
		DeliveryAreas:         areas,
	}, nil
}

// UpdatePharmacyIntegration actualiza configuración de integración.
func (h *PharmacyHandlers) UpdatePharmacyIntegration(ctx context.Context, req gen.UpdatePharmacyIntegrationRequestObject) (gen.UpdatePharmacyIntegrationResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyAdmin, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	var areas []infra.DeliveryAreaView
	for _, a := range b.DeliveryAreas {
		areas = append(areas, infra.DeliveryAreaView{
			City:        a.City,
			PostalCodes: a.PostalCodes,
		})
	}

	if err := h.queries.UpdateIntegrationSettings(ctx, req.OrgId, string(b.Level), b.WebhookUrl, b.CatalogMappingEnabled, areas); err != nil {
		return nil, err
	}

	return gen.UpdatePharmacyIntegration200JSONResponse{
		Level:                 gen.PharmacyIntegrationSettingsLevel(b.Level),
		WebhookUrl:            b.WebhookUrl,
		CatalogMappingEnabled: b.CatalogMappingEnabled,
		DeliveryAreas:         b.DeliveryAreas,
	}, nil
}

// ListPharmacyProductMappings lista mapeos de catálogo.
func (h *PharmacyHandlers) ListPharmacyProductMappings(ctx context.Context, req gen.ListPharmacyProductMappingsRequestObject) (gen.ListPharmacyProductMappingsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyAdmin, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	limit := 20
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	items, total, err := h.queries.ListProductMappings(ctx, req.OrgId, limit, offset)
	if err != nil {
		return nil, err
	}

	genItems := make([]gen.PharmacyProductMappingItem, 0, len(items))
	for _, it := range items {
		upAt := it.UpdatedAt
		genItems = append(genItems, gen.PharmacyProductMappingItem{
			CatalogId:    it.CatalogID,
			StandardName: it.StandardName,
			PharmacySku:  it.PharmacySKU,
			PharmacyName: it.PharmacyName,
			PriceCents:   it.PriceCents,
			InStock:      it.InStock,
			UpdatedAt:    &upAt,
		})
	}

	return gen.ListPharmacyProductMappings200JSONResponse{
		Items: genItems,
		Total: total,
	}, nil
}

// UpsertPharmacyProductMapping guarda un mapeo de catálogo.
func (h *PharmacyHandlers) UpsertPharmacyProductMapping(ctx context.Context, req gen.UpsertPharmacyProductMappingRequestObject) (gen.UpsertPharmacyProductMappingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyAdmin, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	b := req.Body
	if b == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	if err := h.queries.UpsertProductMapping(ctx, req.OrgId, req.CatalogId, b.PharmacySku, b.PharmacyName, b.PriceCents, b.InStock); err != nil {
		return nil, err
	}

	now := time.Now()
	return gen.UpsertPharmacyProductMapping200JSONResponse{
		CatalogId:    req.CatalogId,
		StandardName: b.PharmacyName,
		PharmacySku:  b.PharmacySku,
		PharmacyName: b.PharmacyName,
		PriceCents:   b.PriceCents,
		InStock:      b.InStock,
		UpdatedAt:    &now,
	}, nil
}

// DeletePharmacyProductMapping elimina un mapeo de catálogo.
func (h *PharmacyHandlers) DeletePharmacyProductMapping(ctx context.Context, req gen.DeletePharmacyProductMappingRequestObject) (gen.DeletePharmacyProductMappingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PharmacyAdmin, authz.OrgResource{ID: req.OrgId}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if err := h.queries.DeleteProductMapping(ctx, req.OrgId, req.CatalogId); err != nil {
		return nil, err
	}

	return gen.DeletePharmacyProductMapping204Response{}, nil
}

// -----------------------------------------------------------------------------
// View conversion helpers
// -----------------------------------------------------------------------------

func (h *PharmacyHandlers) detailViewToGen(d *infra.QuoteRequestDetailView) gen.QuoteRequestDetail {
	items := make([]struct {
		CatalogItemId       *uuid.UUID `json:"catalog_item_id,omitempty"`
		Dose                string     `json:"dose"`
		ItemId              uuid.UUID  `json:"item_id"`
		MedicationName      string     `json:"medication_name"`
		PrescriptionItemId  uuid.UUID  `json:"prescription_item_id"`
		Quantity            float32    `json:"quantity"`
		SubstitutionAllowed bool       `json:"substitution_allowed"`
		Unit                string     `json:"unit"`
	}, 0, len(d.Items))

	for _, it := range d.Items {
		itemUUID, _ := uuid.Parse(it.ItemID)
		items = append(items, struct {
			CatalogItemId       *uuid.UUID `json:"catalog_item_id,omitempty"`
			Dose                string     `json:"dose"`
			ItemId              uuid.UUID  `json:"item_id"`
			MedicationName      string     `json:"medication_name"`
			PrescriptionItemId  uuid.UUID  `json:"prescription_item_id"`
			Quantity            float32    `json:"quantity"`
			SubstitutionAllowed bool       `json:"substitution_allowed"`
			Unit                string     `json:"unit"`
		}{
			CatalogItemId:       it.CatalogID,
			Dose:                it.Dose,
			ItemId:              itemUUID,
			MedicationName:      it.NameSnapshot,
			PrescriptionItemId:  itemUUID,
			Quantity:            float32(it.Quantity),
			SubstitutionAllowed: it.SubstitutionAllowed,
			Unit:                it.Unit,
		})
	}

	invs := make([]struct {
		PharmacyId   uuid.UUID  `json:"pharmacy_id"`
		PharmacyName string     `json:"pharmacy_name"`
		RespondedAt  *time.Time `json:"responded_at,omitempty"`
		SentAt       time.Time  `json:"sent_at"`
		Status       string     `json:"status"`
	}, 0, len(d.Invitations))

	for _, inv := range d.Invitations {
		sentAt := time.Now()
		if inv.SentAt != nil {
			sentAt = *inv.SentAt
		}
		invs = append(invs, struct {
			PharmacyId   uuid.UUID  `json:"pharmacy_id"`
			PharmacyName string     `json:"pharmacy_name"`
			RespondedAt  *time.Time `json:"responded_at,omitempty"`
			SentAt       time.Time  `json:"sent_at"`
			Status       string     `json:"status"`
		}{
			PharmacyId:   inv.OrgID,
			PharmacyName: "Pharmacy " + inv.OrgID.String()[:8],
			SentAt:       sentAt,
			Status:       string(inv.Status),
		})
	}

	quotes := make([]struct {
		CreatedAt           time.Time  `json:"created_at"`
		DeliveryFeeCents    int        `json:"delivery_fee_cents"`
		EstimatedDeliveryAt *time.Time `json:"estimated_delivery_at,omitempty"`
		EstimatedReadyAt    *time.Time `json:"estimated_ready_at,omitempty"`
		ExpiresAt           time.Time  `json:"expires_at"`
		Lines               []struct {
			ItemId            uuid.UUID  `json:"item_id"`
			ProductName       string     `json:"product_name"`
			Quantity          float32    `json:"quantity"`
			SubstitutedItemId *uuid.UUID `json:"substituted_item_id,omitempty"`
			Substitution      bool       `json:"substitution"`
			TotalPriceCents   int        `json:"total_price_cents"`
			UnitPriceCents    int        `json:"unit_price_cents"`
		} `json:"lines"`
		PaymentOnDeliveryOnly *bool     `json:"payment_on_delivery_only,omitempty"`
		PharmacyId            uuid.UUID `json:"pharmacy_id"`
		PharmacyName          string    `json:"pharmacy_name"`
		QuoteId               uuid.UUID `json:"quote_id"`
		TotalCents            int       `json:"total_cents"`
	}, 0, len(d.Quotes))

	for _, q := range d.Quotes {
		lines := make([]struct {
			ItemId            uuid.UUID  `json:"item_id"`
			ProductName       string     `json:"product_name"`
			Quantity          float32    `json:"quantity"`
			SubstitutedItemId *uuid.UUID `json:"substituted_item_id,omitempty"`
			Substitution      bool       `json:"substitution"`
			TotalPriceCents   int        `json:"total_price_cents"`
			UnitPriceCents    int        `json:"unit_price_cents"`
		}, 0, len(q.Lines))

		for _, l := range q.Lines {
			lineUUID, _ := uuid.Parse(l.ItemID)
			prodName := "Medication " + l.ItemID
			isSub := l.Substitution != nil
			var subID *uuid.UUID
			if isSub {
				prodName = l.Substitution.Name
				subID = &l.Substitution.CatalogID
			}
			lines = append(lines, struct {
				ItemId            uuid.UUID  `json:"item_id"`
				ProductName       string     `json:"product_name"`
				Quantity          float32    `json:"quantity"`
				SubstitutedItemId *uuid.UUID `json:"substituted_item_id,omitempty"`
				Substitution      bool       `json:"substitution"`
				TotalPriceCents   int        `json:"total_price_cents"`
				UnitPriceCents    int        `json:"unit_price_cents"`
			}{
				ItemId:            lineUUID,
				ProductName:       prodName,
				Quantity:          float32(l.Qty),
				SubstitutedItemId: subID,
				Substitution:      isSub,
				TotalPriceCents:   int(l.UnitPrice * l.Qty * 100),
				UnitPriceCents:    int(l.UnitPrice * 100),
			})
		}

		delivFee := 0
		if q.DeliveryFee != nil {
			delivFee = int(*q.DeliveryFee * 100)
		}
		var readyAt, delivAt *time.Time
		if q.ETA != nil {
			if t, err := time.Parse(time.RFC3339, *q.ETA); err == nil {
				readyAt = &t
			}
		}

		pod := true
		quotes = append(quotes, struct {
			CreatedAt           time.Time  `json:"created_at"`
			DeliveryFeeCents    int        `json:"delivery_fee_cents"`
			EstimatedDeliveryAt *time.Time `json:"estimated_delivery_at,omitempty"`
			EstimatedReadyAt    *time.Time `json:"estimated_ready_at,omitempty"`
			ExpiresAt           time.Time  `json:"expires_at"`
			Lines               []struct {
				ItemId            uuid.UUID  `json:"item_id"`
				ProductName       string     `json:"product_name"`
				Quantity          float32    `json:"quantity"`
				SubstitutedItemId *uuid.UUID `json:"substituted_item_id,omitempty"`
				Substitution      bool       `json:"substitution"`
				TotalPriceCents   int        `json:"total_price_cents"`
				UnitPriceCents    int        `json:"unit_price_cents"`
			} `json:"lines"`
			PaymentOnDeliveryOnly *bool     `json:"payment_on_delivery_only,omitempty"`
			PharmacyId            uuid.UUID `json:"pharmacy_id"`
			PharmacyName          string    `json:"pharmacy_name"`
			QuoteId               uuid.UUID `json:"quote_id"`
			TotalCents            int       `json:"total_cents"`
		}{
			CreatedAt:             q.ReceivedAt,
			DeliveryFeeCents:      delivFee,
			EstimatedDeliveryAt:   delivAt,
			EstimatedReadyAt:      readyAt,
			ExpiresAt:             q.ValidUntil,
			Lines:                 lines,
			PaymentOnDeliveryOnly: &pod,
			PharmacyId:            q.OrgID,
			PharmacyName:          "Pharmacy " + q.OrgID.String()[:8],
			QuoteId:               q.ID,
			TotalCents:            int(q.Total * 100),
		})
	}

	return gen.QuoteRequestDetail{
		Id:              d.ID,
		PrescriptionId:  d.PrescriptionID,
		GuardianId:      &d.GuardianID,
		Status:          string(d.Status),
		FulfillmentType: string(d.Mode),
		Items:           items,
		Invitations:     invs,
		Quotes:          quotes,
		CreatedAt:       d.CreatedAt,
		ExpiresAt:       d.ExpiresAt,
	}
}

func (h *PharmacyHandlers) orderDetailViewToGen(o *infra.OrderDetailView) gen.OrderDetail {
	lines := make([]struct {
		ItemId          uuid.UUID `json:"item_id"`
		ProductName     string    `json:"product_name"`
		Quantity        float32   `json:"quantity"`
		Substitution    bool      `json:"substitution"`
		TotalPriceCents int       `json:"total_price_cents"`
		UnitPriceCents  int       `json:"unit_price_cents"`
	}, 0, len(o.Lines))

	for _, l := range o.Lines {
		lineUUID, _ := uuid.Parse(l.ItemID)
		prodName := "Medication " + l.ItemID
		if l.SubstitutedName != nil {
			prodName = *l.SubstitutedName
		} else if l.FreeText != nil {
			prodName = *l.FreeText
		}
		lines = append(lines, struct {
			ItemId          uuid.UUID `json:"item_id"`
			ProductName     string    `json:"product_name"`
			Quantity        float32   `json:"quantity"`
			Substitution    bool      `json:"substitution"`
			TotalPriceCents int       `json:"total_price_cents"`
			UnitPriceCents  int       `json:"unit_price_cents"`
		}{
			ItemId:          lineUUID,
			ProductName:     prodName,
			Quantity:        float32(l.Qty),
			Substitution:    l.Substituted != nil,
			TotalPriceCents: int(l.UnitPrice * l.Qty * 100),
			UnitPriceCents:  int(l.UnitPrice * 100),
		})
	}

	timeline := make([]struct {
		ChangedAt time.Time `json:"changed_at"`
		Note      *string   `json:"note,omitempty"`
		Status    string    `json:"status"`
	}, 0, len(o.Timeline))

	for _, t := range o.Timeline {
		timeline = append(timeline, struct {
			ChangedAt time.Time `json:"changed_at"`
			Note      *string   `json:"note,omitempty"`
			Status    string    `json:"status"`
		}{
			ChangedAt: t.At,
			Note:      t.Reason,
			Status:    string(t.Status),
		})
	}

	fee := int(o.Totals.DeliveryFee * 100)
	shared := []string{"contact_phone", "delivery_address", "pet_name", "prescription_items"}
	payMethod := "cash_on_delivery"
	payStatus := "pending"

	return gen.OrderDetail{
		Id:                     o.ID,
		QuoteRequestId:         o.QuoteRequestID,
		PrescriptionId:         o.PrescriptionID,
		GuardianId:             &o.GuardianID,
		PharmacyId:             o.PharmacyOrgID,
		PharmacyName:           o.PharmacyName,
		PharmacyPhone:          o.PharmacyPhone,
		PharmacyAddress:        &o.PharmacyCity,
		Status:                 string(o.Status),
		FulfillmentType:        string(o.Mode),
		Lines:                  lines,
		DeliveryFeeCents:       &fee,
		TotalCents:             int(o.Totals.Total * 100),
		PaymentMethod:          &payMethod,
		PaymentStatus:          &payStatus,
		StatusHistory:          timeline,
		SharedDataWithPharmacy: &shared,
		CreatedAt:              o.CreatedAt,
		UpdatedAt:              &o.UpdatedAt,
	}
}

func (h *PharmacyHandlers) orderDetailViewToPharmacyGen(o *infra.OrderDetailView) gen.PharmacyOrderDetail {
	lines := make([]struct {
		ItemId             uuid.UUID  `json:"item_id"`
		PrescriptionItemId *uuid.UUID `json:"prescription_item_id,omitempty"`
		ProductName        string     `json:"product_name"`
		Quantity           float32    `json:"quantity"`
		Substitution       bool       `json:"substitution"`
		TotalPriceCents    int        `json:"total_price_cents"`
		UnitPriceCents     int        `json:"unit_price_cents"`
	}, 0, len(o.Lines))

	for _, l := range o.Lines {
		lineUUID, _ := uuid.Parse(l.ItemID)
		prodName := "Medication " + l.ItemID
		if l.SubstitutedName != nil {
			prodName = *l.SubstitutedName
		} else if l.FreeText != nil {
			prodName = *l.FreeText
		}
		lines = append(lines, struct {
			ItemId             uuid.UUID  `json:"item_id"`
			PrescriptionItemId *uuid.UUID `json:"prescription_item_id,omitempty"`
			ProductName        string     `json:"product_name"`
			Quantity           float32    `json:"quantity"`
			Substitution       bool       `json:"substitution"`
			TotalPriceCents    int        `json:"total_price_cents"`
			UnitPriceCents     int        `json:"unit_price_cents"`
		}{
			ItemId:             lineUUID,
			PrescriptionItemId: &lineUUID,
			ProductName:        prodName,
			Quantity:           float32(l.Qty),
			Substitution:       l.Substituted != nil,
			TotalPriceCents:    int(l.UnitPrice * l.Qty * 100),
			UnitPriceCents:     int(l.UnitPrice * 100),
		})
	}

	timeline := make([]struct {
		ChangedAt time.Time `json:"changed_at"`
		Note      *string   `json:"note,omitempty"`
		Status    string    `json:"status"`
	}, 0, len(o.Timeline))

	for _, t := range o.Timeline {
		timeline = append(timeline, struct {
			ChangedAt time.Time `json:"changed_at"`
			Note      *string   `json:"note,omitempty"`
			Status    string    `json:"status"`
		}{
			ChangedAt: t.At,
			Note:      t.Reason,
			Status:    string(t.Status),
		})
	}

	fee := int(o.Totals.DeliveryFee * 100)
	petName := "Patient"
	petSpecies := "Canine"
	tutorName := o.GuardianID.String()

	return gen.PharmacyOrderDetail{
		Id:               o.ID,
		QuoteRequestId:   o.QuoteRequestID,
		PrescriptionId:   o.PrescriptionID,
		TutorName:        &tutorName,
		TutorPhone:       &o.ContactPhone,
		PetName:          &petName,
		PetSpecies:       &petSpecies,
		Status:           string(o.Status),
		FulfillmentType:  string(o.Mode),
		Lines:            lines,
		DeliveryFeeCents: &fee,
		TotalCents:       int(o.Totals.Total * 100),
		StatusHistory:    timeline,
		CreatedAt:        o.CreatedAt,
		UpdatedAt:        &o.UpdatedAt,
	}
}

func valOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
