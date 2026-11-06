package app

import (
	"context"

	"github.com/google/uuid"
)

// TopOrgItem representa una clínica destacada por volumen de actividad (PP-F3-PRD-008, PP-F3-API-006).
type TopOrgItem struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	DisplayName    string    `json:"display_name"`
	Metric         string    `json:"metric"`
	Count          int64     `json:"count"`
}

// PlatformStatsRepository define el puerto para persistencia de estadísticas globales (ADR-001 §2).
type PlatformStatsRepository interface {
	GetStats(ctx context.Context, granularity, from, to string) ([]map[string]any, error)
	GetTopOrganizations(ctx context.Context, metric, period string) ([]TopOrgItem, error)
	RefreshDailyStats(ctx context.Context) error
}

// PlatformStatsService gestiona las estadísticas globales de plataforma (PP-F3-DAT-008, PP-F3-API-006).
type PlatformStatsService struct {
	repo PlatformStatsRepository
}

func NewPlatformStatsService(repo PlatformStatsRepository) *PlatformStatsService {
	return &PlatformStatsService{repo: repo}
}

// GetStats devuelve la serie temporal de métricas agregadas por día/semana/mes.
func (s *PlatformStatsService) GetStats(ctx context.Context, granularity, from, to string) ([]map[string]any, error) {
	if s.repo == nil {
		return []map[string]any{}, nil
	}
	return s.repo.GetStats(ctx, granularity, from, to)
}

// GetTopOrganizations devuelve las clínicas con mayor actividad según la métrica indicada.
func (s *PlatformStatsService) GetTopOrganizations(ctx context.Context, metric, period string) ([]TopOrgItem, error) {
	if s.repo == nil {
		return []TopOrgItem{}, nil
	}
	return s.repo.GetTopOrganizations(ctx, metric, period)
}

// RefreshDailyStats genera y guarda las métricas del día en platform.stats_daily.
func (s *PlatformStatsService) RefreshDailyStats(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	return s.repo.RefreshDailyStats(ctx)
}
