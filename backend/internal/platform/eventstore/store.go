package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Querier es lo mínimo que Store necesita para leer fuera del UoW: un pool o
// una transacción sirven.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store es el Event Store v1 (F1 ARQ-004, PP-F2-ARQ-003/005). Append y Load
// reciben la transacción del Unit of Work; ReadAll lee por posición global
// desde el pool. Las lecturas devuelven siempre la versión vigente del
// payload (cadena de upcasters del Registry).
type Store struct {
	registry *Registry
	pool     Querier

	eventsAppended  *prometheus.CounterVec
	conflictCounter prometheus.Counter
	lockWait        prometheus.Histogram
}

// NewStore construye el store. reg nil = sin registro de métricas (pruebas
// unitarias); los colectores existen igualmente.
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
		lockWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "eventstore_append_lock_wait_seconds",
			Help:    "Espera del advisory lock global de Append (orden total, ADR-009 §3). Revisión si p95 > 20 ms sostenido.",
			Buckets: []float64{.0001, .0005, .001, .0025, .005, .01, .02, .05, .1, .25, .5, 1, 2},
		}),
	}
	if reg != nil {
		reg.MustRegister(s.eventsAppended, s.conflictCounter, s.lockWait)
	}
	return s
}

// Registry devuelve el registry de tipos del store.
func (s *Store) Registry() *Registry { return s.registry }

