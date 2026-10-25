package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Querier es lo mínimo que Store necesita para ReadAll (un pool sirve).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store es el Event Store v0 (ARQ-004). Append y Load reciben la transacción
// del Unit of Work; ReadAll lee por posición global desde el pool.
type Store struct {
	registry *Registry
	pool     Querier

	eventsAppended  *prometheus.CounterVec
	conflictCounter prometheus.Counter
}

// NewStore construye el store. reg nil = sin métricas (pruebas unitarias).
func NewStore(registry *Registry, pool Querier, reg prometheus.Registerer) *Store {
	s := &Store{
		registry: registry,
		pool:     pool,
		eventsAppended: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "events_appended_total",
			Help: "Eventos persistidos en el Event Store, por tipo de stream y de evento.",
		}, []string{"stream_type", "event_type"}),
		conflictCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "eventstore_concurrency_conflicts_total",
			Help: "Conflictos de concurrencia optimista (versión de stream no coincide).",
		}),
	}
	if reg != nil {
		reg.MustRegister(s.eventsAppended, s.conflictCounter)
	}
	return s
}

// Append inserta events al stream con concurrencia optimista (03 §4):
//
//  1. INSERT INTO es.streams ... ON CONFLICT DO NOTHING (si expectedVersion == 0).
//  2. UPDATE es.streams SET version = version + n WHERE stream_id AND version =
//     expected RETURNING version → 0 filas → *kernel.ConcurrencyError. El UPDATE
//     toma el row lock y serializa a los escritores del mismo stream.
//  3. INSERT INTO es.events con stream_version = expected + i; la restricción
//     UNIQUE (stream_id, stream_version) es la red de seguridad.
//  4. INSERT INTO es.outbox por evento (misma transacción; consumo en F2).
//
// Devuelve los eventos tal como quedaron persistidos.
func (s *Store) Append(
	ctx context.Context,
	tx pgx.Tx,
	streamID uuid.UUID,
	streamType string,
	expectedVersion int64,
	events []NewEvent,
) ([]StoredEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// 1. Stream nuevo.
	if expectedVersion == ExpectedNewStream {
		if _, err := tx.Exec(ctx, `
			INSERT INTO es.streams (stream_id, stream_type, version) VALUES ($1, $2, 0)
			ON CONFLICT (stream_id) DO NOTHING`, streamID, streamType); err != nil {
			return nil, fmt.Errorf("eventstore: insertar stream: %w", err)
		}
	}

	// 2. Reserva de versiones bajo row lock. RETURNING da la versión final.
	var finalVersion int64
	err := tx.QueryRow(ctx, `
		UPDATE es.streams SET version = version + $3, updated_at = now()
		 WHERE stream_id = $1 AND version = $2
		 RETURNING version`,
		streamID, expectedVersion, int64(len(events))).Scan(&finalVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.conflictCounter.Inc()
			return nil, s.concurrencyError(ctx, tx, streamID, expectedVersion)
		}
		return nil, fmt.Errorf("eventstore: reservar versión: %w", err)
	}

	// 3. Insert de eventos con stream_version = expected + i.
	stored := make([]StoredEvent, 0, len(events))
	for i, ev := range events {
		payload, err := toEvent(ev)
		if err != nil {
			return nil, err
		}
		eventType, schemaVersion, raw, err := s.registry.Encode(payload)
		if err != nil {
			return nil, err
		}
		if ev.Type != "" && ev.Type != eventType {
			return nil, fmt.Errorf("eventstore: tipo declarado %q no coincide con el payload %q", ev.Type, eventType)
		}
		metaJSON, err := json.Marshal(ev.Metadata)
		if err != nil {
			return nil, fmt.Errorf("eventstore: serializar metadata: %w", err)
		}
		var pos int64
		err = tx.QueryRow(ctx, `
			INSERT INTO es.events (event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING global_position`,
			ev.ID, streamID, streamType, expectedVersion+int64(i)+1, eventType, schemaVersion, raw, metaJSON, ev.OccurredAt).Scan(&pos)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				s.conflictCounter.Inc()
				return nil, s.concurrencyError(ctx, tx, streamID, expectedVersion)
			}
			return nil, fmt.Errorf("eventstore: insertar evento: %w", err)
		}
		stored = append(stored, StoredEvent{
			GlobalPosition: pos,
			StreamID:       streamID,
			StreamType:     streamType,
			StreamVersion:  expectedVersion + int64(i) + 1,
			Type:           eventType,
			SchemaVersion:  schemaVersion,
			Payload:        raw,
			Metadata:       ev.Metadata,
			OccurredAt:     ev.OccurredAt,
		})
	}
	if finalVersion != expectedVersion+int64(len(events)) {
		return nil, fmt.Errorf("eventstore: versión final inconsistente: got %d want %d", finalVersion, expectedVersion+int64(len(events)))
	}

	// 4. Outbox en la misma transacción (ARQ-006).
	rows := make([]outbox.Row, len(stored))
	for i, e := range stored {
		rows[i] = outbox.Row{GlobalPosition: e.GlobalPosition, EventID: e.EventID, EventType: e.Type}
	}
	if err := outbox.Enqueue(ctx, tx, rows); err != nil {
		return nil, err
	}

	for _, e := range stored {
		s.eventsAppended.WithLabelValues(streamType, e.Type).Inc()
	}
	return stored, nil
}

func toEvent(ev NewEvent) (Event, error) {
	e, ok := ev.Payload.(Event)
	if !ok {
		return nil, fmt.Errorf("eventstore: payload %T no implementa eventstore.Event", ev.Payload)
	}
	return e, nil
}

func (s *Store) concurrencyError(ctx context.Context, tx pgx.Tx, streamID uuid.UUID, expected int64) error {
	var actual int64
	if err := tx.QueryRow(ctx, `SELECT version FROM es.streams WHERE stream_id = $1`, streamID).Scan(&actual); err != nil {
		actual = -1
	}
	return &kernel.ConcurrencyError{Expected: expected, Actual: actual}
}

// Load devuelve los eventos del stream desde fromVersion (exclusive) en orden.
func (s *Store) Load(ctx context.Context, tx pgx.Tx, streamID uuid.UUID, fromVersion int64) ([]StoredEvent, error) {
	rows, err := tx.Query(ctx, `
		SELECT global_position, event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at
		  FROM es.events
		 WHERE stream_id = $1 AND stream_version > $2
		 ORDER BY stream_version`, streamID, fromVersion)
	if err != nil {
		return nil, fmt.Errorf("eventstore: load: %w", err)
	}
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReadAll lee por posición global (rebuild en F2 y pruebas QA-003).
func (s *Store) ReadAll(ctx context.Context, fromPosition int64, limit int) ([]StoredEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT global_position, event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at
		  FROM es.events
		 WHERE global_position > $1
		 ORDER BY global_position
		 LIMIT $2`, fromPosition, limit)
	if err != nil {
		return nil, fmt.Errorf("eventstore: readall: %w", err)
	}
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEvent(row pgx.Row) (StoredEvent, error) {
	var e StoredEvent
	var metaJSON []byte
	if err := row.Scan(&e.GlobalPosition, &e.EventID, &e.StreamID, &e.StreamType, &e.StreamVersion, &e.Type,
		&e.SchemaVersion, &e.Payload, &metaJSON, &e.OccurredAt); err != nil {
		return e, fmt.Errorf("eventstore: escanear evento: %w", err)
	}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &e.Metadata); err != nil {
			return e, fmt.Errorf("eventstore: metadata inválida: %w", err)
		}
	}
	return e, nil
}
