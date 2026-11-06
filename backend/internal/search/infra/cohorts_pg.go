package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
)

// PGCohortStore implementa app.CohortStore con PostgreSQL (PP-F3-DOM-009, PP-F3-API-004).
type PGCohortStore struct {
	pool *pgxpool.Pool
}

func NewPGCohortStore(pool *pgxpool.Pool) *PGCohortStore {
	return &PGCohortStore{pool: pool}
}

func (s *PGCohortStore) RunVaccineDue(ctx context.Context, orgID uuid.UUID, daysAhead int) ([]app.CohortRow, error) {
	if s.pool == nil {
		return []app.CohortRow{}, nil
	}
	untilDate := time.Now().AddDate(0, 0, daysAhead).Format("2006-01-02")
	query := `
		SELECT p.id, p.name, p.species, g.display_name, g.phone,
		       (SELECT max(started_at) FROM read_clinical.encounters WHERE pet_id = p.id AND status = 'signed') as last_enc,
		       'Vacuna próxima a vencer' as reason
		FROM read_clinical.vaccinations v
		JOIN read_petregistry.pets p ON p.id = v.pet_id
		JOIN read_clinical.care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active'
		LEFT JOIN identity.user_accounts g ON g.id = cl.guardian_user_id
		WHERE cl.organization_id = $1 AND v.next_due_date IS NOT NULL AND v.next_due_date <= $2
		GROUP BY p.id, p.name, p.species, g.display_name, g.phone
		LIMIT 1000
	`
	sqlRows, err := s.pool.Query(ctx, query, orgID, untilDate)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []app.CohortRow
	for sqlRows.Next() {
		var r app.CohortRow
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &r.GuardianPhone, &r.LastEncounterAt, &r.Reason); err == nil {
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func (s *PGCohortStore) RunNoVisitSince(ctx context.Context, orgID uuid.UUID, months int) ([]app.CohortRow, error) {
	if s.pool == nil {
		return []app.CohortRow{}, nil
	}
	query := `
		SELECT p.id, p.name, p.species, g.display_name, g.phone,
		       max(e.started_at) as last_enc,
		       'Sin visita registrada recientemente' as reason
		FROM read_clinical.care_links cl
		JOIN read_petregistry.pets p ON p.id = cl.pet_id
		LEFT JOIN identity.user_accounts g ON g.id = cl.guardian_user_id
		LEFT JOIN read_clinical.encounters e ON e.pet_id = p.id AND e.organization_id = cl.organization_id AND e.status = 'signed'
		WHERE cl.organization_id = $1 AND cl.status = 'active'
		GROUP BY p.id, p.name, p.species, g.display_name, g.phone
		HAVING max(e.started_at) IS NULL OR max(e.started_at) < now() - ($2 || ' months')::interval
		LIMIT 1000
	`
	sqlRows, err := s.pool.Query(ctx, query, orgID, months)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []app.CohortRow
	for sqlRows.Next() {
		var r app.CohortRow
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &r.GuardianPhone, &r.LastEncounterAt, &r.Reason); err == nil {
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func (s *PGCohortStore) RunDiagnosis(ctx context.Context, orgID uuid.UUID, months int, diagQuery string) ([]app.CohortRow, error) {
	if s.pool == nil {
		return []app.CohortRow{}, nil
	}
	query := `
		SELECT p.id, p.name, p.species, g.display_name, g.phone,
		       max(e.started_at) as last_enc,
		       ('Diagnóstico: ' || coalesce(d.description, d.code, 'historial')) as reason
		FROM read_clinical.encounters e
		JOIN read_clinical.diagnoses d ON d.encounter_id = e.id
		JOIN read_petregistry.pets p ON p.id = e.pet_id
		JOIN read_clinical.care_links cl ON cl.pet_id = p.id AND cl.organization_id = e.organization_id AND cl.status = 'active'
		LEFT JOIN identity.user_accounts g ON g.id = cl.guardian_user_id
		WHERE e.organization_id = $1 AND e.status = 'signed'
		  AND e.started_at >= now() - ($2 || ' months')::interval
		  AND (d.code ILIKE $3 OR d.description ILIKE $4)
		GROUP BY p.id, p.name, p.species, g.display_name, g.phone, d.description, d.code
		LIMIT 1000
	`
	sqlRows, err := s.pool.Query(ctx, query, orgID, months, diagQuery, "%"+diagQuery+"%")
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []app.CohortRow
	for sqlRows.Next() {
		var r app.CohortRow
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &r.GuardianPhone, &r.LastEncounterAt, &r.Reason); err == nil {
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func (s *PGCohortStore) RunSpeciesAgeWeight(ctx context.Context, orgID uuid.UUID, species string, minAge, maxAge *int, minWeight, maxWeight *float64) ([]app.CohortRow, error) {
	if s.pool == nil {
		return []app.CohortRow{}, nil
	}
	query := `
		SELECT p.id, p.name, p.species, g.display_name, g.phone,
		       (SELECT max(started_at) FROM read_clinical.encounters WHERE pet_id = p.id AND status = 'signed') as last_enc,
		       'Perfil biológico coincidente' as reason
		FROM read_clinical.care_links cl
		JOIN read_petregistry.pets p ON p.id = cl.pet_id
		LEFT JOIN identity.user_accounts g ON g.id = cl.guardian_user_id
		WHERE cl.organization_id = $1 AND cl.status = 'active'
	`
	args := []any{orgID}
	argIdx := 2

	if species != "" {
		query += fmt.Sprintf(" AND p.species = $%d", argIdx)
		args = append(args, species)
	}

	query += " GROUP BY p.id, p.name, p.species, g.display_name, g.phone LIMIT 1000"

	sqlRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var rows []app.CohortRow
	for sqlRows.Next() {
		var r app.CohortRow
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &r.GuardianPhone, &r.LastEncounterAt, &r.Reason); err == nil {
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func (s *PGCohortStore) RecordExportAudit(ctx context.Context, orgID, userID uuid.UUID, key, reason string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO platform.cohort_exports (id, organization_id, user_id, cohort_key, reason, exported_at)
		VALUES ($1, $2, $3, $4, $5, now())
	`, uuid.New(), orgID, userID, key, reason)
	return err
}

func (s *PGCohortStore) CheckCampaignRateLimit(ctx context.Context, orgID uuid.UUID, key string) (bool, error) {
	if s.pool == nil {
		return false, nil
	}
	var lastSent time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT max(dispatched_at) FROM platform.cohort_campaigns
		WHERE organization_id = $1 AND cohort_key = $2
	`, orgID, key).Scan(&lastSent)
	if err == nil && !lastSent.IsZero() && time.Since(lastSent) < 7*24*time.Hour {
		return true, nil
	}
	return false, nil
}

func (s *PGCohortStore) RecordCampaign(ctx context.Context, orgID, userID, campaignID uuid.UUID, key string, count int) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO platform.cohort_campaigns (id, organization_id, user_id, cohort_key, recipients_count, dispatched_at)
		VALUES ($1, $2, $3, $4, $5, now())
	`, campaignID, orgID, userID, key, count)
	return err
}
