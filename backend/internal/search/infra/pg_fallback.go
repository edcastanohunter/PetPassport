package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// PGFallback implementa el modo de búsqueda degradado sobre PostgreSQL (ARQ-004).
type PGFallback struct {
	pool *pgxpool.Pool
}

// NewPGFallback construye una instancia de PGFallback.
func NewPGFallback(pool *pgxpool.Pool) *PGFallback {
	return &PGFallback{pool: pool}
}

// SearchPatientsPG busca pacientes en PostgreSQL usando trigramas y coincidencias exactas.
func (p *PGFallback) SearchPatientsPG(ctx context.Context, req app.PatientSearchRequest, principal auth.Principal) (*app.PatientSearchResponse, error) {
	size := req.Size
	if size <= 0 || size > 50 {
		size = 20
	}

	q := strings.TrimSpace(req.Q)
	likePattern := "%" + q + "%"

	var rows pgx.Rows
	var err error

	if req.OrgID != uuid.Nil {
		// Búsqueda clínica en PostgreSQL
		query := `
			SELECT p.id, p.name, p.species, COALESCE(p.breed, ''), p.sex,
			       RIGHT(COALESCE(p.microchip, ''), 4), p.status, p.guardian_display_name,
			       CASE WHEN cl.organization_id IS NOT NULL THEN 'linked' ELSE 'custody' END AS access_level
			FROM read_petregistry.pets p
			LEFT JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active' AND cl.org_suspended = false
			LEFT JOIN (
				SELECT DISTINCT pet_id FROM read_clinical.encounter_custody WHERE organization_id = $1
			) cust ON cust.pet_id = p.id
			WHERE (cl.organization_id IS NOT NULL OR cust.pet_id IS NOT NULL)
			  AND p.status = 'active'
			  AND ($2 = '' OR p.name ILIKE $3 OR p.microchip ILIKE $3 OR p.guardian_display_name ILIKE $3)
			  AND ($4 = '' OR p.species = $4)
			ORDER BY p.updated_at DESC
			LIMIT $5
		`
		rows, err = p.pool.Query(ctx, query, req.OrgID, q, likePattern, req.Species, size)
	} else {
		// Búsqueda tutor
		query := `
			SELECT p.id, p.name, p.species, COALESCE(p.breed, ''), p.sex,
			       RIGHT(COALESCE(p.microchip, ''), 4), p.status, p.guardian_display_name,
			       'guardian' AS access_level
			FROM read_petregistry.pets p
			WHERE p.guardian_id = $1
			  AND p.status = 'active'
			  AND ($2 = '' OR p.name ILIKE $3 OR p.microchip ILIKE $3)
			  AND ($4 = '' OR p.species = $4)
			ORDER BY p.updated_at DESC
			LIMIT $5
		`
		rows, err = p.pool.Query(ctx, query, principal.UserID, q, likePattern, req.Species, size)
	}

	if err != nil {
		return nil, fmt.Errorf("fallback pg query: %w", err)
	}
	defer rows.Close()

	var items []app.PatientSearchResultItem
	for rows.Next() {
		var item app.PatientSearchResultItem
		var petID uuid.UUID
		err := rows.Scan(
			&petID,
			&item.Pet.Name,
			&item.Pet.Species,
			&item.Pet.Breed,
			&item.Pet.Sex,
			&item.Pet.MicrochipSuffix,
			&item.Pet.Status,
			&item.GuardianDisplayName,
			&item.AccessLevel,
		)
		if err != nil {
			return nil, err
		}
		item.Pet.ID = petID.String()
		items = append(items, item)
	}

	return &app.PatientSearchResponse{
		Data:      items,
		Facets:    map[string][]app.FacetBucket{},
		Freshness: app.FreshnessInfo{LagSeconds: 0},
		Mode:      "degraded",
	}, nil
}

