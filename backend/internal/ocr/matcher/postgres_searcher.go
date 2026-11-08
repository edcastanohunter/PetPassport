package matcher

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	medapp "github.com/edcastanohunter/petpassport/backend/internal/medications/app"
	medinfra "github.com/edcastanohunter/petpassport/backend/internal/medications/infra"
)

// PostgresMedicationSearcher implementa MedicationSearcher consultando el catálogo ref.medications.
type PostgresMedicationSearcher struct {
	store *medinfra.Store
}

func NewPostgresMedicationSearcher(pool *pgxpool.Pool) *PostgresMedicationSearcher {
	return &PostgresMedicationSearcher{
		store: medinfra.NewStore(pool),
	}
}

func (s *PostgresMedicationSearcher) SearchCandidates(ctx context.Context, text string, species string, limit int) ([]CatalogItem, error) {
	if s.store == nil {
		return nil, nil
	}
	meds, err := s.store.ListMedications(ctx, medapp.MedicationFilter{
		Q:       text,
		Species: species,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]CatalogItem, 0, len(meds))
	for _, m := range meds {
		specList := make([]string, 0, len(m.Species))
		for _, sp := range m.Species {
			specList = append(specList, string(sp))
		}
		conc := ""
		if m.Concentration != nil {
			conc = *m.Concentration
		}
		items = append(items, CatalogItem{
			ID:            m.ID,
			Name:          m.Name,
			Concentration: conc,
			Form:          string(m.Form),
			Species:       specList,
			Controlled:    m.Controlled,
			UsageCount:    int64(m.UsageCount),
			SearchScore:   0.8,
		})
	}
	return items, nil
}

var _ MedicationSearcher = (*PostgresMedicationSearcher)(nil)
