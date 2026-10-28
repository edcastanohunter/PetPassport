// Package http implementa las operaciones de organizaciones, miembros e
// invitaciones del contrato OpenAPI (PP-F1-API-006/007) y el lookup por
// código público (API-005). Cada handler: principal → authz.Can con la
// x-pp-action del contrato → comando por el bus (Idempotency-Key o
// If-Match) → respuesta. Las operaciones /admin/organizations (API-008)
// viven en admin.go. Errores → problem+json (ARQ-010).
package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// MeRenderer construye el DTO Me (lo implementa identity/http; cmd/api lo
// inyecta para no acoplar módulos, ADR-001).
type MeRenderer interface {
	RenderMe(ctx context.Context, p auth.Principal) (gen.Me, error)
}

// OrgHandlers implementa las operaciones de organizaciones del contrato.
type OrgHandlers struct {
	svc         *app.Service
	idem        *idempotency.Store
	memberships auth.MembershipLoader // recarga tras aceptar invitación (caché invalidada)
	me          MeRenderer
	logger      *slog.Logger
	ipHashSalt  string // ip_hash del audit log en acciones admin
}

// NewOrgHandlers construye los handlers.
func NewOrgHandlers(svc *app.Service, idem *idempotency.Store, memberships auth.MembershipLoader, me MeRenderer, logger *slog.Logger) *OrgHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrgHandlers{svc: svc, idem: idem, memberships: memberships, me: me, logger: logger}
}

func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: "sin principal en el contexto"}
	}
	return p, nil
}

// withCommand adjunta el CommandContext del request (DOM-012).
func withCommand(ctx context.Context, p auth.Principal, idemKey string, expected int64) context.Context {
	info := reqctx.From(ctx)
	return app.WithCommandContext(ctx, app.CommandContext{
		Principal: app.PrincipalInfo{
			UserID: p.UserID, Email: p.Email, EmailVerified: p.EmailVerified, Roles: p.GlobalRoles,
		},
		CorrelationID: info.CorrelationID, CausationID: info.RequestID, Source: "api",
		IdempotencyKey: idemKey, ExpectedVersion: expected,
	})
}

// can evalúa la política; una denegación sobre una org visible es 403.
func can(p auth.Principal, action authz.Action, res authz.Resource) error {
	if d := authz.Can(p, action, res); !d.Allow {
		return &kernel.ForbiddenError{Reason: d.Reason}
	}
	return nil
}

// myRole devuelve el rol del principal en la org (nil si no es miembro).
func myRole(p auth.Principal, orgID uuid.UUID) *gen.OrgRole {
	for _, m := range p.Memberships {
		if m.OrganizationID == orgID {
			r := gen.OrgRole(m.Role)
			return &r
		}
	}
	return nil
}

// --------------------------------------------------------- organizaciones

// CreateOrganization registra una organización (Idempotency-Key, PRD-011).
func (h *OrgHandlers) CreateOrganization(ctx context.Context, req gen.CreateOrganizationRequestObject) (gen.CreateOrganizationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.OrgRegister, nil); err != nil {
		return nil, err
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	raw := httpserver.RawBodyFrom(ctx)
	hash := sha256.Sum256(raw)
	outcome, err := h.idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayed{status: outcome.Status, body: outcome.Body}, nil
	}
	b := req.Body
	cmd := domain.RegisterOrganization{
		Type: string(b.Type), LegalName: b.LegalName, DisplayName: b.DisplayName,
		TaxCountry: b.TaxId.Country, TaxValue: b.TaxId.Value,
		Address:      fromAddress(b.Address),
		ContactEmail: string(b.Contact.Email), ContactPhone: b.Contact.Phone, Website: deref(b.Contact.Website),
		FounderID: p.UserID,
	}
	res, err := h.svc.Dispatch(withCommand(ctx, p, key.String(), 0), cmd)
	if err != nil {
		_ = h.idem.Abort(ctx, p.UserID, key)
		return nil, err
	}
	view, err := h.svc.Queries().GetOrg(ctx, res.AggregateID)
	if err != nil {
		return nil, err
	}
	role := gen.OrgRole(adminRole(view.Type))
	body := toOrganization(view, &role)
	rawBody, _ := json.Marshal(body)
	if err := h.idem.Commit(ctx, p.UserID, key, http.StatusCreated, rawBody); err != nil {
		return nil, err
	}
	location := "/api/v1/organizations/" + view.ID.String()
	return gen.CreateOrganization201JSONResponse{
		Headers: gen.CreateOrganization201ResponseHeaders{ETag: &view.Version, Location: &location},
		Body:    body,
	}, nil
}

