// Package http implementa las operaciones `me` y `legal` del contrato OpenAPI
// (PP-F1-API-003) sobre el strict server generado. Solo traduce: el esquema
// valida la forma, app normaliza a VOs y decide, aquí se mapea a DTOs.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Handlers implementa las operaciones de identity del contrato.
type Handlers struct {
	svc        *app.Service
	ipHashSalt string
	now        func() time.Time
}

// NewHandlers construye los handlers.
func NewHandlers(svc *app.Service, ipHashSalt string) *Handlers {
	return &Handlers{svc: svc, ipHashSalt: ipHashSalt, now: time.Now}
}

// errUnauthenticated no debería ocurrir: el guard rechaza antes. Defensa en profundidad.
var errUnauthenticated = errors.New("sin principal en el contexto")

func (h *Handlers) principal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return auth.Principal{}, &kernel.ForbiddenError{Reason: errUnauthenticated.Error()}
	}
	return p, nil
}

func (h *Handlers) requestMeta(ctx context.Context) (*uuid.UUID, []byte) {
	info := reqctx.From(ctx)
	var corr *uuid.UUID
	if id, err := uuid.Parse(info.CorrelationID); err == nil {
		corr = &id
	}
	var ipHash []byte
	if info.RemoteAddr != "" && h.ipHashSalt != "" {
		ipHash = audit.HashIP(h.ipHashSalt, info.RemoteAddr, h.now())
	}
	return corr, ipHash
}

