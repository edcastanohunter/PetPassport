# F4 · 05 — Datos y persistencia

Convenciones de F1 (DAT-008) vigentes.

## 1. Prescriptions

**[PP-F4-DAT-001] (MUST)**

```sql
CREATE SCHEMA read_prescriptions;

CREATE TABLE read_prescriptions.prescriptions (
  id uuid PRIMARY KEY, version bigint NOT NULL,
  pet_id uuid NOT NULL, guardian_id uuid NOT NULL, country char(2) NOT NULL,
  source text NOT NULL CHECK (source IN ('issued','external')),
  status text NOT NULL CHECK (status IN ('draft','issued','confirmed','revoked','expired','exhausted')),
  issuer_id uuid, issuer_org_id uuid, credential jsonb, encounter_id uuid,
  issued_at timestamptz, signature jsonb,
  valid_until date, refills_allowed smallint NOT NULL DEFAULT 0, refills_used smallint NOT NULL DEFAULT 0,
  items jsonb NOT NULL DEFAULT '[]',                 -- PrescriptionItem[] (snapshot)
  items_remaining jsonb NOT NULL DEFAULT '{}',       -- {item_id: qty_remaining_current_refill}
  external_meta jsonb, validation jsonb, confirmation jsonb,
  has_controlled boolean NOT NULL DEFAULT false,
  pdf_object_key text, pdf_version int,
  archived_by_guardian boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX prescriptions_guardian_idx ON read_prescriptions.prescriptions (guardian_id, created_at DESC);
CREATE INDEX prescriptions_pet_idx ON read_prescriptions.prescriptions (pet_id, created_at DESC);
CREATE INDEX prescriptions_issuer_idx ON read_prescriptions.prescriptions (issuer_org_id, issuer_id, issued_at DESC);
CREATE INDEX prescriptions_expiry_idx ON read_prescriptions.prescriptions (valid_until) WHERE status IN ('issued','confirmed');

CREATE TABLE read_prescriptions.images (
  prescription_id uuid NOT NULL REFERENCES read_prescriptions.prescriptions(id), page smallint NOT NULL,
  object_key text NOT NULL, content_type text NOT NULL, width int, height int, sha256 bytea NOT NULL,
  scan_status text NOT NULL DEFAULT 'pending', uploaded_at timestamptz NOT NULL, delete_after date,
  PRIMARY KEY (prescription_id, page)
);

CREATE TABLE read_prescriptions.dispensations (
  id uuid PRIMARY KEY, prescription_id uuid NOT NULL, order_id uuid NOT NULL, pharmacy_org_id uuid NOT NULL,
  at timestamptz NOT NULL, lines jsonb NOT NULL, partial boolean NOT NULL
);
CREATE INDEX dispensations_rx_idx ON read_prescriptions.dispensations (prescription_id, at);

CREATE TABLE read_prescriptions.extraction_jobs (
  prescription_id uuid NOT NULL, attempt smallint NOT NULL,
  status text NOT NULL CHECK (status IN ('queued','preprocessing','extracting','matching','done','reupload_requested','failed','refused')),
  extractor_id text, legibility numeric(4,3), warnings jsonb, result_object_key text,
  input_tokens int, output_tokens int, cost_usd numeric(10,5), latency_ms int, error text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (prescription_id, attempt)
);

CREATE TABLE read_prescriptions.verifications (   -- contador de verificaciones públicas
  prescription_id uuid NOT NULL, at timestamptz NOT NULL, ip_hash bytea, via text NOT NULL CHECK (via IN ('public','partner'))
);
CREATE INDEX verifications_rx_idx ON read_prescriptions.verifications (prescription_id, at DESC);
```

## 2. Pharmacy Orders

**[PP-F4-DAT-002] (MUST)**

