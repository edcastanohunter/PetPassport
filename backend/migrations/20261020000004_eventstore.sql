-- +goose Up
-- Event Store v0 (PP-F1-DAT-003, ADR-002, docs/phase-1/06 §3). Append-only:
-- el trigger impide UPDATE/DELETE sobre es.events incluso al owner.

CREATE SCHEMA IF NOT EXISTS es;

CREATE TABLE es.streams (
  stream_id    uuid PRIMARY KEY,
  stream_type  text NOT NULL,
  version      bigint NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX streams_type_idx ON es.streams (stream_type);

CREATE TABLE es.events (
  global_position bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id        uuid NOT NULL UNIQUE,
  stream_id       uuid NOT NULL REFERENCES es.streams(stream_id),
  stream_type     text NOT NULL,
  stream_version  bigint NOT NULL,
  event_type      text NOT NULL,
  schema_version  smallint NOT NULL DEFAULT 1,
  payload         jsonb NOT NULL,
  metadata        jsonb NOT NULL,
  occurred_at     timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (stream_id, stream_version)
);
CREATE INDEX events_stream_type_pos_idx ON es.events (stream_type, global_position);
CREATE INDEX events_type_pos_idx        ON es.events (event_type, global_position);
CREATE INDEX events_metadata_corr_idx   ON es.events ((metadata->>'correlation_id'));

-- Append-only: ni UPDATE ni DELETE, ni siquiera por el owner por accidente.
CREATE OR REPLACE FUNCTION es.forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'es.events is append-only'; END $$;
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON es.events
  FOR EACH ROW EXECUTE FUNCTION es.forbid_mutation();

CREATE TABLE es.outbox (
  global_position bigint PRIMARY KEY REFERENCES es.events(global_position),
  event_id        uuid NOT NULL,
  event_type      text NOT NULL,
  enqueued_at     timestamptz NOT NULL DEFAULT now(),
  published_at    timestamptz,
  attempts        int NOT NULL DEFAULT 0,
  last_error      text
);
CREATE INDEX outbox_pending_idx ON es.outbox (global_position) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS es.outbox;
DROP TRIGGER IF EXISTS events_append_only ON es.events;
DROP FUNCTION IF EXISTS es.forbid_mutation();
DROP TABLE IF EXISTS es.events;
DROP TABLE IF EXISTS es.streams;
DROP SCHEMA IF EXISTS es;
