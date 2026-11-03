package domain

import (
	"time"

	"github.com/google/uuid"
)

// Constantes de índices y alias principales (ADR-013).
const (
	IndexPets          = "pets"
	IndexClinical      = "clinical"
	IndexMedications   = "medications"
	IndexOrganizations = "organizations"

	AliasPetsWrite          = "pets-write"
	AliasClinicalWrite      = "clinical-write"
	AliasMedicationsWrite   = "medications-write"
	AliasOrganizationsWrite = "organizations-write"
)

// AccessScope define los permisos denormalizados embebidos en los documentos (DOM-003, ADR-013).
type AccessScope struct {
	GuardianID    string   `json:"guardian_id,omitempty"`
	OrgIDsLinked  []string `json:"org_ids_linked,omitempty"`
	OrgIDsCustody []string `json:"org_ids_custody,omitempty"`
}

// PetDoc es la representación desnormalizada de una mascota en OpenSearch (índice pets).
type PetDoc struct {
	PetID               string      `json:"pet_id"`
	Name                string      `json:"name"`
	NameSuggest         []string    `json:"name_suggest,omitempty"`
	Species             string      `json:"species"`
	Breed               string      `json:"breed,omitempty"`
	Sex                 string      `json:"sex"`
	BirthDate           *string     `json:"birth_date,omitempty"` // YYYY-MM-DD
	Microchip           string      `json:"microchip,omitempty"`
	MicrochipSuffix     string      `json:"microchip_suffix,omitempty"`
	Status              string      `json:"status"` // active | deceased
	GuardianDisplayName string      `json:"guardian_display_name,omitempty"`
	Provisional         bool        `json:"provisional"`
	Access              AccessScope `json:"access"`
	LastEncounterAt     *time.Time  `json:"last_encounter_at,omitempty"`
	UpdatedAt           time.Time   `json:"updated_at"`
	Version             int64       `json:"version"`
}

// DiagnosisItem representa un diagnóstico estructurado en ClinicalDoc.
type DiagnosisItem struct {
	Text      string `json:"text"`
	Code      string `json:"code,omitempty"`
	Certainty string `json:"certainty"` // confirmed | presumptive | ruled_out
}

// MedicationSubDoc representa un medicamento prescrito dentro de un encuentro.
type MedicationSubDoc struct {
	Name      string  `json:"name"`
	CatalogID *string `json:"catalog_id,omitempty"`
}

// ClinicalDoc es la representación desnormalizada de un encuentro o entrada de carnet (índice clinical).
type ClinicalDoc struct {
	DocKind          string             `json:"doc_kind"` // encounter | addendum | card_entry
	PetID            string             `json:"pet_id"`
	PetName          string             `json:"pet_name"`
	Species          string             `json:"species"`
	OrganizationID   string             `json:"organization_id"`
	OrganizationName string             `json:"organization_name"`
	ProfessionalID   string             `json:"professional_id,omitempty"`
	OccurredAt       time.Time          `json:"occurred_at"`
	EncounterType    string             `json:"encounter_type"` // consultation, vaccination, emergency, etc.
	Diagnoses        []DiagnosisItem    `json:"diagnoses,omitempty"`
	Text             string             `json:"text"` // anamnesis + hallazgos + plan concatenados
	Medications      []MedicationSubDoc `json:"medications,omitempty"`
	Procedures       []string           `json:"procedures,omitempty"`
	VaccineProduct   *string            `json:"vaccine_product,omitempty"`
	Locale           string             `json:"locale"` // es | en
	Year             int                `json:"year"`
	Access           AccessScope        `json:"access"`
	Position         int64              `json:"position"` // global_position para idempotencia external_gte
}

// MedicationDoc representa una entrada del catálogo de medicamentos (índice medications).
type MedicationDoc struct {
	CatalogID            string   `json:"catalog_id"`
	Name                 string   `json:"name"`
	NameSuggest          []string `json:"name_suggest,omitempty"`
	ActiveIngredients    []string `json:"active_ingredients,omitempty"`
	Form                 string   `json:"form"`
	Concentration        *string  `json:"concentration,omitempty"`
	Presentation         *string  `json:"presentation,omitempty"`
	Manufacturer         *string  `json:"manufacturer,omitempty"`
	Species              []string `json:"species,omitempty"`
	Routes               []string `json:"routes,omitempty"`
	PrescriptionRequired bool     `json:"prescription_required"`
	Controlled           bool     `json:"controlled"`
	Status               string   `json:"status"` // active | discontinued | merged
	Synonyms             []string `json:"synonyms,omitempty"`
	ATCVetCode           *string  `json:"atcvet_code,omitempty"`
	UsageCount           int      `json:"usage_count"`
}

// OrgDoc representa una organización en el directorio público/búsqueda (índice organizations).
type OrgDoc struct {
	OrganizationID string    `json:"organization_id"`
	Type           string    `json:"type"`
	DisplayName    string    `json:"display_name"`
	City           string    `json:"city,omitempty"`
	Region         string    `json:"region,omitempty"`
	Country        string    `json:"country,omitempty"`
	Geo            *GeoPoint `json:"geo,omitempty"`
	Services       []string  `json:"services,omitempty"`
	Listed         bool      `json:"listed"`
	Status         string    `json:"status"`
}

// GeoPoint representa coordenadas geográficas {lat, lon}.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// PetAccessRecord representa los permisos de acceso a una mascota leídos de PostgreSQL/Caché.
type PetAccessRecord struct {
	PetID         uuid.UUID
	GuardianID    uuid.UUID
	LinkedOrgIDs  []uuid.UUID
	CustodyOrgIDs []uuid.UUID
}

// HasAccess verifica si un usuario o clínica tiene acceso a esta mascota.
func (p *PetAccessRecord) HasAccess(userID uuid.UUID, orgID uuid.UUID) (bool, string) {
	if userID != uuid.Nil && p.GuardianID == userID {
		return true, "guardian"
	}
	if orgID != uuid.Nil {
		for _, id := range p.LinkedOrgIDs {
			if id == orgID {
				return true, "linked"
			}
		}
		for _, id := range p.CustodyOrgIDs {
			if id == orgID {
				return true, "custody"
			}
		}
	}
	return false, "none"
}
