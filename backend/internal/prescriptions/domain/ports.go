package domain

import (
	"context"

	"github.com/google/uuid"
)

// EncounterPlanItem representa un ítem de medicación proveniente de un encuentro clínico firmado (PP-F4-DOM-013).
type EncounterPlanItem struct {
	CatalogID    *uuid.UUID `json:"catalog_id,omitempty"`
	Name         string     `json:"name"`
	DoseValue    float64    `json:"dose_value"`
	DoseUnit     string     `json:"dose_unit"`
	Route        string     `json:"route"`
	Frequency    string     `json:"frequency"`
	DurationDays *int       `json:"duration_days,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
}

// RegulatoryRule representa los límites legales por país (PP-F4-DOM-012, PP-F4-DAT-005).
type RegulatoryRule struct {
	Country                        string
	DefaultValidityDays            int
	MaxValidityDays                int
	MaxRefills                     int
	ControlledOrderable            bool
	RequiresCredentialVerification bool
}

// CredentialCheck consulta la credencial verificada del profesional (PP-F4-DOM-013).
type CredentialCheck interface {
	Verified(ctx context.Context, userID uuid.UUID, country string) (*CredentialRef, bool, error)
}

// EncounterPlanReader lee ítems de planes de encuentros firmados (PP-F4-DOM-013).
type EncounterPlanReader interface {
	SignedPlanItems(ctx context.Context, encounterID uuid.UUID, petID uuid.UUID, orgID uuid.UUID) ([]EncounterPlanItem, bool, error)
}

// RegulatoryRules consulta las reglas vigentes por país (PP-F4-DOM-013).
type RegulatoryRules interface {
	ForCountry(ctx context.Context, country string) (*RegulatoryRule, error)
}

// CatalogEquivalence comprueba sustancias controladas y equivalencia farmacéutica (PP-F4-DOM-013).
type CatalogEquivalence interface {
	IsControlled(ctx context.Context, catalogID uuid.UUID) (bool, error)
	AreEquivalent(ctx context.Context, a, b uuid.UUID) (bool, error)
}

// PetAccessChecker comprueba si una organización tiene acceso clínico activo a la mascota (CareLink).
type PetAccessChecker interface {
	HasActiveAccess(ctx context.Context, orgID uuid.UUID, petID uuid.UUID) (bool, error)
}
