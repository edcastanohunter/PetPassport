//go:build integration

// Respaldo frío de es.events (PP-F1-DAT-009) contra Postgres real: la
// exportación de un snapshot contiene exactamente los eventos visibles, en
// orden, y su verificación offline coincide con el manifiesto.
package integration

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore/export"
)

func TestBackup_DAT009_ExportSnapshotMatchesEventStore(t *testing.T) {
	st, _, pool, unit := newEventStore(t)
	ctx := context.Background()
	count := func() int64 {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM es.events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	base := count()
	for i := 0; i < 3; i++ {
		if err := appendOne(t, st, unit, uuid.New(), 0, 4); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := export.OpenSnapshot(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	// Un append durante la exportación no entra en el snapshot ya abierto.
	if err := appendOne(t, st, unit, uuid.New(), 0, 2); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m, err := export.Write(ctx, snap, &buf, 5, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	inSnapshot := snap.Count()
	snap.Close(ctx)
	if m.Events != base+12 || inSnapshot != base+12 {
		t.Fatalf("exportados %d, en snapshot %d; want %d", m.Events, inSnapshot, base+12)
	}
	if total := count(); total != base+14 {
		t.Fatalf("es.events = %d, want %d", total, base+14)
	}

	got, err := export.Verify(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matches(m) {
		t.Fatalf("Verify = %+v, want %+v", got, m)
	}
}

// El respaldo frío es restaurable sin el proveedor: importar en un Event Store
// vacío reproduce eventos, posiciones y streams, y los appends siguientes
// continúan tras la última posición restaurada.
func TestBackup_DAT009_ImportRestoresIntoEmptyStore(t *testing.T) {
	ctx := context.Background()
	srcStore, _, srcPool, srcUnit := newEventStore(t)
	streamID := uuid.New()
	if err := appendOne(t, srcStore, srcUnit, streamID, 0, 3); err != nil {
		t.Fatal(err)
	}
	if err := appendOne(t, srcStore, srcUnit, uuid.New(), 0, 2); err != nil {
		t.Fatal(err)
	}
	snap, err := export.OpenSnapshot(ctx, srcPool)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m, err := export.Write(ctx, snap, &buf, 2, time.Now)
	snap.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dstStore, _, dstPool, dstUnit := newEventStore(t)
	tx, err := dstPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := export.Import(ctx, tx, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if !got.Matches(m) {
		t.Fatalf("import = %+v, want %+v", got, m)
	}

	// Mismos eventos y versión de stream; la concurrencia optimista sigue valiendo.
	var version int64
	if err := dstPool.QueryRow(ctx, `SELECT version FROM es.streams WHERE stream_id = $1`, streamID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("versión del stream = %d, want 3", version)
	}
	if err := appendOne(t, dstStore, dstUnit, streamID, 3, 1); err != nil {
		t.Fatalf("append tras restaurar: %v", err)
	}
	var maxPos int64
	if err := dstPool.QueryRow(ctx, `SELECT max(global_position) FROM es.events`).Scan(&maxPos); err != nil {
		t.Fatal(err)
	}
	if maxPos <= m.LastPosition {
		t.Fatalf("la secuencia no continúa: max %d tras %d", maxPos, m.LastPosition)
	}

	// Un segundo import sobre datos se rechaza.
	tx2, err := dstPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()
	if _, err := export.Import(ctx, tx2, bytes.NewReader(buf.Bytes())); !errors.Is(err, export.ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
}
