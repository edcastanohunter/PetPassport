-- +goose Up
-- rebuild: clinical.medical_timeline
-- Event Store v1 (PP-F2-DAT-001, ADR-009), scheduler de comandos y worker
-- (PP-F2-DAT-006) y primera proyección asíncrona read_clinical.medical_timeline
-- (PP-F2-DAT-002 §medical_timeline; el resto de read_clinical llega en M2.1).

-- Snapshots (ARQ-004): una fila por stream; nunca fuente de verdad.
CREATE TABLE es.snapshots (
  stream_id       uuid PRIMARY KEY REFERENCES es.streams(stream_id),
  stream_version  bigint NOT NULL,
  schema_version  smallint NOT NULL,
  state           jsonb NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- Outbox por stream con dead-letter (ARQ-008). next_attempt_at (no listado en
-- DAT-001) persiste el backoff entre reintentos para que sobreviva a reinicios
-- del worker (decisión registrada en el PR de M2.0).
ALTER TABLE es.outbox
  ADD COLUMN stream_id uuid,
  ADD COLUMN dead_letter boolean NOT NULL DEFAULT false,
  ADD COLUMN next_attempt_at timestamptz;
UPDATE es.outbox o SET stream_id = e.stream_id FROM es.events e WHERE e.global_position = o.global_position; -- backfill
ALTER TABLE es.outbox ALTER COLUMN stream_id SET NOT NULL;
-- Compatibilidad una versión hacia atrás (F1 DAT-002): el api anterior inserta
-- sin stream_id durante el despliegue; el trigger lo completa desde es.events.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION es.outbox_fill_stream_id() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.stream_id IS NULL THEN
    SELECT e.stream_id INTO NEW.stream_id FROM es.events e WHERE e.global_position = NEW.global_position;
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER outbox_fill_stream_id BEFORE INSERT ON es.outbox
  FOR EACH ROW EXECUTE FUNCTION es.outbox_fill_stream_id();
CREATE INDEX outbox_pending_stream_idx ON es.outbox (stream_id, global_position) WHERE published_at IS NULL;

-- Estados de suscripción (ARQ-002/006/007).
ALTER TABLE platform.projection_checkpoints
  ADD COLUMN status text NOT NULL DEFAULT 'running' CHECK (status IN ('running','paused','failed','rebuilding')),
  ADD COLUMN last_error text,
  ADD COLUMN failed_at timestamptz,
  ADD COLUMN mode text NOT NULL DEFAULT 'async' CHECK (mode IN ('sync','async'));

-- Lectura eficiente por tipo para suscripciones filtradas.
CREATE INDEX events_pos_type_idx ON es.events (global_position) INCLUDE (event_type, stream_id);

-- Scheduler de comandos (ARQ-009, DAT-006).
CREATE SCHEMA IF NOT EXISTS scheduler;
COMMENT ON SCHEMA scheduler IS 'Comandos programados por process managers (worker)';
CREATE TABLE scheduler.command_jobs (
  id uuid PRIMARY KEY, dedup_key text NOT NULL UNIQUE,
  run_at timestamptz NOT NULL, command_type text NOT NULL, command jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','running','done','failed','cancelled')),
  attempts int NOT NULL DEFAULT 0, last_error text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX command_jobs_due_idx ON scheduler.command_jobs (run_at) WHERE status = 'pending';

CREATE TABLE platform.worker_instances (
  group_name text PRIMARY KEY, instance_id text NOT NULL, hostname text, started_at timestamptz NOT NULL, heartbeat_at timestamptz NOT NULL
);

-- Modo mantenimiento de la API (rebuild --sync de proyecciones primarias,
-- ARQ-006): mientras exista la fila, /api/v1 responde 503.
CREATE TABLE platform.maintenance (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  reason    text NOT NULL,
  since     timestamptz NOT NULL DEFAULT now()
);

-- Clinical: línea de tiempo (proyección asíncrona clinical.medical_timeline).
CREATE SCHEMA IF NOT EXISTS read_clinical;
COMMENT ON SCHEMA read_clinical IS 'Read models de Clinical Records';
CREATE TABLE read_clinical.medical_timeline (
  pet_id          uuid NOT NULL,
  position        bigint NOT NULL,          -- global_position del evento origen (orden estable)
  occurred_at     timestamptz NOT NULL,
  kind            text NOT NULL,
  ref_id          uuid NOT NULL,
  organization_id uuid,
  title           text NOT NULL,
  summary         text,
  visibility      text NOT NULL CHECK (visibility IN ('guardian','org','both')),
  PRIMARY KEY (pet_id, position)
);
CREATE INDEX timeline_pet_time_idx ON read_clinical.medical_timeline (pet_id, occurred_at DESC, position DESC);

-- Privilegios (mínimo privilegio, F1 SEC-010). es.events sigue append-only.
GRANT SELECT, INSERT, UPDATE, DELETE ON es.snapshots TO pp_app_writer;
GRANT UPDATE (published_at, attempts, last_error, dead_letter, next_attempt_at) ON es.outbox TO pp_app_writer;
GRANT USAGE ON SCHEMA scheduler, read_clinical TO pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA scheduler, read_clinical TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA scheduler, read_clinical TO pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA scheduler, read_clinical
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA scheduler, read_clinical
  GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA scheduler, read_clinical
  GRANT SELECT ON TABLES TO pp_app_reader;
REVOKE CREATE ON SCHEMA scheduler, read_clinical FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS read_clinical.medical_timeline;
DROP SCHEMA IF EXISTS read_clinical;
DROP TABLE IF EXISTS platform.maintenance;
DROP TABLE IF EXISTS platform.worker_instances;
DROP TABLE IF EXISTS scheduler.command_jobs;
DROP SCHEMA IF EXISTS scheduler;
DROP INDEX IF EXISTS es.events_pos_type_idx;
ALTER TABLE platform.projection_checkpoints
  DROP COLUMN IF EXISTS mode, DROP COLUMN IF EXISTS failed_at, DROP COLUMN IF EXISTS last_error, DROP COLUMN IF EXISTS status;
DROP INDEX IF EXISTS es.outbox_pending_stream_idx;
REVOKE UPDATE (published_at, attempts, last_error, dead_letter, next_attempt_at) ON es.outbox FROM pp_app_writer;
DROP TRIGGER IF EXISTS outbox_fill_stream_id ON es.outbox;
DROP FUNCTION IF EXISTS es.outbox_fill_stream_id();
ALTER TABLE es.outbox DROP COLUMN IF EXISTS next_attempt_at, DROP COLUMN IF EXISTS dead_letter, DROP COLUMN IF EXISTS stream_id;
DROP TABLE IF EXISTS es.snapshots;
