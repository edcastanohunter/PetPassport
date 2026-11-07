package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Source indica el origen de la receta (PP-F4-DOM-001, ADR-018).
type Source string

const (
	SourceIssued   Source = "issued"
	SourceExternal Source = "external"
)

// Status indica el estado de la receta (PP-F4-DOM-004).
type Status string

const (
	StatusDraft     Status = "draft"
	StatusIssued    Status = "issued"
	StatusConfirmed Status = "confirmed"
	StatusRevoked   Status = "revoked"
	StatusExpired   Status = "expired"
	StatusExhausted Status = "exhausted"
)

// DoseVO representa la dosis unitaria del medicamento.
type DoseVO struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// QuantityVO representa la cantidad a dispensar.
type QuantityVO struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // pack | unit | ml | g
}

// ExtractionRefVO referencia la correspondencia con la extracción OCR si procede.
type ExtractionRefVO struct {
	ItemIndex  int     `json:"item_index"`
	Confidence float64 `json:"confidence"`
}

// PrescriptionItem es un ítem dentro de la receta (PP-F4-DOM-005).
type PrescriptionItem struct {
	ItemID              string           `json:"item_id"`
	CatalogID           *uuid.UUID       `json:"catalog_id,omitempty"`
	FreeText            *string          `json:"free_text,omitempty"`
	NameSnapshot        string           `json:"name_snapshot"`
	Presentation        string           `json:"presentation,omitempty"`
	Dose                DoseVO           `json:"dose"`
	Route               string           `json:"route"`
	Frequency           string           `json:"frequency"`
	DurationDays        *int             `json:"duration_days,omitempty"`
	QuantityToDispense  QuantityVO       `json:"quantity_to_dispense"`
	SubstitutionAllowed bool             `json:"substitution_allowed"`
	Instructions        string           `json:"instructions,omitempty"`
	Controlled          bool             `json:"controlled"`
	ExtractionRef       *ExtractionRefVO `json:"extraction_ref,omitempty"`
}

// Validity define la vigencia y refills permitidos para la receta (PP-F4-DOM-005).
type Validity struct {
	ValidUntil     kernel.CivilDate `json:"valid_until"`
	RefillsAllowed int              `json:"refills_allowed"` // 0..5
}

// Signature captura la firma digital/inmutable de una receta emitida (PP-F4-DOM-005).
type Signature struct {
	SignedBy      uuid.UUID `json:"signed_by"`
	SignedAt      time.Time `json:"signed_at"`
	StreamVersion int64     `json:"stream_version"`
	ContentHash   string    `json:"content_hash"`
}

// DispensationLine detalla la cantidad entregada para un ítem de la receta.
type DispensationLine struct {
	ItemID string  `json:"item_id"`
	Qty    float64 `json:"qty"`
}

// Dispensation registra la entrega efectuada por una farmacia contra la receta (PP-F4-DOM-005).
type Dispensation struct {
	OrderID       uuid.UUID          `json:"order_id"`
	PharmacyOrgID uuid.UUID          `json:"pharmacy_org_id"`
	At            time.Time          `json:"at"`
	Lines         []DispensationLine `json:"lines"`
	Partial       bool               `json:"partial"`
}

// ImageRef es una referencia a la imagen de una receta externa (PP-F4-DOM-005).
type ImageRef struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	Page        int    `json:"page"`
	Width       *int   `json:"width,omitempty"`
	Height      *int   `json:"height,omitempty"`
	Sha256      []byte `json:"sha256"`
}

// ExternalMeta contiene los datos declarados/extraídos de una receta externa (PP-F4-DOM-005).
type ExternalMeta struct {
	VetName     *string           `json:"vet_name,omitempty"`
	VetRegistry *string           `json:"vet_registry,omitempty"`
	ClinicName  *string           `json:"clinic_name,omitempty"`
	IssuedOn    *kernel.CivilDate `json:"issued_on,omitempty"`
}

// CredentialRef representa la credencial verificada del veterinario al emitir (PP-F4-DOM-005).
type CredentialRef struct {
	Country        string    `json:"country"`
	RegistryNumber string    `json:"registry_number"`
	VerifiedAt     time.Time `json:"verified_at"`
}

// PharmacistValidation registra la validación profesional de un farmacéutico sobre una receta externa.
type PharmacistValidation struct {
	By     uuid.UUID `json:"by"`
	OrgID  uuid.UUID `json:"org_id"`
	Status string    `json:"status"` // validated | rejected
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
}

// Confirmation registra la confirmación del tutor sobre los ítems extraídos de una receta externa.
type Confirmation struct {
	ConfirmedAt time.Time      `json:"confirmed_at"`
	Diff        map[string]any `json:"diff,omitempty"`
}

// CandidateProduct representa una sugerencia de producto del catálogo.
type CandidateProduct struct {
	CatalogID uuid.UUID `json:"catalog_id"`
	Name      string    `json:"name"`
	Score     float64   `json:"score"`
}

// ExtractedProduct contiene la información del producto detectado por OCR.
type ExtractedProduct struct {
	Text       string             `json:"text"`
	Candidates []CandidateProduct `json:"candidates,omitempty"`
}

// ExtractedItem representa un ítem detectado en el análisis de visión.
type ExtractedItem struct {
	Index      int                `json:"index"`
	RawText    string             `json:"raw_text"`
	Product    ExtractedProduct   `json:"product"`
	Dose       string             `json:"dose"`
	Frequency  string             `json:"frequency"`
	Duration   string             `json:"duration"`
	Quantity   string             `json:"quantity"`
	Confidence map[string]float64 `json:"confidence"`
	BBox       []float64          `json:"bbox,omitempty"`
}

// ExtractedHeader representa el encabezado detectado en la receta externa.
type ExtractedHeader struct {
	VetName    *string            `json:"vet_name,omitempty"`
	Registry   *string            `json:"registry,omitempty"`
	Clinic     *string            `json:"clinic,omitempty"`
	Date       *string            `json:"date,omitempty"`
	PetName    *string            `json:"pet_name,omitempty"`
	Species    *string            `json:"species,omitempty"`
	Confidence map[string]float64 `json:"confidence"`
}

// Extraction contiene el resultado estructurado de OCR (PP-F4-DOM-007).
type Extraction struct {
	Version     int             `json:"version"`
	ExtractorID string          `json:"extractor_id"`
	Legibility  float64         `json:"legibility"`
	Warnings    []string        `json:"warnings"`
	Header      ExtractedHeader `json:"header"`
	Items       []ExtractedItem `json:"items"`
	RawText     *string         `json:"raw_text,omitempty"`
	CostUSD     float64         `json:"cost_usd"`
	LatencyMs   int             `json:"latency_ms"`
}