// SearchClinicalPG busca en historiales clínicos de PostgreSQL limitados a los últimos 90 días (ARQ-004).
func (s *PGFallback) SearchClinicalPG(ctx context.Context, req app.ClinicalSearchRequest, principal auth.Principal) (*app.ClinicalSearchResponse, error) {
	size := req.Size
	if size <= 0 || size > 50 {
		size = 20
	}

	q := strings.TrimSpace(req.Q)
	likePattern := "%" + q + "%"
	ninetyDaysAgo := time.Now().AddDate(0, 0, -90)

	var rows pgx.Rows
	var err error

	if req.OrgID != uuid.Nil {
		query := `
			SELECT e.id, e.pet_id, p.name, p.species, e.type, e.started_at, e.organization_id, o.display_name, e.diagnoses
			FROM read_clinical.encounters e
			JOIN read_petregistry.pets p ON p.id = e.pet_id
			JOIN read_partnernetwork.organizations o ON o.id = e.organization_id
			LEFT JOIN read_petregistry.pet_care_links cl ON cl.pet_id = e.pet_id AND cl.organization_id = $1 AND cl.status = 'active' AND cl.org_suspended = false
			LEFT JOIN read_clinical.encounter_custody cust ON cust.encounter_id = e.id AND cust.organization_id = $1
			WHERE (cl.organization_id IS NOT NULL OR cust.organization_id IS NOT NULL)
			  AND e.status = 'signed'
			  AND e.started_at >= $2
			  AND ($3 = '' OR e.diagnoses::text ILIKE $4 OR e.reason ILIKE $4 OR p.name ILIKE $4)
			  AND ($5 = '' OR p.species = $5)
			  AND ($6 = '' OR e.type = $6)
			ORDER BY e.started_at DESC
			LIMIT $7
		`
		rows, err = s.pool.Query(ctx, query, req.OrgID, ninetyDaysAgo, q, likePattern, req.Species, req.Type, size)
	} else {
		query := `
			SELECT e.id, e.pet_id, p.name, p.species, e.type, e.started_at, e.organization_id, o.display_name, e.diagnoses
			FROM read_clinical.encounters e
			JOIN read_petregistry.pets p ON p.id = e.pet_id
			JOIN read_partnernetwork.organizations o ON o.id = e.organization_id
			WHERE p.guardian_id = $1
			  AND e.status = 'signed'
			  AND e.started_at >= $2
			  AND ($3 = '' OR e.diagnoses::text ILIKE $4 OR e.reason ILIKE $4 OR p.name ILIKE $4)
			  AND ($5 = '' OR p.species = $5)
			ORDER BY e.started_at DESC
			LIMIT $6
		`
		rows, err = s.pool.Query(ctx, query, principal.UserID, ninetyDaysAgo, q, likePattern, req.Species, size)
	}

	if err != nil {
		return nil, fmt.Errorf("fallback pg clinical query: %w", err)
	}
	defer rows.Close()

	grouped := make(map[string]*app.ClinicalSearchResultItem)
	var orderedPetIDs []string

	for rows.Next() {
		var encID, petID, orgID uuid.UUID
		var petName, species, encType, orgName string
		var startedAt time.Time
		var diagnoses []domain.DiagnosisItem

		if err := rows.Scan(&encID, &petID, &petName, &species, &encType, &startedAt, &orgID, &orgName, &diagnoses); err != nil {
			return nil, err
		}

		pIDStr := petID.String()
		group, exists := grouped[pIDStr]
		if !exists {
			group = &app.ClinicalSearchResultItem{}
			group.Pet.ID = pIDStr
			group.Pet.Name = petName
			group.Pet.Species = species
			grouped[pIDStr] = group
			orderedPetIDs = append(orderedPetIDs, pIDStr)
		}

		hit := app.ClinicalHitItem{
			DocKind:       "encounter",
			RefID:         encID.String(),
			OccurredAt:    startedAt,
			Organization:  map[string]string{"id": orgID.String(), "name": orgName},
			EncounterType: encType,
			Diagnoses:     diagnoses,
		}
		group.Hits = append(group.Hits, hit)
	}

	var data []app.ClinicalSearchResultItem
	for _, id := range orderedPetIDs {
		data = append(data, *grouped[id])
	}

	return &app.ClinicalSearchResponse{
		Data:      data,
		Facets:    map[string][]app.FacetBucket{},
		Freshness: app.FreshnessInfo{LagSeconds: 0},
		Mode:      "degraded",
	}, nil
}

