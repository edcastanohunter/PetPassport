package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// GeoPoint representa coordenadas geográficas.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// DirectoryOrgItem representa la ficha pública mínima de una clínica en el directorio (PP-F3-DOM-010, PP-F3-API-005, SEC-008).
type DirectoryOrgItem struct {
	ID           uuid.UUID `json:"id"`
	DisplayName  string    `json:"display_name"`
	Type         string    `json:"type"`
	City         string    `json:"city"`
	Neighborhood *string   `json:"neighborhood,omitempty"`
	Services     []string  `json:"services"`
	Phone        *string   `json:"phone,omitempty"`
	Geo          *GeoPoint `json:"geo,omitempty"`
	DistanceKm   *float64  `json:"distance_km,omitempty"`
}

// DirectoryService implementa las consultas públicas del directorio opt-in.
type DirectoryService struct {
	pool *pgxpool.Pool
}

func NewDirectoryService(pool *pgxpool.Pool) *DirectoryService {
	return &DirectoryService{pool: pool}
}

// Search busca organizaciones activas y opt-in en el directorio público.
// Aplica anonimización de geo a 3 decimales (≈ 100 m) y nunca expone dirección exacta ni pacientes (SEC-008).
func (s *DirectoryService) Search(ctx context.Context, q, city, country string, limit int) ([]DirectoryOrgItem, error) {
	if s.pool == nil {
		return []DirectoryOrgItem{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	query := `
		SELECT id, display_name, type, city, directory_neighborhood, directory_services,
		       directory_show_phone, contact_phone, directory_geo
		FROM read_partnernetwork.organizations
		WHERE directory_listed = true AND status = 'active'
	`
	var args []any
	argIdx := 1

	if strings.TrimSpace(city) != "" {
		query += fmt.Sprintf(" AND city ILIKE $%d", argIdx)
		args = append(args, "%"+strings.TrimSpace(city)+"%")
		argIdx++
	}

	if strings.TrimSpace(q) != "" {
		query += fmt.Sprintf(" AND (display_name ILIKE $%d OR city ILIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+strings.TrimSpace(q)+"%")
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY display_name ASC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []DirectoryOrgItem
	for rows.Next() {
		item, err := scanDirectoryRow(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	return items, nil
}

// Get obtiene la ficha pública mínima de una organización por su ID.
func (s *DirectoryService) Get(ctx context.Context, id uuid.UUID) (DirectoryOrgItem, error) {
	if s.pool == nil {
		return DirectoryOrgItem{}, &kernel.NotFoundError{Resource: "organization"}
	}

	row := s.pool.QueryRow(ctx, `
		SELECT id, display_name, type, city, directory_neighborhood, directory_services,
		       directory_show_phone, contact_phone, directory_geo
		FROM read_partnernetwork.organizations
		WHERE id = $1 AND directory_listed = true AND status = 'active'
	`, id)

	item, err := scanDirectoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return DirectoryOrgItem{}, &kernel.NotFoundError{Resource: "organization"}
	}
	return item, err
}

func scanDirectoryRow(row interface{ Scan(dest ...any) error }) (DirectoryOrgItem, error) {
	var it DirectoryOrgItem
	var showPhone bool
	var phone string
	var geoRaw []byte
	var neigh *string

	err := row.Scan(&it.ID, &it.DisplayName, &it.Type, &it.City, &neigh, &it.Services, &showPhone, &phone, &geoRaw)
	if err != nil {
		return it, err
	}

	it.Neighborhood = neigh
	if showPhone && phone != "" {
		it.Phone = &phone
	}

	if len(geoRaw) > 0 {
		var g struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		if err := json.Unmarshal(geoRaw, &g); err == nil && (g.Lat != 0 || g.Lon != 0) {
			// Redondeo de coordenadas a 3 decimales (≈ 100 metros) para preservar privacidad (SEC-008)
			roundLat := math.Round(g.Lat*1000) / 1000
			roundLon := math.Round(g.Lon*1000) / 1000
			it.Geo = &GeoPoint{Lat: roundLat, Lon: roundLon}
		}
	}

	return it, nil
}
