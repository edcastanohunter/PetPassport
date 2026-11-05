-- +goose Up
-- Medications catalog (PP-F3-DAT-002, ADR-016).
-- ref.medications, ref.medication_suggestions, ref.medication_import_batches,
-- ref.medication_import_rows, ref.medications_changes.

CREATE TABLE IF NOT EXISTS ref.medications (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  name_norm text NOT NULL,
  active_ingredients jsonb NOT NULL DEFAULT '[]',
  form text NOT NULL,
  concentration text,
  presentation text,
  routes text[] NOT NULL DEFAULT '{}',
  species text[] NOT NULL DEFAULT '{}',
  manufacturer text,
  manufacturer_norm text,
  atcvet_code text,
  regulatory jsonb,
  controlled boolean NOT NULL DEFAULT false,
  prescription_required boolean NOT NULL DEFAULT true,
  default_dose_unit text,
  default_route text,
  synonyms text[] NOT NULL DEFAULT '{}',
  status text NOT NULL CHECK (status IN ('active','discontinued','merged')),
  merged_into uuid REFERENCES ref.medications(id),
  source jsonb NOT NULL,
  usage_count int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS medications_identity_uq ON ref.medications (name_norm, coalesce(manufacturer_norm,''), form, coalesce(concentration,'')) WHERE status <> 'merged';
CREATE INDEX IF NOT EXISTS medications_name_trgm ON ref.medications USING gin (name_norm gin_trgm_ops);
CREATE INDEX IF NOT EXISTS medications_species_idx ON ref.medications USING gin (species);
CREATE INDEX IF NOT EXISTS medications_status_idx ON ref.medications (status);

CREATE TABLE IF NOT EXISTS ref.medication_suggestions (
  id uuid PRIMARY KEY,
  proposed_by uuid NOT NULL,
  organization_id uuid NOT NULL,
  name text NOT NULL,
  notes text,
  photo_object_key text,
  status text NOT NULL CHECK (status IN ('pending','approved','rejected')),
  reviewed_by uuid,
  reason text,
  medication_id uuid REFERENCES ref.medications(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  reviewed_at timestamptz
);

CREATE INDEX IF NOT EXISTS medication_suggestions_org_idx ON ref.medication_suggestions (organization_id);
CREATE INDEX IF NOT EXISTS medication_suggestions_status_idx ON ref.medication_suggestions (status);

CREATE TABLE IF NOT EXISTS ref.medication_import_batches (
  id uuid PRIMARY KEY,
  source text NOT NULL,
  status text NOT NULL,
  dry_run boolean NOT NULL,
  created int NOT NULL DEFAULT 0,
  updated int NOT NULL DEFAULT 0,
  skipped int NOT NULL DEFAULT 0,
  errors jsonb,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  reverted_at timestamptz
);

CREATE TABLE IF NOT EXISTS ref.medication_import_rows (
  batch_id uuid NOT NULL REFERENCES ref.medication_import_batches(id),
  medication_id uuid NOT NULL,
  action text NOT NULL,
  previous jsonb,
  PRIMARY KEY (batch_id, medication_id)
);

CREATE TABLE IF NOT EXISTS ref.medications_changes (
  seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  medication_id uuid NOT NULL,
  changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS medications_changes_seq_idx ON ref.medications_changes (seq);

GRANT SELECT, INSERT, UPDATE, DELETE ON ref.medications, ref.medication_suggestions, ref.medication_import_batches, ref.medication_import_rows, ref.medications_changes TO pp_app_writer;
GRANT SELECT ON ref.medications, ref.medication_suggestions, ref.medication_import_batches, ref.medication_import_rows, ref.medications_changes TO pp_app_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA ref TO pp_app_writer;

-- +goose Down
DROP TABLE IF EXISTS ref.medications_changes;
DROP TABLE IF EXISTS ref.medication_import_rows;
DROP TABLE IF EXISTS ref.medication_import_batches;
DROP TABLE IF EXISTS ref.medication_suggestions;
DROP TABLE IF EXISTS ref.medications;
