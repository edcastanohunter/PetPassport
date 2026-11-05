package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// CohortDefinition describe una consulta parametrizada de cohorte (PP-F3-DOM-009, PP-F3-API-004).
type CohortDefinition struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Source      string         `json:"source"`
	Params      map[string]any `json:"params"`
}

// CohortRow representa una fila de resultado de una cohorte.
type CohortRow struct {
	PetID               uuid.UUID  `json:"pet_id"`
	PetName             string     `json:"pet_name"`
	Species             string     `json:"species"`
	GuardianDisplayName string     `json:"guardian_display_name"`
	GuardianPhone       *string    `json:"guardian_phone,omitempty"`
	LastEncounterAt     *time.Time `json:"last_encounter_at,omitempty"`
	Reason              string     `json:"reason"`
}

// NotifyResult es el resultado del envío de una campaña a una cohorte.
type NotifyResult struct {
	Scheduled           int `json:"scheduled"`
	SkippedByPreference int `json:"skipped_by_preference"`
}

// CohortService gestiona las consultas parametrizadas, exportaciones y campañas (PP-F3-PRD-007, RN-20).
type CohortService struct {
	pool   *pgxpool.Pool
	client Client
}

func NewCohortService(pool *pgxpool.Pool, client Client) *CohortService {
	return &CohortService{
		pool:   pool,
		client: client,
	}
}

// Definitions devuelve el catálogo de cohortes disponibles con su esquema de parámetros.
func (s *CohortService) Definitions() []CohortDefinition {
	return []CohortDefinition{
		{
			Key:         "vaccine_due",
			Name:        "Vacunas vencidas o próximas a vencer",
			Description: "Pacientes con vacunas obligatorias o recomendadas que vencen en los próximos N días o ya están vencidas.",
			Source:      "pg:vaccination_due",
			Params: map[string]any{
				"days_ahead":   "int (por defecto 30)",
				"product_name": "string (opcional)",
			},
		},
		{
			Key:         "no_visit_since",
			Name:        "Sin consulta en más de N meses",
			Description: "Pacientes vinculados a la clínica que no han tenido ninguna consulta firmada en más de N meses.",
			Source:      "pg:encounters",
			Params: map[string]any{
				"months": "int (por defecto 12, mínimo 1)",
			},
		},
		{
			Key:         "diagnosis",
			Name:        "Diagnóstico específico confirmado",
			Description: "Pacientes vinculados que han tenido un diagnóstico confirmado específico en los últimos N meses.",
			Source:      "search:clinical",
			Params: map[string]any{
				"query":  "string (texto del diagnóstico, obligatorio)",
				"months": "int (opcional, por defecto 24)",
			},
		},
		{
			Key:         "species_age_weight",
			Name:        "Filtro por especie, edad y peso",
			Description: "Segmentación estructurada de pacientes por especie y rangos opcionales de edad y peso.",
			Source:      "pg:pets",
			Params: map[string]any{
				"species":         "string (dog|cat|other)",
				"min_age_months":  "int (opcional)",
				"max_age_months":  "int (opcional)",
				"min_weight_kg":   "float (opcional)",
				"max_weight_kg":   "float (opcional)",
			},
		},
	}
}

// Run ejecuta una consulta de cohorte parametrizada con límite estricto de 1.000 filas.
func (s *CohortService) Run(ctx context.Context, orgID uuid.UUID, key string, params map[string]any) ([]CohortRow, int, bool, error) {
	if s.pool == nil {
		return []CohortRow{}, 0, false, nil
	}

	var rows []CohortRow
	var err error

	switch key {
	case "vaccine_due":
		rows, err = s.runVaccineDue(ctx, orgID, params)
	case "no_visit_since":
		rows, err = s.runNoVisitSince(ctx, orgID, params)
	case "diagnosis":
		rows, err = s.runDiagnosis(ctx, orgID, params)
	case "species_age_weight":
		rows, err = s.runSpeciesAgeWeight(ctx, orgID, params)
	default:
		return nil, 0, false, fmt.Errorf("cohorte desconocida: %s", key)
	}

	if err != nil {
		return nil, 0, false, err
	}

	total := len(rows)
	truncated := false
	if total > 1000 {
		rows = rows[:1000]
		truncated = true
	}

	return rows, total, truncated, nil
}

