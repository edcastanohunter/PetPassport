-- +goose Up
-- rebuild: clinical.vaccination_due
-- Clinical: carnet de vacunación (PP-F2-DAT-003). `card_entries` es proyección
-- síncrona (misma tx que el Append); `vaccination_due` es asíncrona (ARQ-012);
-- `ref.*` son catálogos CRUD auditados (no ES) y `record_shares` el estado
-- operacional de los enlaces compartidos (ARQ-013, no se reconstruye).

CREATE TABLE read_clinical.card_entries (
  id               uuid PRIMARY KEY,
  pet_id           uuid NOT NULL,
  kind             text NOT NULL CHECK (kind IN ('vaccine','deworming','ectoparasite')),
  catalog_id       uuid, product_name text NOT NULL,
  administered_at  date NOT NULL,
  lot text, manufacturer text, expiry_date date, route text, site text, dose_label text, weight_kg numeric(6,2),
  source           text NOT NULL CHECK (source IN ('recorded','declared','imported')),
  verification     text NOT NULL CHECK (verification IN ('verified','unverified','disputed')),
  recorded_by_user uuid, recorded_by_org uuid, declared_by uuid,
  recorded_at      timestamptz,
  encounter_id     uuid,
  next_due_date    date, next_due_basis text,
  status           text NOT NULL CHECK (status IN ('active','voided')),
  void_reason      text,
  evidence_object_key text,
  version          bigint NOT NULL,
  created_at       timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX card_entries_pet_idx ON read_clinical.card_entries (pet_id, administered_at DESC);
CREATE UNIQUE INDEX card_entries_dup_uq ON read_clinical.card_entries (pet_id, kind, lower(product_name), administered_at) WHERE status = 'active';

-- Especie copiada al carnet (CardSpeciesSynced) y versión del stream.
CREATE TABLE read_clinical.cards (
  pet_id uuid PRIMARY KEY, species text NOT NULL, version bigint NOT NULL, updated_at timestamptz NOT NULL
);

CREATE TABLE read_clinical.vaccination_due (
  pet_id uuid NOT NULL, entry_id uuid NOT NULL, kind text NOT NULL, product_name text NOT NULL,
  due_date date NOT NULL, basis text NOT NULL,
  status text NOT NULL CHECK (status IN ('upcoming','overdue','satisfied','cancelled')),
  PRIMARY KEY (pet_id, entry_id)
);
CREATE INDEX vaccination_due_date_idx ON read_clinical.vaccination_due (due_date) WHERE status IN ('upcoming','overdue');

-- Catálogos (CRUD, auditados). Pertenecen a Clinical Records.
CREATE SCHEMA IF NOT EXISTS ref;
CREATE TABLE ref.vaccines (
  id uuid PRIMARY KEY, name text NOT NULL, species text[] NOT NULL, diseases text[] NOT NULL,
  type text NOT NULL CHECK (type IN ('core','non_core')), routes text[] NOT NULL,
  active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX vaccines_name_uq ON ref.vaccines (lower(name));
CREATE TABLE ref.vaccination_protocols (
  id uuid PRIMARY KEY, country char(2) NOT NULL, species text NOT NULL,
  vaccine_id uuid NOT NULL REFERENCES ref.vaccines(id),
  initial_series jsonb NOT NULL,          -- [{min_age_weeks, min_interval_days}]
  booster_interval_months int, legal_validity_months int, notes text,
  active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (country, species, vaccine_id)
);
CREATE TABLE ref.preventive_protocols (
  id uuid PRIMARY KEY, country char(2) NOT NULL, species text NOT NULL, kind text NOT NULL,
  interval_days int NOT NULL, notes text, active boolean NOT NULL DEFAULT true,
  UNIQUE (country, species, kind, interval_days)
);
CREATE TABLE ref.diagnosis_codes (system text NOT NULL, code text NOT NULL, label text NOT NULL, species text[], PRIMARY KEY (system, code));

CREATE TABLE read_clinical.record_shares (
  id uuid PRIMARY KEY, pet_id uuid NOT NULL, scope text NOT NULL CHECK (scope IN ('vaccination_card','full_record')),
  token_hash bytea NOT NULL UNIQUE, pin_hash text, expires_at timestamptz NOT NULL,
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
  access_count int NOT NULL DEFAULT 0, last_access_at timestamptz,
  failed_pin_attempts int NOT NULL DEFAULT 0, locked_until timestamptz
);
CREATE INDEX record_shares_pet_idx ON read_clinical.record_shares (pet_id) WHERE revoked_at IS NULL;

GRANT USAGE ON SCHEMA ref TO pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT, UPDATE, DELETE ON read_clinical.card_entries, read_clinical.cards,
  read_clinical.vaccination_due, read_clinical.record_shares TO pp_app_writer;
GRANT SELECT ON read_clinical.card_entries, read_clinical.cards, read_clinical.vaccination_due TO pp_app_reader;
-- ref.*: la API admin (CRUD auditado) escribe con pp_app_writer (DAT-003).
GRANT SELECT, INSERT, UPDATE, DELETE ON ref.vaccines, ref.vaccination_protocols,
  ref.preventive_protocols, ref.diagnosis_codes TO pp_app_writer;
GRANT SELECT ON ref.vaccines, ref.vaccination_protocols, ref.preventive_protocols, ref.diagnosis_codes TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS read_clinical.record_shares;
DROP TABLE IF EXISTS ref.diagnosis_codes;
DROP TABLE IF EXISTS ref.preventive_protocols;
DROP TABLE IF EXISTS ref.vaccination_protocols;
DROP TABLE IF EXISTS ref.vaccines;
DROP SCHEMA IF EXISTS ref;
DROP TABLE IF EXISTS read_clinical.vaccination_due;
DROP TABLE IF EXISTS read_clinical.cards;
DROP TABLE IF EXISTS read_clinical.card_entries;
