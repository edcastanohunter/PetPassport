-- +goose Up
-- Notifications: preferencias, recordatorios programados, entregas y supresiones
-- (PP-F2-DAT-005, ADR-012). Módulo NO event-sourced: no hay rebuild; las
-- tablas son el estado. Las entregas llevan el destinatario por id; la
-- dirección se resuelve al enviar (identity.user_accounts) y solo se guarda
-- su hash. Los destinatarios sin cuenta (invitados) llevan la dirección en
-- payload._to, que se purga al enviar (retención 30 días de payload).

CREATE SCHEMA IF NOT EXISTS notifications;
COMMENT ON SCHEMA notifications IS 'Notificaciones: preferencias, programadas, entregas (ADR-012)';

CREATE TABLE notifications.preferences (
  user_id uuid PRIMARY KEY,
  email   jsonb NOT NULL DEFAULT '{"appointments":true,"preventive_care":true,"clinical":true,"account":true,"security":true}',
  digest  jsonb NOT NULL DEFAULT '{"clinic_daily_agenda":false}',
  unsubscribe_token_hash bytea,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notifications.scheduled (
  id           uuid PRIMARY KEY,
  dedup_key    text NOT NULL UNIQUE,          -- rule|subject|recipient|offset
  rule         text NOT NULL,
  category     text NOT NULL,
  template     text NOT NULL,
  recipient_id uuid NOT NULL,
  subject_type text NOT NULL,
  subject_id   uuid NOT NULL,
  due_at       timestamptz NOT NULL,
  payload      jsonb NOT NULL,
  status       text NOT NULL CHECK (status IN ('pending','dispatched','cancelled')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scheduled_due_idx ON notifications.scheduled (due_at) WHERE status = 'pending';
CREATE INDEX scheduled_subject_idx ON notifications.scheduled (subject_type, subject_id) WHERE status = 'pending';

CREATE TABLE notifications.deliveries (
  id            uuid PRIMARY KEY,
  dedup_key     text NOT NULL UNIQUE,
  channel       text NOT NULL CHECK (channel IN ('email')),
  category      text NOT NULL,
  template      text NOT NULL,
  template_version int NOT NULL,
  locale        text NOT NULL,
  recipient_id  uuid NOT NULL,
  recipient_address_hash bytea NOT NULL,
  payload       jsonb NOT NULL,
  status        text NOT NULL CHECK (status IN ('queued','sending','sent','failed','suppressed')),
  attempts      int NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  last_error    text,
  provider_message_id text,
  sent_at       timestamptz,
  dead_letter   boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now()
);
-- Cola: 'queued' pendiente o 'failed' que aún reintenta (no dead-letter).
CREATE INDEX deliveries_queue_idx ON notifications.deliveries (next_attempt_at)
  WHERE status IN ('queued','failed','sending') AND NOT dead_letter;
CREATE INDEX deliveries_recipient_idx ON notifications.deliveries (recipient_id, created_at DESC);
CREATE INDEX deliveries_throttle_idx ON notifications.deliveries (recipient_id, template, sent_at DESC)
  WHERE status = 'sent';
CREATE INDEX deliveries_dead_letter_idx ON notifications.deliveries (created_at) WHERE dead_letter;

CREATE TABLE notifications.suppressions (   -- bounces/quejas (NOT-007)
  address_hash bytea PRIMARY KEY,
  reason       text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

GRANT USAGE ON SCHEMA notifications TO pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA notifications TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA notifications TO pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA notifications
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA notifications
  GRANT SELECT ON TABLES TO pp_app_reader;
REVOKE CREATE ON SCHEMA notifications FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS notifications.suppressions;
DROP TABLE IF EXISTS notifications.deliveries;
DROP TABLE IF EXISTS notifications.scheduled;
DROP TABLE IF EXISTS notifications.preferences;
DROP SCHEMA IF EXISTS notifications;