// LockTotalOrder toma el lock de orden total (ADR-009 §3) hasta el fin de tx:
// mientras se mantiene, ningún Append puede insertar eventos. Lo usan Append y
// el rebuild de proyecciones síncronas para excluir escrituras concurrentes.
func LockTotalOrder(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('es.append'))`)
	return err
}

// Append inserta events al stream con concurrencia optimista (F1 03 §4) y
// orden total (PP-F2-ARQ-003, ADR-009 §3):
//
//  1. INSERT INTO es.streams ... ON CONFLICT DO NOTHING (si expectedVersion == 0).
//  2. UPDATE es.streams SET version = version + n WHERE stream_id AND version =
//     expected RETURNING version → 0 filas → *kernel.ConcurrencyError. El UPDATE
//     toma el row lock y serializa a los escritores del mismo stream.
//  3. pg_advisory_xact_lock(hashtext('es.append')): primera sentencia tras el
//     row lock (orden fijo stream → global, sin deadlocks). Se libera al
//     confirmar, después de que la transacción sea visible: las posiciones se
//     asignan y se hacen visibles en orden estricto y un lector nunca ve N+1
//     antes que N.
//  4. INSERT INTO es.events con stream_version = expected + i y recorded_at =
//     clock_timestamp() (tomado bajo el lock: monótono con global_position;
//     now() sería el inicio de la tx y podría retroceder).
//  5. INSERT INTO es.outbox por evento (misma transacción, con stream_id).
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

	// 3. Orden total: lock global hasta el commit.
	lockStart := time.Now()
	if err := LockTotalOrder(ctx, tx); err != nil {
		return nil, fmt.Errorf("eventstore: lock de orden total: %w", err)
	}
	s.lockWait.Observe(time.Since(lockStart).Seconds())

	// 4. Insert de eventos con stream_version = expected + i.
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
		var recordedAt time.Time
		err = tx.QueryRow(ctx, `
			INSERT INTO es.events (event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at, recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, clock_timestamp())
			RETURNING global_position, recorded_at`,
			ev.ID, streamID, streamType, expectedVersion+int64(i)+1, eventType, schemaVersion, raw, metaJSON, ev.OccurredAt).Scan(&pos, &recordedAt)
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
			EventID:        ev.ID,
			StreamID:       streamID,
			StreamType:     streamType,
			StreamVersion:  expectedVersion + int64(i) + 1,
			Type:           eventType,
			SchemaVersion:  schemaVersion,
			Payload:        raw,
			Metadata:       ev.Metadata,
			OccurredAt:     ev.OccurredAt,
			RecordedAt:     recordedAt,
		})
	}
	if finalVersion != expectedVersion+int64(len(events)) {
		return nil, fmt.Errorf("eventstore: versión final inconsistente: got %d want %d", finalVersion, expectedVersion+int64(len(events)))
	}

	// 5. Outbox en la misma transacción (F1 ARQ-006, PP-F2-DAT-001).
	rows := make([]outbox.Row, len(stored))
	for i, e := range stored {
		rows[i] = outbox.Row{GlobalPosition: e.GlobalPosition, EventID: e.EventID, EventType: e.Type, StreamID: e.StreamID}
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

const selectEvent = `
	SELECT global_position, event_id, stream_id, stream_type, stream_version, event_type, schema_version, payload, metadata, occurred_at, recorded_at
	  FROM es.events`

// Load devuelve los eventos del stream desde fromVersion (exclusive) en orden,
// upcasteados a la versión vigente.
func (s *Store) Load(ctx context.Context, tx pgx.Tx, streamID uuid.UUID, fromVersion int64) ([]StoredEvent, error) {
	rows, err := tx.Query(ctx, selectEvent+`
		 WHERE stream_id = $1 AND stream_version > $2
		 ORDER BY stream_version`, streamID, fromVersion)
	if err != nil {
		return nil, fmt.Errorf("eventstore: load: %w", err)
	}
	return s.collect(rows)
}

// ReadAll lee por posición global desde el pool (pruebas y herramientas).
func (s *Store) ReadAll(ctx context.Context, fromPosition int64, limit int) ([]StoredEvent, error) {
	return s.ReadRange(ctx, s.pool, fromPosition, 0, nil, limit)
}

// ReadRange lee eventos con global_position en (from, to] (to ≤ 0 = sin cota)
// filtrando por tipos en SQL (nil = todos), en orden de posición. q puede ser
// la transacción del lote de una suscripción (ARQ-002).
func (s *Store) ReadRange(ctx context.Context, q Querier, from, to int64, types []string, limit int) ([]StoredEvent, error) {
	if to <= 0 {
		to = 1<<63 - 1
	}
	var rows pgx.Rows
	var err error
	if types == nil {
		rows, err = q.Query(ctx, selectEvent+`
			 WHERE global_position > $1 AND global_position <= $2
			 ORDER BY global_position
			 LIMIT $3`, from, to, limit)
	} else {
		rows, err = q.Query(ctx, selectEvent+`
			 WHERE global_position > $1 AND global_position <= $2 AND event_type = ANY($3)
			 ORDER BY global_position
			 LIMIT $4`, from, to, types, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: readrange: %w", err)
	}
	return s.collect(rows)
}

// ReadPositions lee los eventos de las posiciones dadas, en orden de posición
// (consumidor de outbox).
func (s *Store) ReadPositions(ctx context.Context, q Querier, positions []int64) ([]StoredEvent, error) {
	rows, err := q.Query(ctx, selectEvent+`
		 WHERE global_position = ANY($1)
		 ORDER BY global_position`, positions)
	if err != nil {
		return nil, fmt.Errorf("eventstore: readpositions: %w", err)
	}
	return s.collect(rows)
}

// Head devuelve la mayor global_position visible (0 si no hay eventos). Con
// orden total (ARQ-003), toda posición ≤ Head ya es visible.
func (s *Store) Head(ctx context.Context, q Querier) (int64, error) {
	var head int64
	if err := q.QueryRow(ctx, `SELECT coalesce(max(global_position), 0) FROM es.events`).Scan(&head); err != nil {
		return 0, fmt.Errorf("eventstore: head: %w", err)
	}
	return head, nil
}

// StoredVersions lista los pares tipo@versión presentes en es.events
// (verificación de arranque del worker, ARQ-005). Recorre la tabla: coste
// asumible en el arranque con el volumen de F2–F3.
func (s *Store) StoredVersions(ctx context.Context) ([]StoredVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT event_type, schema_version FROM es.events ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("eventstore: versiones persistidas: %w", err)
	}
	defer rows.Close()
	var out []StoredVersion
	for rows.Next() {
		var sv StoredVersion
		if err := rows.Scan(&sv.Type, &sv.SchemaVersion); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) collect(rows pgx.Rows) ([]StoredEvent, error) {
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if e, err = s.registry.Upcast(e); err != nil {
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
		&e.SchemaVersion, &e.Payload, &metaJSON, &e.OccurredAt, &e.RecordedAt); err != nil {
		return e, fmt.Errorf("eventstore: escanear evento: %w", err)
	}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &e.Metadata); err != nil {
			return e, fmt.Errorf("eventstore: metadata inválida: %w", err)
		}
	}
	return e, nil
}
