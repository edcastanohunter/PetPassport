package authz

import (
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

func TestPharmacyOrders_SuspendedOrg(t *testing.T) {
	pAdmin := auth.Principal{
		UserID: uuid.New(),
		Memberships: []auth.Membership{
			{OrganizationID: sPharma, OrgType: "pharmacy", Role: "pharmacy_admin", OrgStatus: "suspended"},
		},
	}
	d := Can(pAdmin, PharmacyQuote, OrgResource{ID: sPharma})
	if d.Allow || d.Reason != ReasonOrgSuspended {
		t.Fatalf("expected ReasonOrgSuspended, got %+v", d)
	}

	d = Can(pAdmin, PharmacyOrders, OrgResource{ID: sPharma})
	if d.Allow || d.Reason != ReasonOrgSuspended {
		t.Fatalf("expected ReasonOrgSuspended, got %+v", d)
	}
}

func TestPharmacyOrders_PharmacistDeniedPharmacyAdmin(t *testing.T) {
	pharmacist := auth.Principal{
		UserID: uuid.New(),
		Memberships: []auth.Membership{
			{OrganizationID: sPharma, OrgType: "pharmacy", Role: "pharmacist", OrgStatus: "active"},
		},
	}
	d := Can(pharmacist, PharmacyAdmin, OrgResource{ID: sPharma})
	if d.Allow {
		t.Fatalf("pharmacist should not have pharmacy:admin access")
	}
}

func TestPharmacyOrders_OrderResourceChecks(t *testing.T) {
	guardianID := uuid.New()
	strangerID := uuid.New()
	tutorPrincipal := auth.Principal{UserID: guardianID}
	strangerPrincipal := auth.Principal{UserID: strangerID}

	ordRes := OrderResource{
		ID:            uuid.New(),
		GuardianID:    guardianID,
		PharmacyOrgID: sPharma,
	}

	if !Can(tutorPrincipal, OrderRead, ordRes).Allow {
		t.Errorf("guardian should read order")
	}
	if Can(strangerPrincipal, OrderRead, ordRes).Allow {
		t.Errorf("stranger should not read order")
	}
	if !Can(tutorPrincipal, OrderCancel, ordRes).Allow {
		t.Errorf("guardian should cancel order")
	}
	if Can(strangerPrincipal, OrderCancel, ordRes).Allow {
		t.Errorf("stranger should not cancel order")
	}
}
