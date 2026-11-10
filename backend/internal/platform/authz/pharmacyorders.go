package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Acciones de Pharmacy Orders F4 (PP-F4-SEC-001, API-005, API-006, API-007).
const (
	QuoteRequest     Action = "quote:request"
	QuoteRead        Action = "quote:read"
	PharmacyQuote    Action = "pharmacy:quote"
	PharmacyValidate Action = "pharmacy:validate"
	PharmacyOrders   Action = "pharmacy:orders"
	PharmacyAdmin    Action = "pharmacy:admin"
	OrderRead        Action = "order:read"
	OrderCancel      Action = "order:cancel"
	OrderRate        Action = "order:rate"
)

// QuoteRequestResource es la vista de autorización de una solicitud de cotización.
type QuoteRequestResource struct {
	ID         uuid.UUID
	GuardianID uuid.UUID
}

// OrderResource es la vista de autorización de un pedido.
type OrderResource struct {
	ID            uuid.UUID
	GuardianID    uuid.UUID
	PharmacyOrgID uuid.UUID
}

var pharmacyStaffRoles = map[string]bool{
	"pharmacy_admin": true,
	"pharmacist":     true,
}

func init() {
	Register(QuoteRequest, quoteRequestPolicy)
	Register(QuoteRead, quoteReadPolicy)
	Register(PharmacyQuote, pharmacyQuotePolicy)
	Register(PharmacyValidate, pharmacyValidatePolicy)
	Register(PharmacyOrders, pharmacyOrdersPolicy)
	Register(PharmacyAdmin, pharmacyAdminPolicy)
	Register(OrderRead, orderReadPolicy)
	Register(OrderCancel, orderCancelPolicy)
	Register(OrderRate, orderRatePolicy)
}

func checkPharmacyMembership(p auth.Principal, orgID uuid.UUID, allowedRoles map[string]bool) Decision {
	m, ok := membership(p, orgID)
	if !ok || m.OrgType != "pharmacy" {
		return deny("pharmacy membership required")
	}
	if m.OrgStatus == "suspended" {
		return deny(ReasonOrgSuspended)
	}
	if m.OrgStatus != "active" {
		return deny("organization not active")
	}
	if !allowedRoles[m.Role] {
		return deny("insufficient pharmacy role")
	}
	return allow
}

// quoteRequestPolicy: solo el tutor (G). SuperAdmin no cotiza según matriz SEC-001.
func quoteRequestPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return deny("super_admin cannot request quotes")
	}
	if res == nil {
		return allow
	}
	if qr, ok := res.(QuoteRequestResource); ok {
		if qr.GuardianID != uuid.Nil && p.UserID != qr.GuardianID {
			return deny("only guardian can manage quote request")
		}
		return allow
	}
	return deny("invalid quote request resource")
}

// quoteReadPolicy: el tutor de la solicitud o super_admin.
func quoteReadPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if res == nil {
		return allow
	}
	if qr, ok := res.(QuoteRequestResource); ok {
		if qr.GuardianID != uuid.Nil && p.UserID != qr.GuardianID {
			return deny("only guardian can read quote request")
		}
		return allow
	}
	return deny("invalid quote request resource")
}

// pharmacyQuotePolicy: pharmacy_admin o pharmacist de la org de farmacia activa.
func pharmacyQuotePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		return checkPharmacyMembership(p, o.ID, pharmacyStaffRoles)
	}
	return deny("org resource required")
}

// pharmacyValidatePolicy: pharmacy_admin o pharmacist de la org de farmacia activa.
func pharmacyValidatePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		return checkPharmacyMembership(p, o.ID, pharmacyStaffRoles)
	}
	return deny("org resource required")
}

// pharmacyOrdersPolicy: pharmacy_admin o pharmacist de la org de farmacia activa.
func pharmacyOrdersPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		return checkPharmacyMembership(p, o.ID, pharmacyStaffRoles)
	}
	if ord, ok := res.(OrderResource); ok && ord.PharmacyOrgID != uuid.Nil {
		return checkPharmacyMembership(p, ord.PharmacyOrgID, pharmacyStaffRoles)
	}
	return deny("org or order resource required")
}

// pharmacyAdminPolicy: solo pharmacy_admin de la org de farmacia activa.
func pharmacyAdminPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if o, ok := res.(OrgResource); ok && o.ID != uuid.Nil {
		return checkPharmacyMembership(p, o.ID, map[string]bool{"pharmacy_admin": true})
	}
	return deny("org resource required")
}

// orderReadPolicy: tutor de la orden o super_admin.
func orderReadPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if res == nil {
		return allow
	}
	if ord, ok := res.(OrderResource); ok {
		if ord.GuardianID != uuid.Nil && p.UserID != ord.GuardianID {
			return deny("only guardian can read order")
		}
		return allow
	}
	return deny("invalid order resource")
}

// orderCancelPolicy: tutor de la orden o super_admin.
func orderCancelPolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return Decision{Allow: true, Reason: "super_admin"}
	}
	if ord, ok := res.(OrderResource); ok {
		if ord.GuardianID != uuid.Nil && p.UserID != ord.GuardianID {
			return deny("only guardian can cancel order")
		}
		return allow
	}
	return deny("invalid order resource")
}

// orderRatePolicy: tutor de la orden (G) exclusivamente.
func orderRatePolicy(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	if isSuperAdmin(p) {
		return deny("super_admin cannot rate orders")
	}
	if ord, ok := res.(OrderResource); ok {
		if ord.GuardianID != uuid.Nil && p.UserID != ord.GuardianID {
			return deny("only guardian can rate order")
		}
		return allow
	}
	return deny("invalid order resource")
}
