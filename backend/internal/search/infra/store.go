package infra

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresSearchStore gestiona la persistencia de colas y administración de búsqueda en PostgreSQL.
type PostgresSearchStore struct {
	pool *pgxpool.Pool
}

// NewPostgresSearchStore crea una nueva instancia de PostgresSearchStore.
func NewPostgresSearchStore(pool *pgxpool.Pool) *PostgresSearchStore {
	return &PostgresSearchStore{pool: pool}
}

// DeleteIndexed elimina un documento indexado de search.index_queue.
func (s *PostgresSearchStore) DeleteIndexed(ctx context.Context, indexName, docID string, version int64) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM search.index_queue
		WHERE index = $1 AND doc_id = $2 AND version <= $3
	`, indexName, docID, version)
	return err
}

// EnqueueDoc encola o actualiza un documento en search.index_queue.
func (s *PostgresSearchStore) EnqueueDoc(ctx context.Context, indexName, docID string, petID *uuid.UUID, version int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO search.index_queue (index, doc_id, pet_id, version, status, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		ON CONFLICT (index, doc_id) DO UPDATE SET
			version = GREATEST(search.index_queue.version, EXCLUDED.version),
			status = 'pending',
			updated_at = EXCLUDED.updated_at
	`, indexName, docID, petID, version, time.Now())
	return err
}

// CreateReindexJob inserta un nuevo trabajo de reindexación en search.reindex_jobs.
func (s *PostgresSearchStore) CreateReindexJob(ctx context.Context, id uuid.UUID, alias, fromIndex, toIndex string, startedBy *uuid.UUID, reason string, startedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO search.reindex_jobs (id, alias, from_index, to_index, status, started_by, reason, started_at)
		VALUES ($1, $2, $3, $4, 'running', $5, $6, $7)
	`, id, alias, fromIndex, toIndex, startedBy, reason, startedAt)
	return err
}

// CompleteReindexJob marca como completado un trabajo de reindexación.
func (s *PostgresSearchStore) CompleteReindexJob(ctx context.Context, id uuid.UUID, finishedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE search.reindex_jobs
		SET status = 'completed', finished_at = $2
		WHERE id = $1
	`, id, finishedAt)
	return err
}

// RetryFailedIndexQueue reintenta documentos con error en search.index_queue.
func (s *PostgresSearchStore) RetryFailedIndexQueue(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE search.index_queue
		SET status = 'pending', attempts = 0, last_error = NULL, updated_at = now()
		WHERE status = 'failed'
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
