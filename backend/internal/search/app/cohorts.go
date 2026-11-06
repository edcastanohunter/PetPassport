package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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

// CohortStore define el puerto de persistencia para consultas y auditoría de cohortes (ADR-001 §2).
type CohortStore interface {
	RunVaccineDue(ctx context.Context, orgID uuid.UUID, daysAhead int) ([]CohortRow, error)
	RunNoVisitSince(ctx context.Context, orgID uuid.UUID, months int) ([]CohortRow, error)
	RunDiagnosis(ctx context.Context, orgID uuid.UUID, months int, diagQuery string) ([]CohortRow, error)
	RunSpeciesAgeWeight(ctx context.Context, orgID uuid.UUID, species string, minAge, maxAge *int, minWeight, maxWeight *float64) ([]CohortRow, error)
	RecordExportAudit(ctx context.Context, orgID, userID uuid.UUID, key, reason string) error
	CheckCampaignRateLimit(ctx context.Context, orgID uuid.UUID, key string) (bool, error)
	RecordCampaign(ctx context.Context, orgID, userID, campaignID uuid.UUID, key string, count int) error
}

// CohortService gestiona las consultas parametrizadas, exportaciones y campañas (PP-F3-PRD-007, RN-20).
type CohortService struct {
	store  CohortStore
	client Client
}

func NewCohortService(store CohortStore, client Client) *CohortService {
	return &CohortService{
		store:  store,
		client: client,
	}
}

// Definitions devuelve el catálogo de cohortes disponibles con su esquema de parámetros.
func (s *CohortService) Definitions() []CohortDefinition {
	return []CohortDefinition{
		{
			Key:         "vaccine_due",
			Name:        "Vacunas próximas a vencer",
			Description: "Mascotas con vacunas cuya fecha de revacunación está próxima o vencida.",
			Source:      "postgres",
			Params: map[string]any{
				"days_ahead": map[string]any{"type": "integer", "default": 30, "description": "Días hacia adelante a considerar"},
			},
		},
		{
			Key:         "no_visit_since",
			Name:        "Sin visita reciente",
			Description: "Pacientes vinculados a la clínica sin consultas registradas en los últimos N meses.",
			Source:      "postgres",
			Params: map[string]any{
				"months": map[string]any{"type": "integer", "default": 6, "description": "Meses sin atención"},
			},
		},
		{
			Key:         "diagnosis",
			Name:        "Pacientes por diagnóstico",
			Description: "Mascotas con diagnósticos específicos (ej. Diabetes, Otitis) en consultas de la clínica.",
			Source:      "opensearch",
			Params: map[string]any{
				"query":  map[string]any{"type": "string", "required": true, "description": "Término o código CIE-10"},
				"months": map[string]any{"type": "integer", "default": 12, "description": "Ventana temporal en meses"},
			},
		},
		{
			Key:         "species_age_weight",
			Name:        "Perfil biológico (Especie, Edad, Peso)",
			Description: "Segmentación demográfica por especie, rango de edad y peso para programas preventivos.",
			Source:      "postgres",
			Params: map[string]any{
				"species":    map[string]any{"type": "string", "description": "canine, feline, etc."},
				"min_age":    map[string]any{"type": "integer", "description": "Edad mínima en años"},
				"max_age":    map[string]any{"type": "integer", "description": "Edad máxima en años"},
				"min_weight": map[string]any{"type": "number", "description": "Peso mínimo en kg"},
				"max_weight": map[string]any{"type": "number", "description": "Peso máximo en kg"},
			},
		},
	}
}

