-- +goose Up
-- Read models de Partner Network (PP-F1-DAT-006, docs/phase-1/06 §6).
-- public_code_reservations y unicidad de tax_id por índice único (ADR-002).

CREATE TABLE read_partnernetwork.organizations (
  id            uuid PRIMARY KEY,
  version       bigint NOT NULL,
  type          text NOT NULL CHECK (type IN ('clinic','pharmacy')),
  legal_name    text NOT NULL,
  display_name  text NOT NULL,
  tax_country   char(2) NOT NULL,
  tax_value     text NOT NULL,
  address       jsonb NOT NULL,
  city          text NOT NULL,
  country       char(2) NOT NULL,
  contact_email citext NOT NULL,
  contact_phone text NOT NULL,
  website       text,
  public_code   text NOT NULL UNIQUE,
  status        text NOT NULL CHECK (status IN ('pending_verification','active','suspended','rejected')),
  status_reason text,
  founder_id    uuid NOT NULL,
  member_count  int NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL
);
CREATE UNIQUE INDEX organizations_tax_idx ON read_partnernetwork.organizations (tax_country, tax_value);
CREATE INDEX organizations_status_idx ON read_partnernetwork.organizations (status, created_at);
CREATE INDEX organizations_name_trgm ON read_partnernetwork.organizations USING gin (display_name gin_trgm_ops);

CREATE TABLE read_partnernetwork.org_members (
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  user_id         uuid NOT NULL,
  role            text NOT NULL,
  status          text NOT NULL CHECK (status IN ('active','removed')),
  joined_at       timestamptz NOT NULL,
  removed_at      timestamptz,
  PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX org_members_user_idx ON read_partnernetwork.org_members (user_id) WHERE status = 'active';

CREATE TABLE read_partnernetwork.invitations (
  id              uuid PRIMARY KEY,
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  email           citext NOT NULL,
  role            text NOT NULL,
  token_hash      bytea NOT NULL UNIQUE,
  status          text NOT NULL CHECK (status IN ('pending','accepted','revoked','expired')),
  expires_at      timestamptz NOT NULL,
  resend_count    int NOT NULL DEFAULT 0,
  last_sent_at    timestamptz,
  resend_window_start timestamptz,                    -- inicio de la ventana de 24 h de reenvíos (API-007)
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX invitations_pending_email_idx ON read_partnernetwork.invitations (organization_id, email) WHERE status = 'pending';

CREATE TABLE read_partnernetwork.public_code_reservations (
  public_code     text PRIMARY KEY,
  organization_id uuid NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS read_partnernetwork.public_code_reservations;
DROP TABLE IF EXISTS read_partnernetwork.invitations;
DROP TABLE IF EXISTS read_partnernetwork.org_members;
DROP TABLE IF EXISTS read_partnernetwork.organizations;
