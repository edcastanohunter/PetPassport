//go:build integration

// Migración Event Store v1 sobre datos existentes (PP-F2-DAT-001): el
// backfill de es.outbox.stream_id deja todas las filas completas y la
// migración no bloquea los appends del api anterior más de lo medido.
//
// Por defecto usa un volumen pequeño (CI). Para medir el tiempo de la ventana
// de despliegue con un volumen realista:
//
//	PP_MIGRATION_EVENTS=500000 go test -tags integration -run TestMigration_EventStoreV1Backfill ./test/integration/
package integration

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

func TestMigration_EventStoreV1Backfill(t *testing.T) {
	n := 5000
	if v := os.Getenv("PP_MIGRATION_EVENTS"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n <= 0 {
			t.Fatalf("PP_MIGRATION_EVENTS inválido: %q", v)
		}
	}
	url := testkit.Postgres(t)
	ctx := context.Background()
	// Volver al esquema de F1 (antes de 0008) y poblarlo como lo dejaría el api de F1.
	testkit.MigrateDown(t, url)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	streams := max(n/10, 1) // ~10 eventos por stream
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO es.streams (stream_id, stream_type, version)
		    SELECT md5('s' || i)::uuid, 'Pet', 10 FROM generate_series(0, $1::int - 1) i`, []any{streams}},
		{`INSERT INTO es.events (event_id, stream_id, stream_type, stream_version, event_type, payload, metadata, occurred_at)
		    SELECT gen_random_uuid(), md5('s' || (i % $1::int))::uuid, 'Pet', i / $1::int + 1,
		           'PetProfileUpdated', '{"changes":{}}', '{"correlation_id":"x"}', now()
		      FROM generate_series(0, $2::int - 1) i`, []any{streams, n}},
		// Un tercio pendiente, como un outbox de F1 que nadie consumía.
		{`INSERT INTO es.outbox (global_position, event_id, event_type, published_at)
		    SELECT global_position, event_id, event_type,
		           CASE WHEN global_position % 3 = 0 THEN NULL ELSE now() END
		      FROM es.events`, nil},
		{`ANALYZE es.events`, nil},
		{`ANALYZE es.outbox`, nil},
	} {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("poblar esquema F1: %v", err)
		}
	}

	start := time.Now()
	testkit.Migrate(t, url)
	took := time.Since(start)
	t.Logf("migración 0008 con %d eventos/outbox: %s", n, took)

	var missing int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM es.outbox o JOIN es.events e USING (global_position)
		WHERE o.stream_id IS DISTINCT FROM e.stream_id`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("%d filas del outbox sin stream_id correcto tras el backfill", missing)
	}
}
