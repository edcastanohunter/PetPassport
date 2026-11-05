-- +goose Up
-- Pacientes provisionales y reclamación (PP-F3-DAT-005, PP-F3-DOM-007, PP-F3-PRD-012)
ALTER TABLE read_petregistry.pets
  ADD COLUMN provisional boolean NOT NULL DEFAULT false,
  ADD COLUMN provisional_org_id uuid,
  ADD COLUMN merged_into uuid;

ALTER TABLE read_petregistry.pets ALTER COLUMN guardian_id DROP NOT NULL;

CREATE TABLE read_petregistry.pet_claims (
  token_hash      bytea PRIMARY KEY,
  pet_id          uuid NOT NULL,
  organization_id uuid NOT NULL,
  contact_hash    bytea NOT NULL,
  contact_kind    text NOT NULL,
  status          text NOT NULL CHECK (status IN ('pending','accepted','expired','cancelled')),
  expires_at      timestamptz NOT NULL,
  resend_count    int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pet_claims_pet_idx ON read_petregistry.pet_claims (pet_id, status);

CREATE TABLE read_petregistry.pet_aliases (
  old_pet_id uuid PRIMARY KEY,
  pet_id     uuid NOT NULL,
  merged_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pet_aliases_target_idx ON read_petregistry.pet_aliases (pet_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON read_petregistry.pet_claims TO pp_app_writer;
GRANT SELECT ON read_petregistry.pet_claims TO pp_app_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON read_petregistry.pet_aliases TO pp_app_writer;
GRANT SELECT ON read_petregistry.pet_aliases TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS read_petregistry.pet_aliases;
DROP TABLE IF EXISTS read_petregistry.pet_claims;
ALTER TABLE read_petregistry.pets
  DROP COLUMN IF EXISTS merged_into,
  DROP COLUMN IF EXISTS provisional_org_id,
  DROP COLUMN IF EXISTS provisional;
ALTER TABLE read_petregistry.pets ALTER COLUMN guardian_id SET NOT NULL;