// SuggestPatientsPG proporciona sugerencias rápidas desde PostgreSQL.
func (s *PGFallback) SuggestPatientsPG(ctx context.Context, orgID uuid.UUID, q string, principal auth.Principal) ([]app.SuggestPatientItem, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []app.SuggestPatientItem{}, nil
	}
	likePattern := q + "%"

	var rows pgx.Rows
	var err error

	if orgID != uuid.Nil {
		query := `
			SELECT p.id, p.name, p.species, p.guardian_display_name, RIGHT(COALESCE(p.microchip, ''), 4),
			       CASE WHEN cl.organization_id IS NOT NULL THEN 'linked' ELSE 'custody' END AS access_level
			FROM read_petregistry.pets p
			LEFT JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active' AND cl.org_suspended = false
			LEFT JOIN (
				SELECT DISTINCT pet_id FROM read_clinical.encounter_custody WHERE organization_id = $1
			) cust ON cust.pet_id = p.id
			WHERE (cl.organization_id IS NOT NULL OR cust.pet_id IS NOT NULL)
			  AND p.status = 'active'
			  AND (p.name ILIKE $2 OR p.microchip LIKE $2)
			LIMIT 8
		`
		rows, err = s.pool.Query(ctx, query, orgID, likePattern)
	} else {
		query := `
			SELECT p.id, p.name, p.species, p.guardian_display_name, RIGHT(COALESCE(p.microchip, ''), 4),
			       'guardian' AS access_level
			FROM read_petregistry.pets p
			WHERE p.guardian_id = $1
			  AND p.status = 'active'
			  AND (p.name ILIKE $2 OR p.microchip LIKE $2)
			LIMIT 8
		`
		rows, err = s.pool.Query(ctx, query, principal.UserID, likePattern)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []app.SuggestPatientItem
	for rows.Next() {
		var item app.SuggestPatientItem
		var petID uuid.UUID
		if err := rows.Scan(&petID, &item.Name, &item.Species, &item.GuardianDisplayName, &item.MicrochipSuffix, &item.AccessLevel); err != nil {
			return nil, err
		}
		item.PetID = petID.String()
		items = append(items, item)
	}
	return items, nil
}

// SuggestMedicationsPG autocompleta medicamentos desde PostgreSQL cuando OpenSearch no está disponible (ARQ-004).
func (s *PGFallback) SuggestMedicationsPG(ctx context.Context, q string, species string, limit int) ([]app.SuggestMedicationItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	cleanQ := strings.TrimSpace(q)
	likePattern := "%" + cleanQ + "%"

	query := `
		SELECT id, name, active_ingredients, form, concentration, presentation,
		       manufacturer, species, routes, prescription_required, controlled,
		       default_route, default_dose_unit
		FROM ref.medications
		WHERE status = 'active'
		  AND ($1 = '' OR $1 = ANY(species) OR cardinality(species) = 0)
		  AND ($2 = '' OR name ILIKE $3 OR name_norm % $2)
		ORDER BY usage_count DESC, name ASC
		LIMIT $4
	`

	rows, err := s.pool.Query(ctx, query, species, cleanQ, likePattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []app.SuggestMedicationItem
	for rows.Next() {
		var (
			id                   uuid.UUID
			name, form           string
			conc, pres, manu     *string
			defRoute, defUnit    *string
			specList, routeList  []string
			actJSON              []byte
			prescReq, controlled bool
		)

		if err := rows.Scan(
			&id, &name, &actJSON, &form, &conc, &pres,
			&manu, &specList, &routeList, &prescReq, &controlled,
			&defRoute, &defUnit,
		); err != nil {
			return nil, err
		}

		var ingredients []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(actJSON, &ingredients)
		var ingNames []string
		for _, ing := range ingredients {
			if ing.Name != "" {
				ingNames = append(ingNames, ing.Name)
			}
		}

		items = append(items, app.SuggestMedicationItem{
			CatalogID:            id.String(),
			Name:                 name,
			ActiveIngredients:    ingNames,
			Form:                 form,
			Concentration:        conc,
			Presentation:         pres,
			Manufacturer:         manu,
			Species:              specList,
			Routes:               routeList,
			PrescriptionRequired: prescReq,
			Controlled:           controlled,
			DefaultRoute:         defRoute,
			DefaultDoseUnit:      defUnit,
		})
	}

	return items, nil
}
