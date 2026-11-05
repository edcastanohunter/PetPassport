// Package domain define el modelo de dominio para el catálogo de medicamentos (PP-F3-DOM-005, ADR-016).
package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Form es la forma farmacéutica del medicamento veterinario.
type Form string

const (
	FormTablet         Form = "tablet"
	FormCapsule        Form = "capsule"
	FormOralSuspension Form = "oral_suspension"
	FormOralSolution   Form = "oral_solution"
	FormInjectable     Form = "injectable"
	FormTopical        Form = "topical"
	FormOphthalmic     Form = "ophthalmic"
	FormOtic           Form = "otic"
	FormSpray          Form = "spray"
	FormPowder         Form = "powder"
	FormOther          Form = "other"
)

// Status es el estado del medicamento en el catálogo.
type Status string

const (
	StatusActive       Status = "active"
	StatusDiscontinued Status = "discontinued"
	StatusMerged       Status = "merged"
)

// Ingredient representa un principio activo.
type Ingredient struct {
	Name     string   `json:"name"`
	Strength *float64 `json:"strength,omitempty"`
	Unit     *string  `json:"unit,omitempty"`
}

// Regulatory contiene la información regulatoria (p. ej. registro ICA en Colombia).
type Regulatory struct {
	Country        string  `json:"country"`
	RegistryNumber *string `json:"registry_number,omitempty"`
	Authority      string  `json:"authority"` // "ICA"
}

// Source describe el origen del registro en el catálogo.
type Source struct {
	Kind       string     `json:"kind"` // "manual" | "import" | "suggestion"
	Ref        string     `json:"ref,omitempty"`
	ImportedAt *time.Time `json:"imported_at,omitempty"`
}

// Medication es un medicamento veterinario con identificador estable (ADR-016).
type Medication struct {
	ID                   uuid.UUID    `json:"id"`
	Name                 string       `json:"name"`
	NameNorm             string       `json:"name_norm"`
	ActiveIngredients    []Ingredient `json:"active_ingredients"`
	Form                 Form         `json:"form"`
	Concentration        *string      `json:"concentration,omitempty"`
	Presentation         *string      `json:"presentation,omitempty"`
	Routes               []string     `json:"routes"`
	Species              []string     `json:"species"`
	Manufacturer         *string      `json:"manufacturer,omitempty"`
	ManufacturerNorm     *string      `json:"manufacturer_norm,omitempty"`
	ATCvetCode           *string      `json:"atcvet_code,omitempty"`
	Regulatory           *Regulatory  `json:"regulatory,omitempty"`
	Controlled           bool         `json:"controlled"`
	PrescriptionRequired bool         `json:"prescription_required"`
	DefaultDoseUnit      *string      `json:"default_dose_unit,omitempty"`
	DefaultRoute         *string      `json:"default_route,omitempty"`
	Synonyms             []string     `json:"synonyms"`
	Status               Status       `json:"status"`
	MergedInto           *uuid.UUID   `json:"merged_into,omitempty"`
	Source               Source       `json:"source"`
	UsageCount           int          `json:"usage_count"`
	CreatedAt            time.Time    `json:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at"`
}

// Normalize normaliza un texto eliminando tildes, pasando a minúsculas y limpiando espacios.
func Normalize(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normStr, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return normStr
}