// LookupOrganization devuelve el resumen de una clínica activa por código.
func (h *OrgHandlers) LookupOrganization(ctx context.Context, req gen.LookupOrganizationRequestObject) (gen.LookupOrganizationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.OrgLookup, nil); err != nil {
		return nil, err
	}
	s, err := h.svc.Queries().SummaryByCode(ctx, req.Params.Code)
	if err != nil {
		return nil, err
	}
	return gen.LookupOrganization200JSONResponse(toSummary(s)), nil
}

// GetOrganization: vista completa para miembros y super_admin; resumen
// público para el resto.
func (h *OrgHandlers) GetOrganization(ctx context.Context, req gen.GetOrganizationRequestObject) (gen.GetOrganizationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgRead, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	view, err := h.svc.Queries().GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var body gen.OrganizationOrSummary
	full := authz.Can(p, authz.OrgRead, authz.OrgResource{ID: orgID, MembersOnly: true}).Allow
	if full {
		err = body.FromOrganization(toOrganization(view, myRole(p, orgID)))
	} else {
		err = body.FromOrgSummary(gen.OrgSummary{
			Id: openapi_types.UUID(view.ID), Type: gen.OrganizationType(view.Type), DisplayName: view.DisplayName,
			City: view.City, Country: view.Country, Status: gen.OrgStatus(view.Status),
		})
	}
	if err != nil {
		return nil, err
	}
	return gen.GetOrganization200JSONResponse{Headers: gen.GetOrganization200ResponseHeaders{ETag: &view.Version}, Body: body}, nil
}

// UpdateOrganization edita el perfil (If-Match, PRD-012).
func (h *OrgHandlers) UpdateOrganization(ctx context.Context, req gen.UpdateOrganizationRequestObject) (gen.UpdateOrganizationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgUpdate, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	b := req.Body
	cmd := domain.UpdateOrganizationProfile{OrganizationID: orgID, LegalName: b.LegalName, DisplayName: b.DisplayName}
	if b.TaxId != nil {
		cmd.TaxCountry, cmd.TaxValue = &b.TaxId.Country, &b.TaxId.Value
	}
	if b.Address != nil {
		a := fromAddress(*b.Address)
		cmd.Address = &a
	}
	if b.Contact != nil {
		email := string(b.Contact.Email)
		cmd.ContactEmail, cmd.ContactPhone = &email, &b.Contact.Phone
		website := deref(b.Contact.Website)
		cmd.Website = &website
	}
	if _, err := h.svc.Dispatch(withCommand(ctx, p, "", req.Params.IfMatch), cmd); err != nil {
		return nil, err
	}
	view, err := h.svc.Queries().GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return gen.UpdateOrganization200JSONResponse{
		Headers: gen.UpdateOrganization200ResponseHeaders{ETag: &view.Version},
		Body:    toOrganization(view, myRole(p, orgID)),
	}, nil
}

// ---------------------------------------------------------------- miembros

// ListOrganizationMembers devuelve los miembros activos (solo miembros).
func (h *OrgHandlers) ListOrganizationMembers(ctx context.Context, req gen.ListOrganizationMembersRequestObject) (gen.ListOrganizationMembersResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgRead, authz.OrgResource{ID: orgID, MembersOnly: true}); err != nil {
		return nil, err
	}
	if _, err := h.svc.Queries().GetOrg(ctx, orgID); err != nil {
		return nil, err
	}
	members, err := h.svc.Queries().ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := gen.MemberList{Data: make([]gen.Member, 0, len(members))}
	for _, m := range members {
		out.Data = append(out.Data, toMember(m))
	}
	return gen.ListOrganizationMembers200JSONResponse(out), nil
}

// ChangeMemberRole cambia el rol (O1, O2).
func (h *OrgHandlers) ChangeMemberRole(ctx context.Context, req gen.ChangeMemberRoleRequestObject) (gen.ChangeMemberRoleResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID, userID := uuid.UUID(req.OrgId), uuid.UUID(req.UserId)
	if err := can(p, authz.OrgMemberRole, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	cmd := domain.ChangeMemberRole{OrganizationID: orgID, UserID: userID, Role: string(req.Body.Role)}
	if _, err := h.svc.Dispatch(withCommand(ctx, p, "", 0), cmd); err != nil {
		return nil, memberErr(err)
	}
	members, err := h.svc.Queries().ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.UserID == userID {
			return gen.ChangeMemberRole200JSONResponse(toMember(m)), nil
		}
	}
	return nil, &kernel.NotFoundError{Resource: "member"}
}

// RemoveMember retira un miembro (O2); pierde acceso en el siguiente request.
func (h *OrgHandlers) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgMemberRemove, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	cmd := domain.RemoveMember{OrganizationID: orgID, UserID: uuid.UUID(req.UserId)}
	if _, err := h.svc.Dispatch(withCommand(ctx, p, "", 0), cmd); err != nil {
		return nil, memberErr(err)
	}
	return gen.RemoveMember204Response{}, nil
}

