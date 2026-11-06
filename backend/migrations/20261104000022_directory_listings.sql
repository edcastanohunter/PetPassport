-- +goose Up
-- Directorio público de organizaciones (PP-F3-DOM-010, PP-F3-API-005, PP-F3-SEC-008, PP-F3-PRD-006)

ALTER TABLE read_partnernetwork.organizations
  ADD COLUMN IF NOT EXISTS directory_listed boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS directory_services text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS directory_show_phone boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS directory_neighborhood text,
  ADD COLUMN IF NOT EXISTS directory_geo jsonb;

CREATE INDEX IF NOT EXISTS organizations_directory_idx
  ON read_partnernetwork.organizations (directory_listed, city, type)
  WHERE directory_listed = true;

-- +goose Down
DROP INDEX IF EXISTS read_partnernetwork.organizations_directory_idx;
ALTER TABLE read_partnernetwork.organizations
  DROP COLUMN IF EXISTS directory_geo,
  DROP COLUMN IF EXISTS directory_neighborhood,
  DROP COLUMN IF EXISTS directory_show_phone,
  DROP COLUMN IF EXISTS directory_services,
  DROP COLUMN IF EXISTS directory_listed;
