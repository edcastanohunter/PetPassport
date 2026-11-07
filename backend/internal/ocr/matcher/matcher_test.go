package matcher

import (
	"context"
	"testing"

	"github.com/google/uuid"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

type mockSearcher struct {
	items []CatalogItem
}

func (m *mockSearcher) SearchCandidates(ctx context.Context, text string, species string, limit int) ([]CatalogItem, error) {
	return m.items, nil
}

func TestMatcher_AutoSelection(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	searcher := &mockSearcher{
		items: []CatalogItem{
			{
				ID:            id1,
				Name:          "Amoxicilina 500mg Comprimidos",
				Concentration: "500mg",
				Form:          "comprimidos",
				SearchScore:   0.95,
				UsageCount:    100,
			},
			{
				ID:          id2,
				Name:        "Ampicilina 250mg",
				SearchScore: 0.30,
				UsageCount:  5,
			},
		},
	}

	m := NewMatcher(searcher, ocrdomain.DefaultThresholds())

	candidates, autoSelected, err := m.MatchItem(context.Background(), "Amoxicilina 500mg comprimidos", "Amox 500", "Canino")
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// top1 should be >= 0.85 and top2 < 0.60
	if !autoSelected {
		t.Errorf("expected autoSelected=true (top1=%f, top2=%f)", candidates[0].Score, candidates[1].Score)
	}
	if candidates[0].CatalogID != id1 {
		t.Errorf("expected top candidate to be %s, got %s", id1, candidates[0].CatalogID)
	}
}

func TestMatcher_NoAutoSelection_WhenAmbiguous(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	// Dos candidatos con puntuación cercana alta
	searcher := &mockSearcher{
		items: []CatalogItem{
			{
				ID:          id1,
				Name:        "Meloxicam 0.5% Gotas",
				SearchScore: 0.88,
				UsageCount:  20,
			},
			{
				ID:          id2,
				Name:        "Meloxicam 1.5% Suspensión",
				SearchScore: 0.85,
				UsageCount:  25,
			},
		},
	}

	m := NewMatcher(searcher, ocrdomain.DefaultThresholds())

	candidates, autoSelected, err := m.MatchItem(context.Background(), "Meloxicam", "Meloxicam", "Felino")
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Ambiguity: top2 is also high (>= 0.60) -> no auto-selection, guardian must choose
	if autoSelected {
		t.Errorf("expected autoSelected=false due to ambiguity between top candidates")
	}
}
