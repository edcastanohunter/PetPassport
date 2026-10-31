package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Solicitud de vínculo por la clínica, bandeja del tutor y alertas del perfil
// (PP-F2-PRD-020, API-009, DOM-013).

func requestItem(v app.CareLinkRequestView) gen.CareLinkRequestItem {
	item := gen.CareLinkRequestItem{
		Id: openapi_types.UUID(v.ID), PetId: openapi_types.UUID(v.PetID), PetName: v.PetName,
		OrganizationId: openapi_types.UUID(v.OrganizationID), Status: gen.CareLinkRequestItemStatus(v.Status),
		ExpiresAt: v.ExpiresAt, CreatedAt: v.CreatedAt, ResolvedAt: v.ResolvedAt,
	}
	if v.Message != "" {
		item.Message = ptr(v.Message)
	}
	return item
}

// LookupPetForRequest busca mascotas por microchip o email del tutor.
func (h *PetHandlers) LookupPetForRequest(ctx context.Context, req gen.LookupPetForRequestRequestObject) (gen.LookupPetForRequestResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.CareLinkRequest, authz.OrgResource{ID: uuid.UUID(req.OrgId)}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	var chip, email string
	if req.Params.Microchip != nil {
		chip = strings.TrimSpace(*req.Params.Microchip)
	}
	if req.Params.GuardianEmail != nil {
		email = strings.TrimSpace(*req.Params.GuardianEmail)
	}
	if (chip == "") == (email == "") {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "microchip", Code: "REQUIRED_ONE_OF"}}}
	}
	found, err := h.svc.LookupPetForRequest(ctx, p.UserID, chip, email)
	if err != nil {
		return nil, err
	}
	out := gen.PetLookupList{Data: make([]gen.PetLookupResult, 0, len(found))}
	for _, r := range found {
		out.Data = append(out.Data, gen.PetLookupResult{
			PetId: openapi_types.UUID(r.PetID), Name: r.Name,
			Species: gen.Species(r.Species), GuardianMasked: r.GuardianMasked,
		})
	}
	return gen.LookupPetForRequest200JSONResponse(out), nil
}

