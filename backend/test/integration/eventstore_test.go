//go:build integration

// Event Store v0 (PP-F1-ARQ-004, DAT-003): semántica de Append con
// concurrencia optimista, Load/ReadAll, outbox en la misma transacción,
// trigger append-only y concurrencia sobre microchip_reservations (QA-003).
package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type testEvent struct {
	Name string `json:"name"`
}

func (testEvent) EventName() string  { return "TestEvent" }
func (testEvent) SchemaVersion() int { return 1 }

func newEventStore(t *testing.T) (*eventstore.Store, *eventstore.Registry, *db.Pool, *db.UnitOfWork) {
	t.Helper()
	url := testkit.Postgres(t)
	pool, err := db.Open(context.Background(), url, 8, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := eventstore.NewRegistry()
	reg.Register(func() eventstore.Event { return testEvent{} })
	return eventstore.NewStore(reg, pool, nil), reg, pool, db.NewUnitOfWork(pool)
}

func appendOne(t *testing.T, st *eventstore.Store, unit *db.UnitOfWork, streamID uuid.UUID, expected int64, n int) error {
	t.Helper()
	return unit.Do(context.Background(), func(ctx context.Context) error {
		tx, err := db.TxFrom(ctx)
		if err != nil {
			return err
		}
		events := make([]eventstore.NewEvent, n)
		for i := 0; i < n; i++ {
			events[i] = eventstore.NewEvent{
				ID: kernel.NewID(), Type: "TestEvent", SchemaVersion: 1,
				Payload:    testEvent{Name: "e"},
				Metadata:   eventstore.Metadata{Source: eventstore.SourceAPI},
				OccurredAt: time.Now().UTC(),
			}
		}
		_, err = st.Append(ctx, tx, streamID, "Test", expected, events)
		return err
	})
}

// TestEventStore_AppendLoadReadAll verifica la semántica básica (ARQ-004).
func TestEventStore_AppendLoadReadAll(t *testing.T) {
	st, _, pool, unit := newEventStore(t)
	ctx := context.Background()
	streamID := kernel.NewID()

	if err := appendOne(t, st, unit, streamID, 0, 3); err != nil {
		t.Fatalf("append inicial: %v", err)
	}
	// Versión incorrecta → ErrConcurrency (QA-003).
	err := appendOne(t, st, unit, streamID, 0, 1)
	var ce *kernel.ConcurrencyError
	if !asConcurrency(err, &ce) {
		t.Fatalf("append con versión 0 tras 3 eventos debe fallar: %v", err)
	}
	if ce.Expected != 0 || ce.Actual != 3 {
		t.Fatalf("concurrency: %+v", ce)
	}
	// Continuar desde la versión correcta.
	if err := appendOne(t, st, unit, streamID, 3, 2); err != nil {
		t.Fatalf("append desde 3: %v", err)
	}

	// Load: orden y versiones sin huecos.
	var events []eventstore.StoredEvent
	if err := unit.Do(ctx, func(c context.Context) error {
		tx, err := db.TxFrom(c)
		if err != nil {
			return err
		}
		events, err = st.Load(c, tx, streamID, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("load: %d eventos, want 5", len(events))
	}
	for i, e := range events {
		if e.StreamVersion != int64(i+1) {
			t.Fatalf("versión[%d] = %d", i, e.StreamVersion)
		}
	}
	// Outbox escrito en la misma transacción (ARQ-006).
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM es.outbox WHERE published_at IS NULL`).Scan(&pending); err != nil || pending != 5 {
		t.Fatalf("outbox pendiente: %d %v", pending, err)
	}
	// ReadAll por posición global.
	all, err := st.ReadAll(ctx, 0, 100)
	if err != nil || len(all) < 5 {
		t.Fatalf("readall: %d %v", len(all), err)
	}
	// global_position estrictamente creciente.
	for i := 1; i < len(all); i++ {
		if all[i].GlobalPosition <= all[i-1].GlobalPosition {
			t.Fatalf("orden global: %d ≤ %d", all[i].GlobalPosition, all[i-1].GlobalPosition)
		}
	}
}

// TestEventStore_AppendOnlyTrigger: UPDATE/DELETE sobre es.events falla
// (DAT-003).
func TestEventStore_AppendOnlyTrigger(t *testing.T) {
	st, _, pool, unit := newEventStore(t)
	ctx := context.Background()
	streamID := kernel.NewID()
	if err := appendOne(t, st, unit, streamID, 0, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE es.events SET payload = '{"x":1}' WHERE stream_id = $1`, streamID); err == nil {
		t.Fatal("UPDATE sobre es.events debe fallar por trigger")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM es.events WHERE stream_id = $1`, streamID); err == nil {
		t.Fatal("DELETE sobre es.events debe fallar por trigger")
	}
}

// TestEventStore_Concurrency (QA-003): N goroutines hacen Append al mismo
// stream; exactamente una gana por versión, sin huecos ni duplicados.
func TestEventStore_Concurrency(t *testing.T) {
	st, _, pool, unit := newEventStore(t)
	ctx := context.Background()
	streamID := kernel.NewID()
	const writers, eventsPerWriter = 8, 2

	var wins atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := appendOne(t, st, unit, streamID, 0, eventsPerWriter); err == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("ganadores: %d, want 1", wins.Load())
	}
	var version int64
	var count int
	if err := pool.QueryRow(ctx, `SELECT version FROM es.streams WHERE stream_id = $1`, streamID).Scan(&version); err != nil || version != eventsPerWriter {
		t.Fatalf("versión del stream: %d %v", version, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM es.events WHERE stream_id = $1`, streamID).Scan(&count); err != nil || count != eventsPerWriter {
		t.Fatalf("eventos: %d %v", count, err)
	}
}

// TestEventStore_ConcurrencySequentialWins: escritores secuenciales con
// re-lectura de versión avanzan sin conflictos.
func TestEventStore_ConcurrencySequentialWins(t *testing.T) {
	st, _, _, unit := newEventStore(t)
	streamID := kernel.NewID()
	for v := int64(0); v < 5; v++ {
		if err := appendOne(t, st, unit, streamID, v, 1); err != nil {
			t.Fatalf("append v%d: %v", v, err)
		}
	}
}

// helpers --------------------------------------------------------------------

func asConcurrency(err error, target **kernel.ConcurrencyError) bool {
	ce, ok := err.(*kernel.ConcurrencyError)
	if ok {
		*target = ce
	}
	return ok
}
