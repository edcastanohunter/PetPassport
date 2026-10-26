-- Consultas del lado de lectura de Pet Registry (ARQ-009). Nunca leen
-- es.events; siempre filtran por principal (guardian_id = actor) salvo
-- GetPetByID, usado tras la evaluación de authz con el recurso cargado.

-- name: ListPetsByGuardian :many
SELECT id, version, guardian_id, name, species, species_other, breed, sex, neutered,
       birth_date, birth_date_approximate, markings, microchip,
       photo_object_key, photo_thumb_key, photo_content_type, status,
       deceased_at, active_links_count, created_at, updated_at
  FROM read_petregistry.pets
 WHERE guardian_id = sqlc.arg('guardian_id')
   AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
   AND (sqlc.narg('created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('created_at'), sqlc.narg('id')::uuid))
 ORDER BY created_at DESC, id DESC
 LIMIT sqlc.arg('limit');

-- name: GetPetByID :one
SELECT id, version, guardian_id, guardian_display_name, name, species, species_other,
       breed, sex, neutered, birth_date, birth_date_approximate, markings, microchip,
       microchip_corrections, photo_object_key, photo_thumb_key, photo_content_type,
       status, deceased_at, deceased_marked_at, pending_transfer, active_links_count,
       created_at, updated_at
  FROM read_petregistry.pets
 WHERE id = $1;

-- name: ListCareLinks :many
SELECT pet_id, organization_id, org_display_name, org_city, status, org_suspended,
       granted_at, revoked_at
  FROM read_petregistry.pet_care_links
 WHERE pet_id = $1
 ORDER BY granted_at DESC;

-- name: ReserveMicrochip :execrows
INSERT INTO read_petregistry.microchip_reservations (microchip, pet_id)
VALUES ($1, $2)
ON CONFLICT (microchip) DO NOTHING;

-- name: GetMicrochipReservation :one
SELECT microchip, pet_id FROM read_petregistry.microchip_reservations
 WHERE microchip = $1;

-- name: ReleaseMicrochip :exec
DELETE FROM read_petregistry.microchip_reservations
 WHERE microchip = $1 AND pet_id = $2;

-- name: UpsertTransfer :exec
INSERT INTO read_petregistry.pet_transfers (token_hash, pet_id, to_email_hash, expires_at, status)
VALUES ($1, $2, $3, $4, 'pending')
ON CONFLICT (token_hash) DO UPDATE SET
  pet_id = EXCLUDED.pet_id, to_email_hash = EXCLUDED.to_email_hash,
  expires_at = EXCLUDED.expires_at, status = 'pending';

-- name: GetTransferByToken :one
SELECT token_hash, pet_id, to_email_hash, expires_at, status
  FROM read_petregistry.pet_transfers
 WHERE token_hash = $1;

-- name: GetPendingTransferByToken :one
SELECT pet_id, expires_at
  FROM read_petregistry.pet_transfers
 WHERE token_hash = sqlc.arg('token_hash') AND status = 'pending' AND expires_at > sqlc.arg('now')::timestamptz;

-- name: ListPatients :many
-- Pacientes de una clínica (PRD-016): vínculos activos de org no suspendida.
-- q busca por nombre de mascota o de tutor (ILIKE con trigram). Cursor por
-- (granted_at, pet_id) descendente.
SELECT p.id, p.name, p.species, p.breed, p.status, p.birth_date, p.birth_date_approximate,
       p.photo_object_key, p.photo_thumb_key, p.photo_content_type,
       p.guardian_id, p.guardian_display_name, l.granted_at
  FROM read_petregistry.pet_care_links l
  JOIN read_petregistry.pets p ON p.id = l.pet_id
 WHERE l.organization_id = sqlc.arg('organization_id')
   AND l.status = 'active' AND NOT l.org_suspended
   AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status')::text)
   AND (sqlc.narg('q')::text IS NULL
        OR p.name ILIKE '%' || sqlc.narg('q')::text || '%'
        OR p.guardian_display_name ILIKE '%' || sqlc.narg('q')::text || '%')
   AND (sqlc.narg('granted_at')::timestamptz IS NULL
        OR (l.granted_at, l.pet_id) < (sqlc.narg('granted_at')::timestamptz, sqlc.narg('pet_id')::uuid))
 ORDER BY l.granted_at DESC, l.pet_id DESC
 LIMIT sqlc.arg('limit');
