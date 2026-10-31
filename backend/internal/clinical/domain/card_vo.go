package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Value Objects del carnet (PP-F2-DOM-007, docs/phase-2/02 §3.5).

// Límites del carnet.
const (
	MaxCardEntries    = 500 // V6
	MaxProductNameLen = 120
	MaxLotLen         = 40
	MaxDoseLabelLen   = 40
	MaxCardTextLen    = 60  // fabricante, vía, sitio
	MaxVoidReasonLen  = 300 // motivo de anulación
	MaxNoteLen        = 300
	VoidWindowDays    = 30 // V5
	MaxEvidenceKeyLen = 300
)

// EntryKind es el tipo de entrada del carnet.
type EntryKind string

// Tipos admitidos.
const (
	KindVaccine      EntryKind = "vaccine"
	KindDeworming    EntryKind = "deworming"
	KindEctoparasite EntryKind = "ectoparasite"
)

// NewEntryKind valida el tipo.
func NewEntryKind(s string) (EntryKind, error) {
	switch v := EntryKind(strings.TrimSpace(s)); v {
	case KindVaccine, KindDeworming, KindEctoparasite:
		return v, nil
	}
	return "", errInvalid
}

// EntrySource indica el origen de la entrada.
type EntrySource string

// Orígenes.
const (
	SourceRecorded EntrySource = "recorded"
	SourceDeclared EntrySource = "declared"
	SourceImported EntrySource = "imported"
)

// VerificationStatus es el estado de verificación.
type VerificationStatus string

// Estados de verificación.
const (
	Verified   VerificationStatus = "verified"
	Unverified VerificationStatus = "unverified"
	Disputed   VerificationStatus = "disputed"
)

// EntryStatus es el estado de vida de la entrada.
type EntryStatus string

// Estados de entrada.
const (
	EntryActive EntryStatus = "active"
	EntryVoided EntryStatus = "voided"
)

// DueBasis indica cómo se fijó la próxima dosis.
type DueBasis string

// Bases de la próxima dosis.
const (
	BasisProtocol DueBasis = "protocol"
	BasisClinic   DueBasis = "clinic"
	BasisGuardian DueBasis = "guardian"
)

// NextDue es la próxima dosis. Su fecha debe ser posterior a la aplicación (V7).
type NextDue struct {
	Date  kernel.CivilDate `json:"date"`
	Basis DueBasis         `json:"basis"`
}

// ProductRef referencia al producto aplicado. Si hay CatalogID, el nombre se
// copia del catálogo (lo resuelve app antes de llamar al agregado).
type ProductRef struct {
	CatalogID *uuid.UUID `json:"catalog_id,omitempty"`
	Name      string     `json:"name"`
}

// NewProductRef valida el nombre (1–120).
func NewProductRef(catalogID *uuid.UUID, name string) (ProductRef, error) {
	n := strings.TrimSpace(name)
	if l := utf8.RuneCountInString(n); l < 1 || l > MaxProductNameLen {
		return ProductRef{}, errInvalid
	}
	return ProductRef{CatalogID: catalogID, Name: n}, nil
}

func (p ProductRef) key() string { return strings.ToLower(p.Name) }

var lotRe = regexp.MustCompile(`^[A-Z0-9\-/.]{1,40}$`)

// NewLot normaliza a mayúsculas y valida 1–40 de [A-Z0-9-/.].
func NewLot(s string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(s))
	if !lotRe.MatchString(v) {
		return "", errInvalid
	}
	return v, nil
}

// NewDoseLabel valida el texto libre (≤ 40).
func NewDoseLabel(s string) (string, error) {
	v := strings.TrimSpace(s)
	if utf8.RuneCountInString(v) > MaxDoseLabelLen {
		return "", errInvalid
	}
	return v, nil
}

// CardEntry es una entrada del carnet (DOM-006).
type CardEntry struct {
	ID             uuid.UUID          `json:"entry_id"`
	Kind           EntryKind          `json:"kind"`
	Product        ProductRef         `json:"product"`
	AdministeredAt kernel.CivilDate   `json:"administered_at"`
	Lot            *string            `json:"lot,omitempty"`
	Manufacturer   *string            `json:"manufacturer,omitempty"`
	ExpiryDate     *kernel.CivilDate  `json:"expiry_date,omitempty"`
	Route          *string            `json:"route,omitempty"`
	Site           *string            `json:"site,omitempty"`
	DoseLabel      *string            `json:"dose_label,omitempty"`
	WeightKg       *float64           `json:"weight_kg,omitempty"`
	Source         EntrySource        `json:"source"`
	Verification   VerificationStatus `json:"verification"`
	RecordedBy     *EntryActor        `json:"recorded_by,omitempty"`
	RecordedAt     time.Time          `json:"recorded_at,omitempty"`
	DeclaredBy     *uuid.UUID         `json:"declared_by,omitempty"`
	EncounterID    *uuid.UUID         `json:"encounter_id,omitempty"`
	NextDue        *NextDue           `json:"next_due,omitempty"`
	Status         EntryStatus        `json:"status"`
	VoidReason     string             `json:"void_reason,omitempty"`
	EvidenceKey    *string            `json:"evidence_object_key,omitempty"`
}

// EntryActor es {UserID, OrganizationID} (quien registra/verifica/anula).
type EntryActor struct {
	UserID         uuid.UUID `json:"user_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}
