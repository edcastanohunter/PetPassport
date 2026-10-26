// Package domain contiene el agregado Organization, sus value objects,
// comandos y eventos v1 (PP-F1-DOM-006/007/008, docs/phase-1/02 §4). Puro:
// solo stdlib, kernel y uuid (ADR-001 regla 1).
package domain

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// OrganizationType es el tipo de organización (inmutable, O3).
type OrganizationType string

// Tipos admitidos.
const (
	OrgTypeClinic   OrganizationType = "clinic"
	OrgTypePharmacy OrganizationType = "pharmacy"
)

// NewOrganizationType valida y normaliza.
func NewOrganizationType(s string) (OrganizationType, error) {
	v := OrganizationType(strings.ToLower(strings.TrimSpace(s)))
	if v == OrgTypeClinic || v == OrgTypePharmacy {
		return v, nil
	}
	return "", invalid("type")
}

// LegalName es la razón social: 2–120.
type LegalName string

// NewLegalName valida y normaliza (trim).
func NewLegalName(s string) (LegalName, error) {
	s = strings.TrimSpace(s)
	if l := len([]rune(s)); l < 2 || l > 120 {
		return "", invalid("legal_name")
	}
	return LegalName(s), nil
}

// DisplayName es el nombre comercial: 2–80.
type DisplayName string

// NewDisplayName valida y normaliza (trim).
func NewDisplayName(s string) (DisplayName, error) {
	s = strings.TrimSpace(s)
	if l := len([]rune(s)); l < 2 || l > 80 {
		return "", invalid("display_name")
	}
	return DisplayName(s), nil
}

// TaxID es la identificación fiscal: {Country, Value} con validador por país
// enchufable (DOM-007). F1 implementa CO (NIT con dígito de verificación) y
// un genérico alfanumérico.
type TaxID struct {
	Country kernel.CountryCode
	Value   string
}

var taxGenericRe = regexp.MustCompile(`^[A-Z0-9.-]{5,20}$`)

// NewTaxID valida y normaliza (country upper, value trim+upper).
func NewTaxID(country, value string) (TaxID, error) {
	c, err := kernel.NewCountryCode(country)
	if err != nil {
		return TaxID{}, invalid("tax_id.country")
	}
	v := strings.ToUpper(strings.TrimSpace(value))
	switch string(c) {
	case "CO":
		v = strings.ReplaceAll(strings.ReplaceAll(v, ".", ""), " ", "")
		if !validNITCO(v) {
			return TaxID{}, invalidTax()
		}
	default:
		if !taxGenericRe.MatchString(v) {
			return TaxID{}, invalidTax()
		}
	}
	return TaxID{Country: c, Value: v}, nil
}

// validNITCO valida el NIT colombiano con dígito de verificación (algoritmo
// DIAN: factores primos 3..71 desde la derecha, residuo módulo 11; residuo 0
// o 1 → DV = residuo, si no DV = 11 − residuo). Formato NNNNNNNNN-D; los
// puntos de miles se eliminan al normalizar.
func validNITCO(v string) bool {
	base, check, found := strings.Cut(v, "-")
	if !found || len(base) < 3 || len(base) > 15 {
		return false
	}
	if len(check) != 1 || check[0] < '0' || check[0] > '9' {
		return false
	}
	factors := []int{3, 7, 13, 17, 19, 23, 29, 37, 41, 43, 47, 53, 59, 67, 71}
	sum := 0
	idx := 0
	for i := len(base) - 1; i >= 0; i-- {
		d := base[i]
		if d < '0' || d > '9' {
			return false
		}
		sum += int(d-'0') * factors[idx%len(factors)]
		idx++
	}
	dv := sum % 11
	if dv > 1 {
		dv = 11 - dv
	}
	return dv == int(check[0]-'0')
}

func invalidTax() error {
	return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "tax_id.value", Code: "INVALID"}}}
}

// OrgContact son los datos de contacto de la organización (02 §4.2).
type OrgContact struct {
	Email   kernel.Email
	Phone   kernel.Phone
	Website string // URL https opcional
}

var websiteRe = regexp.MustCompile(`^https://[^\s]+$`)

