package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Acciones de facturación F5 (PP-F5-API-001..003, 006).
const (
	BillingRead     Action = "billing:read"
	BillingManage   Action = "billing:manage"
	AdminPlansWrite Action = "admin:plans:write"
	// Administración de facturación de SuperAdmin (PP-F5-API-007).
	AdminBillingRead  Action = "admin:billing:read"
	AdminBillingWrite Action = "admin:billing:write"
)

// BillingResource es la vista de autorización para operaciones de facturación.
// Una cuenta se autoriza por las organizaciones que agrupa (OrgIDs): sin
// ninguna organización conocida solo pasan super_admin y billing_admin, de modo
// que conocer el id de una cuenta ajena no da acceso a ella.
type BillingResource struct {
	OrganizationID uuid.UUID
	AccountID      uuid.UUID
	OrgIDs         []uuid.UUID
}

func init() {
	Register(BillingRead, billingReadPolicy)
	Register(BillingManage, billingManagePolicy)
	Register(AdminPlansWrite, superAdminOnly)
	Register(AdminBillingRead, superAdminOnly)
	Register(AdminBillingWrite, superAdminOnly)
}

func hasBillingAuthority(p auth.Principal, orgID uuid.UUID) bool {
	if isSuperAdmin(p) || p.HasRole("billing_admin") {
		return true
	}
	if orgID == uuid.Nil {
		// Si no hay org específica, basta ser admin de alguna org no suspendida
		for _, m := range p.Memberships {
			if (m.Role == "clinic_admin" || m.Role == "pharmacy_admin") && m.OrgStatus != "suspended" {
				return true
			}
		}
		return false
	}
	m, ok := membership(p, orgID)
	if !ok {
		return false
	}
	if m.OrgStatus == "suspended" {
		return false
	}
	return m.Role == "clinic_admin" || m.Role == "pharmacy_admin"
}

func billingReadPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) || p.HasRole("billing_admin") {
		return allow
	}
	switch r := res.(type) {
	case OrgResource:
		if hasBillingAuthority(p, r.ID) {
			return allow
		}
		return deny("not authorized for organization billing")
	case BillingResource:
		if r.OrganizationID != uuid.Nil && hasBillingAuthority(p, r.OrganizationID) {
			return allow
		}
		for _, orgID := range r.OrgIDs {
			if hasBillingAuthority(p, orgID) {
				return allow
			}
		}
		return deny("not authorized for billing account")
	case nil:
		if hasBillingAuthority(p, uuid.Nil) {
			return allow
		}
		return deny("admin role required")
	default:
		return deny("unsupported billing resource")
	}
}

func billingManagePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) || p.HasRole("billing_admin") {
		return allow
	}
	switch r := res.(type) {
	case OrgResource:
		if hasBillingAuthority(p, r.ID) {
			return allow
		}
		return deny("not organization admin")
	case BillingResource:
		if r.OrganizationID != uuid.Nil && hasBillingAuthority(p, r.OrganizationID) {
			return allow
		}
		for _, orgID := range r.OrgIDs {
			if hasBillingAuthority(p, orgID) {
				return allow
			}
		}
		return deny("not authorized for billing account")
	case nil:
		if hasBillingAuthority(p, uuid.Nil) {
			return allow
		}
		return deny("admin role required")
	default:
		return deny("unsupported billing resource")
	}
}