// memberErr: miembro inexistente → 404.
func memberErr(err error) error {
	if errors.Is(err, domain.ErrMemberNotFound) {
		return &kernel.NotFoundError{Resource: "member"}
	}
	return err
}

// ------------------------------------------------------------ invitaciones

// CreateInvitation invita a un email con un rol (Idempotency-Key, PRD-013).
func (h *OrgHandlers) CreateInvitation(ctx context.Context, req gen.CreateInvitationRequestObject) (gen.CreateInvitationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgMemberInvite, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	key := uuid.UUID(req.Params.IdempotencyKey)
	raw := httpserver.RawBodyFrom(ctx)
	hash := sha256.Sum256(raw)
	outcome, err := h.idem.Begin(ctx, p.UserID, key, hash[:])
	if err != nil {
		return nil, err
	}
	if outcome.Replay {
		return replayed{status: outcome.Status, body: outcome.Body}, nil
	}
	cmd := domain.InviteMember{OrganizationID: orgID, Email: string(req.Body.Email), Role: string(req.Body.Role)}
	res, err := h.svc.Dispatch(withCommand(ctx, p, key.String(), 0), cmd)
	if err != nil {
		_ = h.idem.Abort(ctx, p.UserID, key)
		if errors.As(err, new(*kernel.NotFoundError)) {
			return nil, &kernel.NotFoundError{Resource: "organization"}
		}
		return nil, err
	}
	invitations, err := h.svc.Queries().ListInvitations(ctx, orgID, "", h.svc.Now())
	if err != nil {
		return nil, err
	}
	for _, inv := range invitations {
		if inv.ID == res.AggregateID {
			body := toInvitation(inv)
			rawBody, _ := json.Marshal(body)
			if err := h.idem.Commit(ctx, p.UserID, key, http.StatusCreated, rawBody); err != nil {
				return nil, err
			}
			return gen.CreateInvitation201JSONResponse(body), nil
		}
	}
	return nil, errors.New("partnernetwork: invitación no proyectada")
}

// ListInvitations lista las invitaciones de la organización.
func (h *OrgHandlers) ListInvitations(ctx context.Context, req gen.ListInvitationsRequestObject) (gen.ListInvitationsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgMemberInvite, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	invitations, err := h.svc.Queries().ListInvitations(ctx, orgID, status, h.svc.Now())
	if err != nil {
		return nil, err
	}
	out := gen.InvitationList{Data: make([]gen.Invitation, 0, len(invitations))}
	for _, inv := range invitations {
		out.Data = append(out.Data, toInvitation(inv))
	}
	return gen.ListInvitations200JSONResponse(out), nil
}

// RevokeInvitation revoca una invitación pendiente.
func (h *OrgHandlers) RevokeInvitation(ctx context.Context, req gen.RevokeInvitationRequestObject) (gen.RevokeInvitationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgMemberInvite, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	cmd := domain.RevokeInvitation{OrganizationID: orgID, InvitationID: uuid.UUID(req.InvitationId)}
	if _, err := h.svc.Dispatch(withCommand(ctx, p, "", 0), cmd); err != nil {
		if errors.Is(err, domain.ErrInvitationNotFound) {
			return nil, &kernel.NotFoundError{Resource: "invitation"}
		}
		return nil, err
	}
	return gen.RevokeInvitation204Response{}, nil
}

// ResendInvitation rota el enlace y reenvía el email (máx 3 en 24 h).
func (h *OrgHandlers) ResendInvitation(ctx context.Context, req gen.ResendInvitationRequestObject) (gen.ResendInvitationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgID := uuid.UUID(req.OrgId)
	if err := can(p, authz.OrgMemberInvite, authz.OrgResource{ID: orgID}); err != nil {
		return nil, err
	}
	if err := h.svc.ResendInvitation(ctx, orgID, uuid.UUID(req.InvitationId)); err != nil {
		if errors.Is(err, app.ErrResendLimit) {
			return nil, &problem.DirectError{P: problem.New(problem.InvitationResendLimit, "Límite de reenvíos",
				"Máximo 3 reenvíos por invitación en 24 horas.")}
		}
		return nil, err
	}
	return gen.ResendInvitation204Response{}, nil
}

