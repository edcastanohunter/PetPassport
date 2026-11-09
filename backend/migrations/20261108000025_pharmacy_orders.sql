-- +goose Up
-- F4 M4.2: Cotización de farmacias y pedidos.
-- Requisitos: PP-F4-DAT-002, PP-F4-ARQ-014.

CREATE SCHEMA IF NOT EXISTS read_pharmacyorders;
GRANT USAGE ON SCHEMA read_pharmacyorders TO pp_app_writer, pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_pharmacyorders GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_pharmacyorders GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA read_pharmacyorders GRANT SELECT ON TABLES TO pp_app_reader;

CREATE TABLE IF NOT EXISTS read_pharmacyorders.quote_requests (
  id uuid PRIMARY KEY,
  version bigint NOT NULL,
  prescription_id uuid NOT NULL,
  pet_id uuid NOT NULL,
  guardian_id uuid NOT NULL,
  country char(2) NOT NULL,
  items jsonb NOT NULL,
  mode text NOT NULL,
  area jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('open','selected','closed')),
  expires_at timestamptz NOT NULL,
  selectable_until timestamptz,
  selected_quote_id uuid,
  order_id uuid,
  closed_reason text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS quote_requests_guardian_idx ON read_pharmacyorders.quote_requests (guardian_id, created_at DESC);
CREATE INDEX IF NOT EXISTS quote_requests_rx_open_idx ON read_pharmacyorders.quote_requests (prescription_id) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS quote_requests_expiry_idx ON read_pharmacyorders.quote_requests (expires_at) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS read_pharmacyorders.invitations (
  quote_request_id uuid NOT NULL REFERENCES read_pharmacyorders.quote_requests(id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  status text NOT NULL CHECK (status IN ('pending','sent','quoted','declined','failed','expired')),
  channel text,
  sent_at timestamptz,
  attempts int NOT NULL DEFAULT 0,
  last_error text,
  responded_at timestamptz,
  PRIMARY KEY (quote_request_id, organization_id)
);

CREATE INDEX IF NOT EXISTS invitations_pharmacy_inbox_idx ON read_pharmacyorders.invitations (organization_id, status, sent_at DESC);

CREATE TABLE IF NOT EXISTS read_pharmacyorders.quotes (
  id uuid PRIMARY KEY,
  quote_request_id uuid NOT NULL REFERENCES read_pharmacyorders.quote_requests(id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  lines jsonb NOT NULL,
  subtotal numeric(12,2) NOT NULL,
  delivery_fee numeric(12,2),
  total numeric(12,2) NOT NULL,
  currency char(3) NOT NULL,
  eta text,
  pickup jsonb,
  valid_until timestamptz NOT NULL,
  notes text,
  completeness text NOT NULL,
  received_at timestamptz NOT NULL,
  superseded boolean NOT NULL DEFAULT false
);

CREATE INDEX IF NOT EXISTS quotes_qr_idx ON read_pharmacyorders.quotes (quote_request_id, received_at);

CREATE TABLE IF NOT EXISTS read_pharmacyorders.orders (
  id uuid PRIMARY KEY,
  version bigint NOT NULL,
  quote_request_id uuid NOT NULL REFERENCES read_pharmacyorders.quote_requests(id),
  quote_id uuid NOT NULL,
  prescription_id uuid NOT NULL,
  pet_id uuid NOT NULL,
  guardian_id uuid NOT NULL,
  pharmacy_org_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  mode text NOT NULL,
  delivery_address jsonb,
  contact_phone text NOT NULL,
  lines jsonb NOT NULL,
  totals jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('placed','confirmed','preparing','ready','out_for_delivery','delivered','cancelled','rejected')),
  timeline jsonb NOT NULL DEFAULT '[]',
  dispensation jsonb,
  partner_ref text,
  rating jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS orders_guardian_idx ON read_pharmacyorders.orders (guardian_id, created_at DESC);
CREATE INDEX IF NOT EXISTS orders_pharmacy_idx ON read_pharmacyorders.orders (pharmacy_org_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS orders_rx_idx ON read_pharmacyorders.orders (prescription_id);

CREATE TABLE IF NOT EXISTS read_pharmacyorders.partner_sla (
  organization_id uuid PRIMARY KEY REFERENCES read_partnernetwork.organizations(id),
  window_days int NOT NULL DEFAULT 90,
  invitations int NOT NULL DEFAULT 0,
  quoted int NOT NULL DEFAULT 0,
  declined int NOT NULL DEFAULT 0,
  avg_response_minutes numeric(10,1),
  p50_response_minutes numeric(10,1),
  quote_rate numeric(5,4),
  orders_placed int NOT NULL DEFAULT 0,
  orders_completed int NOT NULL DEFAULT 0,
  orders_rejected int NOT NULL DEFAULT 0,
  rating_avg numeric(3,2),
  rating_count int NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS read_pharmacyorders.pharmacy_integrations (
  organization_id uuid PRIMARY KEY REFERENCES read_partnernetwork.organizations(id),
  level text NOT NULL DEFAULT 'portal',
  webhook_url text,
  catalog_mapping_enabled boolean NOT NULL DEFAULT true,
  delivery_areas jsonb NOT NULL DEFAULT '[]'::jsonb,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS read_pharmacyorders.pharmacy_integrations;
DROP TABLE IF EXISTS read_pharmacyorders.partner_sla;
DROP TABLE IF EXISTS read_pharmacyorders.orders;
DROP TABLE IF EXISTS read_pharmacyorders.quotes;
DROP TABLE IF EXISTS read_pharmacyorders.invitations;
DROP TABLE IF EXISTS read_pharmacyorders.quote_requests;
DROP SCHEMA IF EXISTS read_pharmacyorders CASCADE;
