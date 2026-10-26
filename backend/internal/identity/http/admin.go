package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AdminHandlers implementa las operaciones admin de usuarios y auditoría
// (PP-F1-API-008). authz.Can lo evalúa app.AdminService en cada caso de uso.
type AdminHandlers struct {
	admin *app.AdminService
	base  *Handlers // principal y metadatos de request (correlación, ip_hash)
}

// NewAdminHandlers construye los handlers admin.
func NewAdminHandlers(admin *app.AdminService, base *Handlers) *AdminHandlers {
	return &AdminHandlers{admin: admin, base: base}
}

// AdminListUsers busca usuarios (PRD-018).
func (h *AdminHandlers) AdminListUsers(ctx context.Context, req gen.AdminListUsersRequestObject) (gen.AdminListUsersResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	f := app.AccountSearch{Query: deref(req.Params.Q), Limit: limitOf(req.Params.Limit)}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if req.Params.Cursor != nil {
		t, id, err := decodeCursor(*req.Params.Cursor)
		if err != nil {
			return nil, invalidCursor()
		}
		f.Cursor = &app.AccountCursor{CreatedAt: t, ID: id}
	}
	views, next, err := h.admin.SearchUsers(ctx, p, f)
	if err != nil {
		return nil, err
	}
	page := gen.AdminUserPage{Data: make([]gen.AdminUserRow, 0, len(views))}
	for _, v := range views {
		page.Data = append(page.Data, toAdminUserRow(v))
	}
	if next != nil {
		c := encodeCursor(next.CreatedAt, next.ID)
		page.NextCursor = &c
	}
	return gen.AdminListUsers200JSONResponse(page), nil
}

// AdminSuspendUser suspende la cuenta (plataforma + Keycloak).
func (h *AdminHandlers) AdminSuspendUser(ctx context.Context, req gen.AdminSuspendUserRequestObject) (gen.AdminSuspendUserResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.admin.SuspendUser(ctx, p, h.command(ctx, uuid.UUID(req.UserId), req.Body))
	if err != nil {
		return nil, err
	}
	return gen.AdminSuspendUser200JSONResponse(toAdminUserRow(v)), nil
}

// AdminReactivateUser reactiva la cuenta.
func (h *AdminHandlers) AdminReactivateUser(ctx context.Context, req gen.AdminReactivateUserRequestObject) (gen.AdminReactivateUserResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.admin.ReactivateUser(ctx, p, h.command(ctx, uuid.UUID(req.UserId), req.Body))
	if err != nil {
		return nil, err
	}
	return gen.AdminReactivateUser200JSONResponse(toAdminUserRow(v)), nil
}

// AdminListAuditLog lista el audit log con filtros.
func (h *AdminHandlers) AdminListAuditLog(ctx context.Context, req gen.AdminListAuditLogRequestObject) (gen.AdminListAuditLogResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	f := audit.Filter{From: req.Params.From, To: req.Params.To, Limit: limitOf(req.Params.Limit)}
	if req.Params.ActorId != nil {
		id := uuid.UUID(*req.Params.ActorId)
		f.ActorID = &id
	}
	if req.Params.TargetId != nil {
		id := uuid.UUID(*req.Params.TargetId)
		f.TargetID = &id
	}
	if req.Params.Cursor != nil {
		t, id, err := decodeCursor(*req.Params.Cursor)
		if err != nil {
			return nil, invalidCursor()
		}
		f.Cursor = &audit.Cursor{OccurredAt: t, ID: id}
	}
	rows, next, err := h.admin.AuditEntries(ctx, p, f)
	if err != nil {
		return nil, err
	}
	page := gen.AuditPage{Data: make([]gen.AuditEntry, 0, len(rows))}
	for _, r := range rows {
		page.Data = append(page.Data, toAuditEntry(r))
	}
	if next != nil {
		c := encodeCursor(next.OccurredAt, next.ID)
		page.NextCursor = &c
	}
	return gen.AdminListAuditLog200JSONResponse(page), nil
}

func (h *AdminHandlers) command(ctx context.Context, userID uuid.UUID, body *gen.AdminReasonRequest) app.AdminUserCommand {
	corr, ipHash := h.base.requestMeta(ctx)
	cmd := app.AdminUserCommand{UserID: userID, CorrelationID: corr, IPHash: ipHash}
	if body != nil {
		cmd.Reason = body.Reason
	}
	return cmd
}

func toAdminUserRow(v app.AdminUserView) gen.AdminUserRow {
	a := v.Account
	row := gen.AdminUserRow{
		Id: openapi_types.UUID(a.ID), Email: a.Email.String(), DisplayName: a.DisplayName,
		Status: gen.AdminUserRowStatus(a.Status), GlobalRoles: a.GlobalRoles, CreatedAt: a.CreatedAt,
		LastLoginAt: a.LastLoginAt, PetCount: v.PetCount, Memberships: toMemberships(v.Memberships),
	}
	if row.GlobalRoles == nil {
		row.GlobalRoles = []string{}
	}
	row.StatusReason = optional(a.StatusReason)
	return row
}

func toAuditEntry(r audit.Row) gen.AuditEntry {
	e := gen.AuditEntry{
		Id: openapi_types.UUID(r.ID), OccurredAt: r.OccurredAt, ActorRoles: r.ActorRoles, Action: r.Action,
		TargetType: r.TargetType, Reason: r.Reason,
		ActorId: uuidPtr(r.ActorID), TargetId: uuidPtr(r.TargetID), CorrelationId: uuidPtr(r.CorrelationID),
	}
	if r.Details != nil {
		d := r.Details
		e.Details = &d
	}
	return e
}

func uuidPtr(id *uuid.UUID) *openapi_types.UUID {
	if id == nil {
		return nil
	}
	v := openapi_types.UUID(*id)
	return &v
}

func limitOf(l *int) int {
	if l == nil {
		return 20
	}
	return *l
}

// cursor opaco: base64url de JSON {t, id} (mismo formato que pets).
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw, _ := json.Marshal(map[string]string{"t": t.UTC().Format(time.RFC3339Nano), "id": id.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	var m struct {
		T  string `json:"t"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return time.Time{}, uuid.Nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, m.T)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(m.ID)
	return t, id, err
}

func invalidCursor() error {
	return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "cursor", Code: "INVALID"}}}
}
