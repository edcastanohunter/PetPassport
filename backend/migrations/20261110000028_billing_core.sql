-- +goose Up
-- Billing Core (PP-F5-DAT-001, PP-F5-DAT-002, ADR-022, ADR-025).

CREATE SCHEMA IF NOT EXISTS billing;
GRANT USAGE ON SCHEMA billing TO pp_app_writer, pp_app_reader;

CREATE TABLE billing.accounts (
  id uuid PRIMARY KEY,
  version bigint NOT NULL,
  owner_user_id uuid NOT NULL,
  legal_entity jsonb NOT NULL,
  country char(2) NOT NULL,
  currency char(3) NOT NULL,
  payment_method jsonb,
  provider jsonb,
  status text NOT NULL CHECK (status IN ('active','closed')),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE billing.account_organizations (
  account_id uuid NOT NULL REFERENCES billing.accounts(id),
  organization_id uuid NOT NULL UNIQUE,
  seat_pool text NOT NULL DEFAULT 'own',
  joined_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, organization_id)
);

CREATE TABLE billing.subscriptions (
  id uuid PRIMARY KEY,
  version bigint NOT NULL,
  account_id uuid NOT NULL REFERENCES billing.accounts(id),
  kind text NOT NULL CHECK (kind IN ('clinic','pharmacy')),
  plan_code text NOT NULL,
  plan_version int NOT NULL,
  seats_purchased int,
  tier text,
  cycle text NOT NULL,
  status text NOT NULL CHECK (status IN ('trialing','active','grace','restricted','cancelled','expired')),
  trial_ends_at timestamptz,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  pending_change jsonb,
  dunning jsonb,
  coupon jsonb,
  manual_billing boolean NOT NULL DEFAULT false,
  cancel_at_period_end boolean NOT NULL DEFAULT false,
  cancelled_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (account_id, kind)
);
CREATE INDEX subscriptions_renewal_idx ON billing.subscriptions (period_end) WHERE status IN ('trialing','active','grace','restricted');
CREATE INDEX subscriptions_status_idx ON billing.subscriptions (status, plan_code);

CREATE TABLE billing.charges (
  id uuid PRIMARY KEY,
  subscription_id uuid NOT NULL,
  account_id uuid NOT NULL,
  reason text NOT NULL,
  period_start timestamptz,
  period_end timestamptz,
  amount numeric(12,2) NOT NULL,
  tax numeric(12,2) NOT NULL,
  total numeric(12,2) NOT NULL,
  currency char(3) NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','paid','failed','refunded','partially_refunded','void')),
  refunded numeric(12,2) NOT NULL DEFAULT 0,
  lines jsonb NOT NULL,
  scheduled_at timestamptz NOT NULL,
  paid_at timestamptz,
  failed_at timestamptz,
  error_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);
CREATE INDEX charges_pending_idx ON billing.charges (scheduled_at) WHERE status = 'pending';
CREATE INDEX charges_account_idx ON billing.charges (account_id, created_at DESC);

CREATE TABLE billing.payments (
  id uuid PRIMARY KEY,
  charge_id uuid NOT NULL,
  provider text NOT NULL,
  provider_ref text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('payment','refund')),
  amount numeric(12,2) NOT NULL,
  status text NOT NULL,
  at timestamptz NOT NULL,
  raw_ref text,
  UNIQUE (provider, provider_ref)
);

CREATE TABLE billing.provider_events (
  provider text NOT NULL,
  event_id text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  result text,
  PRIMARY KEY (provider, event_id)
);

CREATE TABLE billing.invoices (
  id uuid PRIMARY KEY,
  charge_id uuid NOT NULL UNIQUE,
  account_id uuid NOT NULL,
  provider text NOT NULL,
  number text,
  status text NOT NULL CHECK (status IN ('draft','issued','accepted','rejected','credited','contingency')),
  cufe text,
  pdf_object_key text,
  xml_object_key text,
  totals jsonb NOT NULL,
  lines jsonb NOT NULL,
  customer_snapshot jsonb NOT NULL,
  issued_at timestamptz,
  accepted_at timestamptz,
  rejection_reason text,
  attempts int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE billing.credit_notes (
  id uuid PRIMARY KEY,
  invoice_id uuid NOT NULL REFERENCES billing.invoices(id),
  payment_id uuid,
  number text,
  status text NOT NULL,
  amount numeric(12,2) NOT NULL,
  pdf_object_key text,
  xml_object_key text,
  issued_at timestamptz,
  reason text
);

CREATE TABLE billing.discrepancies (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL,
  charge_id uuid,
  kind text NOT NULL,
  details jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('open','resolved')),
  detected_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  resolution text,
  resolved_by uuid
);

CREATE TABLE billing.usage_counters (
  organization_id uuid NOT NULL,
  quota_key text NOT NULL,
  period_start date NOT NULL,
  period_end date NOT NULL,
  used bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, quota_key, period_start)
);

CREATE TABLE billing.usage_applied (
  projection text NOT NULL,
  event_id uuid NOT NULL,
  PRIMARY KEY (projection, event_id)
);

CREATE TABLE billing.entitlements (
  organization_id uuid PRIMARY KEY,
  account_id uuid,
  subscription_id uuid,
  plan_code text NOT NULL,
  plan_version int NOT NULL,
  status text NOT NULL,
  seats_limit int,
  seats_used int NOT NULL DEFAULT 0,
  quotas jsonb NOT NULL,
  features text[] NOT NULL,
  period_end timestamptz,
  computed_at timestamptz NOT NULL
);

