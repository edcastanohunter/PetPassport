package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Snapshotter lo implementan los agregados con snapshots (PP-F2-ARQ-004,
// docs/phase-2/03 §5). La tabla y el contrato se crean en M2.0; el
// repositorio genérico (Load = snapshot + eventos posteriores; Save
// best-effort fuera de la tx del comando) se activa en M2.3 con
// ProfessionalAgenda. Los snapshots nunca son fuente de verdad.
type Snapshotter interface {
	// SnapshotState devuelve el estado serializable y su schema_version.
	SnapshotState() (state any, schemaVersion int)
	// RestoreSnapshot restaura el estado; error si schema no es compatible
	// (el repositorio lo ignora y rehidrata desde v0).
	RestoreSnapshot(state json.RawMessage, schemaVersion int) error
}

// Snapshot es la fila de es.snapshots de un stream.
type Snapshot struct {
	StreamID      uuid.UUID
	StreamVersion int64
	SchemaVersion int
	State         json.RawMessage
}

// ErrNoSnapshot indica que el stream no tiene snapshot.
var ErrNoSnapshot = errors.New("eventstore: sin snapshot")

// SnapshotStore lee y escribe es.snapshots (una fila por stream, se reemplaza).
type SnapshotStore struct{}

// Get devuelve el snapshot del stream o ErrNoSnapshot.
func (SnapshotStore) Get(ctx context.Context, q Querier, streamID uuid.UUID) (Snapshot, error) {
	s := Snapshot{StreamID: streamID}
	err := q.QueryRow(ctx, `SELECT stream_version, schema_version, state FROM es.snapshots WHERE stream_id = $1`, streamID).
		Scan(&s.StreamVersion, &s.SchemaVersion, &s.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNoSnapshot
	}
	if err != nil {
		return s, fmt.Errorf("eventstore: leer snapshot: %w", err)
	}
	return s, nil
}

// Save reemplaza el snapshot del stream si el nuevo es más reciente.
func (SnapshotStore) Save(ctx context.Context, tx pgx.Tx, s Snapshot) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO es.snapshots (stream_id, stream_version, schema_version, state)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (stream_id) DO UPDATE
		   SET stream_version = EXCLUDED.stream_version, schema_version = EXCLUDED.schema_version,
		       state = EXCLUDED.state, created_at = now()
		 WHERE es.snapshots.stream_version < EXCLUDED.stream_version`,
		s.StreamID, s.StreamVersion, s.SchemaVersion, s.State)
	if err != nil {
		return fmt.Errorf("eventstore: guardar snapshot: %w", err)
	}
	return nil
}

// DropAll borra todos los snapshots (`cmd/worker snapshots --drop`): sin
// pérdida, se regeneran desde los eventos. Devuelve cuántos borró.
func (SnapshotStore) DropAll(ctx context.Context, tx pgx.Tx) (int64, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM es.snapshots`)
	if err != nil {
		return 0, fmt.Errorf("eventstore: borrar snapshots: %w", err)
	}
	return tag.RowsAffected(), nil
}
