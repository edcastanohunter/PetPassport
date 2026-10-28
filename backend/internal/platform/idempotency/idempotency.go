// Package idempotency implementa el almacén de Idempotency-Key (PP-F1-ARQ-008,
// tabla platform.idempotency_keys de DAT-007). Semántica:
//
//   - clave compuesta (actor_id, key); request_hash = SHA-256 del cuerpo canónico;
//   - misma clave + mismo hash → se devuelve la respuesta almacenada
//     (el handler HTTP añade `Idempotent-Replayed: true`);
//   - misma clave + distinto hash → ErrKeyReused (422 IDEMPOTENCY_KEY_REUSED);
//   - clave en vuelo → ErrRequestInProgress (409 REQUEST_IN_PROGRESS);
//   - expiradas (> 24 h) se roban o borran y el comando se ejecuta de nuevo.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
)

// Errores del protocolo; la capa http los mapea a problem+json.
var (
	ErrRequestInProgress = errors.New("request in progress")
	ErrKeyReused         = errors.New("idempotency key reused with different body")
)

// DefaultTTL es la vigencia de una clave (ARQ-008).
const DefaultTTL = 24 * time.Hour

// RowQuerier es lo que Store necesita (un *db.Pool o *pgxpool.Pool sirven).
type RowQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store es el almacén de claves de idempotencia.
type Store struct {
	pool RowQuerier
	ttl  time.Duration
	now  func() time.Time

	replays prometheus.Counter
}

// Instrument registra idempotency_replays_total (INF-007). Sin llamar = sin métricas.
func (s *Store) Instrument(reg prometheus.Registerer) *Store {
	s.replays = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "idempotency_replays_total",
		Help: "Respuestas devueltas desde una Idempotency-Key ya completada.",
	})
	reg.MustRegister(s.replays)
	return s
}

// NewStore construye el almacén. ttl 0 = DefaultTTL.
func NewStore(pool RowQuerier, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{pool: pool, ttl: ttl, now: time.Now}
}

// Outcome del Begin.
type Outcome struct {
	Started bool // el comando debe ejecutarse; luego Commit.
	Replay  bool // respuesta previa almacenada: devolver tal cual.
	Status  int
	Body    []byte
}

// Begin arranca la ejecución idempotente de una petición.
func (s *Store) Begin(ctx context.Context, actorID, key uuid.UUID, requestHash []byte) (Outcome, error) {
	for attempt := 0; attempt < 2; attempt++ {
		inserted, err := s.tryInsert(ctx, actorID, key, requestHash)
		if err != nil {
			return Outcome{}, err
		}
		if inserted {
			return Outcome{Started: true}, nil
		}
		// Clave existente: leer sin bloquear (NOWAIT) para no esperar al
		// request en vuelo.
		row, err := s.read(ctx, actorID, key)
		if err != nil {
			return Outcome{}, err
		}
		expired := !row.expiresAt.After(s.now())
		switch {
		case expired:
			// Robar la clave expirada y reintentar el insert una vez.
			if err := s.Abort(ctx, actorID, key); err != nil {
				return Outcome{}, err
			}
			continue
		case row.status == "completed":
			if string(row.requestHash) != string(requestHash) {
				return Outcome{}, ErrKeyReused
			}
			if s.replays != nil {
				s.replays.Inc()
			}
			return Outcome{Replay: true, Status: row.responseStatus, Body: row.responseBody}, nil
		default: // in_progress
			return Outcome{}, ErrRequestInProgress
		}
	}
	return Outcome{}, fmt.Errorf("idempotency: no se pudo adquirir la clave tras expirar")
}

func (s *Store) tryInsert(ctx context.Context, actorID, key uuid.UUID, requestHash []byte) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO platform.idempotency_keys (actor_id, key, request_hash, status, expires_at)
		VALUES ($1, $2, $3, 'in_progress', $4)
		ON CONFLICT (actor_id, key) DO NOTHING`,
		actorID, key, requestHash, s.now().Add(s.ttl))
	if err != nil {
		return false, fmt.Errorf("idempotency begin: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

type storedRow struct {
	status         string
	requestHash    []byte
	responseStatus int
	responseBody   []byte
	expiresAt      time.Time
}

func (s *Store) read(ctx context.Context, actorID, key uuid.UUID) (storedRow, error) {
	var row storedRow
	err := s.pool.QueryRow(ctx, `
		SELECT status, request_hash, COALESCE(response_status, 0), response_body, expires_at
		  FROM platform.idempotency_keys
		 WHERE actor_id = $1 AND key = $2
		   FOR UPDATE NOWAIT`, actorID, key).Scan(
		&row.status, &row.requestHash, &row.responseStatus, &row.responseBody, &row.expiresAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" { // lock_not_available
			return row, ErrRequestInProgress
		}
		return row, fmt.Errorf("idempotency read: %w", err)
	}
	return row, nil
}

// Commit guarda la respuesta y marca la clave como completada.
func (s *Store) Commit(ctx context.Context, actorID, key uuid.UUID, status int, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE platform.idempotency_keys
		   SET status = 'completed', response_status = $3, response_body = $4
		 WHERE actor_id = $1 AND key = $2 AND status = 'in_progress'`,
		actorID, key, status, body)
	if err != nil {
		return fmt.Errorf("idempotency commit: %w", err)
	}
	return nil
}

// Abort borra la clave en vuelo (el comando falló; el cliente puede reintentar).
func (s *Store) Abort(ctx context.Context, actorID, key uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM platform.idempotency_keys
		 WHERE actor_id = $1 AND key = $2 AND (status = 'in_progress' OR expires_at < now())`,
		actorID, key)
	if err != nil {
		return fmt.Errorf("idempotency abort: %w", err)
	}
	return nil
}

// DeleteExpired purga claves expiradas (job diario, DAT-007). Devuelve cuántas borró.
func (s *Store) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM platform.idempotency_keys WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("idempotency gc: %w", err)
	}
	return tag.RowsAffected(), nil
}
