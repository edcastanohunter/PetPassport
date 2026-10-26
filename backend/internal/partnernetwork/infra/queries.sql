-- Consultas del lado de lectura de Partner Network (ARQ-009). Las vistas
-- completas se usan tras evaluar authz con el recurso cargado; el lookup por
-- código devuelve el mínimo (nombre, ciudad, tipo) y solo clínicas activas.

-- name: GetOrganizationByID :one
SELECT id, version, type, legal_name, display_name, tax_country, tax_value, address, city, country,
       contact_email::text AS contact_email, contact_phone, website, public_code, status, status_reason,
       founder_id, member_count, created_at, updated_at
  FROM read_partnernetwork.organizations
 WHERE id = $1;

-- name: GetOrgSummaryByCode :one
SELECT id, type, display_name, city, country, status
  FROM read_partnernetwork.organizations
 WHERE public_code = upper(sqlc.arg('code')::text);

-- name: ListActiveMembers :many
SELECT user_id, role, status, joined_at
  FROM read_partnernetwork.org_members
 WHERE organization_id = $1 AND status = 'active'
 ORDER BY joined_at, user_id;

-- name: ListMembershipsOfUser :many
SELECT m.organization_id, o.display_name, o.type, m.role, o.status AS org_status
  FROM read_partnernetwork.org_members m
  JOIN read_partnernetwork.organizations o ON o.id = m.organization_id
 WHERE m.user_id = $1 AND m.status = 'active'
 ORDER BY o.display_name;

-- name: ListInvitations :many
-- status derivado: una pendiente vencida se informa como expired.
SELECT id, organization_id, email::text AS email, role,
       (CASE WHEN status = 'pending' AND expires_at <= sqlc.arg('now')::timestamptz THEN 'expired' ELSE status END)::text AS status,
       expires_at, created_at
  FROM read_partnernetwork.invitations
 WHERE organization_id = sqlc.arg('organization_id')
   AND (sqlc.narg('status')::text IS NULL
        OR (CASE WHEN status = 'pending' AND expires_at <= sqlc.arg('now')::timestamptz THEN 'expired' ELSE status END) = sqlc.narg('status')::text)
 ORDER BY created_at DESC
 LIMIT 200;

-- name: GetPendingInvitationByToken :one
SELECT id, organization_id, email::text AS email, role, status, expires_at, created_at
  FROM read_partnernetwork.invitations
 WHERE token_hash = sqlc.arg('token_hash') AND status = 'pending' AND expires_at > sqlc.arg('now')::timestamptz;

-- name: ReservePublicCode :execrows
INSERT INTO read_partnernetwork.public_code_reservations (public_code, organization_id)
VALUES (upper(sqlc.arg('code')::text), sqlc.arg('organization_id'))
ON CONFLICT (public_code) DO NOTHING;

-- name: GetPublicCodeOwner :one
SELECT organization_id FROM read_partnernetwork.public_code_reservations
 WHERE public_code = upper(sqlc.arg('code')::text);

-- name: RotateInvitationToken :one
-- Reenvío (API-007): sustituye el hash y cuenta el reenvío en una ventana
-- de 24 h; 0 filas = no existe, no está pendiente/vigente o se agotó el cupo.
UPDATE read_partnernetwork.invitations SET
  token_hash = sqlc.arg('token_hash'),
  last_sent_at = sqlc.arg('now')::timestamptz,
  resend_count = CASE WHEN resend_window_start IS NULL OR resend_window_start <= sqlc.arg('now')::timestamptz - interval '24 hours'
                      THEN 1 ELSE resend_count + 1 END,
  resend_window_start = CASE WHEN resend_window_start IS NULL OR resend_window_start <= sqlc.arg('now')::timestamptz - interval '24 hours'
                             THEN sqlc.arg('now')::timestamptz ELSE resend_window_start END
 WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
   AND status = 'pending' AND expires_at > sqlc.arg('now')::timestamptz
   AND (resend_window_start IS NULL OR resend_window_start <= sqlc.arg('now')::timestamptz - interval '24 hours'
        OR resend_count < sqlc.arg('max_resends')::int)
RETURNING email::text AS email, role;

-- name: InvitationIsPending :one
SELECT EXISTS (
  SELECT 1 FROM read_partnernetwork.invitations
   WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
     AND status = 'pending' AND expires_at > sqlc.arg('now')::timestamptz
) AS ok;

-- name: AdminListOrganizations :many
-- Panel admin (PRD-017, API-008): organizaciones por estado, de la más
-- antigua a la más reciente (cola de verificación FIFO). q busca por nombre,
-- razón social o código público (ILIKE escapado por el adaptador). Cursor por
-- (created_at, id) ascendente.
SELECT id, version, type, legal_name, display_name, tax_country, tax_value, address, city, country,
       contact_email::text AS contact_email, contact_phone, website, public_code, status, status_reason,
       founder_id, member_count, created_at, updated_at
  FROM read_partnernetwork.organizations
 WHERE status = sqlc.arg('status')::text
   AND (sqlc.narg('q')::text IS NULL
        OR display_name ILIKE '%' || sqlc.narg('q')::text || '%'
        OR legal_name ILIKE '%' || sqlc.narg('q')::text || '%'
        OR public_code = upper(sqlc.narg('q')::text))
   AND (sqlc.narg('created_at')::timestamptz IS NULL
        OR (created_at, id) > (sqlc.narg('created_at')::timestamptz, sqlc.narg('id')::uuid))
 ORDER BY created_at, id
 LIMIT sqlc.arg('limit');
