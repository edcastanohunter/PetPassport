package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PrescriptionDrafted: la receta nace en borrador (emitida por veterinario).
type PrescriptionDrafted struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	PetID          uuid.UUID          `json:"pet_id"`
	GuardianID     uuid.UUID          `json:"guardian_id"`
	Source         Source             `json:"source"`
	IssuerID       *uuid.UUID         `json:"issuer_id,omitempty"`
	IssuerOrgID    *uuid.UUID         `json:"issuer_org_id,omitempty"`
	EncounterID    *uuid.UUID         `json:"encounter_id,omitempty"`
	Country        string             `json:"country"`
	Items          []PrescriptionItem `json:"items"`
	Validity       Validity           `json:"validity"`
	Notes          *string            `json:"notes,omitempty"`
}

func (PrescriptionDrafted) EventName() string  { return "PrescriptionDrafted" }
func (PrescriptionDrafted) SchemaVersion() int { return 1 }

// PrescriptionDraftUpdated: edición de borrador antes de firmar.
type PrescriptionDraftUpdated struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	Items          []PrescriptionItem `json:"items"`
	Validity       Validity           `json:"validity"`
	Notes          *string            `json:"notes,omitempty"`
}

func (PrescriptionDraftUpdated) EventName() string  { return "PrescriptionDraftUpdated" }
func (PrescriptionDraftUpdated) SchemaVersion() int { return 1 }

// PrescriptionIssued: el veterinario firma y emite formalmente la receta.
type PrescriptionIssued struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	IssuerID       uuid.UUID          `json:"issuer_id"`
	Credential     CredentialRef      `json:"credential"`
	IssuedAt       time.Time          `json:"issued_at"`
	ValidUntil     kernel.CivilDate   `json:"valid_until"`
	RefillsAllowed int                `json:"refills_allowed"`
	Items          []PrescriptionItem `json:"items"`
	StreamVersion  int64              `json:"stream_version"`
	ContentHash    string             `json:"content_hash"`
}

func (PrescriptionIssued) EventName() string  { return "PrescriptionIssued" }
func (PrescriptionIssued) SchemaVersion() int { return 1 }

// PrescriptionRevoked: revocación de la receta emitida por error o motivo justificado.
type PrescriptionRevoked struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
	Reason         string    `json:"reason"`
	By             uuid.UUID `json:"by"`
}

func (PrescriptionRevoked) EventName() string  { return "PrescriptionRevoked" }
func (PrescriptionRevoked) SchemaVersion() int { return 1 }

// ExternalPrescriptionUploaded: el tutor sube imágenes de una receta de otra clínica.
type ExternalPrescriptionUploaded struct {
	PrescriptionID uuid.UUID  `json:"prescription_id"`
	PetID          uuid.UUID  `json:"pet_id"`
	GuardianID     uuid.UUID  `json:"guardian_id"`
	Images         []ImageRef `json:"images"`
	Country        string     `json:"country"`
}

func (ExternalPrescriptionUploaded) EventName() string  { return "ExternalPrescriptionUploaded" }
func (ExternalPrescriptionUploaded) SchemaVersion() int { return 1 }

// PrescriptionExtractionRecorded: resultado del análisis de visión/OCR.
type PrescriptionExtractionRecorded struct {
	PrescriptionID uuid.UUID  `json:"prescription_id"`
	Extraction     Extraction `json:"extraction"`
}

func (PrescriptionExtractionRecorded) EventName() string  { return "PrescriptionExtractionRecorded" }
func (PrescriptionExtractionRecorded) SchemaVersion() int { return 1 }

// PrescriptionReuploadRequested: la imagen era ilegible y se solicita nueva foto.
type PrescriptionReuploadRequested struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
	Reason         string    `json:"reason"`
}

func (PrescriptionReuploadRequested) EventName() string  { return "PrescriptionReuploadRequested" }
func (PrescriptionReuploadRequested) SchemaVersion() int { return 1 }

// ExternalPrescriptionConfirmed: el tutor confirma y ajusta los ítems extraídos.
type ExternalPrescriptionConfirmed struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	Items          []PrescriptionItem `json:"items"`
	Validity       Validity           `json:"validity"`
	ExternalMeta   ExternalMeta       `json:"external_meta"`
	Diff           map[string]any     `json:"diff,omitempty"`
}

func (ExternalPrescriptionConfirmed) EventName() string  { return "ExternalPrescriptionConfirmed" }
func (ExternalPrescriptionConfirmed) SchemaVersion() int { return 1 }

// ExternalPrescriptionReopened: el tutor reabre la receta externa confirmada para corregir datos.
type ExternalPrescriptionReopened struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
}

func (ExternalPrescriptionReopened) EventName() string  { return "ExternalPrescriptionReopened" }
func (ExternalPrescriptionReopened) SchemaVersion() int { return 1 }

// PrescriptionPharmacistValidated: validación o rechazo por un farmacéutico.
type PrescriptionPharmacistValidated struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
	By             uuid.UUID `json:"by"`
	OrgID          uuid.UUID `json:"org_id"`
	Status         string    `json:"status"` // validated | rejected
	Note           string    `json:"note,omitempty"`
}

func (PrescriptionPharmacistValidated) EventName() string  { return "PrescriptionPharmacistValidated" }
func (PrescriptionPharmacistValidated) SchemaVersion() int { return 1 }

// PrescriptionDispensed: entrega de medicación registrada contra la receta.
type PrescriptionDispensed struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	OrderID        uuid.UUID          `json:"order_id"`
	PharmacyOrgID  uuid.UUID          `json:"pharmacy_org_id"`
	Lines          []DispensationLine `json:"lines"`
	Partial        bool               `json:"partial"`
	RefillsUsed    int                `json:"refills_used"`
}

func (PrescriptionDispensed) EventName() string  { return "PrescriptionDispensed" }
func (PrescriptionDispensed) SchemaVersion() int { return 1 }

// PrescriptionExhausted: la receta ha consumido todos sus refills y cantidades.
type PrescriptionExhausted struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
}

func (PrescriptionExhausted) EventName() string  { return "PrescriptionExhausted" }
func (PrescriptionExhausted) SchemaVersion() int { return 1 }

// PrescriptionExpired: la vigencia de la receta ha vencido según la fecha límite.
type PrescriptionExpired struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
}

func (PrescriptionExpired) EventName() string  { return "PrescriptionExpired" }
func (PrescriptionExpired) SchemaVersion() int { return 1 }
