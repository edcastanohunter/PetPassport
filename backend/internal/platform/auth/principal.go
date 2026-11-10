// Package auth valida tokens OIDC (Keycloak) y construye el Principal del
// request. No conoce Keycloak más allá del estándar (ADR-003). Ver
// docs/phase-1/04-iam-keycloak.md §7–§8.
package auth

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Claims son los claims relevantes del access token.
type Claims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Name              string   `json:"name"`
	GivenName         string   `json:"given_name"`
	FamilyName        string   `json:"family_name"`
	PreferredUsername string   `json:"preferred_username"`
	Locale            string   `json:"locale"`
	AuthorizedParty   string   `json:"azp"`
	Type              string   `json:"typ"`
	SessionID         string   `json:"sid"`
	PartnerOrgID      string   `json:"partner_org_id,omitempty"`
	Scope             string   `json:"scope,omitempty"`
	IssuedAt          int64    `json:"iat"`
	NotBefore         int64    `json:"nbf"`
	RealmAccess       struct { //nolint:revive // forma del token de Keycloak
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// Roles devuelve los roles de realm.
func (c Claims) Roles() []string { return c.RealmAccess.Roles }

// DisplayName deriva un nombre para mostrar.
func (c Claims) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.GivenName != "" || c.FamilyName != "" {
		return trimJoin(c.GivenName, c.FamilyName)
	}
	return ""
}

func trimJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

// Membership es la pertenencia a una organización con su rol (se carga desde
// el read model de Partner Network, IAM-008).
type Membership struct {
	OrganizationID uuid.UUID
	DisplayName    string
	OrgType        string
	Role           string
	OrgStatus      string
}

// AccountState es lo que Identity devuelve al aprovisionar/consultar la cuenta.
type AccountState struct {
	Suspended           bool
	OnboardingCompleted bool
	ConsentCurrent      bool
}

// AccountResolver es el puerto hacia Identity (aprovisionamiento JIT, IAM-009).
type AccountResolver interface {
	Resolve(ctx context.Context, c Claims) (AccountState, error)
}

// MembershipLoader es el puerto hacia Partner Network (IAM-008).
type MembershipLoader interface {
	Memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error)
}

// Principal es el actor autenticado del request.
type Principal struct {
	UserID        uuid.UUID
	Email         string
	EmailVerified bool
	GlobalRoles   []string
	Memberships   []Membership
	SessionID     string
	Account       AccountState
	Impersonator  *uuid.UUID // F5
	AuthTime      time.Time
}

// HasRole indica si el principal tiene un rol global.
func (p Principal) HasRole(role string) bool { return slices.Contains(p.GlobalRoles, role) }

type principalKey struct{}
type partnerPrincipalKey struct{}

// PartnerClaims son los claims esperados en el access token de partner OAuth2 (ARQ-005, PTN-002).
type PartnerClaims struct {
	Subject         string `json:"sub"`
	AuthorizedParty string `json:"azp"`
	PartnerOrgID    string `json:"partner_org_id"`
	Scope           string `json:"scope"`
	Type            string `json:"typ"`
	IssuedAt        int64  `json:"iat"`
	NotBefore       int64  `json:"nbf"`
}

// PartnerPrincipal representa la identidad autenticada de un partner B2B (ARQ-005, SEC-002).
type PartnerPrincipal struct {
	OrgID    uuid.UUID
	ClientID string
	Scopes   []string
}

// HasScope verifica si el principal de partner posee un scope específico.
func (p PartnerPrincipal) HasScope(scope string) bool {
	return slices.Contains(p.Scopes, scope)
}

// WithPrincipal guarda el principal en el contexto.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom recupera el principal; ok=false si el request no está autenticado.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithPartnerPrincipal guarda el principal de partner en el contexto.
func WithPartnerPrincipal(ctx context.Context, p PartnerPrincipal) context.Context {
	return context.WithValue(ctx, partnerPrincipalKey{}, p)
}

// PartnerPrincipalFrom recupera el principal de partner; ok=false si no está autenticado como partner.
func PartnerPrincipalFrom(ctx context.Context) (PartnerPrincipal, bool) {
	p, ok := ctx.Value(partnerPrincipalKey{}).(PartnerPrincipal)
	return p, ok
}
