package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ReadPrescriptionSummary es el DTO para listas de recetas (PP-F4-API-002, API-004).
type ReadPrescriptionSummary struct {
	ID                 uuid.UUID  `json:"id"`
	Version            int64      `json:"version"`
	PetID              uuid.UUID  `json:"pet_id"`
	GuardianID         uuid.UUID  `json:"guardian_id"`
	Country            string     `json:"country"`
	Source             string     `json:"source"`
	Status             string     `json:"status"`
	IssuerID           *uuid.UUID `json:"issuer_id,omitempty"`
	IssuerOrgID        *uuid.UUID `json:"issuer_org_id,omitempty"`
	IssuedAt           *time.Time `json:"issued_at,omitempty"`
	ValidUntil         *string    `json:"valid_until,omitempty"`
	RefillsAllowed     int        `json:"refills_allowed"`
	RefillsUsed        int        `json:"refills_used"`
	ItemsCount         int        `json:"items_count"`
	HasControlled      bool       `json:"has_controlled"`
	PDFObjectKey       *string    `json:"pdf_object_key,omitempty"`
	ArchivedByGuardian bool       `json:"archived_by_guardian"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ReadPrescriptionDetail es el DTO para el detalle completo de una receta.
type ReadPrescriptionDetail struct {
	ReadPrescriptionSummary
	EncounterID    *uuid.UUID                   `json:"encounter_id,omitempty"`
	Credential     *domain.CredentialRef        `json:"credential,omitempty"`
	Signature      *domain.Signature            `json:"signature,omitempty"`
	Items          []domain.PrescriptionItem    `json:"items"`
	ItemsRemaining map[string]float64           `json:"items_remaining"`
	ExternalMeta   *domain.ExternalMeta         `json:"external_meta,omitempty"`
	Validation     *domain.PharmacistValidation `json:"validation,omitempty"`
	Confirmation   *domain.Confirmation         `json:"confirmation,omitempty"`
	Images         []domain.ImageRef            `json:"images,omitempty"`
	Dispensations  []domain.Dispensation        `json:"dispensations,omitempty"`
	Verifications  int                          `json:"verifications_count"`
	Notes          *string                      `json:"notes,omitempty"`
}

// PrescriptionDocument contiene los datos renderizados en el Typst (PP-F4-ARQ-011).
type PrescriptionDocument struct {
	ID                 string                `json:"id"`
	Version            int64                 `json:"version"`
	PetID              string                `json:"pet_id"`
	PetName            string                `json:"pet_name"`
	PetSpecies         string                `json:"pet_species"`
	GuardianID         string                `json:"guardian_id"`
	GuardianName       string                `json:"guardian_name"`
	OrgID              string                `json:"org_id"`
	OrgName            string                `json:"org_name"`
	VetName            string                `json:"vet_name"`
	VetRegistry        string                `json:"vet_registry"`
	Country            string                `json:"country"`
	IssuedAt           string                `json:"issued_at"`
	ValidUntil         string                `json:"valid_until"`
	RefillsAllowed     int                   `json:"refills_allowed"`
	HasItems           bool                  `json:"has_items"`
	HasControlled      bool                  `json:"has_controlled"`
	Items              []PrescriptionDocItem `json:"items"`
	ContentHash        string                `json:"content_hash"`
	VerifyURL          string                `json:"verify_url"`
	SignatureAlgorithm string                `json:"signature_algorithm"`
	SignatureAt        string                `json:"signature_at"`
}

// PrescriptionDocItem describe un ítem de medicación en el PDF.
type PrescriptionDocItem struct {
	Name         string  `json:"name"`
	Presentation string  `json:"presentation"`
	DoseValue    float64 `json:"dose_value"`
	DoseUnit     string  `json:"dose_unit"`
	Route        string  `json:"route"`
	Frequency    string  `json:"frequency"`
	DurationDays int     `json:"duration_days"`
	Instructions string  `json:"instructions"`
	Controlled   bool    `json:"controlled"`
}

// ExtractionJobDTO contiene el estado y resultado del análisis de visión (PP-F4-API-003).
type ExtractionJobDTO struct {
	PrescriptionID uuid.UUID          `json:"prescription_id"`
	Status         string             `json:"status"` // queued|preprocessing|extracting|matching|done|reupload_requested|failed|refused
	Attempt        int                `json:"attempt"`
	ExtractorID    *string            `json:"extractor_id,omitempty"`
	Legibility     *float64           `json:"legibility,omitempty"`
	Warnings       []string           `json:"warnings"`
	Extraction     *domain.Extraction `json:"extraction,omitempty"`
	Reason         *string            `json:"reason,omitempty"`
}

// PrescriptionQueries define el puerto de consultas sobre el esquema read_prescriptions.
type PrescriptionQueries interface {
	GetByID(ctx context.Context, id uuid.UUID) (*ReadPrescriptionDetail, error)
	GetExtractionJob(ctx context.Context, id uuid.UUID) (*ExtractionJobDTO, error)
	GetImage(ctx context.Context, id uuid.UUID, page int) (*domain.ImageRef, error)
	AddImage(ctx context.Context, id uuid.UUID, img domain.ImageRef) error
	ListByOrg(ctx context.Context, orgID uuid.UUID, status string, petID *uuid.UUID, issuerID *uuid.UUID, limit int) ([]ReadPrescriptionSummary, error)
	ListByGuardian(ctx context.Context, guardianID uuid.UUID, petID *uuid.UUID, status string, source string, limit int) ([]ReadPrescriptionSummary, error)
	ListByEncounter(ctx context.Context, encounterID uuid.UUID) ([]ReadPrescriptionSummary, error)
	Archive(ctx context.Context, id uuid.UUID, guardianID uuid.UUID) error
	RecordVerification(ctx context.Context, id uuid.UUID, ipHash []byte, via string) error
	SetPDFObjectKey(ctx context.Context, id uuid.UUID, objectKey string, version int) error
	FindExpiredPrescriptions(ctx context.Context, today time.Time) ([]uuid.UUID, error)
}

// Repository persiste y rehidrata el agregado Prescription (PP-F4-ARQ-001, ADR-018).
type Repository interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.Prescription, error)
	Save(ctx context.Context, p *domain.Prescription, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
	ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error)
}

// TxRunner ejecuta una función dentro de una transacción UoW.
type TxRunner interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica los proyectores síncronos en la misma transacción.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// PetAccessReader consulta información básica de la mascota y su tutor.
type PetAccessReader interface {
	GetPetInfo(ctx context.Context, petID uuid.UUID) (name string, species string, guardianID uuid.UUID, err error)
}

// OrgNameReader consulta el nombre de la organización para el PDF.
type OrgNameReader interface {
	GetOrgName(ctx context.Context, orgID uuid.UUID) (string, error)
}

// UserNameReader consulta el nombre del usuario para el PDF.
type UserNameReader interface {
	GetUserName(ctx context.Context, userID uuid.UUID) (string, error)
}

// PDFGenerator genera y persiste el PDF de la receta.
type PDFGenerator interface {
	GenerateAndStore(ctx context.Context, doc PrescriptionDocument) (string, error)
}

// DraftIssuedPrescriptionCmd contiene los datos para iniciar un borrador de receta emitida.
type DraftIssuedPrescriptionCmd struct {
	ID             *uuid.UUID
	PetID          uuid.UUID
	IssuerOrgID    uuid.UUID
	Country        string
	EncounterID    *uuid.UUID
	Items          []domain.PrescriptionItem
	RefillsAllowed int
	ValidUntil     *kernel.CivilDate
	Notes          *string
}

// UpdateDraftCmd contiene los datos para modificar un borrador de receta.
type UpdateDraftCmd struct {
	PrescriptionID uuid.UUID
	Items          []domain.PrescriptionItem
	RefillsAllowed int
	ValidUntil     *kernel.CivilDate
	Notes          *string
}

// IssuePrescriptionCmd contiene la orden de firma y emisión de la receta.
type IssuePrescriptionCmd struct {
	PrescriptionID uuid.UUID
	SignerName     string
}

// RevokePrescriptionCmd contiene los datos para revocar una receta emitida.
type RevokePrescriptionCmd struct {
	PrescriptionID uuid.UUID
	Reason         string
}

// UploadExternalPrescriptionCmd contiene la carga de receta externa por el tutor.
type UploadExternalPrescriptionCmd struct {
	ID      *uuid.UUID
	PetID   uuid.UUID
	Country string
	Images  []domain.ImageRef
}

// OpenQuoteChecker comprueba si una receta tiene cotizaciones de farmacia abiertas (PP-F4-API-003, DOM-013).
type OpenQuoteChecker interface {
	HasOpenQuotes(ctx context.Context, prescriptionID uuid.UUID) (bool, error)
}

// ConfirmExternalPrescriptionCmd confirma los datos extraídos de la receta externa (PP-F4-API-003, PRD-006).
type ConfirmExternalPrescriptionCmd struct {
	PrescriptionID uuid.UUID                 `json:"prescription_id"`
	Items          []domain.PrescriptionItem `json:"items"`
	Validity       domain.Validity           `json:"validity"`
	ExternalMeta   domain.ExternalMeta       `json:"external_meta"`
	Diff           map[string]any            `json:"diff,omitempty"`
	Notes          string                    `json:"notes,omitempty"`
	Attestation    bool                      `json:"attestation"`
}

// RecordDispensationCmd registra una dispensación en farmacia.
type RecordDispensationCmd struct {
	PrescriptionID       uuid.UUID
	PartnerIntegrationID *uuid.UUID
	PharmacyName         string
	Lines                []domain.DispensationLine
	PharmacistValidation *domain.PharmacistValidation
}

// VerificationResult contiene el resultado de la verificación pública (PP-F4-SEC-006).
type VerificationResult struct {
	PrescriptionID     uuid.UUID                 `json:"prescription_id"`
	Valid              bool                      `json:"valid"`
	Status             string                    `json:"status"`
	ContentHash        string                    `json:"content_hash"`
	VerifiedAt         time.Time                 `json:"verified_at"`
	Country            string                    `json:"country"`
	PetName            string                    `json:"pet_name,omitempty"`
	PetSpecies         string                    `json:"pet_species,omitempty"`
	IssuerName         string                    `json:"issuer_name,omitempty"`
	IssuerOrgName      string                    `json:"issuer_org_name,omitempty"`
	VetRegistry        string                    `json:"vet_registry,omitempty"`
	IssuedAt           *time.Time                `json:"issued_at,omitempty"`
	ValidUntil         *string                   `json:"valid_until,omitempty"`
	RefillsAllowed     int                       `json:"refills_allowed"`
	RefillsUsed        int                       `json:"refills_used"`
	Items              []domain.PrescriptionItem `json:"items,omitempty"`
	DispensationsCount int                       `json:"dispensations_count"`
	RevocationReason   string                    `json:"revocation_reason,omitempty"`
	ErrorReason        string                    `json:"error_reason,omitempty"`
}
