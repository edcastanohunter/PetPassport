-- +goose Up
-- Identity & Access (PP-F1-DAT-004) y platform (PP-F1-DAT-007). Ver docs/phase-1/06 §4 y §7.

CREATE TABLE identity.consent_versions (
  kind            text NOT NULL CHECK (kind IN ('terms','privacy')),
  version         text NOT NULL,
  url             text NOT NULL,
  effective_from  date NOT NULL,
  PRIMARY KEY (kind, version)
);

CREATE TABLE identity.user_accounts (
  id                       uuid PRIMARY KEY,                 -- Keycloak sub (ADR-007)
  email                    citext NOT NULL UNIQUE,
  email_verified           boolean NOT NULL DEFAULT false,
  display_name             text NOT NULL DEFAULT '',
  locale                   text NOT NULL DEFAULT 'es' CHECK (locale IN ('es','en')),
  country                  char(2),
  phone                    text,                             -- E.164
  address                  jsonb,
  emergency_contact        jsonb,
  status                   text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
  status_reason            text,
  global_roles             text[] NOT NULL DEFAULT '{}',
  terms_version            text,
  privacy_version          text,
  consent_accepted_at      timestamptz,
  onboarding_completed_at  timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  last_login_at            timestamptz
);
CREATE INDEX user_accounts_display_name_trgm ON identity.user_accounts USING gin (display_name gin_trgm_ops);
CREATE INDEX user_accounts_status_idx ON identity.user_accounts (status);

-- Versiones vigentes (placeholder hasta revisión legal, PP-F1-PRD-020 / M1.5).
INSERT INTO identity.consent_versions (kind, version, url, effective_from) VALUES
  ('terms',   '2026-10-draft', '/legal/terms',   '2026-10-01'),
  ('privacy', '2026-10-draft', '/legal/privacy', '2026-10-01');

CREATE TABLE platform.idempotency_keys (
  actor_id         uuid NOT NULL,
  key              uuid NOT NULL,
  request_hash     bytea NOT NULL,
  status           text NOT NULL CHECK (status IN ('in_progress','completed')),
  response_status  int,
  response_body    jsonb,
  created_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz NOT NULL,
  PRIMARY KEY (actor_id, key)
);
CREATE INDEX idempotency_expires_idx ON platform.idempotency_keys (expires_at);

CREATE TABLE platform.audit_log (
  id              uuid PRIMARY KEY,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  actor_id        uuid,                      -- null = system
  actor_roles     text[] NOT NULL DEFAULT '{}',
  action          text NOT NULL,             -- 'me.update', 'admin.user.suspend', 'authz.deny'
  target_type     text,
  target_id       uuid,
  reason          text,
  details         jsonb,                     -- sin PII en claro
  correlation_id  uuid,
  ip_hash         bytea
);
CREATE INDEX audit_log_target_idx ON platform.audit_log (target_type, target_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx  ON platform.audit_log (actor_id, occurred_at DESC);

CREATE TABLE platform.projection_checkpoints (   -- usado desde F2; creado en F1
  projection_name  text PRIMARY KEY,
  last_position    bigint NOT NULL DEFAULT 0,
  updated_at       timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS platform.projection_checkpoints;
DROP TABLE IF EXISTS platform.audit_log;
DROP TABLE IF EXISTS platform.idempotency_keys;
DROP TABLE IF EXISTS identity.user_accounts;
DROP TABLE IF EXISTS identity.consent_versions;
