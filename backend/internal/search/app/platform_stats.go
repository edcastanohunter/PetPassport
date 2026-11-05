package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TopOrgItem representa una clínica destacada por volumen de actividad (PP-F3-PRD-008, PP-F3-API-006).
type TopOrgItem struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	DisplayName    string    `json:"display_name"`
	Metric         string    `json:"metric"`
	Count          int64     `json:"count"`
}

// PlatformStatsService gestiona las estadísticas globales de plataforma (PP-F3-DAT-008, PP-F3-API-006).
type PlatformStatsService struct {
	pool *pgxpool.Pool
}

func NewPlatformStatsService(pool *pgxpool.Pool) *PlatformStatsService {
	return &PlatformStatsService{pool: pool}
}

// GetStats devuelve la serie temporal de métricas agregadas por día/semana/mes.
func (s *PlatformStatsService) GetStats(ctx context.Context, granularity, from, to string) ([]map[string]any, error) {
	if s.pool == nil {
		return []map[string]any{}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT day, metrics FROM platform.stats_daily
		ORDER BY day ASC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var day time.Time
		var rawMetrics []byte
		if err := rows.Scan(&day, &rawMetrics); err == nil {
			var m map[string]any
			if err := json.Unmarshal(rawMetrics, &m); err == nil {
				m["date"] = day.Format("2006-01-02")
				out = append(out, m)
			}
		}
	}

	if len(out) == 0 {
		// Métricas en tiempo real si aún no hay snapshots diarios
		live, _ := s.computeCurrentMetrics(ctx)
		live["date"] = time.Now().Format("2006-01-02")
		out = append(out, live)
	}

	return out, nil
}

// GetTopOrganizations devuelve las clínicas con mayor actividad según la métrica indicada.
func (s *PlatformStatsService) GetTopOrganizations(ctx context.Context, metric, period string) ([]TopOrgItem, error) {
	if s.pool == nil {
		return []TopOrgItem{}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.display_name, count(e.id) as encounters_count
		FROM read_partnernetwork.organizations o
		LEFT JOIN read_clinical.encounters e ON e.organization_id = o.id AND e.status = 'signed'
		WHERE o.status = 'active'
		GROUP BY o.id, o.display_name
		ORDER BY encounters_count DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TopOrgItem
	for rows.Next() {
		var item TopOrgItem
		item.Metric = "encounters_signed"
		if err := rows.Scan(&item.OrganizationID, &item.DisplayName, &item.Count); err == nil {
			out = append(out, item)
		}
	}
	return out, nil
}

// RefreshDailyStats genera y guarda las métricas del día en platform.stats_daily (asynq job stats:refresh).
func (s *PlatformStatsService) RefreshDailyStats(ctx context.Context) error {
	if s.pool == nil {
		return nil
	}

	metrics, err := s.computeCurrentMetrics(ctx)
	if err != nil {
		return err
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")
	_, err = s.pool.Exec(ctx, `
		INSERT INTO platform.stats_daily (day, metrics, refreshed_at)
		VALUES ($1, $2, now())
		ON CONFLICT (day) DO UPDATE
		SET metrics = EXCLUDED.metrics, refreshed_at = now()
	`, today, data)

	return err
}

func (s *PlatformStatsService) computeCurrentMetrics(ctx context.Context) (map[string]any, error) {
	metrics := map[string]any{
		"users_active":      int64(0),
		"pets":              int64(0),
		"encounters_signed": int64(0),
		"vaccinations":      int64(0),
		"organizations":     map[string]int64{"active": 0, "pending": 0, "suspended": 0},
	}

	if s.pool == nil {
		return metrics, nil
	}

	var usersActive, pets, encountersSigned, activeOrgs int64
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM identity.user_accounts WHERE status = 'active'`).Scan(&usersActive)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM read_petregistry.pets WHERE status = 'active'`).Scan(&pets)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM read_clinical.encounters WHERE status = 'signed'`).Scan(&encountersSigned)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM read_partnernetwork.organizations WHERE status = 'active'`).Scan(&activeOrgs)

	metrics["users_active"] = usersActive
	metrics["pets"] = pets
	metrics["encounters_signed"] = encountersSigned
	metrics["organizations"] = map[string]int64{"active": activeOrgs}

	return metrics, nil
}