// NewOrgContact valida y normaliza el contacto.
func NewOrgContact(email, phone, website string) (OrgContact, error) {
	ve := &kernel.ValidationError{}
	e, err := kernel.NewEmail(email)
	if err != nil {
		ve.Add("contact.email", "INVALID")
	}
	p, err := kernel.NewPhone(phone)
	if err != nil {
		ve.Add("contact.phone", "INVALID")
	}
	w := strings.TrimSpace(website)
	if w != "" && (!websiteRe.MatchString(w) || len(w) > 200) {
		ve.Add("contact.website", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return OrgContact{}, err
	}
	return OrgContact{Email: e, Phone: p, Website: w}, nil
}

// PublicCode es el código corto de la organización: 6 caracteres Crockford
// base32 sin vocales ambiguas (ADR-007). Único por reserva (O5, DAT-006).
type PublicCode string

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var publicCodeRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{6}$`)

// NewPublicCode valida el formato.
func NewPublicCode(s string) (PublicCode, error) {
	if !publicCodeRe.MatchString(strings.ToUpper(s)) {
		return "", invalid("public_code")
	}
	return PublicCode(strings.ToUpper(s)), nil
}

// GeneratePublicCode genera un código aleatorio (crypto/rand via kernel).
func GeneratePublicCode() (PublicCode, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("public code: %w", err)
	}
	out := make([]byte, 6)
	for i, b := range raw {
		out[i] = crockfordAlphabet[int(b)%len(crockfordAlphabet)]
	}
	return PublicCode(out), nil
}

// OrgRole es el rol de un miembro (O1: válido según el tipo).
type OrgRole string

// Roles admitidos.
const (
	RoleClinicAdmin   OrgRole = "clinic_admin"
	RoleVeterinarian  OrgRole = "veterinarian"
	RoleReceptionist  OrgRole = "receptionist"
	RolePharmacyAdmin OrgRole = "pharmacy_admin"
	RolePharmacist    OrgRole = "pharmacist"
)

// RolesForType devuelve los roles válidos para el tipo de organización (O1).
func RolesForType(t OrganizationType) []OrgRole {
	if t == OrgTypeClinic {
		return []OrgRole{RoleClinicAdmin, RoleVeterinarian, RoleReceptionist}
	}
	return []OrgRole{RolePharmacyAdmin, RolePharmacist}
}

// ValidRoleForType implementa O1.
func ValidRoleForType(t OrganizationType, r OrgRole) bool {
	for _, allowed := range RolesForType(t) {
		if allowed == r {
			return true
		}
	}
	return false
}

// IsAdminRole informa si el rol tiene permisos de administración (O2).
func IsAdminRole(r OrgRole) bool {
	return r == RoleClinicAdmin || r == RolePharmacyAdmin
}

// OrgStatus es el ciclo de vida de la organización.
type OrgStatus string

// Estados admitidos.
const (
	OrgPendingVerification OrgStatus = "pending_verification"
	OrgActive              OrgStatus = "active"
	OrgSuspended           OrgStatus = "suspended"
	OrgRejected            OrgStatus = "rejected"
)

// MemberStatus es el estado de una membresía dentro del agregado.
type MemberStatus string

// Estados admitidos.
const (
	MemberStatusActive  MemberStatus = "active"
	MemberStatusRemoved MemberStatus = "removed"
)

// InvitationStatus es el estado de una invitación.
type InvitationStatus string

// Estados admitidos.
const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusRevoked  InvitationStatus = "revoked"
	InvitationStatusExpired  InvitationStatus = "expired"
)

// Member es una entidad interna del agregado (no raíz; 02 §9).
type Member struct {
	UserID   uuid.UUID
	Role     OrgRole
	Status   MemberStatus
	JoinedAt time.Time
}

// Invitation es una entidad interna del agregado. El hash del token
// (SHA-256 de 32 bytes aleatorios, DOM-007) no viaja en los eventos: vive en
// read_partnernetwork.invitations, igual que el de la transferencia de Pet
// (decisión M1.2); app resuelve token → invitación antes de despachar.
type Invitation struct {
	ID        uuid.UUID
	Email     kernel.Email
	Role      OrgRole
	ExpiresAt time.Time
	Status    InvitationStatus
}
