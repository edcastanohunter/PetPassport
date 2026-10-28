// Package app contiene los casos de uso de Clinical Records y los puertos
// que consume de otros contextos (PP-F2-DOM-014, docs/phase-2/02 §6).
package app

import (
	"context"

	"github.com/google/uuid"
)

// AccessLevel es el nivel de acceso de una organización a una mascota.
type AccessLevel string

// Niveles (02 §6): linked = vínculo activo (lectura/escritura clínica);
// custody = sin vínculo pero con encuentros firmados por la organización
// (solo lectura de lo propio, encounter_custody); none = 404.
const (
	AccessNone    AccessLevel = "none"
	AccessLinked  AccessLevel = "linked"
	AccessCustody AccessLevel = "custody"
)

// PetSummary son los datos mínimos de la mascota que Clinical muestra.
type PetSummary struct {
	PetID    uuid.UUID
	Name     string
	Species  string
	Deceased bool
}

// PetAccess es la única puerta de Clinical a Pet Registry (SEC-007: IDOR por
// pet_id). Implementación sobre read_petregistry + read_clinical.encounter_custody.
type PetAccess interface {
	CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (AccessLevel, error)
	GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error)
	PetSummary(ctx context.Context, petID uuid.UUID) (PetSummary, error)
}

// OrgRole es el rol de un usuario en una organización (Partner Network).
type OrgRole string

// Membership es el puerto hacia Partner Network.
type Membership interface {
	RoleOf(ctx context.Context, userID, orgID uuid.UUID) (role OrgRole, active bool, err error)
}
