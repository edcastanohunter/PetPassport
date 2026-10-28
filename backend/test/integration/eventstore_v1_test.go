//go:build integration

// Event Store v1 (PP-F2-ARQ-003/005, DAT-001): orden total con advisory lock
// (gate QA-001), upcasters al leer y verificación de la cadena persistida.
package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

// TestEventStore_QA001_TotalOrder es el gate de orden total (ADR-009 §3,
// docs/phase-2/03 §4): 50 escritores × 100 appends en streams distintos y un
// lector concurrente por global_position. El lector solo avanza (from = última
// vista), así que una posición confirmada tarde quedaría saltada para
// siempre: debe ver las 5.000, en secuencia estrictamente creciente sin
// huecos, con recorded_at monótono no decreciente.
func TestEventStore_QA001_TotalOrder(t *testing.T) {
	const writers, perWriter = 50, 100
	url := testkit.Postgres(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, url, writers+5, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := eventstore.NewRegistry()
	reg.Register(func() eventstore.Event { return testEvent{} })
	metrics := prometheus.NewRegistry()
	st := eventstore.NewStore(reg, pool, metrics)
	unit := db.NewUnitOfWork(pool)

	done := make(chan struct{})
	var seen []eventstore.StoredEvent
	var readerErr error
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		var last int64
		for {
			finished := false
			select {
			case <-done:
				finished = true
			default:
			}
			batch, err := st.ReadAll(ctx, last, 1000)
			if err != nil {
				readerErr = err
				return
			}
			for _, e := range batch {
				seen = append(seen, e)
				last = e.GlobalPosition
			}
			if finished && len(batch) == 0 {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream := kernel.NewID()
			for i := 0; i < perWriter; i++ {
				if err := appendOne(t, st, unit, stream, int64(i), 1); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("writer: %v", err)
	}
	close(done)
	readerWG.Wait()
	if readerErr != nil {
		t.Fatal(readerErr)
	}

	if len(seen) != writers*perWriter {
		t.Fatalf("el lector vio %d eventos, want %d: posiciones saltadas (orden total roto)", len(seen), writers*perWriter)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].GlobalPosition != seen[i-1].GlobalPosition+1 {
			t.Fatalf("hueco o desorden: %d tras %d", seen[i].GlobalPosition, seen[i-1].GlobalPosition)
		}
		if seen[i].RecordedAt.Before(seen[i-1].RecordedAt) {
			t.Fatalf("recorded_at retrocede en %d: %s < %s", seen[i].GlobalPosition, seen[i].RecordedAt, seen[i-1].RecordedAt)
		}
	}
	// Métrica de espera del lock: una observación por Append.
	mfs, err := metrics.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var observed uint64
	for _, mf := range mfs {
		if mf.GetName() == "eventstore_append_lock_wait_seconds" {
			observed = mf.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	if observed != writers*perWriter {
		t.Fatalf("eventstore_append_lock_wait_seconds: %d observaciones, want %d", observed, writers*perWriter)
	}
}

// TestEventStore_OutboxCarriesStreamAndEventID (DAT-001): el outbox lleva
// stream_id y event_id reales (el v0 dejaba event_id a cero).
func TestEventStore_OutboxCarriesStreamAndEventID(t *testing.T) {
	st, _, pool, unit := newEventStore(t)
	ctx := context.Background()
	stream := kernel.NewID()
	if err := appendOne(t, st, unit, stream, 0, 2); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM es.outbox o JOIN es.events e USING (global_position)
		 WHERE o.stream_id = $1 AND o.event_id = e.event_id AND NOT o.dead_letter`, stream).Scan(&n); err != nil || n != 2 {
		t.Fatalf("outbox: %d %v", n, err)
	}
	// Compatibilidad con el api anterior: un INSERT sin stream_id se completa por trigger.
	if err := appendRaw(ctx, pool, stream, 3, "TestEvent", 1, `{"name":"legacy"}`, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO es.outbox (global_position, event_id, event_type)
		SELECT global_position, event_id, event_type FROM es.events WHERE stream_id = $1 AND stream_version = 3`, stream); err != nil {
		t.Fatalf("insert legacy outbox: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM es.outbox WHERE stream_id = $1`, stream).Scan(&n); err != nil || n != 3 {
		t.Fatalf("trigger stream_id: %d %v", n, err)
	}
}

// TestEventStore_UpcastOnRead (ARQ-005): un PetRegistered v1 persistido en F1
// se lee como v2 (alerts: []) en Load y en ReadAll, sin reescribir es.events;
// la verificación de arranque acepta la cadena.
func TestEventStore_UpcastOnRead(t *testing.T) {
	env := newProjectionEnv(t)
	ctx := context.Background()
	pet := kernel.NewID()
	v1 := `{"pet_id":"` + pet.String() + `","guardian_id":"` + kernel.NewID().String() + `","name":"Luna","species":"dog","sex":"female","neutered":"yes"}`
	if err := appendRaw(ctx, env.pool, pet, 1, "PetRegistered", 1, v1, true); err != nil {
		t.Fatal(err)
	}

	all, err := env.store.ReadAll(ctx, 0, 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("readall: %d %v", len(all), err)
	}
	if all[0].SchemaVersion != 2 || !jsonHas(all[0].Payload, "alerts") {
		t.Fatalf("readall no upcastea: v%d %s", all[0].SchemaVersion, all[0].Payload)
	}
	var loaded []eventstore.StoredEvent
	if err := env.unit.Do(ctx, func(c context.Context) error {
		tx, err := db.TxFrom(c)
		if err != nil {
			return err
		}
		loaded, err = env.store.Load(c, tx, pet, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	decoded, err := env.store.Registry().Decode(loaded[0])
	if err != nil {
		t.Fatal(err)
	}
	if ev := decoded.(domain.PetRegistered); ev.Alerts == nil || ev.Name != "Luna" {
		t.Fatalf("load: %+v", ev)
	}
	// es.events intacto.
	var stored int
	if err := env.pool.QueryRow(ctx, `SELECT schema_version FROM es.events WHERE stream_id = $1`, pet).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("es.events reescrito: v%d %v", stored, err)
	}
	versions, err := env.store.StoredVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.Registry().VerifyStored(versions); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Un tipo persistido que el binario no conoce impide arrancar.
	if err := appendRaw(ctx, env.pool, kernel.NewID(), 1, "FromTheFuture", 1, `{}`, true); err != nil {
		t.Fatal(err)
	}
	versions, _ = env.store.StoredVersions(ctx)
	if err := env.store.Registry().VerifyStored(versions); err == nil {
		t.Fatal("VerifyStored debe fallar con un tipo no registrado")
	}
}

// appendRaw inserta un evento con payload y versión arbitrarios (datos
// históricos de una versión anterior del código). withStream crea el stream.
func appendRaw(ctx context.Context, pool *db.Pool, stream uuid.UUID, version int64, typ string, schema int, payload string, withStream bool) error {
	return pool.Do(ctx, func(tx pgxTx) error {
		if withStream {
			if _, err := tx.Exec(ctx, `INSERT INTO es.streams (stream_id, stream_type, version) VALUES ($1, 'Test', $2)
				ON CONFLICT (stream_id) DO UPDATE SET version = EXCLUDED.version`, stream, version); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE es.streams SET version = $2 WHERE stream_id = $1`, stream, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO es.events (event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at)
			VALUES ($1, $2, 'Test', $3, $4, $5, $6, '{}', $7)`,
			kernel.NewID(), stream, version, typ, schema, payload, time.Now().UTC())
		return err
	})
}