// RequestCareLink crea la solicitud (Idempotency-Key → 201).
func (h *PetHandlers) RequestCareLink(ctx context.Context, req gen.RequestCareLinkRequestObject) (gen.RequestCareLinkResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if d := authz.Can(p, authz.CareLinkRequest, authz.OrgResource{ID: orgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	hash := sha256.Sum256(httpserver.RawBodyFrom(ctx))
	outcome, err := h.idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayedRequestCareLink{status: outcome.Status, body: outcome.Body}, nil
	}
	ctx, _ = h.meta(ctx, p, key.String(), 0)
	cmd := domain.RequestCareLink{PetID: uuid.UUID(req.Body.PetId), OrganizationID: orgID, RequestedBy: p.UserID}
	if req.Body.Message != nil {
		cmd.Message = *req.Body.Message
	}
	res, err := h.dispatch(ctx, cmd)
	if err != nil {
		_ = h.idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	view, err := h.svc.GetOrgRequest(ctx, orgID, res.AggregateID)
	if err != nil {
		return nil, err
	}
	body := requestItem(view)
	raw, _ := json.Marshal(body)
	if err := h.idem.Commit(ctx, p.UserID, key, http.StatusCreated, raw); err != nil {
		return nil, err
	}
	return gen.RequestCareLink201JSONResponse(body), nil
}

// ListCareLinkRequests lista las solicitudes de la clínica.
func (h *PetHandlers) ListCareLinkRequests(ctx context.Context, req gen.ListCareLinkRequestsRequestObject) (gen.ListCareLinkRequestsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if d := authz.Can(p, authz.CareLinkRequest, authz.OrgResource{ID: orgID}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	rows, err := h.svc.ListOrgRequests(ctx, orgID, status)
	if err != nil {
		return nil, err
	}
	out := gen.CareLinkRequestList{Data: make([]gen.CareLinkRequestItem, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, requestItem(r))
	}
	return gen.ListCareLinkRequests200JSONResponse(out), nil
}

// GetInbox devuelve la bandeja del tutor autenticado.
func (h *PetHandlers) GetInbox(ctx context.Context, _ gen.GetInboxRequestObject) (gen.GetInboxResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}
	items, err := h.svc.ListInbox(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := gen.InboxList{Data: make([]gen.InboxItem, 0, len(items))}
	for _, it := range items {
		row := gen.InboxItem{
			Id: openapi_types.UUID(it.ID), Kind: gen.InboxItemKind(it.Kind), RefId: openapi_types.UUID(it.RefID),
			Title: it.Title, Status: it.Status, CreatedAt: it.CreatedAt, ExpiresAt: it.ExpiresAt,
		}
		if it.PetID != nil {
			id := openapi_types.UUID(*it.PetID)
			row.PetId = &id
		}
		if it.OrganizationID != nil {
			id := openapi_types.UUID(*it.OrganizationID)
			row.OrganizationId = &id
		}
		if it.PetName != "" {
			row.PetName = ptr(it.PetName)
		}
		if it.Body != "" {
			row.Body = ptr(it.Body)
		}
		out.Data = append(out.Data, row)
	}
	return gen.GetInbox200JSONResponse(out), nil
}

// requestPet carga la mascota y exige que el actor sea quien decide el vínculo.
func (h *PetHandlers) requestPet(ctx context.Context, petID uuid.UUID) (app.PetView, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return app.PetView{}, err
	}
	view, err := h.queries.GetPet(ctx, petID)
	if err != nil {
		return app.PetView{}, err
	}
	if d := authz.Can(p, authz.PetCareLinkGrant, petResource(view)); !d.Allow {
		return app.PetView{}, notFoundOnDeny(d)
	}
	return view, nil
}

func requestNotFound(err error) error {
	if errors.Is(err, domain.ErrRequestNotFound) {
		return &kernel.NotFoundError{Resource: "care_link_request"}
	}
	return err
}

// ApproveCareLinkRequest aprueba la solicitud y crea el vínculo.
func (h *PetHandlers) ApproveCareLinkRequest(ctx context.Context, req gen.ApproveCareLinkRequestRequestObject) (gen.ApproveCareLinkRequestResponseObject, error) {
	view, err := h.requestPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	p, _ := h.principal(ctx)
	ctx, _ = h.meta(ctx, p, "", 0)
	if _, err := h.dispatch(ctx, domain.ApproveCareLinkRequest{
		PetID: view.ID, RequestID: uuid.UUID(req.RequestId), ActorID: p.UserID,
	}); err != nil {
		return nil, requestNotFound(err)
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.ApproveCareLinkRequest200JSONResponse{
		Headers: gen.ApproveCareLinkRequest200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// DeclineCareLinkRequest rechaza la solicitud.
func (h *PetHandlers) DeclineCareLinkRequest(ctx context.Context, req gen.DeclineCareLinkRequestRequestObject) (gen.DeclineCareLinkRequestResponseObject, error) {
	view, err := h.requestPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	p, _ := h.principal(ctx)
	ctx, _ = h.meta(ctx, p, "", 0)
	cmd := domain.DeclineCareLinkRequest{PetID: view.ID, RequestID: uuid.UUID(req.RequestId), ActorID: p.UserID}
	if req.Body != nil && req.Body.Reason != nil {
		cmd.Reason = *req.Body.Reason
	}
	if _, err := h.dispatch(ctx, cmd); err != nil {
		return nil, requestNotFound(err)
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.DeclineCareLinkRequest200JSONResponse{
		Headers: gen.DeclineCareLinkRequest200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// PutPetAlerts reemplaza las alertas del perfil (If-Match).
func (h *PetHandlers) PutPetAlerts(ctx context.Context, req gen.PutPetAlertsRequestObject) (gen.PutPetAlertsResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.queries.GetPet(ctx, uuid.UUID(req.PetId))
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.PetAlerts, petResource(view)); !d.Allow {
		return nil, notFoundOnDeny(d)
	}
	ctx, _ = h.meta(ctx, p, "", int64(req.Params.IfMatch))
	alerts := make([]domain.PetAlert, 0, len(req.Body.Alerts))
	for _, a := range req.Body.Alerts {
		alerts = append(alerts, domain.PetAlert{Kind: string(a.Kind), Text: a.Text})
	}
	if _, err := h.dispatch(ctx, domain.UpdatePetAlerts{PetID: view.ID, ActorID: p.UserID, Alerts: alerts}); err != nil &&
		!errors.Is(err, domain.ErrNoChanges) {
		return nil, err
	}
	updated, err := h.queries.GetPet(ctx, view.ID)
	if err != nil {
		return nil, err
	}
	return gen.PutPetAlerts200JSONResponse{
		Headers: gen.PutPetAlerts200ResponseHeaders{ETag: ptr(updated.Version)},
		Body:    toPet(updated, h.presign()),
	}, nil
}

// replayedRequestCareLink reproduce la respuesta almacenada (ARQ-008).
type replayedRequestCareLink replayedResponse

func (r replayedRequestCareLink) VisitRequestCareLinkResponse(w http.ResponseWriter) error {
	return replayedCreatePetResponse(r).VisitCreatePetResponse(w)
}

var _ gen.RequestCareLinkResponseObject = replayedRequestCareLink{}
