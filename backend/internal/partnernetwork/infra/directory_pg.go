package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
)

// PGDirectoryStore implementa app.DirectoryStore con PostgreSQL (PP-F3-DOM-010, SEC-008).
type PGDirectoryStore struct {
	pool *pgxpool.Pool
}

func NewPGDirectoryStore(pool *pgxpool.Pool) *PGDirectoryStore {
	return &PGDirectoryStore{pool: pool}
}

// Search busca organizaciones activas y opt-in en el directorio público.
func (s *PGDirectoryStore) Search(ctx context.Context, q, city, country string, limit int) ([]app.DirectoryOrgItem, error) {
	if s.pool == nil {
		return []app.DirectoryOrgItem{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := `
		SELECT o.id, o.display_name, o.type, o.address_city,
		       d.neighborhood, d.services,
		       CASE WHEN d.show_phone THEN o.phone ELSE NULL END as public_phone,
		       d.geo_lat, d.geo_lon
		FROM partnernetwork.directory_listings d
		JOIN read_partnernetwork.organizations o ON o.id = d.organization_id
		WHERE d.opt_in = true AND o.status = 'active'
	`
	args := []any{}
	argIdx := 1

	if q != "" {
		query += fmt.Sprintf(" AND (o.display_name ILIKE $%d OR $%d = ANY(d.services))", argIdx, argIdx)
		args = append(args, "%"+q+"%")
		argIdx++
	}
	if city != "" {
		query += fmt.Sprintf(" AND o.address_city ILIKE $%d", argIdx)
		args = append(args, city)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY o.display_name ASC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []app.DirectoryOrgItem
	for rows.Next() {
		var item app.DirectoryOrgItem
		var rawServices []byte
		var lat, lon *float64

		if err := rows.Scan(
			&item.ID, &item.DisplayName, &item.Type, &item.City,
			&item.Neighborhood, &rawServices, &item.Phone,
			&lat, &lon,
		); err != nil {
			return nil, err
		}

		if len(rawServices) > 0 {
			_ = json.Unmarshal(rawServices, &item.Services)
		}
		if item.Services == nil {
			item.Services = []string{}
		}

		if lat != nil && lon != nil {
			roundedLat := math.Round(*lat*1000) / 1000
			roundedLon := math.Round(*lon*1000) / 1000
			item.Geo = &app.GeoPoint{Lat: roundedLat, Lon: roundedLon}
		}

		out = append(out, item)
	}

	return out, nil
}

// SearchNearby busca clínicas cercanas a un centro geográfico dentro de un radio.
func (s *PGDirectoryStore) SearchNearby(ctx context.Context, center app.GeoPoint, radiusKm float64, service, q string, limit int) ([]app.DirectoryOrgItem, error) {
	if s.pool == nil {
		return []app.DirectoryOrgItem{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if radiusKm <= 0 {
		radiusKm = 15.0
	}

	query := `
		SELECT o.id, o.display_name, o.type, o.address_city,
		       d.neighborhood, d.services,
		       CASE WHEN d.show_phone THEN o.phone ELSE NULL END as public_phone,
		       d.geo_lat, d.geo_lon
		FROM partnernetwork.directory_listings d
		JOIN read_partnernetwork.organizations o ON o.id = d.organization_id
		WHERE d.opt_in = true AND o.status = 'active'
		  AND d.geo_lat IS NOT NULL AND d.geo_lon IS NOT NULL
	`
	args := []any{}
	argIdx := 1

	if service != "" {
		query += fmt.Sprintf(" AND $%d = ANY(d.services)", argIdx)
		args = append(args, service)
		argIdx++
	}
	if q != "" {
		query += fmt.Sprintf(" AND o.display_name ILIKE $%d", argIdx)
		args = append(args, "%"+q+"%")
		argIdx++
	}

	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []app.DirectoryOrgItem
	for rows.Next() {
		var item app.DirectoryOrgItem
		var rawServices []byte
		var lat, lon *float64

		if err := rows.Scan(
			&item.ID, &item.DisplayName, &item.Type, &item.City,
			&item.Neighborhood, &rawServices, &item.Phone,
			&lat, &lon,
		); err != nil {
			return nil, err
		}

		if len(rawServices) > 0 {
			_ = json.Unmarshal(rawServices, &item.Services)
		}
		if item.Services == nil {
			item.Services = []string{}
		}

		if lat != nil && lon != nil {
			dist := haversineDistance(center.Lat, center.Lon, *lat, *lon)
			if dist <= radiusKm {
				roundedLat := math.Round(*lat*1000) / 1000
				roundedLon := math.Round(*lon*1000) / 1000
				item.Geo = &app.GeoPoint{Lat: roundedLat, Lon: roundedLon}
				roundedDist := math.Round(dist*10) / 10
				item.DistanceKm = &roundedDist
				out = append(out, item)
			}
		}
	}

	return out, nil
}

// Get obtiene la ficha pública mínima de una organización por su ID.
func (s *PGDirectoryStore) Get(ctx context.Context, id uuid.UUID) (*app.DirectoryOrgItem, error) {
	if s.pool == nil {
		return nil, errors.New("organización no existe o no es visible en el directorio")
	}

	query := `
		SELECT o.id, o.display_name, o.type, o.address_city,
		       d.neighborhood, d.services,
		       CASE WHEN d.show_phone THEN o.phone ELSE NULL END as public_phone,
		       d.geo_lat, d.geo_lon
		FROM partnernetwork.directory_listings d
		JOIN read_partnernetwork.organizations o ON o.id = d.organization_id
		WHERE o.id = $1 AND d.opt_in = true AND o.status = 'active'
	`

	var item app.DirectoryOrgItem
	var rawServices []byte
	var lat, lon *float64

	if err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.DisplayName, &item.Type, &item.City,
		&item.Neighborhood, &rawServices, &item.Phone,
		&lat, &lon,
	); err != nil {
		return nil, errors.New("organización no existe o no es visible en el directorio")
	}

	if len(rawServices) > 0 {
		_ = json.Unmarshal(rawServices, &item.Services)
	}
	if item.Services == nil {
		item.Services = []string{}
	}

	if lat != nil && lon != nil {
		roundedLat := math.Round(*lat*1000) / 1000
		roundedLon := math.Round(*lon*1000) / 1000
		item.Geo = &app.GeoPoint{Lat: roundedLat, Lon: roundedLon}
	}

	return &item, nil
}

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}
