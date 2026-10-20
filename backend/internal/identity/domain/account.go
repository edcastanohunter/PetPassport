// Package domain contiene el modelo de Identity & Access: UserAccount (CRUD, no
// event-sourced, PP-F1-DOM-009) con ACL sobre Keycloak (ADR-003). Keycloak
// autentica; el estado de negocio de la cuenta (onboarding, consentimiento,
// suspensión, perfil) vive aquí.
package domain

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AccountStatus es el estado de la cuenta en la plataforma.
type AccountStatus string

const (
	StatusActive    AccountStatus = "active"
	StatusSuspended AccountStatus = "suspended"
)

// MaxDisplayName es la longitud máxima del nombre para mostrar.
const MaxDisplayName = 120

// ErrConsentOutdated: las versiones aceptadas no son las vigentes (API-011).
var ErrConsentOutdated = kernel.NewDomainError("CONSENT_VERSION_OUTDATED", "Las versiones aceptadas no son las vigentes.")

// ErrAccountSuspended: la cuenta está suspendida.
var ErrAccountSuspended = kernel.NewDomainError("ACCOUNT_SUSPENDED", "La cuenta está suspendida.")

// Consent registra las versiones aceptadas y cuándo.
type Consent struct {
	TermsVersion   string
	PrivacyVersion string
	AcceptedAt     time.Time
}

// ConsentVersions son las versiones vigentes (identity.consent_versions).
type ConsentVersions struct {
	Terms      string
	Privacy    string
	TermsURL   string
	PrivacyURL string
}

// Matches indica si el consentimiento dado cubre las versiones vigentes.
func (v ConsentVersions) Matches(c *Consent) bool {
	return c != nil && c.TermsVersion == v.Terms && c.PrivacyVersion == v.Privacy
}

// UserAccount es la cuenta de la plataforma; ID == sub de Keycloak.
type UserAccount struct {
	ID                    uuid.UUID
	Email                 kernel.Email
	EmailVerified         bool
	DisplayName           string
	Locale                kernel.Locale
	Country               *kernel.CountryCode
	Phone                 *kernel.Phone
	Address               *kernel.Address
	EmergencyContact      *kernel.Contact
	Status                AccountStatus
	StatusReason          string
	GlobalRoles           []string
	Consent               *Consent
	OnboardingCompletedAt *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	LastLoginAt           *time.Time
}

// TokenSnapshot son los datos del token que la cuenta refleja (ACL Keycloak).
type TokenSnapshot struct {
	Email         kernel.Email
	EmailVerified bool
	DisplayName   string
	Locale        kernel.Locale
	Roles         []string
}

// Provision crea la cuenta a partir del primer token visto (JIT, IAM-009).
func Provision(id uuid.UUID, snap TokenSnapshot, now time.Time) *UserAccount {
	locale := snap.Locale
	if locale == "" {
		locale = kernel.LocaleES
	}
	return &UserAccount{
		ID: id, Email: snap.Email, EmailVerified: snap.EmailVerified,
		DisplayName: truncate(snap.DisplayName, MaxDisplayName), Locale: locale,
		Status: StatusActive, GlobalRoles: normalizeRoles(snap.Roles),
		CreatedAt: now, UpdatedAt: now, LastLoginAt: &now,
	}
}

// SyncFromToken refleja en la cuenta lo que el token afirma (email, verificación,
// roles). El nombre solo se toma del token si la cuenta aún no tiene uno.
// Devuelve true si algo cambió.
func (a *UserAccount) SyncFromToken(snap TokenSnapshot, now time.Time) bool {
	changed := false
	if snap.Email != "" && a.Email != snap.Email {
		a.Email = snap.Email
		changed = true
	}
	if a.EmailVerified != snap.EmailVerified {
		a.EmailVerified = snap.EmailVerified
		changed = true
	}
	if roles := normalizeRoles(snap.Roles); !slices.Equal(a.GlobalRoles, roles) {
		a.GlobalRoles = roles
		changed = true
	}
	if a.DisplayName == "" && snap.DisplayName != "" {
		a.DisplayName = truncate(snap.DisplayName, MaxDisplayName)
		changed = true
	}
	if changed {
		a.UpdatedAt = now
	}
	return changed
}

// TouchLogin actualiza last_login_at.
func (a *UserAccount) TouchLogin(now time.Time) { a.LastLoginAt = &now }

// IsSuspended indica si la cuenta está suspendida.
func (a *UserAccount) IsSuspended() bool { return a.Status == StatusSuspended }

// IsOnboarded indica si completó el onboarding.
func (a *UserAccount) IsOnboarded() bool { return a.OnboardingCompletedAt != nil }

