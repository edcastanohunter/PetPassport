// Package infra contiene los adaptadores de Identity: repositorio PostgreSQL
// sobre identity.user_accounts / identity.consent_versions (PP-F1-DAT-004).
// Consultas parametrizadas con pgx; sqlc se introduce en M1.2.
package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PostgresRepository implementa app.Repository.
type PostgresRepository struct {
	pool *db.Pool
}

var _ app.Repository = (*PostgresRepository)(nil)

// NewPostgresRepository construye el repositorio.
func NewPostgresRepository(pool *db.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const selectAccount = `
SELECT id, email::text, email_verified, display_name, locale, country, phone, address, emergency_contact,
       status, COALESCE(status_reason, ''), global_roles, terms_version, privacy_version, consent_accepted_at,
       onboarding_completed_at, created_at, updated_at, last_login_at
  FROM identity.user_accounts WHERE id = $1`

// Get devuelve la cuenta o *kernel.NotFoundError.
func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (*domain.UserAccount, error) {
	row := r.pool.QueryRow(ctx, selectAccount, id)
	acc, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "user_account"}
	}
	if err != nil {
		return nil, fmt.Errorf("identity get: %w", err)
	}
	return acc, nil
}

// Insert crea la cuenta; app.ErrAlreadyExists si la PK o el email ya existen.
func (r *PostgresRepository) Insert(ctx context.Context, a *domain.UserAccount) error {
	addr, contact, err := encodeJSON(a)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO identity.user_accounts
		  (id, email, email_verified, display_name, locale, country, phone, address, emergency_contact,
		   status, status_reason, global_roles, terms_version, privacy_version, consent_accepted_at,
		   onboarding_completed_at, created_at, updated_at, last_login_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14,$15,$16,$17,$18,$19)`,
		a.ID, string(a.Email), a.EmailVerified, a.DisplayName, string(a.Locale), countryPtr(a.Country), phonePtr(a.Phone),
		addr, contact, string(a.Status), a.StatusReason, a.GlobalRoles, consentTerms(a.Consent), consentPrivacy(a.Consent),
		consentAt(a.Consent), a.OnboardingCompletedAt, a.CreatedAt, a.UpdatedAt, a.LastLoginAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return app.ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("identity insert: %w", err)
	}
	return nil
}

// Save persiste el estado completo y la auditoría en una transacción.
func (r *PostgresRepository) Save(ctx context.Context, a *domain.UserAccount, entry *audit.Entry) error {
	addr, contact, err := encodeJSON(a)
	if err != nil {
		return err
	}
	return r.pool.Do(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE identity.user_accounts SET
			  email=$2, email_verified=$3, display_name=$4, locale=$5, country=$6, phone=$7, address=$8,
			  emergency_contact=$9, status=$10, status_reason=NULLIF($11,''), global_roles=$12, terms_version=$13,
			  privacy_version=$14, consent_accepted_at=$15, onboarding_completed_at=$16, updated_at=$17, last_login_at=$18
			WHERE id=$1`,
			a.ID, string(a.Email), a.EmailVerified, a.DisplayName, string(a.Locale), countryPtr(a.Country), phonePtr(a.Phone),
			addr, contact, string(a.Status), a.StatusReason, a.GlobalRoles, consentTerms(a.Consent), consentPrivacy(a.Consent),
			consentAt(a.Consent), a.OnboardingCompletedAt, a.UpdatedAt, a.LastLoginAt)
		if err != nil {
			return fmt.Errorf("identity save: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return &kernel.NotFoundError{Resource: "user_account"}
		}
		if entry != nil {
			return audit.Record(ctx, tx, *entry)
		}
		return nil
	})
}

