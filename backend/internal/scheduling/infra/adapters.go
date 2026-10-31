package infra

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PetAccessAdapter implementa app.PetAccess sobre read_petregistry (SEC-007).
type PetAccessAdapter struct{ pool *pgxpool.Pool }

var _ app.PetAccess = (*PetAccessAdapter)(nil)

// NewPetAccess construye el adaptador.
func NewPetAccess(pool *pgxpool.Pool) *PetAccessAdapter { return &PetAccessAdapter{pool: pool} }

// Pet devuelve los datos mínimos de la mascota.
func (a *PetAccessAdapter) Pet(ctx context.Context, petID uuid.UUID) (app.PetInfo, error) {
	var p app.PetInfo
	err := a.pool.QueryRow(ctx, `
		SELECT id, guardian_id, name, species, status = 'deceased' FROM read_petregistry.pets WHERE id = $1`, petID).
		Scan(&p.ID, &p.GuardianID, &p.Name, &p.Species, &p.Deceased)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PetInfo{}, &kernel.NotFoundError{Resource: "pet"}
	}
	return p, err
}

// Linked indica vínculo activo con la organización activa.
func (a *PetAccessAdapter) Linked(ctx context.Context, petID, orgID uuid.UUID) (bool, error) {
	var ok bool
	err := a.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM read_petregistry.pet_care_links l
		  JOIN read_partnernetwork.organizations o ON o.id = l.organization_id
		  WHERE l.pet_id = $1 AND l.organization_id = $2
		    AND l.status = 'active' AND l.org_suspended = false AND o.status = 'active')`, petID, orgID).Scan(&ok)
	return ok, err
}

// GuardianLinked indica si el usuario es tutor de alguna mascota vinculada a la organización.
func (a *PetAccessAdapter) GuardianLinked(ctx context.Context, guardianID, orgID uuid.UUID) (bool, error) {
	var ok bool
	err := a.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM read_petregistry.pet_care_links l
		  JOIN read_petregistry.pets p ON p.id = l.pet_id
		  JOIN read_partnernetwork.organizations o ON o.id = l.organization_id
		  WHERE p.guardian_id = $1 AND l.organization_id = $2
		    AND l.status = 'active' AND l.org_suspended = false AND o.status = 'active')`, guardianID, orgID).Scan(&ok)
	return ok, err
}

// MembershipAdapter implementa app.Membership sobre read_partnernetwork.
type MembershipAdapter struct{ pool *pgxpool.Pool }

var _ app.Membership = (*MembershipAdapter)(nil)

// NewMembership construye el adaptador.
func NewMembership(pool *pgxpool.Pool) *MembershipAdapter { return &MembershipAdapter{pool: pool} }

// RoleOf devuelve el rol y si la membresía y la organización están activas.
func (a *MembershipAdapter) RoleOf(ctx context.Context, userID, orgID uuid.UUID) (string, bool, error) {
	var role, status, orgStatus string
	err := a.pool.QueryRow(ctx, `
		SELECT m.role, m.status, o.status
		FROM read_partnernetwork.org_members m
		JOIN read_partnernetwork.organizations o ON o.id = m.organization_id
		WHERE m.organization_id = $1 AND m.user_id = $2`, orgID, userID).Scan(&role, &status, &orgStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return role, status == "active" && orgStatus == "active", nil
}

// Org devuelve los datos mínimos de la organización (NotFoundError si no existe).
func (a *MembershipAdapter) Org(ctx context.Context, orgID uuid.UUID) (app.OrgInfo, error) {
	var o app.OrgInfo
	err := a.pool.QueryRow(ctx, `
		SELECT id, display_name, country, type, status FROM read_partnernetwork.organizations WHERE id = $1`, orgID).
		Scan(&o.ID, &o.Name, &o.Country, &o.Type, &o.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.OrgInfo{}, &kernel.NotFoundError{Resource: "organization"}
	}
	return o, err
}

// Veterinarians lista los veterinarios activos de una organización activa.
func (a *MembershipAdapter) Veterinarians(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT m.user_id FROM read_partnernetwork.org_members m
		JOIN read_partnernetwork.organizations o ON o.id = m.organization_id
		WHERE m.organization_id = $1 AND m.role = 'veterinarian' AND m.status = 'active' AND o.status = 'active'`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
