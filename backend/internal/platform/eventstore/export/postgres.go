package export

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Beginner es lo mínimo que la exportación necesita del pool.
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Snapshot es una fuente sobre una transacción REPEATABLE READ de solo
// lectura: todos los lotes ven el mismo estado aunque haya appends en curso
// (las posiciones de una IDENTITY pueden confirmarse fuera de orden).
type Snapshot struct {
	tx    pgx.Tx
	count int64
}

// OpenSnapshot abre la transacción de la exportación. Cerrar con Close.
func OpenSnapshot(ctx context.Context, db Beginner) (*Snapshot, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("export: snapshot: %w", err)
	}
	// En REPEATABLE READ el snapshot se fija con la primera sentencia, no con
	// BEGIN: el recuento lo fija ya, antes de devolver la fuente.
	s := &Snapshot{tx: tx}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM es.events`).Scan(&s.count); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("export: snapshot: %w", err)
	}
	return s, nil
}

// Close termina la transacción (solo lectura: rollback).
func (s *Snapshot) Close(ctx context.Context) { _ = s.tx.Rollback(ctx) }

// Count devuelve el número de eventos visibles en el snapshot.
func (s *Snapshot) Count() int64 { return s.count }

// Batch implementa Source.
func (s *Snapshot) Batch(ctx context.Context, after int64, limit int) ([]Line, error) {
	rows, err := s.tx.Query(ctx, `
		SELECT global_position, event_id, stream_id, stream_type, stream_version, event_type,
		       schema_version, payload, metadata, occurred_at, recorded_at
		  FROM es.events
		 WHERE global_position > $1
		 ORDER BY global_position
		 LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Line, 0, limit)
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.GlobalPosition, &l.EventID, &l.StreamID, &l.StreamType, &l.StreamVersion,
			&l.EventType, &l.SchemaVersion, &l.Payload, &l.Metadata, &l.OccurredAt, &l.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Execer es lo mínimo que Import necesita: una transacción del migrador.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrNotEmpty impide importar sobre un Event Store con datos.
var ErrNotEmpty = errors.New("export: es.events no está vacío")

// Import restaura una exportación verificada en un Event Store vacío (ensayo
// de restauración sin el proveedor, DAT-009): solo INSERT, conserva
// global_position, reconstruye es.streams y reajusta la secuencia. Se ejecuta
// en la transacción del llamante con un rol con INSERT en es.* (migrador).
func Import(ctx context.Context, tx Execer, r io.Reader) (Manifest, error) {
	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM es.events`).Scan(&n); err != nil {
		return Manifest{}, err
	}
	if n > 0 {
		return Manifest{}, ErrNotEmpty
	}
	return read(r, func(l *Line) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO es.streams (stream_id, stream_type, version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $4)
			ON CONFLICT (stream_id) DO UPDATE SET version = EXCLUDED.version, updated_at = EXCLUDED.updated_at`,
			l.StreamID, l.StreamType, l.StreamVersion, l.RecordedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO es.events (global_position, event_id, stream_id, stream_type, stream_version,
			                       event_type, schema_version, payload, metadata, occurred_at, recorded_at)
			OVERRIDING SYSTEM VALUE
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			l.GlobalPosition, l.EventID, l.StreamID, l.StreamType, l.StreamVersion, l.EventType,
			l.SchemaVersion, l.Payload, l.Metadata, l.OccurredAt, l.RecordedAt)
		return err
	}, func() error {
		_, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('es.events', 'global_position'),
			GREATEST((SELECT max(global_position) FROM es.events), 1))`)
		return err
	})
}
