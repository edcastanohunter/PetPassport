-- +goose Up
-- Vistas materializadas/tablas de plataforma, alertas de seguridad y escaneo de adjuntos
-- (PP-F3-DAT-008, PP-F3-SEC-005, PP-F3-SEC-006, PP-F3-ARQ-014)

CREATE TABLE platform.stats_daily (
  day          date PRIMARY KEY,
  metrics      jsonb NOT NULL,
  refreshed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.security_alerts (
  id              uuid PRIMARY KEY,
  rule            text NOT NULL,
  actor_id        uuid,
  organization_id uuid,
  details         jsonb NOT NULL,
  status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewed','dismissed','resolved')),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX security_alerts_status_idx ON platform.security_alerts (status, created_at DESC);
CREATE INDEX security_alerts_actor_idx  ON platform.security_alerts (actor_id, created_at DESC);

CREATE TABLE platform.cohort_campaigns (
  id              uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  cohort_key      text NOT NULL,
  recipient_count int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cohort_campaigns_cooldown_idx ON platform.cohort_campaigns (organization_id, cohort_key, created_at DESC);

ALTER TABLE read_clinical.attachment_uploads
  ADD COLUMN IF NOT EXISTS scan_status text NOT NULL DEFAULT 'pending' CHECK (scan_status IN ('pending', 'clean', 'infected'));

ALTER TABLE read_clinical.encounter_attachments
  ADD COLUMN IF NOT EXISTS scan_status text NOT NULL DEFAULT 'clean' CHECK (scan_status IN ('pending', 'clean', 'infected'));

GRANT SELECT, INSERT, UPDATE, DELETE ON platform.stats_daily TO pp_app_writer;
GRANT SELECT ON platform.stats_daily TO pp_app_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON platform.security_alerts TO pp_app_writer;
GRANT SELECT ON platform.security_alerts TO pp_app_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON platform.cohort_campaigns TO pp_app_writer;
GRANT SELECT ON platform.cohort_campaigns TO pp_app_reader;

-- +goose Down
ALTER TABLE read_clinical.encounter_attachments DROP COLUMN IF EXISTS scan_status;
ALTER TABLE read_clinical.attachment_uploads DROP COLUMN IF EXISTS scan_status;
DROP TABLE IF EXISTS platform.cohort_campaigns;
DROP TABLE IF EXISTS platform.security_alerts;
DROP TABLE IF EXISTS platform.stats_daily;
