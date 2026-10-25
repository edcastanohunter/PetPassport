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
