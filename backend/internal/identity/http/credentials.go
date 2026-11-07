package http

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CredentialHandlers implementa los endpoints de credenciales profesionales (PP-F4-API-001).
type CredentialHandlers struct {
	svc  *app.CredentialService
	base *Handlers
}

// NewCredentialHandlers construye los handlers de credenciales.
func NewCredentialHandlers(svc *app.CredentialService, base *Handlers) *CredentialHandlers {
	return &CredentialHandlers{svc: svc, base: base}
}

// ListMyCredentials lista las credenciales profesionales registradas por el usuario autenticado.
func (h *CredentialHandlers) ListMyCredentials(ctx context.Context, _ gen.ListMyCredentialsRequestObject) (gen.ListMyCredentialsResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeRead, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	creds, err := h.svc.ListMyCredentials(ctx, p)
	if err != nil {
		return nil, err
	}

	data := make([]gen.ProfessionalCredential, 0, len(creds))
	for _, c := range creds {
		data = append(data, toGenCredential(c))
	}

	return gen.ListMyCredentials200JSONResponse{Data: data}, nil
}

// SubmitCredential registra una credencial profesional para moderación.
func (h *CredentialHandlers) SubmitCredential(ctx context.Context, req gen.SubmitCredentialRequestObject) (gen.SubmitCredentialResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	body := req.Body
	if body == nil {
		ve := &kernel.ValidationError{}
		ve.Add("body", "REQUIRED")
		return nil, ve
	}

	issuedAt, err := time.Parse("2006-01-02", body.IssuedAt.String())
	if err != nil {
		ve := &kernel.ValidationError{}
		ve.Add("issued_at", "INVALID_DATE")
		return nil, ve
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil {
		t, err := time.Parse("2006-01-02", body.ExpiresAt.String())
		if err != nil {
			ve := &kernel.ValidationError{}
			ve.Add("expires_at", "INVALID_DATE")
			return nil, ve
		}
		expiresAt = &t
	}

	cmd := app.SubmitCredentialCmd{
		Country:           body.Country,
		Kind:              body.Kind,
		RegistryNumber:    body.RegistryNumber,
		Issuer:            body.Issuer,
		IssuedAt:          issuedAt,
		ExpiresAt:         expiresAt,
		EvidenceObjectKey: body.EvidenceObjectKey,
	}

	cred, err := h.svc.SubmitCredential(ctx, p, cmd)
	if err != nil {
		return nil, err
	}

	return gen.SubmitCredential201JSONResponse(toGenCredential(*cred)), nil
}

// DeleteMyCredential elimina una credencial profesional en estado pending o rejected.
func (h *CredentialHandlers) DeleteMyCredential(ctx context.Context, req gen.DeleteMyCredentialRequestObject) (gen.DeleteMyCredentialResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.MeUpdate, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if err := h.svc.DeleteCredential(ctx, p, req.CredentialId); err != nil {
		return nil, err
	}

	return gen.DeleteMyCredential204Response{}, nil
}

// ListOrgMembersCredentials lista el estado de verificación de las credenciales de los veterinarios de la org.
func (h *CredentialHandlers) ListOrgMembersCredentials(ctx context.Context, req gen.ListOrgMembersCredentialsRequestObject) (gen.ListOrgMembersCredentialsResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.OrgRead, authz.OrgResource{ID: req.OrgId, MembersOnly: true}); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	summaries, err := h.svc.ListByOrg(ctx, p, req.OrgId)
	if err != nil {
		return nil, err
	}

	data := make([]gen.MemberCredentialSummary, 0, len(summaries))
	for _, s := range summaries {
		var exp *time.Time
		if s.ExpiresAt != nil {
			exp = s.ExpiresAt
		}
		data = append(data, gen.MemberCredentialSummary{
			UserId:         s.UserID,
			Country:        s.Country,
			Kind:           s.Kind,
			RegistryNumber: s.RegistryNumber,
			Issuer:         s.Issuer,
			Status:         s.Status,
			ExpiresAt:      exp,
		})
	}

	return gen.ListOrgMembersCredentials200JSONResponse{Data: data}, nil
}

// ListAdminCredentials lista credenciales en la cola de moderación de super_admin.
func (h *CredentialHandlers) ListAdminCredentials(ctx context.Context, req gen.ListAdminCredentialsRequestObject) (gen.ListAdminCredentialsResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}

	creds, err := h.svc.AdminListCredentials(ctx, p, status)
	if err != nil {
		return nil, err
	}

	data := make([]gen.ProfessionalCredential, 0, len(creds))
	for _, c := range creds {
		data = append(data, toGenCredential(c))
	}

	return gen.ListAdminCredentials200JSONResponse{Data: data}, nil
}

// VerifyAdminCredential aprueba una credencial profesional.
func (h *CredentialHandlers) VerifyAdminCredential(ctx context.Context, req gen.VerifyAdminCredentialRequestObject) (gen.VerifyAdminCredentialResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = *req.Body.Reason
	}

	cred, err := h.svc.AdminVerifyCredential(ctx, p, req.CredentialId, reason)
	if err != nil {
		return nil, err
	}

	return gen.VerifyAdminCredential200JSONResponse(toGenCredential(*cred)), nil
}

// RejectAdminCredential rechaza una credencial profesional.
func (h *CredentialHandlers) RejectAdminCredential(ctx context.Context, req gen.RejectAdminCredentialRequestObject) (gen.RejectAdminCredentialResponseObject, error) {
	p, err := h.base.principal(ctx)
	if err != nil {
		return nil, err
	}
	if d := authz.Can(p, authz.AdminUserList, nil); !d.Allow {
		return nil, &kernel.ForbiddenError{Reason: d.Reason}
	}

	if req.Body == nil || req.Body.Reason == "" {
		ve := &kernel.ValidationError{}
		ve.Add("reason", "REQUIRED")
		return nil, ve
	}

	cred, err := h.svc.AdminRejectCredential(ctx, p, req.CredentialId, req.Body.Reason)
	if err != nil {
		return nil, err
	}

	return gen.RejectAdminCredential200JSONResponse(toGenCredential(*cred)), nil
}

func toGenCredential(c domain.ProfessionalCredential) gen.ProfessionalCredential {
	var exp *openapi_types.Date
	if c.ExpiresAt != nil {
		d := openapi_types.Date{Time: *c.ExpiresAt}
		exp = &d
	}
	return gen.ProfessionalCredential{
		Id:                c.ID,
		UserId:            c.UserID,
		Country:           c.Country,
		Kind:              c.Kind,
		RegistryNumber:    c.RegistryNumber,
		Issuer:            c.Issuer,
		IssuedAt:          openapi_types.Date{Time: c.IssuedAt},
		ExpiresAt:         exp,
		Status:            gen.ProfessionalCredentialStatus(c.Status),
		VerifiedAt:        c.ReviewedAt,
		EvidenceObjectKey: c.EvidenceObjectKey,
		CreatedAt:         c.CreatedAt,
	}
}