// ConsentCurrent indica si el consentimiento cubre las versiones vigentes.
func (a *UserAccount) ConsentCurrent(cur ConsentVersions) bool { return cur.Matches(a.Consent) }

// OnboardingData son los datos básicos del onboarding (PRD-003).
type OnboardingData struct {
	DisplayName    string
	Country        kernel.CountryCode
	Locale         kernel.Locale
	Phone          *kernel.Phone
	TermsVersion   string
	PrivacyVersion string
}

// CompleteOnboarding acepta el consentimiento vigente y registra los datos
// básicos. Es idempotente: repetirlo actualiza datos y consentimiento.
func (a *UserAccount) CompleteOnboarding(d OnboardingData, cur ConsentVersions, now time.Time) error {
	if a.IsSuspended() {
		return ErrAccountSuspended
	}
	if err := a.AcceptConsent(d.TermsVersion, d.PrivacyVersion, cur, now); err != nil {
		return err
	}
	a.DisplayName = d.DisplayName
	country := d.Country
	a.Country = &country
	a.Locale = d.Locale
	a.Phone = d.Phone
	if a.OnboardingCompletedAt == nil {
		a.OnboardingCompletedAt = &now
	}
	a.UpdatedAt = now
	return nil
}

// AcceptConsent registra la aceptación si las versiones son las vigentes.
func (a *UserAccount) AcceptConsent(terms, privacy string, cur ConsentVersions, now time.Time) error {
	if a.IsSuspended() {
		return ErrAccountSuspended
	}
	if terms != cur.Terms || privacy != cur.Privacy {
		return ErrConsentOutdated
	}
	a.Consent = &Consent{TermsVersion: terms, PrivacyVersion: privacy, AcceptedAt: now}
	a.UpdatedAt = now
	return nil
}

// ProfileChanges describe una edición parcial (PATCH). Un puntero nil = sin
// cambio; Clear* = poner a null el campo opcional.
type ProfileChanges struct {
	DisplayName           *string
	Locale                *kernel.Locale
	Country               *kernel.CountryCode
	ClearCountry          bool
	Phone                 *kernel.Phone
	ClearPhone            bool
	Address               *kernel.Address
	ClearAddress          bool
	EmergencyContact      *kernel.Contact
	ClearEmergencyContact bool
}

// UpdateProfile aplica los cambios y devuelve los nombres de los campos
// modificados (para auditoría, sin valores).
func (a *UserAccount) UpdateProfile(c ProfileChanges, now time.Time) ([]string, error) {
	if a.IsSuspended() {
		return nil, ErrAccountSuspended
	}
	var fields []string
	if c.DisplayName != nil && *c.DisplayName != a.DisplayName {
		a.DisplayName = *c.DisplayName
		fields = append(fields, "display_name")
	}
	if c.Locale != nil && *c.Locale != a.Locale {
		a.Locale = *c.Locale
		fields = append(fields, "locale")
	}
	switch {
	case c.ClearCountry && a.Country != nil:
		a.Country = nil
		fields = append(fields, "country")
	case c.Country != nil && (a.Country == nil || *a.Country != *c.Country):
		v := *c.Country
		a.Country = &v
		fields = append(fields, "country")
	}
	switch {
	case c.ClearPhone && a.Phone != nil:
		a.Phone = nil
		fields = append(fields, "phone")
	case c.Phone != nil && (a.Phone == nil || *a.Phone != *c.Phone):
		v := *c.Phone
		a.Phone = &v
		fields = append(fields, "phone")
	}
	switch {
	case c.ClearAddress && a.Address != nil:
		a.Address = nil
		fields = append(fields, "address")
	case c.Address != nil && (a.Address == nil || *a.Address != *c.Address):
		v := *c.Address
		a.Address = &v
		fields = append(fields, "address")
	}
	switch {
	case c.ClearEmergencyContact && a.EmergencyContact != nil:
		a.EmergencyContact = nil
		fields = append(fields, "emergency_contact")
	case c.EmergencyContact != nil && (a.EmergencyContact == nil || *a.EmergencyContact != *c.EmergencyContact):
		v := *c.EmergencyContact
		a.EmergencyContact = &v
		fields = append(fields, "emergency_contact")
	}
	if len(fields) > 0 {
		a.UpdatedAt = now
	}
	return fields, nil
}

func normalizeRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		r = strings.TrimSpace(r)
		// Solo roles de negocio; los técnicos de Keycloak no se persisten.
		if r == "" || r == "offline_access" || r == "uma_authorization" || strings.HasPrefix(r, "default-roles-") {
			continue
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
