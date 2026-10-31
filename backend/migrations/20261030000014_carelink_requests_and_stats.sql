-- +goose Up
-- Solicitud de vínculo por la clínica, bandeja del tutor, alertas del perfil y
-- estadísticas de clínica (PP-F2-DAT-007, PRD-020, PRD-026).

-- Alertas del perfil (DOM-013): lista de {kind, text}.
ALTER TABLE read_petregistry.pets ADD COLUMN alerts jsonb NOT NULL DEFAULT '[]';

CREATE TABLE read_petregistry.care_link_requests (
  id                uuid PRIMARY KEY,
  pet_id            uuid NOT NULL REFERENCES read_petregistry.pets(id),
  organization_id   uuid NOT NULL,
  org_display_name  text NOT NULL,
  requested_by      uuid NOT NULL,
  message           text,
  status            text NOT NULL CHECK (status IN ('pending','approved','declined','expired')),
  expires_at        timestamptz NOT NULL,
  created_at        timestamptz NOT NULL,
  resolved_at       timestamptz
);
-- Máximo una solicitud pendiente por (mascota, organización) (PRD-020).
CREATE UNIQUE INDEX care_link_requests_pending_uq
  ON read_petregistry.care_link_requests (pet_id, organization_id) WHERE status = 'pending';
CREATE INDEX care_link_requests_pet_idx
  ON read_petregistry.care_link_requests (pet_id, status, created_at DESC);
CREATE INDEX care_link_requests_org_idx
  ON read_petregistry.care_link_requests (organization_id, status, created_at DESC);

-- Bandeja del tutor (FE-012): una fila por solicitud; las propuestas de cita y
-- los controles se componen en la API desde sus propios read models.
CREATE TABLE read_petregistry.guardian_inbox (
  id               uuid PRIMARY KEY,
  guardian_id      uuid NOT NULL,
  kind             text NOT NULL,
  ref_id           uuid NOT NULL,
  pet_id           uuid,
  organization_id  uuid,
  title            text NOT NULL,
  body             text,
  status           text NOT NULL,
  created_at       timestamptz NOT NULL,
  expires_at       timestamptz,
  resolved_at      timestamptz
);
CREATE INDEX guardian_inbox_idx ON read_petregistry.guardian_inbox (guardian_id, status, created_at DESC);
CREATE UNIQUE INDEX guardian_inbox_ref_uq ON read_petregistry.guardian_inbox (kind, ref_id);

-- Registro de búsquedas de mascota para solicitudes (límite 20/hora/usuario,
-- PRD-020). Operativo, no deriva de eventos; se purga al consultar.
CREATE TABLE read_petregistry.pet_lookup_attempts (
  user_id       uuid NOT NULL,
  attempted_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pet_lookup_attempts_idx ON read_petregistry.pet_lookup_attempts (user_id, attempted_at DESC);

-- Estadísticas mensuales por clínica (PRD-026), proyección asíncrona
-- `partnernetwork.org_stats`. `metrics` guarda contadores por clave.
CREATE TABLE read_partnernetwork.org_stats_monthly (
  organization_id  uuid NOT NULL,
  month            date NOT NULL,
  metrics          jsonb NOT NULL DEFAULT '{}',
  updated_at       timestamptz NOT NULL,
  PRIMARY KEY (organization_id, month)
);

-- Citas ya contadas por estado, para que reprocesar un evento no duplique.
CREATE TABLE read_partnernetwork.org_stats_events (
  event_id  uuid PRIMARY KEY
);

-- +goose Down
DROP TABLE IF EXISTS read_partnernetwork.org_stats_events;
DROP TABLE IF EXISTS read_partnernetwork.org_stats_monthly;
DROP TABLE IF EXISTS read_petregistry.pet_lookup_attempts;
DROP TABLE IF EXISTS read_petregistry.guardian_inbox;
DROP TABLE IF EXISTS read_petregistry.care_link_requests;
ALTER TABLE read_petregistry.pets DROP COLUMN IF EXISTS alerts;
