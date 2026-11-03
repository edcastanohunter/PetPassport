package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// CachedPetAccess almacena en caché distribuida y L1 los accesos de una mascota (ARQ-006, SEC-001).
type CachedPetAccess struct {
	pool  *pgxpool.Pool
	cache *cache.Cache
}

// NewCachedPetAccess construye el cargador con caché de PetAccess.
func NewCachedPetAccess(pool *pgxpool.Pool, c *cache.Cache) *CachedPetAccess {
	return &CachedPetAccess{
		pool:  pool,
		cache: c,
	}
}

type petAccessCacheDTO struct {
	GuardianID string   `msgpack:"g"`
	Linked     []string `msgpack:"l"`
	Custody    []string `msgpack:"c"`
}

// GetPetAccess obtiene los accesos de la mascota desde caché o PostgreSQL.
func (c *CachedPetAccess) GetPetAccess(ctx context.Context, petID uuid.UUID) (*domain.PetAccessRecord, error) {
	key := fmt.Sprintf("%s:petaccess:%s", cache.SchemaVersion, petID)

	// Intentar desde caché L1 / Redis
	if c.cache != nil {
		var dto petAccessCacheDTO
		err := c.cache.Get(ctx, key, &dto)
		if err == nil {
			gUUID, _ := uuid.Parse(dto.GuardianID)
			rec := &domain.PetAccessRecord{
				PetID:      petID,
				GuardianID: gUUID,
			}
			for _, s := range dto.Linked {
				if u, err := uuid.Parse(s); err == nil {
					rec.LinkedOrgIDs = append(rec.LinkedOrgIDs, u)
				}
			}
			for _, s := range dto.Custody {
				if u, err := uuid.Parse(s); err == nil {
					rec.CustodyOrgIDs = append(rec.CustodyOrgIDs, u)
				}
			}
			return rec, nil
		}
	}

	// Cargar de PostgreSQL
	rec, err := c.loadFromDB(ctx, petID)
	if err != nil {
		return nil, err
	}

	// Guardar en caché con TTL de 60 s (ARQ-006)
	if c.cache != nil && rec != nil {
		dto := petAccessCacheDTO{
			GuardianID: rec.GuardianID.String(),
		}
		for _, u := range rec.LinkedOrgIDs {
			dto.Linked = append(dto.Linked, u.String())
		}
		for _, u := range rec.CustodyOrgIDs {
			dto.Custody = append(dto.Custody, u.String())
		}
		_ = c.cache.Set(ctx, key, dto, 60*time.Second)
	}

	return rec, nil
}

func (c *CachedPetAccess) loadFromDB(ctx context.Context, petID uuid.UUID) (*domain.PetAccessRecord, error) {
	var guardianID uuid.UUID
	err := c.pool.QueryRow(ctx, "SELECT guardian_id FROM read_petregistry.pets WHERE id = $1", petID).Scan(&guardianID)
	if err != nil {
		return nil, err
	}

	rec := &domain.PetAccessRecord{
		PetID:      petID,
		GuardianID: guardianID,
	}

	// Linked orgs
	rows, err := c.pool.Query(ctx, `
		SELECT organization_id
		FROM read_petregistry.pet_care_links
		WHERE pet_id = $1 AND status = 'active' AND org_suspended = false
	`, petID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var orgID uuid.UUID
			if err := rows.Scan(&orgID); err == nil {
				rec.LinkedOrgIDs = append(rec.LinkedOrgIDs, orgID)
			}
		}
	}

	// Custody orgs
	cRows, err := c.pool.Query(ctx, `
		SELECT DISTINCT organization_id
		FROM read_clinical.encounter_custody
		WHERE pet_id = $1
	`, petID)
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var orgID uuid.UUID
			if err := cRows.Scan(&orgID); err == nil {
				rec.CustodyOrgIDs = append(rec.CustodyOrgIDs, orgID)
			}
		}
	}

	return rec, nil
}

// InvalidatePetAccess invalida la clave de caché cuando cambian vínculos o custodia.
func (c *CachedPetAccess) InvalidatePetAccess(ctx context.Context, petID uuid.UUID) error {
	if c.cache == nil {
		return nil
	}
	key := fmt.Sprintf("%s:petaccess:%s", cache.SchemaVersion, petID)
	return c.cache.Delete(ctx, key)
}

// GetPetIDsByOrg devuelve los IDs de mascotas vinculadas a una organización.
func (c *CachedPetAccess) GetPetIDsByOrg(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT pet_id FROM read_petregistry.pet_care_links
		WHERE organization_id = $1
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var petIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			petIDs = append(petIDs, id)
		}
	}
	return petIDs, rows.Err()
}