// GetInvitation previsualiza una invitación por token (404 sin distinguir).
func (h *OrgHandlers) GetInvitation(ctx context.Context, req gen.GetInvitationRequestObject) (gen.GetInvitationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.OrgInvitationRead, nil); err != nil {
		return nil, err
	}
	inv, err := h.svc.Queries().InvitationByTokenHash(ctx, kernel.HashToken(req.Token), h.svc.Now())
	if err != nil {
		return nil, err
	}
	org, err := h.svc.Queries().GetOrg(ctx, inv.OrganizationID)
	if err != nil {
		return nil, err
	}
	return gen.GetInvitation200JSONResponse{
		Organization: gen.OrgSummary{
			Id: openapi_types.UUID(org.ID), Type: gen.OrganizationType(org.Type), DisplayName: org.DisplayName,
			City: org.City, Country: org.Country, Status: gen.OrgStatus(org.Status),
		},
		Role: gen.OrgRole(inv.Role), EmailMasked: kernel.Email(inv.Email).Masked(), ExpiresAt: inv.ExpiresAt,
	}, nil
}

// AcceptInvitation acepta con la sesión del email invitado y devuelve Me con
// la membresía nueva (caché invalidada → recarga).
func (h *OrgHandlers) AcceptInvitation(ctx context.Context, req gen.AcceptInvitationRequestObject) (gen.AcceptInvitationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := can(p, authz.OrgInvitationJoin, nil); err != nil {
		return nil, err
	}
	if _, err := h.svc.AcceptInvitationToken(withCommand(ctx, p, "", 0), req.Token); err != nil {
		return nil, err
	}
	if h.memberships != nil {
		ms, err := h.memberships.Memberships(ctx, p.UserID)
		if err != nil {
			return nil, err
		}
		p.Memberships = ms
	}
	if h.me == nil {
		return nil, errors.New("partnernetwork: MeRenderer no configurado")
	}
	me, err := h.me.RenderMe(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.AcceptInvitation200JSONResponse(me), nil
}

// ----------------------------------------------------------------- mapeos

func adminRole(orgType string) string {
	if orgType == string(domain.OrgTypeClinic) {
		return string(domain.RoleClinicAdmin)
	}
	return string(domain.RolePharmacyAdmin)
}

func toOrganization(v app.OrgView, role *gen.OrgRole) gen.Organization {
	o := gen.Organization{
		Id: openapi_types.UUID(v.ID), Version: v.Version, Type: gen.OrganizationType(v.Type),
		LegalName: v.LegalName, DisplayName: v.DisplayName,
		TaxId: gen.TaxId{Country: v.TaxID.Country, Value: v.TaxID.Value},
		Address: gen.Address{
			Country: v.Address.Country, Region: v.Address.Region, City: v.Address.City,
			Line1: v.Address.Line1, Line2: v.Address.Line2, PostalCode: v.Address.PostalCode,
		},
		Contact:    gen.OrgContact{Email: openapi_types.Email(v.Contact.Email), Phone: v.Contact.Phone},
		PublicCode: v.PublicCode, Status: gen.OrgStatus(v.Status), MyRole: role,
		MemberCount: &v.MemberCount, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
	if v.Contact.Website != "" {
		o.Contact.Website = &v.Contact.Website
	}
	if v.StatusReason != "" {
		o.StatusReason = &v.StatusReason
	}
	return o
}

func toSummary(s app.OrgSummary) gen.OrgSummary {
	return gen.OrgSummary{
		Id: openapi_types.UUID(s.ID), Type: gen.OrganizationType(s.Type), DisplayName: s.DisplayName,
		City: s.City, Country: s.Country, Status: gen.OrgStatus(s.Status),
	}
}

func toMember(m app.MemberView) gen.Member {
	return gen.Member{
		UserId: openapi_types.UUID(m.UserID), Email: m.Email, DisplayName: m.DisplayName,
		Role: gen.OrgRole(m.Role), Status: gen.MemberStatus(m.Status), JoinedAt: m.JoinedAt,
	}
}

// toInvitation: el email se devuelve completo a los admins de la org (lo
// escribieron ellos); enmascarado solo en la previsualización pública.
func toInvitation(i app.InvitationView) gen.Invitation {
	return gen.Invitation{
		Id: openapi_types.UUID(i.ID), Email: i.Email, Role: gen.OrgRole(i.Role),
		Status: gen.InvitationStatus(i.Status), ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
	}
}

func fromAddress(a gen.Address) domain.AddressInput {
	return domain.AddressInput{
		Country: a.Country, Region: a.Region, City: a.City, Line1: a.Line1, Line2: a.Line2, PostalCode: a.PostalCode,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// replayed reproduce la respuesta almacenada con Idempotent-Replayed: true
// (ARQ-008) para los POST de creación de este módulo.
type replayed struct {
	status int
	body   []byte
}

func (r replayed) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(r.status)
	_, err := w.Write(r.body)
	return err
}

func (r replayed) VisitCreateOrganizationResponse(w http.ResponseWriter) error { return r.write(w) }
func (r replayed) VisitCreateInvitationResponse(w http.ResponseWriter) error   { return r.write(w) }

var (
	_ gen.CreateOrganizationResponseObject = replayed{}
	_ gen.CreateInvitationResponseObject   = replayed{}
)
