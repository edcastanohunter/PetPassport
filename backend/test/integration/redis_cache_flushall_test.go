//go:build integration

// Prueba de integración PP-F3-QA-006 (ARQ-005, ARQ-006, DAT-004):
// "FLUSHALL a mitad de escenario de integración sin fallos"
// Si Redis se vacía o se cae, el sistema sigue siendo completamente correcto
// (fail-open a base de datos / proveedor subyacente, cache miss sin error al usuario).
package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	clinicalapp "github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
	ppredis "github.com/edcastanohunter/petpassport/backend/internal/platform/redis"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type mockPetAccessProvider struct {
	calls atomic.Int64
	orgID uuid.UUID
}

func (m *mockPetAccessProvider) CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (clinicalapp.AccessLevel, error) {
	return clinicalapp.AccessLinked, nil
}

func (m *mockPetAccessProvider) GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (m *mockPetAccessProvider) PetSummary(ctx context.Context, petID uuid.UUID) (clinicalapp.PetSummary, error) {
	return clinicalapp.PetSummary{PetID: petID, Name: "Firulais"}, nil
}

func (m *mockPetAccessProvider) LinkedOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	m.calls.Add(1)
	return []uuid.UUID{m.orgID}, nil
}

func (m *mockPetAccessProvider) CustodyOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockPetAccessProvider) Alerts(ctx context.Context, petID uuid.UUID) ([]string, error) {
	return nil, nil
}

type mockMembershipProvider struct {
	calls atomic.Int64
}

func (m *mockMembershipProvider) Memberships(ctx context.Context, userID uuid.UUID) ([]auth.Membership, error) {
	m.calls.Add(1)
	return []auth.Membership{
		{
			OrganizationID: uuid.New(),
			Role:           "veterinarian",
			DisplayName:    "Clínica Test",
			OrgStatus:      "active",
		},
	}, nil
}

func TestRedisCache_QA006_FlushallDoesNotBreakSystem(t *testing.T) {
	redisAddr := testkit.Redis(t)

	cfg := ppredis.Config{
		Addr:         redisAddr,
		DB:           0,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		PoolSize:     10,
	}

	client, err := ppredis.NewClient(cfg)
	if err != nil {
		t.Fatalf("conectar a redis: %v", err)
	}
	defer func() { _ = client.Close() }()

	distCache := cache.New(client)

	ctx := context.Background()
	petID := uuid.New()
	orgID := uuid.New()

	// 1. Probar PetAccess con caché distribuida y FLUSHALL
	petAccessMock := &mockPetAccessProvider{orgID: orgID}
	cachedAccess := clinicalinfra.NewCachedPetAccessAdapter(petAccessMock, distCache)

	// Primera llamada: miss -> llamada al proveedor
	lvl, err := cachedAccess.CanOrgAccess(ctx, petID, orgID)
	if err != nil {
		t.Fatalf("primera llamada: %v", err)
	}
	if lvl != clinicalapp.AccessLinked {
		t.Fatalf("esperado AccessLinked, obtenido %v", lvl)
	}
	if calls := petAccessMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada al proveedor, obtenidas %d", calls)
	}

	// Segunda llamada: hit en caché -> no incrementa llamada al proveedor
	lvl, err = cachedAccess.CanOrgAccess(ctx, petID, orgID)
	if err != nil || lvl != clinicalapp.AccessLinked {
		t.Fatalf("segunda llamada: %v (lvl=%v)", err, lvl)
	}
	if calls := petAccessMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada (cache hit), obtenidas %d", calls)
	}

	// 2. EJECUTAR FLUSHALL en Redis a mitad del flujo
	if err := client.RDB().FlushAll(ctx).Err(); err != nil {
		t.Fatalf("flushall: %v", err)
	}

	// Invalidamos L1 para simular que no está en la memoria del proceso local
	distCache.L1().Clear()

	// Tercera llamada inmediatamente tras FLUSHALL:
	// El sistema NO se rompe: lee de la fuente de verdad (mock/DB) y repuebla caché
	lvl, err = cachedAccess.CanOrgAccess(ctx, petID, orgID)
	if err != nil {
		t.Fatalf("llamada tras FLUSHALL no debe fallar: %v", err)
	}
	if lvl != clinicalapp.AccessLinked {
		t.Fatalf("esperado AccessLinked tras flushall, obtenido %v", lvl)
	}
	if calls := petAccessMock.calls.Load(); calls != 2 {
		t.Fatalf("esperadas 2 llamadas tras FLUSHALL (miss y re-fetch), obtenidas %d", calls)
	}

	// Cuarta llamada: vuelve a ser hit de caché repoblada
	lvl, err = cachedAccess.CanOrgAccess(ctx, petID, orgID)
	if err != nil || lvl != clinicalapp.AccessLinked {
		t.Fatalf("llamada posterior a repoblación: %v (lvl=%v)", err, lvl)
	}
	if calls := petAccessMock.calls.Load(); calls != 2 {
		t.Fatalf("esperadas 2 llamadas (cache hit repoblado), obtenidas %d", calls)
	}

	// 3. Probar MembershipCache con WithCache y FLUSHALL
	memberMock := &mockMembershipProvider{}
	memberCache := auth.NewMembershipCache(memberMock, 5*time.Minute, 0).WithCache(distCache)

	userID := uuid.New()

	// Primera llamada membresías: llamada al proveedor
	mems, err := memberCache.Memberships(ctx, userID)
	if err != nil {
		t.Fatalf("membresías primera llamada: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("esperada 1 membresía, obtenidas %d", len(mems))
	}
	if calls := memberMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada a membresías, obtenidas %d", calls)
	}

	// Segunda llamada: hit
	_, err = memberCache.Memberships(ctx, userID)
	if err != nil {
		t.Fatalf("membresías segunda llamada: %v", err)
	}
	if calls := memberMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada a membresías (hit), obtenidas %d", calls)
	}

	// FLUSHALL de nuevo
	if err := client.RDB().FlushAll(ctx).Err(); err != nil {
		t.Fatalf("segundo flushall: %v", err)
	}
	distCache.L1().Clear()

	// Tercera llamada membresías tras FLUSHALL: debe funcionar correctamente
	mems, err = memberCache.Memberships(ctx, userID)
	if err != nil {
		t.Fatalf("membresías tras FLUSHALL falló: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("esperada 1 membresía tras flushall, obtenidas %d", len(mems))
	}
	if calls := memberMock.calls.Load(); calls != 2 {
		t.Fatalf("esperadas 2 llamadas a membresías tras flushall, obtenidas %d", calls)
	}
}