// GetLegalCurrent devuelve las versiones vigentes (público).
func (h *Handlers) GetLegalCurrent(ctx context.Context, _ gen.GetLegalCurrentRequestObject) (gen.GetLegalCurrentResponseObject, error) {
	cur, err := h.svc.LegalCurrent(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetLegalCurrent200JSONResponse{Body: gen.LegalCurrent{
		TermsVersion: cur.Terms, PrivacyVersion: cur.Privacy, TermsUrl: cur.TermsURL, PrivacyUrl: cur.PrivacyURL,
	}}, nil
}

// GetMe devuelve la cuenta propia.
func (h *Handlers) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.GetMe(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.GetMe200JSONResponse{Body: toMe(view, p)}, nil
}

// RenderMe construye el DTO Me para otros módulos (aceptar una invitación
// devuelve Me con la membresía nueva). GetMe evalúa me:read.
func (h *Handlers) RenderMe(ctx context.Context, p auth.Principal) (gen.Me, error) {
	view, err := h.svc.GetMe(ctx, p)
	if err != nil {
		return gen.Me{}, err
	}
	return toMe(view, p), nil
}

// CompleteOnboarding ejecuta el onboarding.
func (h *Handlers) CompleteOnboarding(ctx context.Context, req gen.CompleteOnboardingRequestObject) (gen.CompleteOnboardingResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	corr, ipHash := h.requestMeta(ctx)
	b := req.Body
	view, err := h.svc.CompleteOnboarding(ctx, p, app.CompleteOnboardingCommand{
		DisplayName: b.DisplayName, Country: b.Country, Locale: string(b.Locale), Phone: b.Phone,
		TermsVersion: b.AcceptTermsVersion, PrivacyVersion: b.AcceptPrivacyVersion,
		CorrelationID: corr, IPHash: ipHash,
	})
	if err != nil {
		return nil, err
	}
	return gen.CompleteOnboarding200JSONResponse{Body: toMe(view, p)}, nil
}

// UpdateMe edita el perfil. Semántica JSON Merge Patch: `null` vacía un campo
// opcional; ausente = sin cambio. Los nulls explícitos se detectan en el cuerpo
// crudo porque el DTO generado no distingue null de ausente.
func (h *Handlers) UpdateMe(ctx context.Context, req gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	corr, ipHash := h.requestMeta(ctx)
	nulls := explicitNulls(httpserver.RawBodyFrom(ctx))
	b := req.Body
	cmd := app.UpdateProfileCommand{
		DisplayName: b.DisplayName, Country: b.Country, Phone: b.Phone,
		ClearCountry: nulls["country"], ClearPhone: nulls["phone"],
		ClearAddress: nulls["address"], ClearEmergencyContact: nulls["emergency_contact"],
		CorrelationID: corr, IPHash: ipHash,
	}
	if b.Locale != nil {
		l := string(*b.Locale)
		cmd.Locale = &l
	}
	if b.Address != nil {
		cmd.Address = fromAddress(*b.Address)
	}
	if b.EmergencyContact != nil {
		cmd.EmergencyContact = fromContact(*b.EmergencyContact)
	}
	view, err := h.svc.UpdateProfile(ctx, p, cmd)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse{Body: toMe(view, p)}, nil
}

// AcceptConsent registra el re-consentimiento.
func (h *Handlers) AcceptConsent(ctx context.Context, req gen.AcceptConsentRequestObject) (gen.AcceptConsentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	corr, ipHash := h.requestMeta(ctx)
	if err := h.svc.AcceptConsent(ctx, p, app.AcceptConsentCommand{
		TermsVersion: req.Body.TermsVersion, PrivacyVersion: req.Body.PrivacyVersion, CorrelationID: corr, IPHash: ipHash,
	}); err != nil {
		return nil, err
	}
	return gen.AcceptConsent204Response{}, nil
}

// explicitNulls devuelve las claves de primer nivel cuyo valor JSON es null.
func explicitNulls(raw []byte) map[string]bool {
	out := map[string]bool{}
	if len(raw) == 0 {
		return out
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}
	for k, v := range m {
		if strings.TrimSpace(string(v)) == "null" {
			out[k] = true
		}
	}
	return out
}

func toMe(v app.MeView, p auth.Principal) gen.Me {
	a := v.Account
	me := gen.Me{
		Id:                 openapi_types.UUID(a.ID),
		Email:              openapi_types.Email(a.Email.String()),
		EmailVerified:      a.EmailVerified,
		DisplayName:        a.DisplayName,
		Locale:             gen.Locale(a.Locale),
		GlobalRoles:        a.GlobalRoles,
		Memberships:        toMemberships(p.Memberships),
		OnboardingRequired: !a.IsOnboarded(),
		ConsentOutdated:    !a.ConsentCurrent(v.Current),
		Status:             gen.MeStatus(a.Status),
	}
	if a.Country != nil {
		s := a.Country.String()
		me.Country = &s
	}
	if a.Phone != nil {
		s := a.Phone.String()
		me.Phone = &s
	}
	if a.Address != nil {
		me.Address = toAddress(*a.Address)
	}
	if a.EmergencyContact != nil {
		me.EmergencyContact = toContact(*a.EmergencyContact)
	}
	if a.Consent != nil {
		me.Consent = &gen.Consent{TermsVersion: a.Consent.TermsVersion, PrivacyVersion: a.Consent.PrivacyVersion, AcceptedAt: a.Consent.AcceptedAt}
	}
	return me
}

func toMemberships(ms []auth.Membership) []gen.Membership {
	out := make([]gen.Membership, 0, len(ms))
	for _, m := range ms {
		out = append(out, gen.Membership{
			OrganizationId: openapi_types.UUID(m.OrganizationID), DisplayName: m.DisplayName, Type: gen.MembershipType(m.OrgType),
			Role: gen.MembershipRole(m.Role), OrgStatus: gen.MembershipOrgStatus(m.OrgStatus),
		})
	}
	return out
}

func toAddress(a kernel.Address) *gen.Address {
	out := &gen.Address{Country: a.Country.String(), City: a.City, Line1: a.Line1}
	out.Region = optional(a.Region)
	out.Line2 = optional(a.Line2)
	out.PostalCode = optional(a.PostalCode)
	return out
}

func fromAddress(a gen.Address) *kernel.Address {
	return &kernel.Address{
		Country: kernel.CountryCode(a.Country), Region: deref(a.Region), City: a.City,
		Line1: a.Line1, Line2: deref(a.Line2), PostalCode: deref(a.PostalCode),
	}
}

func toContact(c kernel.Contact) *gen.EmergencyContact {
	return &gen.EmergencyContact{Name: c.Name, Phone: c.Phone.String(), Relationship: optional(c.Relationship)}
}

func fromContact(c gen.EmergencyContact) *kernel.Contact {
	return &kernel.Contact{Name: c.Name, Phone: kernel.Phone(c.Phone), Relationship: deref(c.Relationship)}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// compile-time: los handlers cubren las operaciones de identity del contrato.
var _ interface {
	GetLegalCurrent(context.Context, gen.GetLegalCurrentRequestObject) (gen.GetLegalCurrentResponseObject, error)
	GetMe(context.Context, gen.GetMeRequestObject) (gen.GetMeResponseObject, error)
	UpdateMe(context.Context, gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error)
	CompleteOnboarding(context.Context, gen.CompleteOnboardingRequestObject) (gen.CompleteOnboardingResponseObject, error)
	AcceptConsent(context.Context, gen.AcceptConsentRequestObject) (gen.AcceptConsentResponseObject, error)
} = (*Handlers)(nil)
