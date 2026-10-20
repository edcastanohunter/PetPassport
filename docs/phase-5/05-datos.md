# F5 · 05 — Datos y persistencia

## 1. Billing

**[PP-F5-DAT-001] (MUST)**

```sql
CREATE SCHEMA billing;

CREATE TABLE billing.accounts (
  id uuid PRIMARY KEY, version bigint NOT NULL, owner_user_id uuid NOT NULL,
  legal_entity jsonb NOT NULL,           -- cifrado de campos PII por DEK del owner en eventos; aquí en claro (read model) con acceso restringido
  country char(2) NOT NULL, currency char(3) NOT NULL,
  payment_method jsonb, provider jsonb,  -- {name, customer_id} ; {kind, token_ref, last4, expires_at}
  status text NOT NULL CHECK (status IN ('active','closed')),
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE TABLE billing.account_organizations (account_id uuid NOT NULL REFERENCES billing.accounts(id), organization_id uuid NOT NULL UNIQUE, seat_pool text NOT NULL DEFAULT 'own', joined_at timestamptz NOT NULL, PRIMARY KEY (account_id, organization_id));

CREATE TABLE billing.subscriptions (
  id uuid PRIMARY KEY, version bigint NOT NULL, account_id uuid NOT NULL REFERENCES billing.accounts(id),
  kind text NOT NULL CHECK (kind IN ('clinic','pharmacy')),
  plan_code text NOT NULL, plan_version int NOT NULL, seats_purchased int, tier text, cycle text NOT NULL,
  status text NOT NULL CHECK (status IN ('trialing','active','grace','restricted','cancelled','expired')),
  trial_ends_at timestamptz, period_start timestamptz NOT NULL, period_end timestamptz NOT NULL,
  pending_change jsonb, dunning jsonb, coupon jsonb, manual_billing boolean NOT NULL DEFAULT false,
  cancel_at_period_end boolean NOT NULL DEFAULT false, cancelled_at timestamptz,
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
  UNIQUE (account_id, kind)
);
CREATE INDEX subscriptions_renewal_idx ON billing.subscriptions (period_end) WHERE status IN ('trialing','active','grace','restricted');
CREATE INDEX subscriptions_status_idx ON billing.subscriptions (status, plan_code);

CREATE TABLE billing.charges (
  id uuid PRIMARY KEY, subscription_id uuid NOT NULL, account_id uuid NOT NULL,
  reason text NOT NULL, period_start timestamptz, period_end timestamptz,
  amount numeric(12,2) NOT NULL, tax numeric(12,2) NOT NULL, total numeric(12,2) NOT NULL, currency char(3) NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','paid','failed','refunded','partially_refunded','void')),
  refunded numeric(12,2) NOT NULL DEFAULT 0, lines jsonb NOT NULL,
  scheduled_at timestamptz NOT NULL, paid_at timestamptz, failed_at timestamptz, error_code text,
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX charges_pending_idx ON billing.charges (scheduled_at) WHERE status = 'pending';
CREATE INDEX charges_account_idx ON billing.charges (account_id, created_at DESC);

CREATE TABLE billing.payments (id uuid PRIMARY KEY, charge_id uuid NOT NULL, provider text NOT NULL, provider_ref text NOT NULL, kind text NOT NULL CHECK (kind IN ('payment','refund')), amount numeric(12,2) NOT NULL, status text NOT NULL, at timestamptz NOT NULL, raw_ref text, UNIQUE (provider, provider_ref));
CREATE TABLE billing.provider_events (provider text NOT NULL, event_id text NOT NULL, received_at timestamptz NOT NULL DEFAULT now(), processed_at timestamptz, result text, PRIMARY KEY (provider, event_id));

CREATE TABLE billing.invoices (
  id uuid PRIMARY KEY, charge_id uuid NOT NULL UNIQUE, account_id uuid NOT NULL, provider text NOT NULL,
  number text, status text NOT NULL CHECK (status IN ('draft','issued','accepted','rejected','credited','contingency')),
  cufe text, pdf_object_key text, xml_object_key text, totals jsonb NOT NULL, lines jsonb NOT NULL, customer_snapshot jsonb NOT NULL,
  issued_at timestamptz, accepted_at timestamptz, rejection_reason text, attempts int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE billing.credit_notes (id uuid PRIMARY KEY, invoice_id uuid NOT NULL REFERENCES billing.invoices(id), payment_id uuid, number text, status text NOT NULL, amount numeric(12,2) NOT NULL, pdf_object_key text, xml_object_key text, issued_at timestamptz, reason text);

CREATE TABLE billing.discrepancies (id uuid PRIMARY KEY, account_id uuid NOT NULL, charge_id uuid, kind text NOT NULL, details jsonb NOT NULL, status text NOT NULL CHECK (status IN ('open','resolved')), detected_at timestamptz NOT NULL DEFAULT now(), resolved_at timestamptz, resolution text, resolved_by uuid);

CREATE TABLE billing.usage_counters (organization_id uuid NOT NULL, quota_key text NOT NULL, period_start date NOT NULL, period_end date NOT NULL, used bigint NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL, PRIMARY KEY (organization_id, quota_key, period_start));
CREATE TABLE billing.usage_applied (projection text NOT NULL, event_id uuid NOT NULL, PRIMARY KEY (projection, event_id));

CREATE TABLE billing.entitlements (
  organization_id uuid PRIMARY KEY, account_id uuid, subscription_id uuid, plan_code text NOT NULL, plan_version int NOT NULL,
  status text NOT NULL, seats_limit int, seats_used int NOT NULL DEFAULT 0, quotas jsonb NOT NULL, features text[] NOT NULL, period_end timestamptz,
  computed_at timestamptz NOT NULL
);

CREATE TABLE billing.provider_routing (country char(2) PRIMARY KEY, payment_provider text NOT NULL, invoicing_provider text NOT NULL, tax_rule text NOT NULL);
CREATE TABLE billing.metrics_daily (day date PRIMARY KEY, metrics jsonb NOT NULL, computed_at timestamptz NOT NULL);
CREATE TABLE billing.coupons (code text PRIMARY KEY, kind text NOT NULL, value numeric(12,2) NOT NULL, duration_months int, max_uses int, uses int NOT NULL DEFAULT 0, valid_until date, plans text[], active boolean NOT NULL DEFAULT true);
```

