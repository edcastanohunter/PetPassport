package infra

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
)

// PGPlatformStatsRepository implementa app.PlatformStatsRepository con PostgreSQL (PP-F3-DAT-008).
type PGPlatformStatsRepository struct {
	pool *pgxpool.Pool
}

func NewPGPlatformStatsRepository(pool *pgxpool.Pool) *PGPlatformStatsRepository {
	return &PGPlatformStatsRepository{pool: pool}
}

// GetStats devuelve la serie temporal de métricas agregadas por día/semana/mes.
func (r *PGPlatformStatsRepository) GetStats(ctx context.Context, granularity, from, to string) ([]map[string]any, error) {
	if r.pool == nil {
		return []map[string]any{}, nil
	}

	rows, err := r.pool.Query(ctx, `
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
		live, _ := r.computeCurrentMetrics(ctx)
		live["date"] = time.Now().Format("2006-01-02")
		out = append(out, live)
	}

	return out, nil
}

// GetTopOrganizations devuelve las clínicas con mayor actividad según la métrica indicada.
func (r *PGPlatformStatsRepository) GetTopOrganizations(ctx context.Context, metric, period string) ([]app.TopOrgItem, error) {
	if r.pool == nil {
		return []app.TopOrgItem{}, nil
	}

	rows, err := r.pool.Query(ctx, `
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

	var out []app.TopOrgItem
	for rows.Next() {
		var item app.TopOrgItem
		item.Metric = "encounters_signed"
		if err := rows.Scan(&item.OrganizationID, &item.DisplayName, &item.Count); err == nil {
			out = append(out, item)
		}
	}
	return out, nil
}

// RefreshDailyStats genera y guarda las métricas del día en platform.stats_daily (asynq job stats:refresh).
func (r *PGPlatformStatsRepository) RefreshDailyStats(ctx context.Context) error {
	if r.pool == nil {
		return nil
	}

	metrics, err := r.computeCurrentMetrics(ctx)
	if err != nil {
		return err
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")
	_, err = r.pool.Exec(ctx, `
		INSERT INTO platform.stats_daily (day, metrics, refreshed_at)
		VALUES ($1, $2, now())
		ON CONFLICT (day) DO UPDATE
		SET metrics = EXCLUDED.metrics, refreshed_at = now()
	`, today, data)

	return err
}

func (r *PGPlatformStatsRepository) computeCurrentMetrics(ctx context.Context) (map[string]any, error) {
	metrics := map[string]any{
		"users_active":      int64(0),
		"pets":              int64(0),
		"encounters_signed": int64(0),
		"vaccinations":      int64(0),
		"organizations":     map[string]int64{"active": 0, "pending": 0, "suspended": 0},
	}

	if r.pool == nil {
		return metrics, nil
	}

	var usersActive, pets, encountersSigned, activeOrgs int64
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM identity.user_accounts WHERE status = 'active'`).Scan(&usersActive)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM read_petregistry.pets WHERE status = 'active'`).Scan(&pets)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM read_clinical.encounters WHERE status = 'signed'`).Scan(&encountersSigned)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM read_partnernetwork.organizations WHERE status = 'active'`).Scan(&activeOrgs)

	metrics["users_active"] = usersActive
	metrics["pets"] = pets
	metrics["encounters_signed"] = encountersSigned
	metrics["organizations"] = map[string]int64{"active": activeOrgs}

	return metrics, nil
}
