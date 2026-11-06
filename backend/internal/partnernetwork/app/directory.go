package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
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

// DirectoryStore define el puerto de persistencia para el directorio público (ADR-001 §2).
type DirectoryStore interface {
	Search(ctx context.Context, q, city, country string, limit int) ([]DirectoryOrgItem, error)
	SearchNearby(ctx context.Context, center GeoPoint, radiusKm float64, service, q string, limit int) ([]DirectoryOrgItem, error)
	Get(ctx context.Context, id uuid.UUID) (*DirectoryOrgItem, error)
}

// DirectoryService implementa las consultas públicas del directorio opt-in.
type DirectoryService struct {
	store DirectoryStore
}

func NewDirectoryService(store DirectoryStore) *DirectoryService {
	return &DirectoryService{store: store}
}

// Get obtiene la ficha pública mínima de una organización por su ID.
func (s *DirectoryService) Get(ctx context.Context, id uuid.UUID) (*DirectoryOrgItem, error) {
	if s.store == nil {
		return nil, errors.New("organización no existe o no es visible en el directorio")
	}
	return s.store.Get(ctx, id)
}

// Search busca organizaciones activas y opt-in en el directorio público.
// Aplica anonimización de geo a 3 decimales (≈ 100 m) y nunca expone dirección exacta ni pacientes (SEC-008).
func (s *DirectoryService) Search(ctx context.Context, q, city, country string, limit int) ([]DirectoryOrgItem, error) {
	if s.store == nil {
		return []DirectoryOrgItem{}, nil
	}
	return s.store.Search(ctx, strings.TrimSpace(q), strings.TrimSpace(city), country, limit)
}

// SearchNearby busca organizaciones dentro de un radio geográfico.
func (s *DirectoryService) SearchNearby(ctx context.Context, center GeoPoint, radiusKm float64, service, q string, limit int) ([]DirectoryOrgItem, error) {
	if s.store == nil {
		return []DirectoryOrgItem{}, nil
	}
	if center.Lat < -90 || center.Lat > 90 || center.Lon < -180 || center.Lon > 180 {
		return nil, errors.New("coordenadas inválidas")
	}
	return s.store.SearchNearby(ctx, center, radiusKm, strings.TrimSpace(service), strings.TrimSpace(q), limit)
}
