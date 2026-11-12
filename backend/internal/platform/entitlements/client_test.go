package entitlements

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type dummyReader struct {
	sets       map[uuid.UUID]kernel.EntitlementSet
	orgTypes   map[uuid.UUID]string
	verifiedAt map[uuid.UUID]time.Time
}

func (r *dummyReader) GetEntitlements(ctx context.Context, orgID uuid.UUID) (*kernel.EntitlementSet, error) {
	if s, ok := r.sets[orgID]; ok {
		return &s, nil
	}
	return nil, nil
}

func (r *dummyReader) GetOrgTypeAndVerifiedAt(ctx context.Context, orgID uuid.UUID) (string, *time.Time, error) {
	t, ok := r.orgTypes[orgID]
	if !ok {
		t = "clinic"
	}
	v, okV := r.verifiedAt[orgID]
	if okV {
		return t, &v, nil
	}
	return t, nil, nil
}

func TestClient_ResolutionAndDefaults(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reader := &dummyReader{
		sets:       make(map[uuid.UUID]kernel.EntitlementSet),
		orgTypes:   make(map[uuid.UUID]string),
		verifiedAt: make(map[uuid.UUID]time.Time),
	}

	client := NewClient(reader, nil).WithClock(func() time.Time { return now })
	ctx := context.Background()

	// 1. Existing entitlements in DB
	existingOrg := uuid.New()
	reader.sets[existingOrg] = kernel.EntitlementSet{
		OrgID:       existingOrg,
		PlanCode:    "pro",
		PlanVersion: 1,
		Status:      kernel.EntitlementActive,
		Seats:       kernel.SeatAllocation{Limit: 5, Used: 2},
		Quotas: map[kernel.QuotaKey]kernel.Quota{
			kernel.QuotaQuotesPerMonth: {Limit: 500, Used: 10},
		},
		Features:   []string{"agenda", "encounters", "cohorts"},
		ComputedAt: now,
	}

	set1, err := client.For(ctx, existingOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if set1.PlanCode != "pro" || set1.Status != kernel.EntitlementActive || set1.Seats.Limit != 5 {
		t.Fatalf("unexpected set: %+v", set1)
	}

	// 2. Pharmacy default without billing account
	pharmacyOrg := uuid.New()
	reader.orgTypes[pharmacyOrg] = "pharmacy"

	setPharma, err := client.For(ctx, pharmacyOrg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setPharma.PlanCode != "pharmacy_basic" || setPharma.Status != kernel.EntitlementActive {
		t.Fatalf("expected pharmacy_basic active for pharmacy without account, got %+v", setPharma)
	}

	// 3. Clinic verified 10 days ago (within 30 days) -> trialing
	clinicNew := uuid.New()
	reader.orgTypes[clinicNew] = "clinic"
	reader.verifiedAt[clinicNew] = now.AddDate(0, 0, -10)

	setClinicNew, err := client.For(ctx, clinicNew)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setClinicNew.PlanCode != "pro" || setClinicNew.Status != kernel.EntitlementTrialing {
		t.Fatalf("expected pro trialing for newly verified clinic, got %+v", setClinicNew)
	}

	// 4. Clinic verified 45 days ago (trial expired, no account) -> restricted
	clinicOld := uuid.New()
	reader.orgTypes[clinicOld] = "clinic"
	reader.verifiedAt[clinicOld] = now.AddDate(0, 0, -45)

	setClinicOld, err := client.For(ctx, clinicOld)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setClinicOld.Status != kernel.EntitlementRestricted {
		t.Fatalf("expected restricted for clinic with expired trial without account, got %+v", setClinicOld)
	}

	// 5. Invalidate
	client.Invalidate(ctx, existingOrg)

	// 6. Local fallback
	memOrg := uuid.New()
	client.SetLocalFallback(memOrg, set1)
	setMem, err := client.For(ctx, memOrg)
	if err != nil || setMem.OrgID != existingOrg {
		t.Fatalf("local fallback failed: %v", err)
	}
}
