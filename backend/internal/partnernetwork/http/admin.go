package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Operaciones de administración de organizaciones (PP-F1-API-008, PRD-017):
// solo super_admin; las transiciones exigen motivo (verify: opcional) y
// escriben audit_log en la misma transacción que el evento (RN-06).

// WithIPHashSalt fija la sal del ip_hash del audit log (SEC-007).
func (h *OrgHandlers) WithIPHashSalt(salt string) *OrgHandlers {
	h.ipHashSalt = salt
	return h
}

// AdminListOrganizations devuelve la cola por estado (por defecto pendientes).
func (h *OrgHandlers) AdminListOrganizations(ctx context.Context, req gen.AdminListOrganizationsRequestObject) (gen.AdminListOrganizationsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.AdminOrgList, nil); err != nil {
		return nil, err
	}
	f := app.AdminOrgFilter{Query: deref(req.Params.Q), Limit: 20}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		c, err := decodeOrgCursor(*req.Params.Cursor)
		if err != nil {
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "cursor", Code: "INVALID"}}}
		}
		f.Cursor = &c
	}
	views, next, err := h.svc.AdminListOrganizations(ctx, f)
	if err != nil {
		return nil, err
	}
	page := gen.AdminOrganizationPage{Data: make([]gen.Organization, 0, len(views))}
	for _, v := range views {
		page.Data = append(page.Data, toAdminOrganization(v, p))
	}
	if next != nil {
		c := encodeOrgCursor(*next)
		page.NextCursor = &c
	}
	return gen.AdminListOrganizations200JSONResponse(page), nil
}

// AdminVerifyOrganization: pending_verification → active (+ email).
func (h *OrgHandlers) AdminVerifyOrganization(ctx context.Context, req gen.AdminVerifyOrganizationRequestObject) (gen.AdminVerifyOrganizationResponseObject, error) {
	reason := ""
	if req.Body != nil {
		reason = deref(req.Body.Reason)
	}
	v, err := h.adminTransition(ctx, uuid.UUID(req.OrgId), authz.AdminOrgVerify, "verify", reason, false)
	if err != nil {
		return nil, err
	}
	return gen.AdminVerifyOrganization200JSONResponse{Headers: gen.AdminVerifyOrganization200ResponseHeaders{ETag: &v.Version}, Body: v}, nil
}

// AdminRejectOrganization: pending_verification → rejected (+ email con motivo).
func (h *OrgHandlers) AdminRejectOrganization(ctx context.Context, req gen.AdminRejectOrganizationRequestObject) (gen.AdminRejectOrganizationResponseObject, error) {
	v, err := h.adminTransition(ctx, uuid.UUID(req.OrgId), authz.AdminOrgReject, "reject", reasonOf(req.Body), true)
	if err != nil {
		return nil, err
	}
	return gen.AdminRejectOrganization200JSONResponse{Headers: gen.AdminRejectOrganization200ResponseHeaders{ETag: &v.Version}, Body: v}, nil
}

// AdminSuspendOrganization: active → suspended (DOM-011: pacientes ocultos).
func (h *OrgHandlers) AdminSuspendOrganization(ctx context.Context, req gen.AdminSuspendOrganizationRequestObject) (gen.AdminSuspendOrganizationResponseObject, error) {
	v, err := h.adminTransition(ctx, uuid.UUID(req.OrgId), authz.AdminOrgSuspend, "suspend", reasonOf(req.Body), true)
	if err != nil {
		return nil, err
	}
	return gen.AdminSuspendOrganization200JSONResponse{Headers: gen.AdminSuspendOrganization200ResponseHeaders{ETag: &v.Version}, Body: v}, nil
}

// AdminReactivateOrganization: suspended → active.
func (h *OrgHandlers) AdminReactivateOrganization(ctx context.Context, req gen.AdminReactivateOrganizationRequestObject) (gen.AdminReactivateOrganizationResponseObject, error) {
	v, err := h.adminTransition(ctx, uuid.UUID(req.OrgId), authz.AdminOrgReactivate, "reactivate", reasonOf(req.Body), true)
	if err != nil {
		return nil, err
	}
	return gen.AdminReactivateOrganization200JSONResponse{Headers: gen.AdminReactivateOrganization200ResponseHeaders{ETag: &v.Version}, Body: v}, nil
}

// adminTransition: authz → motivo → comando con auditoría en la misma tx →
// vista admin. El estado de origen lo valida el agregado (409
// INVALID_STATUS_TRANSITION).
func (h *OrgHandlers) adminTransition(ctx context.Context, orgID uuid.UUID, action authz.Action, verb, rawReason string, required bool) (gen.Organization, error) {
	p, err := principal(ctx)
	if err != nil {
		return gen.Organization{}, err
	}
	if err := can(p, action, authz.OrgResource{ID: orgID}); err != nil {
		return gen.Organization{}, err
	}
	reason, err := app.AdminReason(rawReason, required)
	if err != nil {
		return gen.Organization{}, err
	}
	cmd, err := app.AdminTransitionCommand(verb, orgID, reason)
	if err != nil {
		return gen.Organization{}, err
	}
	actor := p.UserID
	entry := audit.Entry{
		ActorID: &actor, ActorRoles: p.GlobalRoles, Action: "admin.org." + verb, Reason: reason,
		CorrelationID: h.correlationID(ctx), IPHash: h.ipHash(ctx),
	}
	cctx := app.WithAdminAudit(withCommand(ctx, p, "", 0), entry)
	if _, err := h.svc.Dispatch(cctx, cmd); err != nil {
		return gen.Organization{}, err
	}
	view, err := h.svc.Queries().GetOrg(ctx, orgID)
	if err != nil {
		return gen.Organization{}, err
	}
	return toAdminOrganization(h.svc.AdminOrg(ctx, view), p), nil
}

func reasonOf(b *gen.AdminReasonRequest) string {
	if b == nil {
		return ""
	}
	return b.Reason
}

func (h *OrgHandlers) correlationID(ctx context.Context) *uuid.UUID {
	if id, err := uuid.Parse(reqctx.From(ctx).CorrelationID); err == nil {
		return &id
	}
	return nil
}

func (h *OrgHandlers) ipHash(ctx context.Context) []byte {
	info := reqctx.From(ctx)
	if info.RemoteAddr == "" || h.ipHashSalt == "" {
		return nil
	}
	return audit.HashIP(h.ipHashSalt, info.RemoteAddr, time.Now())
}

func toAdminOrganization(v app.AdminOrgView, p auth.Principal) gen.Organization {
	o := toOrganization(v.OrgView, myRole(p, v.ID))
	if v.Founder != nil {
		o.Founder = &gen.OrgFounder{Id: openapi_types.UUID(v.FounderID), Email: v.Founder.Email, DisplayName: v.Founder.DisplayName}
	}
	return o
}

// cursor opaco: base64url de JSON {t, id}.
func encodeOrgCursor(c app.OrgCursor) string {
	raw, _ := json.Marshal(map[string]string{"t": c.CreatedAt.UTC().Format(time.RFC3339Nano), "id": c.ID.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeOrgCursor(s string) (app.OrgCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return app.OrgCursor{}, err
	}
	var m struct {
		T  string `json:"t"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return app.OrgCursor{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, m.T)
	if err != nil {
		return app.OrgCursor{}, err
	}
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return app.OrgCursor{}, err
	}
	return app.OrgCursor{CreatedAt: t, ID: id}, nil
}
