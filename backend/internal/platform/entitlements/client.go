package entitlements

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Reader define las consultas a base de datos requeridas para cargar entitlements y metadatos de la organización.
type Reader interface {
	GetEntitlements(ctx context.Context, orgID uuid.UUID) (*kernel.EntitlementSet, error)
	GetOrgTypeAndVerifiedAt(ctx context.Context, orgID uuid.UUID) (orgType string, verifiedAt *time.Time, err error)
}

// Client gestiona la consulta de entitlements con caché L1 (5 s), Redis (60 s) y fallback a base de datos (PP-F5-ARQ-004).
type Client struct {
	reader Reader
	cache  *cache.Cache
	l1     *cache.L1Cache
	clock  func() time.Time

	mu      sync.RWMutex
	memSets map[uuid.UUID]kernel.EntitlementSet // Fallback local en memoria si no hay DB ni Redis
}

// NewClient crea un cliente de entitlements.
func NewClient(reader Reader, distCache *cache.Cache) *Client {
	var l1 *cache.L1Cache
	if distCache != nil {
		l1 = distCache.L1()
	} else {
		l1 = cache.NewL1Cache(5 * time.Second)
	}
	return &Client{
		reader:  reader,
		cache:   distCache,
		l1:      l1,
		clock:   time.Now,
		memSets: make(map[uuid.UUID]kernel.EntitlementSet),
	}
}

// WithClock fija el reloj para pruebas.
func (c *Client) WithClock(clock func() time.Time) *Client {
	c.clock = clock
	return c
}

// SetLocalFallback permite fijar entitlements en memoria para pruebas o mocks.
func (c *Client) SetLocalFallback(orgID uuid.UUID, set kernel.EntitlementSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memSets[orgID] = set
}

// CacheKey genera la clave canónica de caché para los entitlements de una organización.
func CacheKey(orgID uuid.UUID) string {
	return fmt.Sprintf("pp:v1:entitlements:%s", orgID.String())
}

// For recupera el conjunto efectivo de entitlements de una organización (PP-F5-DOM-011).
func (c *Client) For(ctx context.Context, orgID uuid.UUID) (kernel.EntitlementSet, error) {
	// 1. Fallback en memoria para pruebas unitarias
	c.mu.RLock()
	setMem, hasMem := c.memSets[orgID]
	c.mu.RUnlock()
	if hasMem {
		return setMem, nil
	}

	key := CacheKey(orgID)

	// 2. Consultar caché distribuida L1 / Redis si está configurada
	if c.cache != nil {
		var cached kernel.EntitlementSet
		if err := c.cache.Get(ctx, key, &cached); err == nil {
			return cached, nil
		}
	}

	// 3. Consultar base de datos
	if c.reader != nil {
		set, err := c.reader.GetEntitlements(ctx, orgID)
		if err == nil && set != nil {
			if c.cache != nil {
				_ = c.cache.Set(ctx, key, *set, 60*time.Second)
			}
			return *set, nil
		}

		// Si no existe fila en billing.entitlements, calcular EntitlementSet por defecto (DOM-008)
		orgType, verifiedAt, err := c.reader.GetOrgTypeAndVerifiedAt(ctx, orgID)
		if err == nil {
			defaultSet := c.computeDefault(orgID, orgType, verifiedAt)
			if c.cache != nil {
				_ = c.cache.Set(ctx, key, defaultSet, 60*time.Second)
			}
			return defaultSet, nil
		}
	}

	// 4. Default absoluto seguro
	return c.computeDefault(orgID, "clinic", nil), nil
}

// Invalidate purga la clave de entitlements de la caché tras eventos de suscripción o uso.
func (c *Client) Invalidate(ctx context.Context, orgID uuid.UUID) {
	key := CacheKey(orgID)
	if c.cache != nil {
		_ = c.cache.Delete(ctx, key)
	}
	if c.l1 != nil {
		c.l1.Delete(key)
	}
}

// computeDefault determina los entitlements por defecto según el tipo y antigüedad (DOM-008).
func (c *Client) computeDefault(orgID uuid.UUID, orgType string, verifiedAt *time.Time) kernel.EntitlementSet {
	now := c.clock().UTC()

	if orgType == "pharmacy" {
		// Farmacia sin suscripción -> Pharmacy Basic (gratuito)
		return kernel.EntitlementSet{
			OrgID:       orgID,
			PlanCode:    "pharmacy_basic",
			PlanVersion: 1,
			Status:      kernel.EntitlementActive,
			Seats:       kernel.SeatAllocation{Limit: 0, Used: 0},
			Quotas: map[kernel.QuotaKey]kernel.Quota{
				kernel.QuotaQuotesPerMonth: {Limit: 50, Used: 0, SoftLimit: 55},
			},
			Features:   []string{"quote_portal", "orders"},
			ComputedAt: now,
		}
	}

	// Clínica: trialing si dentro de los 30 días desde verificación, si no restricted (DOM-008)
	if verifiedAt != nil && now.Sub(*verifiedAt).Hours()/24 <= 30 {
		trialEnd := verifiedAt.AddDate(0, 0, 30)
		return kernel.EntitlementSet{
			OrgID:       orgID,
			PlanCode:    "pro",
			PlanVersion: 1,
			Status:      kernel.EntitlementTrialing,
			Seats:       kernel.SeatAllocation{Limit: 2, Used: 0},
			Quotas: map[kernel.QuotaKey]kernel.Quota{
				kernel.QuotaQuotesPerMonth: {Limit: 500, Used: 0, SoftLimit: 550},
				kernel.QuotaSMSPerMonth:    {Limit: 100, Used: 0, SoftLimit: 110},
			},
			Features:   []string{"agenda", "encounters", "vaccination_cards", "prescriptions", "quotes", "cohorts", "directory"},
			PeriodEnd:  &trialEnd,
			ComputedAt: now,
		}
	}

	// Clínica sin prueba activa ni suscripción contratada -> restricted (no existe gratuito para clínicas)
	return kernel.EntitlementSet{
		OrgID:       orgID,
		PlanCode:    "starter",
		PlanVersion: 1,
		Status:      kernel.EntitlementRestricted,
		Seats:       kernel.SeatAllocation{Limit: 0, Used: 0},
		Quotas: map[kernel.QuotaKey]kernel.Quota{
			kernel.QuotaQuotesPerMonth: {Limit: 0, Used: 0, SoftLimit: 0},
		},
		Features:   []string{"agenda", "encounters", "vaccination_cards", "prescriptions"},
		ComputedAt: now,
	}
}
