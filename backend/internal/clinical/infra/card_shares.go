package infra

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ShareStore implementa app.Shares sobre read_clinical.record_shares
// (ARQ-013, estado operacional: no se reconstruye desde eventos).
type ShareStore struct{ pool *pgxpool.Pool }

var _ app.Shares = (*ShareStore)(nil)

// NewShareStore construye el almacén.
func NewShareStore(pool *pgxpool.Pool) *ShareStore { return &ShareStore{pool: pool} }

// ErrShareLimit es SHARE_LIMIT: ya hay MaxActiveShares enlaces activos.
var ErrShareLimit = kernel.NewDomainError("SHARE_LIMIT", "La mascota ya tiene el máximo de enlaces activos.")

// Create inserta el enlace; serializa por mascota para respetar el máximo.
func (s *ShareStore) Create(ctx context.Context, r app.ShareRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('share:' || $1::text, 0))`, r.PetID); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM read_clinical.record_shares
		WHERE pet_id = $1 AND revoked_at IS NULL AND expires_at > now()`, r.PetID).Scan(&active); err != nil {
		return err
	}
	if active >= app.MaxActiveShares {
		return ErrShareLimit
	}
	var pin any
	if r.PinHash != "" {
		pin = r.PinHash
	}
	if _, err := tx.Exec(ctx, `INSERT INTO read_clinical.record_shares
		(id, pet_id, scope, token_hash, pin_hash, expires_at, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.ID, r.PetID, r.Scope, r.TokenHash, pin, r.ExpiresAt, r.CreatedBy, r.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const shareColumns = `id, pet_id, scope, pin_hash IS NOT NULL, expires_at, created_at, revoked_at, access_count, last_access_at`

func scanShare(row pgx.Row) (app.ShareView, error) {
	var v app.ShareView
	err := row.Scan(&v.ID, &v.PetID, &v.Scope, &v.HasPIN, &v.ExpiresAt, &v.CreatedAt, &v.RevokedAt, &v.AccessCount, &v.LastAccess)
	return v, err
}

// List devuelve los enlaces no revocados de la mascota (sin token).
func (s *ShareStore) List(ctx context.Context, petID uuid.UUID) ([]app.ShareView, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+shareColumns+` FROM read_clinical.record_shares
		WHERE pet_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, petID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.ShareView{}
	for rows.Next() {
		v, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Revoke revoca el enlace (404 si no es de la mascota o ya estaba revocado).
func (s *ShareStore) Revoke(ctx context.Context, petID, id uuid.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE read_clinical.record_shares SET revoked_at = $3
		WHERE id = $1 AND pet_id = $2 AND revoked_at IS NULL`, id, petID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &kernel.NotFoundError{Resource: "share"}
	}
	return nil
}

// ByTokenHash busca por el hash SHA-256 del token (404 si no existe).
func (s *ShareStore) ByTokenHash(ctx context.Context, hash []byte) (app.ShareRecord, error) {
	var r app.ShareRecord
	var pin *string
	err := s.pool.QueryRow(ctx, `SELECT `+shareColumns+`, token_hash, pin_hash, failed_pin_attempts, locked_until
		FROM read_clinical.record_shares WHERE token_hash = $1`, hash).
		Scan(&r.ID, &r.PetID, &r.Scope, &r.HasPIN, &r.ExpiresAt, &r.CreatedAt, &r.RevokedAt, &r.AccessCount, &r.LastAccess,
			&r.TokenHash, &pin, &r.FailedPins, &r.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, &kernel.NotFoundError{Resource: "share"}
	}
	if pin != nil {
		r.PinHash = *pin
	}
	return r, err
}

// RecordAccess suma un acceso.
func (s *ShareStore) RecordAccess(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE read_clinical.record_shares
		SET access_count = access_count + 1, last_access_at = $2 WHERE id = $1`, id, at)
	return err
}

// PinFailed suma un intento fallido; al llegar a max bloquea hasta lockUntil.
func (s *ShareStore) PinFailed(ctx context.Context, id uuid.UUID, lockUntil time.Time, max int) (bool, error) {
	var locked bool
	err := s.pool.QueryRow(ctx, `UPDATE read_clinical.record_shares SET
		failed_pin_attempts = CASE WHEN failed_pin_attempts + 1 >= $3 THEN 0 ELSE failed_pin_attempts + 1 END,
		locked_until = CASE WHEN failed_pin_attempts + 1 >= $3 THEN $2::timestamptz ELSE locked_until END
		WHERE id = $1 RETURNING failed_pin_attempts = 0`, id, lockUntil, max).Scan(&locked)
	return locked, err
}

// PinOK reinicia el contador de intentos.
func (s *ShareStore) PinOK(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE read_clinical.record_shares SET failed_pin_attempts = 0 WHERE id = $1`, id)
	return err
}
