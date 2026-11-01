-- +goose Up
-- PP-F3-DAT-003 / PP-F3-ARQ-009: Leases de worker por runner y tablas base de búsqueda

CREATE TABLE IF NOT EXISTS platform.worker_leases (
  name text PRIMARY KEY,
  holder text NOT NULL,
  acquired_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL
);

CREATE SCHEMA IF NOT EXISTS search;

CREATE TABLE IF NOT EXISTS search.index_queue (
  index text NOT NULL,
  doc_id text NOT NULL,
  pet_id uuid,
  version bigint NOT NULL,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','queued','failed')),
  attempts int NOT NULL DEFAULT 0,
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (index, doc_id)
);

CREATE INDEX IF NOT EXISTS index_queue_pending_idx ON search.index_queue (index, updated_at) WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS search.reindex_jobs (
  id uuid PRIMARY KEY,
  alias text NOT NULL,
  from_index text,
  to_index text NOT NULL,
  status text NOT NULL,
  progress jsonb,
  started_by uuid,
  reason text,
  started_at timestamptz,
  finished_at timestamptz
);

-- +goose Down
DROP TABLE IF EXISTS search.reindex_jobs;
DROP TABLE IF EXISTS search.index_queue;
DROP SCHEMA IF EXISTS search;
DROP TABLE IF EXISTS platform.worker_leases;
