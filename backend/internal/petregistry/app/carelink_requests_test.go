package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type stubRequests struct {
	byChip   map[string][]PetLookupResult
	byGuard  map[uuid.UUID][]PetLookupResult
	inbox    []InboxItem
	lastChip string
}

func (s *stubRequests) PetsByMicrochip(_ context.Context, chip string, _ int) ([]PetLookupResult, error) {
	s.lastChip = chip
	return s.byChip[chip], nil
}

func (s *stubRequests) PetsByGuardian(_ context.Context, id uuid.UUID, _ int) ([]PetLookupResult, error) {
	return s.byGuard[id], nil
}

func (s *stubRequests) GetRequest(context.Context, uuid.UUID, uuid.UUID) (CareLinkRequestView, error) {
	return CareLinkRequestView{}, nil
}

func (s *stubRequests) GetOrgRequest(context.Context, uuid.UUID, uuid.UUID) (CareLinkRequestView, error) {
	return CareLinkRequestView{}, nil
}

func (s *stubRequests) ListOrgRequests(context.Context, uuid.UUID, string, int) ([]CareLinkRequestView, error) {
	return nil, nil
}

func (s *stubRequests) ListInbox(context.Context, uuid.UUID) ([]InboxItem, error) {
	return s.inbox, nil
}

// countingLimiter permite hasta max intentos.
type countingLimiter struct{ n, max int }

func (l *countingLimiter) Allow(context.Context, uuid.UUID, int, time.Duration) (bool, error) {
	l.n++
	return l.n <= l.max, nil
}

type staticSource []InboxItem

func (s staticSource) InboxItems(context.Context, uuid.UUID) ([]InboxItem, error) { return s, nil }

func lookupService(reqs *stubRequests, lim LookupLimiter, g GuardianLookup) *Service {
	return NewService(Options{RequestQueries: reqs, LookupLimiter: lim, Guardian: g})
}

func TestLookup_MasksGuardianAndNeverLeaksTheFullName(t *testing.T) {
	petID := uuid.New()
	reqs := &stubRequests{byChip: map[string][]PetLookupResult{
		"982000123456789": {{PetID: petID, Name: "Luna", Species: "dog", GuardianDisplayName: "Ana María Pérez"}},
	}}
	s := lookupService(reqs, &countingLimiter{max: 20}, guardians{})
	got, err := s.LookupPetForRequest(context.Background(), uuid.New(), "982000123456789", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if got[0].GuardianMasked != "A*** M*** P***" || got[0].GuardianDisplayName != "" {
		t.Fatalf("tutor sin enmascarar: %+v", got[0])
	}
}

func TestLookup_ByGuardianEmail(t *testing.T) {
	gid, petID := uuid.New(), uuid.New()
	reqs := &stubRequests{byGuard: map[uuid.UUID][]PetLookupResult{
		gid: {{PetID: petID, Name: "Rocky", Species: "cat", GuardianDisplayName: "Luis Gómez"}},
	}}
	s := lookupService(reqs, &countingLimiter{max: 20}, guardians{gid: {Email: "luis@example.com"}})
	got, err := s.LookupPetForRequest(context.Background(), uuid.New(), "", "Luis@Example.com")
	if err != nil || len(got) != 1 || got[0].PetID != petID {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestLookup_NoResultsIsAnEmptyListAndTakesTheFloor(t *testing.T) {
	s := lookupService(&stubRequests{}, &countingLimiter{max: 20}, guardians{})
	start := time.Now()
	got, err := s.LookupPetForRequest(context.Background(), uuid.New(), "", "nadie@example.com")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, err %v", got, err)
	}
	if elapsed := time.Since(start); elapsed < lookupFloor-20*time.Millisecond {
		t.Fatalf("respuesta demasiado rápida (%s): oráculo de existencia por tiempo", elapsed)
	}
}

func TestLookup_InvalidInputCountsButReturnsNothing(t *testing.T) {
	lim := &countingLimiter{max: 20}
	reqs := &stubRequests{}
	s := lookupService(reqs, lim, guardians{})
	got, err := s.LookupPetForRequest(context.Background(), uuid.New(), "12", "")
	if err != nil || len(got) != 0 || lim.n != 1 || reqs.lastChip != "" {
		t.Fatalf("got %+v err %v intentos %d chip %q", got, err, lim.n, reqs.lastChip)
	}
}

func TestLookup_RateLimited(t *testing.T) {
	s := lookupService(&stubRequests{}, &countingLimiter{max: 0}, guardians{})
	if _, err := s.LookupPetForRequest(context.Background(), uuid.New(), "", "a@b.co"); err != ErrLookupRateLimited {
		t.Fatalf("err = %v, quería RATE_LIMITED", err)
	}
}

func TestInbox_MergesSourcesNewestFirst(t *testing.T) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	own := InboxItem{ID: uuid.New(), Kind: "care_link_request", CreatedAt: at}
	older := InboxItem{ID: uuid.New(), Kind: "appointment_proposal", CreatedAt: at.Add(-time.Hour)}
	newest := InboxItem{ID: uuid.New(), Kind: "follow_up", CreatedAt: at.Add(time.Hour)}
	s := NewService(Options{
		RequestQueries: &stubRequests{inbox: []InboxItem{own}},
		InboxSources:   []InboxSource{staticSource{older}, staticSource{newest}},
	})
	got, err := s.ListInbox(context.Background(), uuid.New())
	if err != nil || len(got) != 3 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if got[0].Kind != "follow_up" || got[1].Kind != "care_link_request" || got[2].Kind != "appointment_proposal" {
		t.Fatalf("orden: %v %v %v", got[0].Kind, got[1].Kind, got[2].Kind)
	}
}

func TestMaskName(t *testing.T) {
	for in, want := range map[string]string{"ana": "A***", "  luis  gómez ": "L*** G***", "": ""} {
		if got := MaskName(in); got != want {
			t.Errorf("MaskName(%q) = %q, quería %q", in, got, want)
		}
	}
	_ = kernel.RealClock{}
}
