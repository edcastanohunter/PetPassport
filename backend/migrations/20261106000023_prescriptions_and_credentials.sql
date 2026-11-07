-- +goose Up
-- F4 M4.0: Credenciales profesionales, read_prescriptions, partner network y reglas regulatorias.
-- Requisitos: PP-F4-DAT-001, PP-F4-DAT-003, PP-F4-DAT-005.

-- 1. Identity: credenciales profesionales (PP-F4-DAT-003, PP-F4-DOM-003)
CREATE TABLE IF NOT EXISTS identity.professional_credentials (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES identity.user_accounts(id),
  country char(2) NOT NULL,
  kind text NOT NULL DEFAULT 'veterinarian',
  registry_number text NOT NULL,
  issuer text NOT NULL,
  issued_at date NOT NULL,
  expires_at date,
  evidence_object_key text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','verified','rejected','expired')),
  reviewed_by uuid REFERENCES identity.user_accounts(id),
  reason text,
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS credentials_registry_uq ON identity.professional_credentials (country, registry_number) WHERE status = 'verified';
CREATE UNIQUE INDEX IF NOT EXISTS credentials_user_country_uq ON identity.professional_credentials (user_id, country) WHERE status IN ('pending','verified');
CREATE INDEX IF NOT EXISTS credentials_user_idx ON identity.professional_credentials (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS credentials_status_idx ON identity.professional_credentials (status, created_at DESC);

-- 2. Partner Network: integración, mapeos, webhooks y llamadas (PP-F4-DAT-003)
CREATE TABLE IF NOT EXISTS read_partnernetwork.partner_integrations (
  organization_id uuid PRIMARY KEY REFERENCES read_partnernetwork.organizations(id),
  mode text NOT NULL CHECK (mode IN ('portal','api','adapter')),
  adapter_slug text,
  status text NOT NULL CHECK (status IN ('active','suspended')),
  keycloak_client_id text,
  sandbox_client_id text,
  capabilities jsonb NOT NULL DEFAULT '{"pickup":true,"delivery":false,"substitutions":true}',
  service_areas jsonb NOT NULL DEFAULT '[]',
  hours jsonb,
  auto_decline_outside_area boolean NOT NULL DEFAULT false,
  webhook jsonb,
  rate_limits jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS read_partnernetwork.product_mappings (
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  catalog_id uuid NOT NULL REFERENCES ref.medications(id),
  partner_sku text NOT NULL,
  partner_name text,
  base_price numeric(12,2),
  active boolean NOT NULL DEFAULT true,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, catalog_id)
);
CREATE INDEX IF NOT EXISTS product_mappings_sku_idx ON read_partnernetwork.product_mappings (organization_id, partner_sku);

CREATE TABLE IF NOT EXISTS read_partnernetwork.webhook_deliveries (
  id uuid PRIMARY KEY,
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  event text NOT NULL,
  delivery_id uuid NOT NULL UNIQUE,
  payload_hash bytea NOT NULL,
  payload_object_key text,
  status text NOT NULL CHECK (status IN ('queued','sent','failed')),
  attempts int NOT NULL DEFAULT 0,
  last_status_code int,
  last_error text,
  next_attempt_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz
);
CREATE INDEX IF NOT EXISTS webhook_deliveries_org_idx ON read_partnernetwork.webhook_deliveries (organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS webhook_deliveries_retry_idx ON read_partnernetwork.webhook_deliveries (next_attempt_at) WHERE status = 'queued';

CREATE TABLE IF NOT EXISTS read_partnernetwork.partner_calls (
  id uuid NOT NULL,
  organization_id uuid NOT NULL,
  direction text NOT NULL CHECK (direction IN ('in','out')),
  endpoint text NOT NULL,
  status_code int,
  latency_ms int,
  payload_digest bytea,
  correlation_id uuid,
  at timestamptz NOT NULL,
  PRIMARY KEY (id, at)
) PARTITION BY RANGE (at);

CREATE TABLE IF NOT EXISTS read_partnernetwork.partner_calls_default PARTITION OF read_partnernetwork.partner_calls DEFAULT;

-- 3. Read Prescriptions: recetas, imágenes, dispensaciones, extracción y verificaciones (PP-F4-DAT-001)
CREATE SCHEMA IF NOT EXISTS read_prescriptions;
GRANT USAGE ON SCHEMA read_prescriptions TO pp_app_writer, pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_prescriptions GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_prescriptions GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_prescriptions GRANT SELECT ON TABLES TO pp_app_reader;

CREATE TABLE IF NOT EXISTS read_prescriptions.prescriptions (
  id uuid PRIMARY KEY,
  version bigint NOT NULL,
  pet_id uuid NOT NULL,
  guardian_id uuid NOT NULL,
  country char(2) NOT NULL,
  source text NOT NULL CHECK (source IN ('issued','external')),
  status text NOT NULL CHECK (status IN ('draft','issued','confirmed','revoked','expired','exhausted')),
  issuer_id uuid,
  issuer_org_id uuid,
  credential jsonb,
  encounter_id uuid,
  issued_at timestamptz,
  signature jsonb,
  valid_until date,
  refills_allowed smallint NOT NULL DEFAULT 0,
  refills_used smallint NOT NULL DEFAULT 0,
  items jsonb NOT NULL DEFAULT '[]',
  items_remaining jsonb NOT NULL DEFAULT '{}',
  external_meta jsonb,
  validation jsonb,
  confirmation jsonb,
  has_controlled boolean NOT NULL DEFAULT false,
  pdf_object_key text,
  pdf_version int,
  archived_by_guardian boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS prescriptions_guardian_idx ON read_prescriptions.prescriptions (guardian_id, created_at DESC);
CREATE INDEX IF NOT EXISTS prescriptions_pet_idx ON read_prescriptions.prescriptions (pet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS prescriptions_issuer_idx ON read_prescriptions.prescriptions (issuer_org_id, issuer_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS prescriptions_expiry_idx ON read_prescriptions.prescriptions (valid_until) WHERE status IN ('issued','confirmed');

CREATE TABLE IF NOT EXISTS read_prescriptions.images (
  prescription_id uuid NOT NULL REFERENCES read_prescriptions.prescriptions(id) ON DELETE CASCADE,
  page smallint NOT NULL,
  object_key text NOT NULL,
  content_type text NOT NULL,
  width int,
  height int,
  sha256 bytea NOT NULL,
  scan_status text NOT NULL DEFAULT 'pending',
  uploaded_at timestamptz NOT NULL,
  delete_after date,
  PRIMARY KEY (prescription_id, page)
);

CREATE TABLE IF NOT EXISTS read_prescriptions.dispensations (
  id uuid PRIMARY KEY,
  prescription_id uuid NOT NULL REFERENCES read_prescriptions.prescriptions(id) ON DELETE CASCADE,
  order_id uuid NOT NULL,
  pharmacy_org_id uuid NOT NULL,
  at timestamptz NOT NULL,
  lines jsonb NOT NULL,
  partial boolean NOT NULL
);
CREATE INDEX IF NOT EXISTS dispensations_rx_idx ON read_prescriptions.dispensations (prescription_id, at);

CREATE TABLE IF NOT EXISTS read_prescriptions.extraction_jobs (
  prescription_id uuid NOT NULL REFERENCES read_prescriptions.prescriptions(id) ON DELETE CASCADE,
  attempt smallint NOT NULL,
  status text NOT NULL CHECK (status IN ('queued','preprocessing','extracting','matching','done','reupload_requested','failed','refused')),
  extractor_id text,
  legibility numeric(4,3),
  warnings jsonb,
  result_object_key text,
  input_tokens int,
  output_tokens int,
  cost_usd numeric(10,5),
  latency_ms int,
  error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (prescription_id, attempt)
);

CREATE TABLE IF NOT EXISTS read_prescriptions.verifications (
  prescription_id uuid NOT NULL REFERENCES read_prescriptions.prescriptions(id) ON DELETE CASCADE,
  at timestamptz NOT NULL,
  ip_hash bytea,
  via text NOT NULL CHECK (via IN ('public','partner'))
);
CREATE INDEX IF NOT EXISTS verifications_rx_idx ON read_prescriptions.verifications (prescription_id, at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA read_prescriptions TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA read_prescriptions TO pp_app_reader;

-- 4. Reglas regulatorias (PP-F4-DAT-005, PP-F4-DOM-012)
CREATE TABLE IF NOT EXISTS ref.regulatory_rules (
  country char(2) PRIMARY KEY,
  default_validity_days int NOT NULL,
  max_validity_days int NOT NULL,
  max_refills smallint NOT NULL,
  controlled_orderable boolean NOT NULL DEFAULT false,
  requires_credential_verification boolean NOT NULL DEFAULT true,
  notes text,
  updated_by uuid REFERENCES identity.user_accounts(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO ref.regulatory_rules (
  country, default_validity_days, max_validity_days, max_refills, controlled_orderable, requires_credential_verification, notes
) VALUES (
  'CO', 30, 180, 5, false, true, 'Reglas de prescripción veterinaria para Colombia'
) ON CONFLICT (country) DO NOTHING;

GRANT SELECT, INSERT, UPDATE ON ref.regulatory_rules TO pp_app_writer;
GRANT SELECT ON ref.regulatory_rules TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS ref.regulatory_rules CASCADE;
DROP SCHEMA IF EXISTS read_prescriptions CASCADE;
DROP TABLE IF EXISTS read_partnernetwork.partner_calls CASCADE;
DROP TABLE IF EXISTS read_partnernetwork.webhook_deliveries CASCADE;
DROP TABLE IF EXISTS read_partnernetwork.product_mappings CASCADE;
DROP TABLE IF EXISTS read_partnernetwork.partner_integrations CASCADE;
DROP TABLE IF EXISTS identity.professional_credentials CASCADE;