Roles: `pp_app_writer` con acceso; `billing.accounts.legal_entity` y `invoices.customer_snapshot` solo legibles por rutas `billing:*` (authz) y por el job de facturación; vistas sin PII para reportes (`billing.v_subscriptions_report`).

## 2. Planes y reglas fiscales

**[PP-F5-DAT-002] (MUST)**

```sql
CREATE TABLE ref.plans (
  code text NOT NULL, version int NOT NULL, kind text NOT NULL, name jsonb NOT NULL,   -- {es, en}
  pricing jsonb NOT NULL, trial_days int NOT NULL DEFAULT 0, seats jsonb, quotas jsonb NOT NULL, features text[] NOT NULL,
  status text NOT NULL CHECK (status IN ('draft','published','retired')), effective_from date, published_at timestamptz, published_by uuid,
  PRIMARY KEY (code, version)
);
CREATE TABLE ref.tax_rules (code text PRIMARY KEY, country char(2) NOT NULL, rate numeric(5,4) NOT NULL, applies_to text NOT NULL, exemptions jsonb, notes text);
```

Semillas `CO`: planes de 01-producto §3 con precios *placeholder* marcados `draft` hasta decisión de negocio; `tax_rules`: `CO_IVA_19`.

## 3. Privacy

**[PP-F5-DAT-003] (MUST)**

```sql
CREATE SCHEMA privacy;
CREATE TABLE privacy.subject_keys (
  subject_id uuid PRIMARY KEY, dek_encrypted bytea NOT NULL, kek_id text NOT NULL,
  status text NOT NULL CHECK (status IN ('active','shredding','shredded')),
  created_at timestamptz NOT NULL DEFAULT now(), shred_requested_at timestamptz, shredded_at timestamptz
);
-- Esta tabla se excluye del backup lógico general y se respalda por separado (cifrada con KMS); ver OPS-003.
CREATE TABLE privacy.kek_rotations (kek_id text PRIMARY KEY, created_at timestamptz NOT NULL, retired_at timestamptz, rewrapped_count bigint);
CREATE TABLE privacy.data_subject_requests (
  id uuid PRIMARY KEY, subject_id uuid NOT NULL, subject_kind text NOT NULL CHECK (subject_kind IN ('user','organization')),
  kind text NOT NULL CHECK (kind IN ('export','erasure','rectification')),
  status text NOT NULL CHECK (status IN ('requested','verified','processing','ready','completed','cancelled','rejected')),
  requested_at timestamptz NOT NULL DEFAULT now(), verified_at timestamptz, grace_ends_at timestamptz, started_at timestamptz, completed_at timestamptz,
  artifact_object_key text, artifact_expires_at timestamptz, download_token_hash bytea, downloaded_at timestamptz,
  legal_holds jsonb NOT NULL DEFAULT '[]', report jsonb, reason text, handled_by uuid
);
CREATE INDEX dsr_subject_idx ON privacy.data_subject_requests (subject_id, requested_at DESC);
CREATE INDEX dsr_due_idx ON privacy.data_subject_requests (grace_ends_at) WHERE status = 'verified' AND kind = 'erasure';
CREATE TABLE privacy.retention_rules (data_class text PRIMARY KEY, action text NOT NULL CHECK (action IN ('anonymize','delete')), after_interval interval NOT NULL, legal_basis text NOT NULL, enabled boolean NOT NULL DEFAULT true);
CREATE TABLE privacy.retention_runs (id uuid PRIMARY KEY, ran_at timestamptz NOT NULL DEFAULT now(), report jsonb NOT NULL);
CREATE TABLE privacy.processing_activities (id uuid PRIMARY KEY, name text NOT NULL, purpose text NOT NULL, data_classes text[] NOT NULL, subjects text[] NOT NULL, processors text[] NOT NULL, legal_basis text NOT NULL, retention text NOT NULL, updated_at timestamptz NOT NULL);
```