```sql
CREATE SCHEMA read_pharmacyorders;

CREATE TABLE read_pharmacyorders.quote_requests (
  id uuid PRIMARY KEY, version bigint NOT NULL,
  prescription_id uuid NOT NULL, pet_id uuid NOT NULL, guardian_id uuid NOT NULL, country char(2) NOT NULL,
  items jsonb NOT NULL, mode text NOT NULL, area jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('open','selected','closed')),
  expires_at timestamptz NOT NULL, selectable_until timestamptz, selected_quote_id uuid, order_id uuid, closed_reason text,
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX quote_requests_guardian_idx ON read_pharmacyorders.quote_requests (guardian_id, created_at DESC);
CREATE INDEX quote_requests_rx_open_idx ON read_pharmacyorders.quote_requests (prescription_id) WHERE status = 'open';
CREATE INDEX quote_requests_expiry_idx ON read_pharmacyorders.quote_requests (expires_at) WHERE status = 'open';

CREATE TABLE read_pharmacyorders.invitations (
  quote_request_id uuid NOT NULL REFERENCES read_pharmacyorders.quote_requests(id), organization_id uuid NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','sent','quoted','declined','failed','expired')),
  channel text, sent_at timestamptz, attempts int NOT NULL DEFAULT 0, last_error text, responded_at timestamptz,
  PRIMARY KEY (quote_request_id, organization_id)
);
CREATE INDEX invitations_pharmacy_inbox_idx ON read_pharmacyorders.invitations (organization_id, status, sent_at DESC);

CREATE TABLE read_pharmacyorders.quotes (
  id uuid PRIMARY KEY, quote_request_id uuid NOT NULL, organization_id uuid NOT NULL,
  lines jsonb NOT NULL, subtotal numeric(12,2) NOT NULL, delivery_fee numeric(12,2), total numeric(12,2) NOT NULL, currency char(3) NOT NULL,
  eta text, pickup jsonb, valid_until timestamptz NOT NULL, notes text, completeness text NOT NULL,
  received_at timestamptz NOT NULL, superseded boolean NOT NULL DEFAULT false
);
CREATE INDEX quotes_qr_idx ON read_pharmacyorders.quotes (quote_request_id, received_at);

CREATE TABLE read_pharmacyorders.orders (
  id uuid PRIMARY KEY, version bigint NOT NULL,
  quote_request_id uuid NOT NULL, quote_id uuid NOT NULL, prescription_id uuid NOT NULL,
  pet_id uuid NOT NULL, guardian_id uuid NOT NULL, pharmacy_org_id uuid NOT NULL,
  mode text NOT NULL, delivery_address jsonb, contact_phone text NOT NULL,
  lines jsonb NOT NULL, totals jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('placed','confirmed','preparing','ready','out_for_delivery','delivered','cancelled','rejected')),
  timeline jsonb NOT NULL DEFAULT '[]', dispensation jsonb, partner_ref text, rating jsonb,
  created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX orders_guardian_idx ON read_pharmacyorders.orders (guardian_id, created_at DESC);
CREATE INDEX orders_pharmacy_idx ON read_pharmacyorders.orders (pharmacy_org_id, status, created_at DESC);
CREATE INDEX orders_rx_idx ON read_pharmacyorders.orders (prescription_id);

CREATE TABLE read_pharmacyorders.partner_sla (
  organization_id uuid PRIMARY KEY, window_days int NOT NULL DEFAULT 90,
  invitations int NOT NULL DEFAULT 0, quoted int NOT NULL DEFAULT 0, declined int NOT NULL DEFAULT 0,
  avg_response_minutes numeric(10,1), p50_response_minutes numeric(10,1), quote_rate numeric(5,4),
  orders_placed int NOT NULL DEFAULT 0, orders_completed int NOT NULL DEFAULT 0, orders_rejected int NOT NULL DEFAULT 0,
  rating_avg numeric(3,2), rating_count int NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL
);
```

`delivery_address` y `contact_phone` en `orders` son PII: cifrado en reposo por la BD gestionada; acceso restringido por authz (tutor, farmacia del pedido, SuperAdmin con motivo); se anonimizan a los 180 días tras `delivered`/`cancelled` (job).

## 3. Partner Integration e identidad

**[PP-F4-DAT-003] (MUST)**

