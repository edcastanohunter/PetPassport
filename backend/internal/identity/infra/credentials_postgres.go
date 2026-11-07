package infra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PostgresCredentialStore implementa app.CredentialStore.
type PostgresCredentialStore struct {
	pool *db.Pool
}

var _ app.CredentialStore = (*PostgresCredentialStore)(nil)

// NewPostgresCredentialStore crea una nueva instancia del almacén.
func NewPostgresCredentialStore(pool *db.Pool) *PostgresCredentialStore {
	return &PostgresCredentialStore{pool: pool}
}

// #nosec G101 -- esta constante SQL consulta la tabla professional_credentials, no contiene credenciales secretas.
const baseLicenseQuery = `
SELECT id, user_id, country, kind, registry_number, issuer, issued_at, expires_at,
       evidence_object_key, status, reviewed_by, reason, reviewed_at, created_at, updated_at
  FROM identity.professional_credentials`

func scanCredential(row pgx.Row) (*domain.ProfessionalCredential, error) {
	var (
		c                 domain.ProfessionalCredential
		issuedAt          time.Time
		expiresAt         *time.Time
		evidenceObjectKey string
		status            string
		reviewedBy        *uuid.UUID
		reason            *string
		reviewedAt        *time.Time
	)

	err := row.Scan(
		&c.ID,
		&c.UserID,
		&c.Country,
		&c.Kind,
		&c.RegistryNumber,
		&c.Issuer,
		&issuedAt,
		&expiresAt,
		&evidenceObjectKey,
		&status,
		&reviewedBy,
		&reason,
		&reviewedAt,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.Country = strings.TrimSpace(c.Country)
	c.IssuedAt = issuedAt
	c.ExpiresAt = expiresAt
	c.EvidenceObjectKey = evidenceObjectKey
	c.Status = domain.CredentialStatus(status)
	c.ReviewedBy = reviewedBy
	c.Reason = reason
	c.ReviewedAt = reviewedAt

	return &c, nil
}

func (s *PostgresCredentialStore) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.ProfessionalCredential, error) {
	rows, err := s.pool.Query(ctx, baseLicenseQuery+` WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()

	var result []domain.ProfessionalCredential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		result = append(result, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []domain.ProfessionalCredential{}
	}
	return result, nil
}

func (s *PostgresCredentialStore) GetByID(ctx context.Context, id uuid.UUID) (*domain.ProfessionalCredential, error) {
	row := s.pool.QueryRow(ctx, baseLicenseQuery+` WHERE id = $1`, id)
	c, err := scanCredential(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get credential by id: %w", err)
	}
	return c, nil
}

func (s *PostgresCredentialStore) GetVerifiedByUserAndCountry(ctx context.Context, userID uuid.UUID, country string) (*domain.ProfessionalCredential, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	row := s.pool.QueryRow(ctx, baseLicenseQuery+` WHERE user_id = $1 AND country = $2 AND status = 'verified'`, userID, country)
	c, err := scanCredential(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "verified_credential"}
	}
	if err != nil {
		return nil, fmt.Errorf("get verified credential: %w", err)
	}
	return c, nil
}

func (s *PostgresCredentialStore) Create(ctx context.Context, cred *domain.ProfessionalCredential) error {
	const insertSQL = `
INSERT INTO identity.professional_credentials (
  id, user_id, country, kind, registry_number, issuer, issued_at, expires_at,
  evidence_object_key, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := s.pool.Exec(
		ctx,
		insertSQL,
		cred.ID,
		cred.UserID,
		cred.Country,
		cred.Kind,
		cred.RegistryNumber,
		cred.Issuer,
		cred.IssuedAt,
		cred.ExpiresAt,
		cred.EvidenceObjectKey,
		string(cred.Status),
		cred.CreatedAt,
		cred.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrCredentialAlreadyActive
		}
		return fmt.Errorf("insert credential: %w", err)
	}
	return nil
}

func (s *PostgresCredentialStore) Update(ctx context.Context, cred *domain.ProfessionalCredential) error {
	const updateSQL = `
UPDATE identity.professional_credentials
   SET status = $2, reviewed_by = $3, reason = $4, reviewed_at = $5, updated_at = $6
 WHERE id = $1`

	tag, err := s.pool.Exec(
		ctx,
		updateSQL,
		cred.ID,
		string(cred.Status),
		cred.ReviewedBy,
		cred.Reason,
		cred.ReviewedAt,
		cred.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCredentialNotFound
	}
	return nil
}

func (s *PostgresCredentialStore) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	const deleteSQL = `DELETE FROM identity.professional_credentials WHERE id = $1 AND user_id = $2 AND status IN ('pending', 'rejected')`
	tag, err := s.pool.Exec(ctx, deleteSQL, id, userID)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCredentialNotFound
	}
	return nil
}

func (s *PostgresCredentialStore) ListByOrgMembers(ctx context.Context, memberUserIDs []uuid.UUID) ([]app.MemberCredentialSummary, error) {
	if len(memberUserIDs) == 0 {
		return []app.MemberCredentialSummary{}, nil
	}

	const query = `
SELECT user_id, country, kind, registry_number, issuer, status, expires_at
  FROM identity.professional_credentials
 WHERE user_id = ANY($1)
 ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, query, memberUserIDs)
	if err != nil {
		return nil, fmt.Errorf("list org members credentials: %w", err)
	}
	defer rows.Close()

	var result []app.MemberCredentialSummary
	for rows.Next() {
		var m app.MemberCredentialSummary
		if err := rows.Scan(&m.UserID, &m.Country, &m.Kind, &m.RegistryNumber, &m.Issuer, &m.Status, &m.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan member credential: %w", err)
		}
		m.Country = strings.TrimSpace(m.Country)
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []app.MemberCredentialSummary{}
	}
	return result, nil
}

func (s *PostgresCredentialStore) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]app.MemberCredentialSummary, error) {
	const query = `
SELECT c.user_id, c.country, c.kind, c.registry_number, c.issuer, c.status, c.expires_at
  FROM identity.professional_credentials c
  JOIN read_partnernetwork.organization_members m ON m.user_id = c.user_id
 WHERE m.organization_id = $1
 ORDER BY c.created_at DESC`

	rows, err := s.pool.Query(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("list org credentials: %w", err)
	}
	defer rows.Close()

	var result []app.MemberCredentialSummary
	for rows.Next() {
		var m app.MemberCredentialSummary
		var country string
		var exp *time.Time
		if err := rows.Scan(&m.UserID, &country, &m.Kind, &m.RegistryNumber, &m.Issuer, &m.Status, &exp); err != nil {
			return nil, fmt.Errorf("scan org credential: %w", err)
		}
		m.Country = strings.TrimSpace(country)
		m.ExpiresAt = exp
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []app.MemberCredentialSummary{}
	}
	return result, nil
}

func (s *PostgresCredentialStore) ListByStatus(ctx context.Context, status string) ([]domain.ProfessionalCredential, error) {
	var (
		rows pgx.Rows
		err  error
	)
	status = strings.TrimSpace(status)
	if status == "" {
		rows, err = s.pool.Query(ctx, baseLicenseQuery+` ORDER BY created_at DESC`)
	} else {
		rows, err = s.pool.Query(ctx, baseLicenseQuery+` WHERE status = $1 ORDER BY created_at DESC`, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list credentials by status: %w", err)
	}
	defer rows.Close()

	var result []domain.ProfessionalCredential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		result = append(result, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []domain.ProfessionalCredential{}
	}
	return result, nil
}
