-- +goose Up
-- Invitaciones de organizaciones a una cuenta de facturación (PP-F5-PRD-008):
-- la cuenta invita, el administrador de la organización invitada acepta o
-- rechaza. Aceptar vincula la organización (OrganizationAttached).

CREATE TABLE billing.org_invitations (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES billing.accounts(id),
  organization_id uuid NOT NULL,
  seat_pool text NOT NULL DEFAULT 'own' CHECK (seat_pool IN ('own','shared')),
  status text NOT NULL CHECK (status IN ('pending','accepted','declined','revoked')),
  invited_by uuid NOT NULL,
  created_at timestamptz NOT NULL,
  resolved_at timestamptz,
  resolved_by uuid
);
-- Una sola invitación pendiente por cuenta y organización.
CREATE UNIQUE INDEX org_invitations_pending_idx ON billing.org_invitations (account_id, organization_id) WHERE status = 'pending';
CREATE INDEX org_invitations_org_idx ON billing.org_invitations (organization_id, status);

GRANT SELECT, INSERT, UPDATE ON billing.org_invitations TO pp_app_writer;
GRANT SELECT ON billing.org_invitations TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS billing.org_invitations;
