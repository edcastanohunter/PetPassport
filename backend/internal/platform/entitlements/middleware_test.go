package entitlements

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type dummyExemptCmd struct{}

func (dummyExemptCmd) CommandName() string { return "billing.Subscribe" }

type dummyNonConsumingCmd struct{}

func (dummyNonConsumingCmd) CommandName() string { return "clinical.SignEncounter" }

type dummyConsumingCmd struct {
	orgID uuid.UUID
	req   kernel.Requirement
}

func (c dummyConsumingCmd) CommandName() string          { return "clinical.ScheduleAppointment" }
func (c dummyConsumingCmd) Consumes() kernel.Requirement { return c.req }
func (c dummyConsumingCmd) TargetOrgID() uuid.UUID       { return c.orgID }

func TestEnforceEntitlements_Middleware(t *testing.T) {
	fakeEnt := kernel.NewFakeEntitlements()
	orgID := uuid.New()

	baseSet := kernel.EntitlementSet{
		OrgID:       orgID,
		PlanCode:    "starter",
		PlanVersion: 1,
		Status:      kernel.EntitlementActive,
		Seats:       kernel.SeatAllocation{Limit: 2, Used: 2},
		Quotas: map[kernel.QuotaKey]kernel.Quota{
			kernel.QuotaQuotesPerMonth: {Limit: 100, Used: 105, SoftLimit: 110}, // 105 is within 110 soft limit
		},
		Features:   []string{"agenda", "encounters"},
		ComputedAt: time.Now().UTC(),
	}
	fakeEnt.Set(orgID, baseSet)

	b := bus.New()
	b.Use(EnforceEntitlements(fakeEnt))

	var executed bool
	handler := func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		executed = true
		return bus.Result{AggregateID: uuid.New(), Version: 1}, nil
	}

	b.Register(dummyExemptCmd{}, handler)
	b.Register(dummyNonConsumingCmd{}, handler)
	b.Register(dummyConsumingCmd{}, handler)

	ctx := context.Background()

	// 1. Comandos exentos
	executed = false
	_, err := b.Dispatch(ctx, dummyExemptCmd{})
	if err != nil || !executed {
		t.Fatalf("expected exempt command to succeed, err=%v", err)
	}

	// 2. Comandos no consumidores
	executed = false
	_, err = b.Dispatch(ctx, dummyNonConsumingCmd{})
	if err != nil || !executed {
		t.Fatalf("expected non-consuming command to succeed, err=%v", err)
	}

	// 3. Comando consumidor dentro del límite blando (used 105 + 2 = 107 <= 110 soft limit)
	executed = false
	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.QuotaReq(kernel.QuotaQuotesPerMonth, 2),
	})
	if err != nil || !executed {
		t.Fatalf("expected soft-limit command to succeed, err=%v", err)
	}

	// 4. Comando consumidor que supera el límite blando (used 105 + 10 = 115 > 110)
	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.QuotaReq(kernel.QuotaQuotesPerMonth, 10),
	})
	if err == nil {
		t.Fatal("expected PlanLimitReachedError when exceeding soft limit")
	}
	limitErr, ok := err.(PlanLimitReachedError)
	if !ok || limitErr.Quota != string(kernel.QuotaQuotesPerMonth) {
		t.Fatalf("unexpected error type: %T %v", err, err)
	}

	// 5. Asientos agotados (used 2 + 1 > limit 2)
	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.SeatReq(),
	})
	if err == nil {
		t.Fatal("expected PlanLimitReachedError when seats are exhausted")
	}
	limitErr2, ok := err.(PlanLimitReachedError)
	if !ok || limitErr2.Quota != "seats" {
		t.Fatalf("unexpected error: %v", err)
	}

	// 6. Capacidad no contratada
	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.FeatureReq("partner_api"),
	})
	if err == nil {
		t.Fatal("expected PlanLimitReachedError for missing feature")
	}

	// 7. Capacidad existente
	executed = false
	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.FeatureReq("agenda"),
	})
	if err != nil || !executed {
		t.Fatalf("expected feature check to pass, err=%v", err)
	}

	// 8. Organización restringida
	restrictedSet := baseSet
	restrictedSet.Status = kernel.EntitlementRestricted
	fakeEnt.Set(orgID, restrictedSet)

	_, err = b.Dispatch(ctx, dummyConsumingCmd{
		orgID: orgID,
		req:   kernel.FeatureReq("agenda"),
	})
	if err == nil {
		t.Fatal("expected RestrictedError when org is restricted")
	}
	if _, ok := err.(RestrictedError); !ok {
		t.Fatalf("expected RestrictedError, got %T %v", err, err)
	}
}
