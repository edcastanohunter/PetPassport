-- +goose Up
-- rebuild: scheduling.appointments, scheduling.agendas
-- Scheduling: citas y agendas de profesionales (PP-F2-DAT-004, ADR-011).
-- `appointments` y `agendas` son proyecciones síncronas (misma tx que el
-- Append); `slot_reservations` es la AUTORIDAD de no-solapamiento y se
-- escribe en la misma tx que el Append del comando que reserva (no se
-- reconstruye por replay: la restricción de exclusión es el contrato).

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE SCHEMA IF NOT EXISTS read_scheduling;
COMMENT ON SCHEMA read_scheduling IS 'Read models y reservas de Scheduling';

CREATE TABLE read_scheduling.appointments (
  id                       uuid PRIMARY KEY,
  version                  bigint NOT NULL,
  pet_id                   uuid NOT NULL,
  guardian_id              uuid NOT NULL,
  organization_id          uuid NOT NULL,
  professional_id          uuid,
  type                     text NOT NULL CHECK (type IN ('consultation','follow_up','vaccination','surgery','procedure','other')),
  origin                   text NOT NULL CHECK (origin IN ('guardian','clinic')),
  status                   text NOT NULL CHECK (status IN ('requested','proposed','confirmed','checked_in','completed','cancelled','no_show','rejected')),
  reason                   text NOT NULL,
  preferences              jsonb NOT NULL DEFAULT '[]',
  slot_start               timestamptz,
  slot_end                 timestamptz,
  tz                       text,
  proposal_expires_at      timestamptz,
  encounter_id             uuid,
  cancellation             jsonb,
  cancellation_policy_hours int,
  history                  jsonb NOT NULL DEFAULT '[]',
  requested_by             uuid,
  created_at               timestamptz NOT NULL,
  updated_at               timestamptz NOT NULL
);
CREATE INDEX appointments_org_day_idx ON read_scheduling.appointments (organization_id, slot_start)
  WHERE status IN ('confirmed','checked_in');
CREATE INDEX appointments_org_inbox_idx ON read_scheduling.appointments (organization_id, created_at DESC)
  WHERE status IN ('requested','proposed');
CREATE INDEX appointments_pet_idx ON read_scheduling.appointments (pet_id, slot_start DESC);
CREATE INDEX appointments_guardian_idx ON read_scheduling.appointments (guardian_id, created_at DESC);
CREATE INDEX appointments_noshow_idx ON read_scheduling.appointments (slot_end) WHERE status = 'confirmed';

-- Autoridad de no-solapamiento (ADR-011). Una fila por cita con reserva.
-- `provisional` (propuesta de alternativa, 48 h) y `confirmed` bloquean igual
-- mientras no hayan expirado; las provisionales vencidas se purgan antes de
-- reservar (la restricción no conoce expires_at).
CREATE TABLE read_scheduling.slot_reservations (
  appointment_id  uuid PRIMARY KEY,
  professional_id uuid NOT NULL,
  during          tstzrange NOT NULL CHECK (NOT isempty(during)),
  kind            text NOT NULL CHECK (kind IN ('confirmed','provisional')),
  expires_at      timestamptz,
  CONSTRAINT slot_reservations_no_overlap EXCLUDE USING gist (professional_id WITH =, during WITH &&)
);

CREATE TABLE read_scheduling.agendas (
  organization_id           uuid NOT NULL,
  professional_id           uuid NOT NULL,
  version                   bigint NOT NULL,
  tz                        text NOT NULL,
  weekly_template           jsonb NOT NULL,
  slot_minutes              jsonb NOT NULL,
  exceptions                jsonb NOT NULL DEFAULT '{}',
  lead_time_min_hours       int NOT NULL,
  lead_time_max_days        int NOT NULL,
  cancellation_policy_hours int NOT NULL,
  active                    boolean NOT NULL,
  updated_at                timestamptz NOT NULL,
  PRIMARY KEY (organization_id, professional_id)
);

GRANT USAGE ON SCHEMA read_scheduling TO pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA read_scheduling TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA read_scheduling TO pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_scheduling
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_scheduling
  GRANT SELECT ON TABLES TO pp_app_reader;
REVOKE CREATE ON SCHEMA read_scheduling FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS read_scheduling.agendas;
DROP TABLE IF EXISTS read_scheduling.slot_reservations;
DROP TABLE IF EXISTS read_scheduling.appointments;
DROP SCHEMA IF EXISTS read_scheduling;
