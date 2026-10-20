//go:build integration

// Prueba de integración de M1.0: PostgreSQL real, migraciones up/down/up,
// esquemas y roles presentes, pool de la app con ping (PP-F1-DAT-002, DAT-001).
package integration

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

func TestMigrations_UpDownUp(t *testing.T) {
	url := testkit.Postgres(t) // up
	testkit.MigrateDown(t, url)
	testkit.Migrate(t, url) // up de nuevo

	ctx := context.Background()
	pool, err := db.Open(ctx, url, 2, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.schemata
		WHERE schema_name IN ('es','identity','read_petregistry','read_partnernetwork','platform')`).Scan(&n)
	if err != nil || n != 5 {
		t.Fatalf("expected 5 schemas, got %d (err=%v)", n, err)
	}
	err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_roles WHERE rolname IN ('pp_app_writer','pp_app_reader')`).Scan(&n)
	if err != nil || n != 2 {
		t.Fatalf("expected 2 roles, got %d (err=%v)", n, err)
	}
	err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname IN ('citext','pg_trgm')`).Scan(&n)
	if err != nil || n != 2 {
		t.Fatalf("expected 2 extensions, got %d (err=%v)", n, err)
	}
}
