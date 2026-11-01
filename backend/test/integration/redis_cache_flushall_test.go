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
}

func (m *mockPetAccessProvider) CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (clinicalapp.AccessLevel, error) {
	m.calls.Add(1)
	return clinicalapp.AccessLinked, nil
}

func (m *mockPetAccessProvider) GetPetAccess(ctx context.Context, petID uuid.UUID) (guardianID uuid.UUID, linkedOrgs []uuid.UUID, custodyOrgs []uuid.UUID, err error) {
	m.calls.Add(1)
	return uuid.New(), []uuid.UUID{uuid.New()}, nil, nil
}

func (m *mockPetAccessProvider) RecordCustody(ctx context.Context, petID, orgID uuid.UUID, encounterID uuid.UUID) error {
	m.calls.Add(1)
	return nil
}

type mockMembershipProvider struct {
	calls atomic.Int64
}

func (m *mockMembershipProvider) UserMemberships(ctx context.Context, userID uuid.UUID) ([]auth.Membership, error) {
	m.calls.Add(1)
	return []auth.Membership{
		{
			OrgID:   uuid.New(),
			Role:    auth.RoleVet,
			OrgName: "Clínica Test",
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
	defer client.Close()

	distCache := cache.New(client)

	// 1. Probar PetAccess con caché distribuida y FLUSHALL
	petAccessMock := &mockPetAccessProvider{}
	cachedAccess := clinicalinfra.NewCachedPetAccessAdapter(petAccessMock, distCache)

	ctx := context.Background()
	petID := uuid.New()
	orgID := uuid.New()

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
	if err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if calls := petAccessMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada (cache hit), obtenidas %d", calls)
	}

	// 2. EJECUTAR FLUSHALL en Redis a mitad del flujo
	if err := client.Raw().FlushAll(ctx).Err(); err != nil {
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
	if err != nil {
		t.Fatalf("llamada posterior a repoblación: %v", err)
	}
	if calls := petAccessMock.calls.Load(); calls != 2 {
		t.Fatalf("esperadas 2 llamadas (cache hit repoblado), obtenidas %d", calls)
	}

	// 3. Probar MembershipCache con WithCache y FLUSHALL
	memberMock := &mockMembershipProvider{}
	memberCache := auth.NewMembershipCache(memberMock.UserMemberships, 5*time.Minute, auth.WithCache(distCache))

	userID := uuid.New()

	// Primera llamada membresías: llamada al proveedor
	mems, err := memberCache.Get(ctx, userID)
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
	_, err = memberCache.Get(ctx, userID)
	if err != nil {
		t.Fatalf("membresías segunda llamada: %v", err)
	}
	if calls := memberMock.calls.Load(); calls != 1 {
		t.Fatalf("esperada 1 llamada a membresías (hit), obtenidas %d", calls)
	}

	// FLUSHALL de nuevo
	if err := client.Raw().FlushAll(ctx).Err(); err != nil {
		t.Fatalf("segundo flushall: %v", err)
	}
	distCache.L1().Clear()

	// Tercera llamada membresías tras FLUSHALL: debe funcionar correctamente
	mems, err = memberCache.Get(ctx, userID)
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