// CurrentConsent devuelve la versión vigente (mayor effective_from ≤ hoy) por tipo.
func (r *PostgresRepository) CurrentConsent(ctx context.Context) (domain.ConsentVersions, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (kind) kind, version, url FROM identity.consent_versions
		 WHERE effective_from <= CURRENT_DATE ORDER BY kind, effective_from DESC, version DESC`)
	if err != nil {
		return domain.ConsentVersions{}, fmt.Errorf("consent versions: %w", err)
	}
	defer rows.Close()
	var cur domain.ConsentVersions
	for rows.Next() {
		var kind, version, url string
		if err := rows.Scan(&kind, &version, &url); err != nil {
			return domain.ConsentVersions{}, err
		}
		switch kind {
		case "terms":
			cur.Terms, cur.TermsURL = version, url
		case "privacy":
			cur.Privacy, cur.PrivacyURL = version, url
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ConsentVersions{}, err
	}
	if cur.Terms == "" || cur.Privacy == "" {
		return domain.ConsentVersions{}, errors.New("consent versions: sin versiones vigentes")
	}
	return cur, nil
}

func scanAccount(row pgx.Row) (*domain.UserAccount, error) {
	var (
		a                                 domain.UserAccount
		email, locale, status             string
		country, phone                    *string
		addr, contact                     []byte
		termsVersion, privacyVersion      *string
		consentAt, onboardedAt, lastLogin *time.Time
		createdAt, updatedAt              time.Time
	)
	err := row.Scan(&a.ID, &email, &a.EmailVerified, &a.DisplayName, &locale, &country, &phone, &addr, &contact,
		&status, &a.StatusReason, &a.GlobalRoles, &termsVersion, &privacyVersion, &consentAt,
		&onboardedAt, &createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	a.Email = kernel.Email(email)
	a.Locale = kernel.Locale(locale)
	a.Status = domain.AccountStatus(status)
	if country != nil {
		c := kernel.CountryCode(*country)
		a.Country = &c
	}
	if phone != nil {
		p := kernel.Phone(*phone)
		a.Phone = &p
	}
	if len(addr) > 0 {
		var v kernel.Address
		if err := json.Unmarshal(addr, &v); err != nil {
			return nil, fmt.Errorf("address json: %w", err)
		}
		a.Address = &v
	}
	if len(contact) > 0 {
		var v kernel.Contact
		if err := json.Unmarshal(contact, &v); err != nil {
			return nil, fmt.Errorf("contact json: %w", err)
		}
		a.EmergencyContact = &v
	}
	if termsVersion != nil && privacyVersion != nil && consentAt != nil {
		a.Consent = &domain.Consent{TermsVersion: *termsVersion, PrivacyVersion: *privacyVersion, AcceptedAt: *consentAt}
	}
	a.OnboardingCompletedAt = onboardedAt
	a.CreatedAt, a.UpdatedAt, a.LastLoginAt = createdAt, updatedAt, lastLogin
	if a.GlobalRoles == nil {
		a.GlobalRoles = []string{}
	}
	return &a, nil
}

func encodeJSON(a *domain.UserAccount) (addr, contact []byte, err error) {
	if a.Address != nil {
		if addr, err = json.Marshal(a.Address); err != nil {
			return nil, nil, fmt.Errorf("address json: %w", err)
		}
	}
	if a.EmergencyContact != nil {
		if contact, err = json.Marshal(a.EmergencyContact); err != nil {
			return nil, nil, fmt.Errorf("contact json: %w", err)
		}
	}
	return addr, contact, nil
}

func countryPtr(c *kernel.CountryCode) *string {
	if c == nil {
		return nil
	}
	s := string(*c)
	return &s
}

func phonePtr(p *kernel.Phone) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

func consentTerms(c *domain.Consent) *string {
	if c == nil {
		return nil
	}
	return &c.TermsVersion
}

func consentPrivacy(c *domain.Consent) *string {
	if c == nil {
		return nil
	}
	return &c.PrivacyVersion
}

func consentAt(c *domain.Consent) *time.Time {
	if c == nil {
		return nil
	}
	return &c.AcceptedAt
}