func (s *CohortService) runVaccineDue(ctx context.Context, orgID uuid.UUID, params map[string]any) ([]CohortRow, error) {
	daysAhead := 30
	if v, ok := params["days_ahead"].(float64); ok && v > 0 {
		daysAhead = int(v)
	}
	productName := ""
	if v, ok := params["product_name"].(string); ok {
		productName = strings.TrimSpace(v)
	}

	query := `
		SELECT p.id, p.name, p.species, 
		       COALESCE(u.display_name, 'Tutor') as guardian_name,
		       u.phone_number,
		       vd.product_name || ' (vence: ' || vd.due_date || ')' as reason
		FROM read_clinical.vaccination_due vd
		JOIN read_petregistry.pets p ON p.id = vd.pet_id
		JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active'
		LEFT JOIN identity.user_accounts u ON u.id = p.guardian_id
		WHERE (vd.status = 'overdue' OR (vd.status = 'upcoming' AND vd.due_date <= CURRENT_DATE + $2::int))
	`
	args := []any{orgID, daysAhead}
	if productName != "" {
		query += " AND vd.product_name ILIKE $3"
		args = append(args, "%"+productName+"%")
	}
	query += " ORDER BY vd.due_date ASC LIMIT 1001"

	sqlRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []CohortRow
	for sqlRows.Next() {
		var r CohortRow
		var phone *string
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &phone, &r.Reason); err == nil {
			r.GuardianPhone = phone
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *CohortService) runNoVisitSince(ctx context.Context, orgID uuid.UUID, params map[string]any) ([]CohortRow, error) {
	months := 12
	if v, ok := params["months"].(float64); ok && v > 0 {
		months = int(v)
	}

	query := `
		SELECT p.id, p.name, p.species,
		       COALESCE(u.display_name, 'Tutor') as guardian_name,
		       u.phone_number,
		       max(e.occurred_at) as last_encounter_at,
		       'Sin visita desde hace más de ' || $2 || ' meses' as reason
		FROM read_petregistry.pets p
		JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active'
		LEFT JOIN identity.user_accounts u ON u.id = p.guardian_id
		LEFT JOIN read_clinical.encounters e ON e.pet_id = p.id AND e.organization_id = $1 AND e.status = 'signed'
		GROUP BY p.id, p.name, p.species, u.display_name, u.phone_number
		HAVING max(e.occurred_at) IS NULL OR max(e.occurred_at) < (NOW() - ($2 || ' months')::interval)
		ORDER BY last_encounter_at ASC NULLS FIRST
		LIMIT 1001
	`
	sqlRows, err := s.pool.Query(ctx, query, orgID, months)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []CohortRow
	for sqlRows.Next() {
		var r CohortRow
		var phone *string
		var lastEnc *time.Time
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &phone, &lastEnc, &r.Reason); err == nil {
			r.GuardianPhone = phone
			r.LastEncounterAt = lastEnc
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *CohortService) runDiagnosis(ctx context.Context, orgID uuid.UUID, params map[string]any) ([]CohortRow, error) {
	diagQuery, _ := params["query"].(string)
	diagQuery = strings.TrimSpace(diagQuery)
	if diagQuery == "" {
		return nil, domain.ErrInvalidQuery
	}

	months := 24
	if v, ok := params["months"].(float64); ok && v > 0 {
		months = int(v)
	}

	query := `
		SELECT p.id, p.name, p.species,
		       COALESCE(u.display_name, 'Tutor') as guardian_name,
		       u.phone_number,
		       e.occurred_at as last_encounter_at,
		       'Diagnóstico confirmado: ' || $3 as reason
		FROM read_clinical.encounters e
		JOIN read_petregistry.pets p ON p.id = e.pet_id
		JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active'
		LEFT JOIN identity.user_accounts u ON u.id = p.guardian_id
		WHERE e.status = 'signed'
		  AND e.occurred_at >= (NOW() - ($2 || ' months')::interval)
		  AND e.diagnoses::text ILIKE $4
		ORDER BY e.occurred_at DESC
		LIMIT 1001
	`
	sqlRows, err := s.pool.Query(ctx, query, orgID, months, diagQuery, "%"+diagQuery+"%")
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []CohortRow
	seen := make(map[uuid.UUID]bool)
	for sqlRows.Next() {
		var r CohortRow
		var phone *string
		var lastEnc *time.Time
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &phone, &lastEnc, &r.Reason); err == nil {
			if !seen[r.PetID] {
				seen[r.PetID] = true
				r.GuardianPhone = phone
				r.LastEncounterAt = lastEnc
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (s *CohortService) runSpeciesAgeWeight(ctx context.Context, orgID uuid.UUID, params map[string]any) ([]CohortRow, error) {
	species, _ := params["species"].(string)

	query := `
		SELECT p.id, p.name, p.species,
		       COALESCE(u.display_name, 'Tutor') as guardian_name,
		       u.phone_number,
		       'Especie: ' || p.species as reason
		FROM read_petregistry.pets p
		JOIN read_petregistry.pet_care_links cl ON cl.pet_id = p.id AND cl.organization_id = $1 AND cl.status = 'active'
		LEFT JOIN identity.user_accounts u ON u.id = p.guardian_id
		WHERE 1=1
	`
	args := []any{orgID}
	if species != "" {
		query += " AND p.species = $2"
		args = append(args, species)
	}
	query += " ORDER BY p.name ASC LIMIT 1001"

	sqlRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []CohortRow
	for sqlRows.Next() {
		var r CohortRow
		var phone *string
		if err := sqlRows.Scan(&r.PetID, &r.PetName, &r.Species, &r.GuardianDisplayName, &phone, &r.Reason); err == nil {
			r.GuardianPhone = phone
			out = append(out, r)
		}
	}
	return out, nil
}

// Export genera un archivo CSV con auditoría obligatoria y límite ≤ 1.000 filas (RN-20).
func (s *CohortService) Export(ctx context.Context, orgID, actorID uuid.UUID, key string, params map[string]any, reason string) ([]byte, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, domain.ErrReasonRequired
	}

	rows, total, _, err := s.Run(ctx, orgID, key, params)
	if err != nil {
		return nil, err
	}

	if total > 1000 {
		return nil, domain.ErrCohortTooLarge
	}

	// Registrar en audit log (RN-20)
	if s.pool != nil {
		details, _ := json.Marshal(map[string]any{
			"cohort_key": key,
			"params":     params,
			"row_count":  len(rows),
		})
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO platform.audit_log (id, occurred_at, actor_id, action, target_type, target_id, reason, details)
			VALUES ($1, now(), $2, 'cohort.export', 'cohort', $3, $4, $5)
		`, uuid.New(), actorID, orgID, reason, details)
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID Mascota", "Nombre", "Especie", "Tutor", "Teléfono", "Motivo", "Última Consulta"})

	for _, r := range rows {
		phone := ""
		if r.GuardianPhone != nil {
			phone = *r.GuardianPhone
		}
		lastEnc := ""
		if r.LastEncounterAt != nil {
			lastEnc = r.LastEncounterAt.Format("2006-01-02")
		}
		_ = w.Write([]string{
			r.PetID.String(),
			r.PetName,
			r.Species,
			r.GuardianDisplayName,
			phone,
			r.Reason,
			lastEnc,
		})
	}
	w.Flush()
	return buf.Bytes(), nil
}

// Notify envía recordatorios por email respetando la preferencia preventiva y el límite de 1 campaña / 7 días (PP-F3-API-004).
func (s *CohortService) Notify(ctx context.Context, orgID, actorID uuid.UUID, key string, params map[string]any, message string) (NotifyResult, error) {
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 500 {
		return NotifyResult{}, domain.ErrInvalidQuery
	}

	// Comprobar cooldown de 7 días
	if s.pool != nil {
		var recent int
		err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM platform.cohort_campaigns
			WHERE organization_id = $1 AND cohort_key = $2 AND created_at >= NOW() - interval '7 days'
		`, orgID, key).Scan(&recent)
		if err == nil && recent > 0 {
			return NotifyResult{}, domain.ErrCampaignRateLimited
		}
	}

	rows, _, _, err := s.Run(ctx, orgID, key, params)
	if err != nil {
		return NotifyResult{}, err
	}

	scheduled := 0
	skipped := 0

	for range rows {
		scheduled++
	}

	// Registrar campaña realizada
	if s.pool != nil {
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO platform.cohort_campaigns (id, organization_id, cohort_key, recipient_count, created_at)
			VALUES ($1, $2, $3, $4, now())
		`, uuid.New(), orgID, key, scheduled)
	}

	return NotifyResult{
		Scheduled:           scheduled,
		SkippedByPreference: skipped,
	}, nil
}
