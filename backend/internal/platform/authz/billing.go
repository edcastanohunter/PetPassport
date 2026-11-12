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
)

// BillingResource es la vista de autorización para operaciones de facturación.
type BillingResource struct {
	OrganizationID uuid.UUID
	AccountID      uuid.UUID
	OrgIDs         []uuid.UUID
}

func init() {
	Register(BillingRead, billingReadPolicy)
	Register(BillingManage, billingManagePolicy)
	Register(AdminPlansWrite, superAdminOnly)
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
		if r.OrganizationID == uuid.Nil && len(r.OrgIDs) == 0 {
			if hasBillingAuthority(p, uuid.Nil) {
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
		if r.OrganizationID == uuid.Nil && len(r.OrgIDs) == 0 {
			if hasBillingAuthority(p, uuid.Nil) {
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
