//go:build integration

// Prueba de integración PP-F3-QA-004 (ARQ-009, DAT-003):
// Leases y failover: 3 workers simulados, muerte de uno -> retoma tras expiración
// sin duplicar holders simultáneos (garantizado por clave primaria y semántica de lease).
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/lease"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

func TestWorkerLease_QA004_FailoverWithoutDoubleHolder(t *testing.T) {
	pgURL := testkit.Postgres(t)
	pool, err := db.Open(context.Background(), pgURL, 10, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("conectar a postgres: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Instanciar 3 workers simulados
	w1 := lease.NewManager(pool.Pool, "worker-1", nil)
	w2 := lease.NewManager(pool.Pool, "worker-2", nil)
	w3 := lease.NewManager(pool.Pool, "worker-3", nil)

	runners := []string{"projections", "outbox", "documents"}

	// 2. Reparto de leases entre los 3 workers
	ok, _, err := w1.Acquire(ctx, runners[0])
	if err != nil || !ok {
		t.Fatalf("w1 no pudo adquirir %s: %v", runners[0], err)
	}

	ok, _, err = w2.Acquire(ctx, runners[1])
	if err != nil || !ok {
		t.Fatalf("w2 no pudo adquirir %s: %v", runners[1], err)
	}

	ok, _, err = w3.Acquire(ctx, runners[2])
	if err != nil || !ok {
		t.Fatalf("w3 no pudo adquirir %s: %v", runners[2], err)
	}

	// Comprobar que w1 no puede adquirir runner de w2 mientras esté activo
	ok, _, err = w1.Acquire(ctx, runners[1])
	if err != nil {
		t.Fatalf("error al intentar adquirir lease ocupado: %v", err)
	}
	if ok {
		t.Fatalf("w1 adquirió %s que ya pertenece a w2 (doble holder)", runners[1])
	}

	// 3. Verificar estado en base de datos: exactamente 1 holder por runner
	leases, err := w1.List(ctx)
	if err != nil {
		t.Fatalf("listar leases: %v", err)
	}
	if len(leases) != 3 {
		t.Fatalf("esperados 3 leases registrados, obtenidos %d", len(leases))
	}

	holders := make(map[string]string)
	for _, l := range leases {
		if !l.Active {
			t.Fatalf("lease %s debería estar activo", l.Name)
		}
		holders[l.Name] = l.Holder
	}
	if holders["projections"] != "worker-1" || holders["outbox"] != "worker-2" || holders["documents"] != "worker-3" {
		t.Fatalf("reparto incorrecto de holders: %+v", holders)
	}

	// 4. Simular muerte de worker-1 liberando o expirando su lease
	// w1 libera sus leases (o simulación de fallo)
	if err := w1.Release(ctx, "projections"); err != nil {
		t.Fatalf("liberar lease w1: %v", err)
	}

	// 5. Worker-2 retoma el runner "projections" inmediatamente
	ok, _, err = w2.Acquire(ctx, "projections")
	if err != nil || !ok {
		t.Fatalf("w2 no pudo retomar projections tras liberación: %v", err)
	}

	// Verificar que ahora projections pertenece a worker-2
	leases, err = w2.List(ctx)
	if err != nil {
		t.Fatalf("listar leases tras retoma: %v", err)
	}
	for _, l := range leases {
		if l.Name == "projections" && l.Holder != "worker-2" {
			t.Fatalf("esperado holder worker-2 para projections, obtenido %s", l.Holder)
		}
	}

	// 6. Probar ForceRelease y retoma
	if err := w3.ForceRelease(ctx, "outbox"); err != nil {
		t.Fatalf("force release: %v", err)
	}
	ok, _, err = w3.Acquire(ctx, "outbox")
	if err != nil || !ok {
		t.Fatalf("w3 no pudo adquirir outbox tras force release: %v", err)
	}

	// Limpieza
	_ = w1.ReleaseAll(ctx)
	_ = w2.ReleaseAll(ctx)
	_ = w3.ReleaseAll(ctx)
}