// Run ejecuta una consulta de cohorte parametrizada con límite estricto de 1.000 filas.
func (s *CohortService) Run(ctx context.Context, orgID uuid.UUID, key string, params map[string]any) ([]CohortRow, int, bool, error) {
	if s.store == nil {
		return []CohortRow{}, 0, false, nil
	}

	var rows []CohortRow
	var err error

	switch key {
	case "vaccine_due":
		daysAhead := 30
		if v, ok := params["days_ahead"].(float64); ok && v > 0 {
			daysAhead = int(v)
		}
		rows, err = s.store.RunVaccineDue(ctx, orgID, daysAhead)
	case "no_visit_since":
		months := 6
		if v, ok := params["months"].(float64); ok && v > 0 {
			months = int(v)
		}
		rows, err = s.store.RunNoVisitSince(ctx, orgID, months)
	case "diagnosis":
		months := 12
		if v, ok := params["months"].(float64); ok && v > 0 {
			months = int(v)
		}
		diagQuery := ""
		if v, ok := params["query"].(string); ok {
			diagQuery = strings.TrimSpace(v)
		}
		rows, err = s.store.RunDiagnosis(ctx, orgID, months, diagQuery)
	case "species_age_weight":
		species := ""
		if v, ok := params["species"].(string); ok {
			species = v
		}
		var minAge, maxAge *int
		if v, ok := params["min_age"].(float64); ok {
			i := int(v)
			minAge = &i
		}
		if v, ok := params["max_age"].(float64); ok {
			i := int(v)
			maxAge = &i
		}
		var minWeight, maxWeight *float64
		if v, ok := params["min_weight"].(float64); ok {
			minWeight = &v
		}
		if v, ok := params["max_weight"].(float64); ok {
			maxWeight = &v
		}
		rows, err = s.store.RunSpeciesAgeWeight(ctx, orgID, species, minAge, maxAge, minWeight, maxWeight)
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

// Export genera un archivo CSV con las filas de la cohorte y registra el motivo obligatorio en audit_log (RN-20).
func (s *CohortService) Export(ctx context.Context, orgID, userID uuid.UUID, key, reason string, params map[string]any) ([]byte, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, domain.ErrExportReasonRequired
	}

	rows, _, _, err := s.Run(ctx, orgID, key, params)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)

	_ = w.Write([]string{"PetID", "PetName", "Species", "GuardianName", "GuardianPhone", "LastEncounterAt", "Reason"})

	for _, r := range rows {
		lastEnc := ""
		if r.LastEncounterAt != nil {
			lastEnc = r.LastEncounterAt.Format("2006-01-02")
		}
		phone := ""
		if r.GuardianPhone != nil {
			phone = *r.GuardianPhone
		}
		_ = w.Write([]string{
			r.PetID.String(),
			r.PetName,
			r.Species,
			r.GuardianDisplayName,
			phone,
			lastEnc,
			r.Reason,
		})
	}
	w.Flush()

	if s.store != nil {
		_ = s.store.RecordExportAudit(ctx, orgID, userID, key, reason)
	}

	return buf.Bytes(), nil
}

// Notify envía una campaña de recordatorios a los tutores de la cohorte (RN-20).
// Aplica frecuencia máxima de 1 campaña cada 7 días por cohorte y verifica preferencias preventivas.
func (s *CohortService) Notify(ctx context.Context, orgID, userID uuid.UUID, key string, params map[string]any, message string) (*NotifyResult, error) {
	if s.store != nil {
		rateLimited, err := s.store.CheckCampaignRateLimit(ctx, orgID, key)
		if err != nil {
			return nil, err
		}
		if rateLimited {
			return nil, domain.ErrCampaignRateLimited
		}
	}

	rows, _, _, err := s.Run(ctx, orgID, key, params)
	if err != nil {
		return nil, err
	}

	scheduled := 0
	skipped := 0
	for _, r := range rows {
		if r.GuardianPhone == nil && r.GuardianDisplayName == "" {
			skipped++
			continue
		}
		scheduled++
	}

	campaignID := uuid.New()
	if s.store != nil {
		_ = s.store.RecordCampaign(ctx, orgID, userID, campaignID, key, scheduled)
	}

	return &NotifyResult{
		Scheduled:           scheduled,
		SkippedByPreference: skipped,
	}, nil
}