```sql
CREATE TABLE identity.professional_credentials (
  id uuid PRIMARY KEY, user_id uuid NOT NULL, country char(2) NOT NULL, kind text NOT NULL DEFAULT 'veterinarian',
  registry_number text NOT NULL, issuer text NOT NULL, issued_at date NOT NULL, expires_at date,
  evidence_object_key text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','verified','rejected','expired')), reviewed_by uuid, reason text, reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX credentials_registry_uq ON identity.professional_credentials (country, registry_number) WHERE status = 'verified';
CREATE UNIQUE INDEX credentials_user_country_uq ON identity.professional_credentials (user_id, country) WHERE status IN ('pending','verified');

CREATE TABLE read_partnernetwork.partner_integrations (
  organization_id uuid PRIMARY KEY,
  mode text NOT NULL CHECK (mode IN ('portal','api','adapter')), adapter_slug text,
  status text NOT NULL CHECK (status IN ('active','suspended')),
  keycloak_client_id text, sandbox_client_id text,
  capabilities jsonb NOT NULL DEFAULT '{"pickup":true,"delivery":false,"substitutions":true}',
  service_areas jsonb NOT NULL DEFAULT '[]', hours jsonb, auto_decline_outside_area boolean NOT NULL DEFAULT false,
  webhook jsonb,                      -- {url, events[], active, secret_ref_current, secret_ref_previous, previous_expires_at}
  rate_limits jsonb,                  -- overrides
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE read_partnernetwork.product_mappings (
  organization_id uuid NOT NULL, catalog_id uuid NOT NULL, partner_sku text NOT NULL, partner_name text,
  base_price numeric(12,2), active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, catalog_id)
);
CREATE INDEX product_mappings_sku_idx ON read_partnernetwork.product_mappings (organization_id, partner_sku);

CREATE TABLE read_partnernetwork.webhook_deliveries (
  id uuid PRIMARY KEY, organization_id uuid NOT NULL, event text NOT NULL, delivery_id uuid NOT NULL UNIQUE,
  payload_hash bytea NOT NULL, payload_object_key text,       -- payload guardado 7 días para reintento manual
  status text NOT NULL CHECK (status IN ('queued','sent','failed')), attempts int NOT NULL DEFAULT 0,
  last_status_code int, last_error text, next_attempt_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz
);
CREATE INDEX webhook_deliveries_org_idx ON read_partnernetwork.webhook_deliveries (organization_id, created_at DESC);
CREATE INDEX webhook_deliveries_retry_idx ON read_partnernetwork.webhook_deliveries (next_attempt_at) WHERE status = 'queued';

CREATE TABLE read_partnernetwork.partner_calls (      -- auditoría RN-31, particionada por mes
  id uuid NOT NULL, organization_id uuid NOT NULL, direction text NOT NULL CHECK (direction IN ('in','out')),
  endpoint text NOT NULL, status_code int, latency_ms int, payload_digest bytea, correlation_id uuid, at timestamptz NOT NULL
) PARTITION BY RANGE (at);
```

Secretos de webhook: valores en el secret manager (o cifrados con KEK en `platform.secrets` si no hay manager), referenciados por `secret_ref_*`; nunca en claro en tablas.

## 4. OCR

**[PP-F4-DAT-004] (MUST)**

```sql
CREATE SCHEMA ocr;
CREATE TABLE ocr.ai_usage_daily (day date NOT NULL, provider text NOT NULL, model text NOT NULL, requests int NOT NULL, input_tokens bigint NOT NULL, output_tokens bigint NOT NULL, cost_usd numeric(12,4) NOT NULL, refusals int NOT NULL DEFAULT 0, PRIMARY KEY (day, provider, model));
CREATE TABLE ocr.budget (id smallint PRIMARY KEY DEFAULT 1, monthly_usd numeric(12,2) NOT NULL, hard_stop boolean NOT NULL DEFAULT true, alert_pct smallint NOT NULL DEFAULT 80, updated_by uuid, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE ocr.metrics_daily (day date PRIMARY KEY, volume int, done int, reupload int, failed int, refused int, avg_latency_ms int, field_edit_rates jsonb, items_total int, items_accepted int, items_edited int, items_removed int, items_added int);
CREATE TABLE ocr.eval_samples (   -- dataset anonimizado para evaluación (SEC-008)
  id uuid PRIMARY KEY, object_key text NOT NULL, labels jsonb NOT NULL, source text NOT NULL CHECK (source IN ('synthetic','donated','production_consented')), consent_ref text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ocr.eval_runs (id uuid PRIMARY KEY, extractor_id text NOT NULL, dataset_version text NOT NULL, metrics jsonb NOT NULL, cost_usd numeric(10,4), ran_at timestamptz NOT NULL DEFAULT now(), git_sha text);
```

## 5. Reglas regulatorias

**[PP-F4-DAT-005] (MUST)** `ref.regulatory_rules (country char(2) PRIMARY KEY, default_validity_days int NOT NULL, max_validity_days int NOT NULL, max_refills smallint NOT NULL, controlled_orderable boolean NOT NULL DEFAULT false, requires_credential_verification boolean NOT NULL DEFAULT true, notes text, updated_by uuid, updated_at timestamptz)`; semilla `CO (30, 180, 5, false, true)`; cambios auditados; caché Redis 1 h con invalidación.

## 6. Retención

**[PP-F4-DAT-006] (MUST)**

| Dato | Retención |
|---|---|
| Imágenes de recetas externas | 90 días tras confirmación si no hay pedido; 2 años si hay pedido (obligación de trazabilidad de dispensación); luego borrado (job diario, `delete_after`). |
| Artefactos de extracción (JSON) | 90 días; métricas agregadas indefinidas. |
| Payloads de webhooks | 7 días; metadatos 180 días. |
| `partner_calls` | 12 meses (particiones mensuales). |
| PII en pedidos (dirección, teléfono) | anonimizada a 180 días tras cierre. |
| Recetas y dispensaciones (eventos) | indefinida (historia del animal; PII del tutor solo por id). |
| Muestras de evaluación | solo con consentimiento explícito o sintéticas; revisión anual. |
