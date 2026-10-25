-- +goose Up
-- Read models de Pet Registry (PP-F1-DAT-005, docs/phase-1/06 §5). La unicidad
-- global del microchip se garantiza por reserva transaccional (ADR-002 §Consecuencias);
-- pet_transfers resuelve token → mascota sin recorrer eventos (se usa desde M1.3).

CREATE SCHEMA IF NOT EXISTS read_petregistry;

CREATE TABLE read_petregistry.pets (
  id              uuid PRIMARY KEY,
  version         bigint NOT NULL,
  guardian_id     uuid NOT NULL,
  guardian_display_name text NOT NULL DEFAULT '',      -- desnormalizado (puerto Identity)
  name            text NOT NULL,
  species         text NOT NULL,
  species_other   text,
  breed           text,
  sex             text NOT NULL,
  neutered        text NOT NULL,
  birth_date      date,
  birth_date_approximate boolean,
  markings        text,
  microchip       text UNIQUE,
  microchip_corrections smallint NOT NULL DEFAULT 0,
  photo_object_key text, photo_thumb_key text, photo_content_type text,
  status          text NOT NULL CHECK (status IN ('active','deceased')),
  deceased_at     date,
  deceased_marked_at timestamptz,
  pending_transfer jsonb,                               -- {to_email_hash, to_email_masked, token_hash, expires_at}
  active_links_count int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL
);
CREATE INDEX pets_guardian_idx ON read_petregistry.pets (guardian_id, status, created_at DESC);
CREATE INDEX pets_name_trgm ON read_petregistry.pets USING gin (name gin_trgm_ops);

CREATE TABLE read_petregistry.pet_care_links (
  pet_id          uuid NOT NULL REFERENCES read_petregistry.pets(id),
  organization_id uuid NOT NULL,
  org_display_name text NOT NULL DEFAULT '',
  org_city        text,
  status          text NOT NULL CHECK (status IN ('active','revoked')),
  org_suspended   boolean NOT NULL DEFAULT false,      -- reacción a OrganizationSuspended
  granted_by      uuid NOT NULL,
  granted_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  PRIMARY KEY (pet_id, organization_id)
);
CREATE INDEX care_links_org_active_idx ON read_petregistry.pet_care_links (organization_id, granted_at DESC)
  WHERE status = 'active';

-- Unicidad global transaccional (ADR-002)
CREATE TABLE read_petregistry.microchip_reservations (
  microchip   text PRIMARY KEY,
  pet_id      uuid NOT NULL,
  reserved_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE read_petregistry.pet_transfers (          -- para resolver token → pet sin recorrer eventos
  token_hash  bytea PRIMARY KEY,
  pet_id      uuid NOT NULL,
  to_email_hash bytea NOT NULL,
  expires_at  timestamptz NOT NULL,
  status      text NOT NULL CHECK (status IN ('pending','accepted','cancelled','expired'))
);

-- +goose Down
DROP TABLE IF EXISTS read_petregistry.pet_transfers;
DROP TABLE IF EXISTS read_petregistry.microchip_reservations;
DROP TABLE IF EXISTS read_petregistry.pet_care_links;
DROP TABLE IF EXISTS read_petregistry.pets;
DROP SCHEMA IF EXISTS read_petregistry;