CREATE TABLE billing.provider_routing (
  country char(2) PRIMARY KEY,
  payment_provider text NOT NULL,
  invoicing_provider text NOT NULL,
  tax_rule text NOT NULL
);

CREATE TABLE billing.metrics_daily (
  day date PRIMARY KEY,
  metrics jsonb NOT NULL,
  computed_at timestamptz NOT NULL
);

CREATE TABLE billing.coupons (
  code text PRIMARY KEY,
  kind text NOT NULL,
  value numeric(12,2) NOT NULL,
  duration_months int,
  max_uses int,
  uses int NOT NULL DEFAULT 0,
  valid_until date,
  plans text[],
  active boolean NOT NULL DEFAULT true
);

-- Planes y reglas fiscales (ref)
CREATE TABLE IF NOT EXISTS ref.plans (
  code text NOT NULL,
  version int NOT NULL,
  kind text NOT NULL,
  name jsonb NOT NULL,
  pricing jsonb NOT NULL,
  trial_days int NOT NULL DEFAULT 0,
  seats jsonb,
  quotas jsonb NOT NULL,
  features text[] NOT NULL,
  status text NOT NULL CHECK (status IN ('draft','published','retired')),
  effective_from date,
  published_at timestamptz,
  published_by uuid,
  PRIMARY KEY (code, version)
);

CREATE TABLE IF NOT EXISTS ref.tax_rules (
  code text PRIMARY KEY,
  country char(2) NOT NULL,
  rate numeric(5,4) NOT NULL,
  applies_to text NOT NULL,
  exemptions jsonb,
  notes text
);

-- Permisos
ALTER DEFAULT PRIVILEGES IN SCHEMA billing GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA billing GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA billing GRANT SELECT ON TABLES TO pp_app_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA billing TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA billing TO pp_app_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON ref.plans, ref.tax_rules TO pp_app_writer;
GRANT SELECT ON ref.plans, ref.tax_rules TO pp_app_reader;

-- Semillas CO en draft (DAT-002)
INSERT INTO ref.tax_rules (code, country, rate, applies_to, notes) VALUES
('CO_IVA_19', 'CO', 0.1900, 'digital_services', 'IVA Colombia 19% general');

INSERT INTO billing.provider_routing (country, payment_provider, invoicing_provider, tax_rule) VALUES
('CO', 'fake', 'fake', 'CO_IVA_19');

INSERT INTO ref.plans (code, version, kind, name, pricing, trial_days, seats, quotas, features, status, effective_from) VALUES
('starter', 1, 'clinic', '{"es": "Clínica Starter", "en": "Clinic Starter"}', '{"model": "flat", "currency": "COP", "monthly": 120000.00, "annual": 1200000.00, "tiers": []}', 0, '{"min": 1, "max": 2}', '{"quotes_per_month": 50, "sms_per_month": 0, "storage_gb": 5}', '{"agenda", "encounters", "vaccination_cards", "prescriptions", "quotes"}', 'published', '2026-11-01'),
('pro', 1, 'clinic', '{"es": "Clínica Pro", "en": "Clinic Pro"}', '{"model": "per_seat", "currency": "COP", "monthly": 75000.00, "annual": 750000.00, "tiers": []}', 30, '{"min": 1}', '{"quotes_per_month": 500, "sms_per_month": 100, "storage_gb": 50}', '{"agenda", "encounters", "vaccination_cards", "prescriptions", "quotes", "cohorts", "directory", "sms"}', 'published', '2026-11-01'),
('enterprise', 1, 'clinic', '{"es": "Clínica Enterprise", "en": "Clinic Enterprise"}', '{"model": "per_seat", "currency": "COP", "monthly": 150000.00, "annual": 1500000.00, "tiers": []}', 0, '{"min": 5}', '{"quotes_per_month": 10000, "sms_per_month": 1000, "storage_gb": 500}', '{"agenda", "encounters", "vaccination_cards", "prescriptions", "quotes", "cohorts", "directory", "sms", "multi_org", "sla_999", "dedicated_support"}', 'published', '2026-11-01'),
('pharmacy_basic', 1, 'pharmacy', '{"es": "Farmacia Basic", "en": "Pharmacy Basic"}', '{"model": "flat", "currency": "COP", "monthly": 0.00, "annual": 0.00, "tiers": []}', 0, NULL, '{"quotes_per_month": 50}', '{"quote_portal", "orders"}', 'published', '2026-11-01'),
('pharmacy_pro', 1, 'pharmacy', '{"es": "Farmacia Pro", "en": "Pharmacy Pro"}', '{"model": "tiered", "currency": "COP", "monthly": 120000.00, "annual": 1200000.00, "tiers": [{"code": "tier_200", "up_to": 200, "price": 120000.00}, {"code": "tier_1000", "up_to": 1000, "price": 350000.00}, {"code": "tier_unlimited", "up_to": -1, "price": 600000.00}]}', 30, NULL, '{"quotes_per_month": 200}', '{"quote_portal", "orders", "partner_api", "webhooks", "metrics", "directory"}', 'published', '2026-11-01');

-- +goose Down
DELETE FROM ref.plans WHERE code IN ('starter', 'pro', 'enterprise', 'pharmacy_basic', 'pharmacy_pro');
DELETE FROM ref.tax_rules WHERE code = 'CO_IVA_19';
DROP TABLE IF EXISTS ref.tax_rules;
DROP TABLE IF EXISTS ref.plans;
DROP SCHEMA IF EXISTS billing CASCADE;
