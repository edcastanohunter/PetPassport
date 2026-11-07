package matcher

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

// CatalogItem representa un medicamento retornado por la búsqueda en catálogo u OpenSearch (PP-F4-OCR-005).
type CatalogItem struct {
	ID            uuid.UUID
	Name          string
	Concentration string
	Form          string
	Species       []string
	Controlled    bool
	UsageCount    int64
	SearchScore   float64 // 0.0 a 1.0 del motor de búsqueda
}

// MedicationSearcher define el puerto para consultar candidatos en OpenSearch o PostgreSQL.
type MedicationSearcher interface {
	SearchCandidates(ctx context.Context, text string, species string, limit int) ([]CatalogItem, error)
}

// Matcher implementa ocrdomain.CatalogMatcher (PP-F4-OCR-005).
type Matcher struct {
	searcher   MedicationSearcher
	thresholds ocrdomain.ThresholdsConfig
}

func NewMatcher(searcher MedicationSearcher, thresholds ocrdomain.ThresholdsConfig) *Matcher {
	return &Matcher{
		searcher:   searcher,
		thresholds: thresholds,
	}
}

// MatchItem consulta medicamentos, aplica la fórmula combinada y determina auto-selección (PP-F4-OCR-005).
func (m *Matcher) MatchItem(ctx context.Context, productText string, rawText string, species string) ([]ocrdomain.CandidateProduct, bool, error) {
	q := strings.TrimSpace(productText)
	if q == "" {
		q = strings.TrimSpace(rawText)
	}
	if q == "" || m.searcher == nil {
		return nil, false, nil
	}

	items, err := m.searcher.SearchCandidates(ctx, q, species, 10)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 && rawText != "" && rawText != q {
		items, err = m.searcher.SearchCandidates(ctx, rawText, species, 10)
		if err != nil {
			return nil, false, err
		}
	}
	if len(items) == 0 {
		return nil, false, nil
	}

	// Calcular puntuación combinada:
	// 0.60 * score_norm + 0.25 * coincidencia_concentracion_forma + 0.15 * usage_norm
	type scoredCandidate struct {
		candidate  ocrdomain.CandidateProduct
		controlled bool
		finalScore float64
	}

	var scored []scoredCandidate
	for _, it := range items {
		normScore := it.SearchScore
		if normScore <= 0 {
			normScore = calculateStringSimilarity(q, it.Name)
		}

		// Coincidencia de concentración o forma farmacéutica
		var doseFormMatch float64
		combinedText := strings.ToLower(q + " " + rawText)
		if it.Concentration != "" && strings.Contains(combinedText, strings.ToLower(it.Concentration)) {
			doseFormMatch += 0.60
		}
		if it.Form != "" && strings.Contains(combinedText, strings.ToLower(it.Form)) {
			doseFormMatch += 0.40
		}
		if doseFormMatch > 1.0 {
			doseFormMatch = 1.0
		}

		// Normalización de uso histórico
		usageNorm := math.Min(1.0, math.Log1p(float64(it.UsageCount))/10.0)

		finalScore := 0.60*normScore + 0.25*doseFormMatch + 0.15*usageNorm
		finalScore = math.Round(finalScore*100) / 100.0

		scored = append(scored, scoredCandidate{
			candidate: ocrdomain.CandidateProduct{
				CatalogID: it.ID,
				Name:      it.Name,
				Score:     finalScore,
			},
			controlled: it.Controlled,
			finalScore: finalScore,
		})
	}

	// Ordenar descendentemente por finalScore
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].finalScore > scored[j].finalScore
	})

	// Top 3 candidatos
	limit := 3
	if len(scored) < limit {
		limit = len(scored)
	}

	var topCandidates []ocrdomain.CandidateProduct
	for i := 0; i < limit; i++ {
		topCandidates = append(topCandidates, scored[i].candidate)
	}

	// Regla de auto-selección: top1 >= 0.85 y top2 < 0.60 (o no hay segundo)
	autoSelected := false
	if len(topCandidates) > 0 && topCandidates[0].Score >= m.thresholds.CandidateAutoTop1 {
		if len(topCandidates) == 1 || topCandidates[1].Score < m.thresholds.CandidateAutoTop2 {
			autoSelected = true
		}
	}

	return topCandidates, autoSelected, nil
}

func calculateStringSimilarity(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return 1.0
	}
	if strings.Contains(b, a) || strings.Contains(a, b) {
		return 0.80
	}
	// Coincidencia de tokens
	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}
	match := 0
	for _, wa := range wordsA {
		for _, wb := range wordsB {
			if wa == wb {
				match++
				break
			}
		}
	}
	return float64(match) / float64(len(wordsA))
}

var _ ocrdomain.CatalogMatcher = (*Matcher)(nil)