## 4. Support

**[PP-F5-DAT-004] (MUST)**

```sql
CREATE SCHEMA support;
CREATE TABLE support.tickets (id uuid PRIMARY KEY, number bigint GENERATED ALWAYS AS IDENTITY, requester_id uuid NOT NULL, organization_id uuid, category text NOT NULL, subject text NOT NULL, status text NOT NULL, priority text NOT NULL, assignee_id uuid, first_response_due timestamptz NOT NULL, resolved_due timestamptz, first_responded_at timestamptz, resolved_at timestamptz, closed_at timestamptz, satisfaction jsonb, external_ref text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE support.messages (id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES support.tickets(id), author_id uuid, author_kind text NOT NULL CHECK (author_kind IN ('user','agent','system')), body text NOT NULL, attachments jsonb NOT NULL DEFAULT '[]', internal boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE support.impersonation_sessions (id uuid PRIMARY KEY, subject_user_id uuid NOT NULL, agent_id uuid, ticket_id uuid, scope text NOT NULL, clinical_allowed boolean NOT NULL, granted_at timestamptz NOT NULL, expires_at timestamptz NOT NULL, started_at timestamptz, revoked_at timestamptz, actions_count int NOT NULL DEFAULT 0);
CREATE INDEX impersonation_active_idx ON support.impersonation_sessions (subject_user_id) WHERE revoked_at IS NULL;
CREATE TABLE support.metrics_daily (day date PRIMARY KEY, metrics jsonb NOT NULL);
```

## 5. Messaging

**[PP-F5-DAT-005] (SHOULD)**

```sql
CREATE SCHEMA messaging;
CREATE TABLE messaging.phone_verifications (user_id uuid NOT NULL, phone text NOT NULL, otp_hash bytea NOT NULL, expires_at timestamptz NOT NULL, attempts int NOT NULL DEFAULT 0, verified_at timestamptz, PRIMARY KEY (user_id, phone));
ALTER TABLE identity.user_accounts ADD COLUMN phone_verified_at timestamptz;
CREATE TABLE messaging.templates (name text NOT NULL, channel text NOT NULL, locale text NOT NULL, provider text NOT NULL, provider_template_id text, status text NOT NULL, body text NOT NULL, variables text[] NOT NULL, PRIMARY KEY (name, channel, locale, provider));
CREATE TABLE messaging.opt_outs (phone_hash bytea NOT NULL, channel text NOT NULL, at timestamptz NOT NULL DEFAULT now(), source text NOT NULL, PRIMARY KEY (phone_hash, channel));
ALTER TABLE notifications.deliveries DROP CONSTRAINT deliveries_channel_check, ADD CONSTRAINT deliveries_channel_check CHECK (channel IN ('email','push','sms','whatsapp')), ADD COLUMN cost_cents int, ADD COLUMN billed_to_org uuid;
ALTER TABLE notifications.preferences ADD COLUMN sms jsonb NOT NULL DEFAULT '{}', ADD COLUMN whatsapp jsonb NOT NULL DEFAULT '{}';
```

## 6. Retención y backups especiales

**[PP-F5-DAT-006] (MUST)** Semillas de `privacy.retention_rules` (ARQ-009); `billing.invoices` PDF/XML en bucket con *object lock* 10 años; `privacy.subject_keys` con backup separado cifrado por KMS y **excluida** del dump general (para que un backup robado no contenga claves junto a datos); documentación de restauración conjunta (OPS-004).

## 7. Métricas

**[PP-F5-DAT-007] (MUST)** `billing.metrics_daily` y `support.metrics_daily` calculadas por job nocturno (`stats:business`) desde read models (sin PII), con fórmulas en `docs/phase-5/metrics.md`; retención indefinida (agregados).
