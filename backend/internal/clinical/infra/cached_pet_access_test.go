package infra

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
)

type mockPetAccess struct {
	guardian uuid.UUID
	linked   []uuid.UUID
	custody  []uuid.UUID
	calls    int64
}

func (m *mockPetAccess) CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (app.AccessLevel, error) {
	for _, id := range m.linked {
		if id == orgID {
			return app.AccessLinked, nil
		}
	}
	for _, id := range m.custody {
		if id == orgID {
			return app.AccessCustody, nil
		}
	}
	return app.AccessNone, nil
}

func (m *mockPetAccess) GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error) {
	atomic.AddInt64(&m.calls, 1)
	return m.guardian, nil
}

func (m *mockPetAccess) PetSummary(ctx context.Context, petID uuid.UUID) (app.PetSummary, error) {
	return app.PetSummary{PetID: petID}, nil
}

func (m *mockPetAccess) LinkedOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	atomic.AddInt64(&m.calls, 1)
	return m.linked, nil
}

func (m *mockPetAccess) CustodyOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	atomic.AddInt64(&m.calls, 1)
	return m.custody, nil
}

func (m *mockPetAccess) Alerts(ctx context.Context, petID uuid.UUID) ([]string, error) {
	return []string{}, nil
}

func TestCachedPetAccess_HitsCache(t *testing.T) {
	guardian := uuid.New()
	linkedOrg := uuid.New()
	custodyOrg := uuid.New()

	inner := &mockPetAccess{
		guardian: guardian,
		linked:   []uuid.UUID{linkedOrg},
		custody:  []uuid.UUID{custodyOrg},
	}

	c := cache.NewCache(nil) // L1 cache in memory
	cached := NewCachedPetAccess(inner, c)

	petID := uuid.New()
	ctx := context.Background()

	// 1st call: loads from DB (calls GuardianOf, LinkedOrgIDs, CustodyOrgIDs = 3 calls)
	lvl, err := cached.CanOrgAccess(ctx, petID, linkedOrg)
	if err != nil || lvl != app.AccessLinked {
		t.Fatalf("expected AccessLinked, got %v (err=%v)", lvl, err)
	}
	if calls := atomic.LoadInt64(&inner.calls); calls != 3 {
		t.Fatalf("expected 3 calls on inner mock, got %d", calls)
	}

	// 2nd call: hits cache, 0 additional calls to inner
	lvl, err = cached.CanOrgAccess(ctx, petID, custodyOrg)
	if err != nil || lvl != app.AccessCustody {
		t.Fatalf("expected AccessCustody, got %v (err=%v)", lvl, err)
	}

	g, err := cached.GuardianOf(ctx, petID)
	if err != nil || g != guardian {
		t.Fatalf("expected guardian %v, got %v", guardian, g)
	}

	if calls := atomic.LoadInt64(&inner.calls); calls != 3 {
		t.Fatalf("expected still 3 calls after cache hits, got %d", calls)
	}

	// Invalidate clears cache
	cached.InvalidatePetAccess(ctx, petID)

	// Next call triggers reload
	_, err = cached.GuardianOf(ctx, petID)
	if err != nil {
		t.Fatal(err)
	}
	if calls := atomic.LoadInt64(&inner.calls); calls != 6 {
		t.Fatalf("expected 6 calls after invalidation reload, got %d", calls)
	}
}
