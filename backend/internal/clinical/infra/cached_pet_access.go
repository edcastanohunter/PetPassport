package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
)

// PetAccessCacheEntry almacena la información de acceso de una mascota en caché (PP-F3-ARQ-006).
type PetAccessCacheEntry struct {
	GuardianID uuid.UUID   `msgpack:"guardian_id"`
	Linked     []uuid.UUID `msgpack:"linked"`
	Custody    []uuid.UUID `msgpack:"custody"`
}

// CachedPetAccessAdapter decora un app.PetAccess con caché compartida L1/L2 (ARQ-006, TTL 60 s).
type CachedPetAccessAdapter struct {
	inner app.PetAccess
	cache *cache.Cache
	ttl   time.Duration
}

var _ app.PetAccess = (*CachedPetAccessAdapter)(nil)

// NewCachedPetAccess construye el decorador con caché para PetAccess.
func NewCachedPetAccess(inner app.PetAccess, c *cache.Cache) *CachedPetAccessAdapter {
	return &CachedPetAccessAdapter{
		inner: inner,
		cache: c,
		ttl:   60 * time.Second,
	}
}

// NewCachedPetAccessAdapter es un alias de NewCachedPetAccess.
func NewCachedPetAccessAdapter(inner app.PetAccess, c *cache.Cache) *CachedPetAccessAdapter {
	return NewCachedPetAccess(inner, c)
}

func (a *CachedPetAccessAdapter) petAccessKey(petID uuid.UUID) string {
	return fmt.Sprintf("pp:v1:petaccess:%s", petID)
}

func (a *CachedPetAccessAdapter) loadEntry(ctx context.Context, petID uuid.UUID) (PetAccessCacheEntry, error) {
	if a.cache == nil {
		return a.fetchFromDB(ctx, petID)
	}

	key := a.petAccessKey(petID)
	var entry PetAccessCacheEntry
	err := a.cache.Fetch(ctx, key, a.ttl, &entry, func(loadCtx context.Context) (any, error) {
		return a.fetchFromDB(loadCtx, petID)
	})
	if err != nil {
		// Fail-open: si la caché falla, consultar directamente a la base de datos
		return a.fetchFromDB(ctx, petID)
	}
	return entry, nil
}

func (a *CachedPetAccessAdapter) fetchFromDB(ctx context.Context, petID uuid.UUID) (PetAccessCacheEntry, error) {
	guardian, err := a.inner.GuardianOf(ctx, petID)
	if err != nil {
		return PetAccessCacheEntry{}, err
	}
	linked, err := a.inner.LinkedOrgIDs(ctx, petID)
	if err != nil {
		return PetAccessCacheEntry{}, err
	}
	custody, err := a.inner.CustodyOrgIDs(ctx, petID)
	if err != nil {
		return PetAccessCacheEntry{}, err
	}
	return PetAccessCacheEntry{
		GuardianID: guardian,
		Linked:     linked,
		Custody:    custody,
	}, nil
}

// CanOrgAccess verifica el acceso a través de la entrada en caché.
func (a *CachedPetAccessAdapter) CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (app.AccessLevel, error) {
	entry, err := a.loadEntry(ctx, petID)
	if err != nil {
		return a.inner.CanOrgAccess(ctx, petID, orgID)
	}

	for _, id := range entry.Linked {
		if id == orgID {
			return app.AccessLinked, nil
		}
	}
	for _, id := range entry.Custody {
		if id == orgID {
			return app.AccessCustody, nil
		}
	}
	return app.AccessNone, nil
}

// GuardianOf devuelve el tutor consultando la entrada en caché.
func (a *CachedPetAccessAdapter) GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error) {
	entry, err := a.loadEntry(ctx, petID)
	if err != nil {
		return a.inner.GuardianOf(ctx, petID)
	}
	return entry.GuardianID, nil
}

// LinkedOrgIDs devuelve las organizaciones vinculadas desde la caché.
func (a *CachedPetAccessAdapter) LinkedOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	entry, err := a.loadEntry(ctx, petID)
	if err != nil {
		return a.inner.LinkedOrgIDs(ctx, petID)
	}
	return entry.Linked, nil
}

// CustodyOrgIDs devuelve las organizaciones con custodia desde la caché.
func (a *CachedPetAccessAdapter) CustodyOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	entry, err := a.loadEntry(ctx, petID)
	if err != nil {
		return a.inner.CustodyOrgIDs(ctx, petID)
	}
	return entry.Custody, nil
}

// PetSummary delega directamente al adaptador interior.
func (a *CachedPetAccessAdapter) PetSummary(ctx context.Context, petID uuid.UUID) (app.PetSummary, error) {
	return a.inner.PetSummary(ctx, petID)
}

// Alerts delega directamente al adaptador interior.
func (a *CachedPetAccessAdapter) Alerts(ctx context.Context, petID uuid.UUID) ([]string, error) {
	return a.inner.Alerts(ctx, petID)
}

// InvalidatePetAccess invalida la clave de acceso de una mascota en L1 y Redis.
func (a *CachedPetAccessAdapter) InvalidatePetAccess(ctx context.Context, petID uuid.UUID) {
	if a.cache != nil {
		_ = a.cache.Invalidate(ctx, a.petAccessKey(petID))
	}
}
