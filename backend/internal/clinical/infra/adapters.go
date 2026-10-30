package infra

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PetAccessAdapter implementa app.PetAccess sobre read_petregistry y
// read_clinical.encounter_custody (SEC-007).
type PetAccessAdapter struct{ pool *pgxpool.Pool }

var _ app.PetAccess = (*PetAccessAdapter)(nil)

// NewPetAccess construye el adaptador.
func NewPetAccess(pool *pgxpool.Pool) *PetAccessAdapter { return &PetAccessAdapter{pool: pool} }

// CanOrgAccess resuelve el nivel de acceso de una organización a una mascota.
func (a *PetAccessAdapter) CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (app.AccessLevel, error) {
	var linked, custody bool
	err := a.pool.QueryRow(ctx, `
		SELECT
		  EXISTS (
		    SELECT 1 FROM read_petregistry.pet_care_links l
		    JOIN read_partnernetwork.organizations o ON o.id = l.organization_id
		    WHERE l.pet_id = $1 AND l.organization_id = $2
		      AND l.status = 'active' AND l.org_suspended = false AND o.status = 'active'
		  ),
		  EXISTS (
		    SELECT 1 FROM read_clinical.encounter_custody c
		    WHERE c.pet_id = $1 AND c.organization_id = $2
		  )`, petID, orgID).Scan(&linked, &custody)
	if err != nil {
		return app.AccessNone, err
	}
	switch {
	case linked:
		return app.AccessLinked, nil
	case custody:
		return app.AccessCustody, nil
	default:
		return app.AccessNone, nil
	}
}

// GuardianOf devuelve el tutor de la mascota.
func (a *PetAccessAdapter) GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error) {
	var guardian uuid.UUID
	err := a.pool.QueryRow(ctx, `SELECT guardian_id FROM read_petregistry.pets WHERE id = $1`, petID).Scan(&guardian)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, &kernel.NotFoundError{Resource: "pet"}
	}
	return guardian, err
}

// PetSummary devuelve los datos mínimos de la mascota.
func (a *PetAccessAdapter) PetSummary(ctx context.Context, petID uuid.UUID) (app.PetSummary, error) {
	var s app.PetSummary
	err := a.pool.QueryRow(ctx, `
		SELECT id, name, species, status = 'deceased', to_char(birth_date, 'YYYY-MM-DD') FROM read_petregistry.pets WHERE id = $1`, petID).
		Scan(&s.PetID, &s.Name, &s.Species, &s.Deceased, &s.BirthDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PetSummary{}, &kernel.NotFoundError{Resource: "pet"}
	}
	return s, err
}

// LinkedOrgIDs lista organizaciones con vínculo activo y org activa.
func (a *PetAccessAdapter) LinkedOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT l.organization_id FROM read_petregistry.pet_care_links l
		JOIN read_partnernetwork.organizations o ON o.id = l.organization_id
		WHERE l.pet_id = $1 AND l.status = 'active' AND l.org_suspended = false AND o.status = 'active'`,
		petID)
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

// CustodyOrgIDs lista organizaciones con custodia de encuentros firmados.
func (a *PetAccessAdapter) CustodyOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT DISTINCT organization_id FROM read_clinical.encounter_custody WHERE pet_id = $1`, petID)
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

// Alerts devuelve las alertas del perfil (sin columna en M2.1: vacío).
func (*PetAccessAdapter) Alerts(context.Context, uuid.UUID) ([]string, error) { return []string{}, nil }

// MembershipAdapter implementa app.Membership sobre read_partnernetwork.
type MembershipAdapter struct{ pool *pgxpool.Pool }

var _ app.Membership = (*MembershipAdapter)(nil)

// NewMembership construye el adaptador.
func NewMembership(pool *pgxpool.Pool) *MembershipAdapter { return &MembershipAdapter{pool: pool} }

// RoleOf devuelve el rol y si la membresía y la organización están activas.
func (a *MembershipAdapter) RoleOf(ctx context.Context, userID, orgID uuid.UUID) (app.OrgRole, bool, error) {
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
	return app.OrgRole(role), status == "active" && orgStatus == "active", nil
}

// OrganizationName devuelve el display_name de la organización ("" si no existe).
func (a *MembershipAdapter) OrganizationName(ctx context.Context, orgID uuid.UUID) (string, error) {
	var name string
	err := a.pool.QueryRow(ctx, `SELECT display_name FROM read_partnernetwork.organizations WHERE id = $1`, orgID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// AppointmentAdapter implementa app.AppointmentAccess sobre el read model de
// Scheduling (read_scheduling.appointments, M2.3). Es la única lectura de
// Clinical hacia Scheduling (ADR-001: vía puerto, sin importar su paquete).
type AppointmentAdapter struct{ pool *pgxpool.Pool }

var _ app.AppointmentAccess = AppointmentAdapter{}

// NewAppointmentAdapter construye el adaptador.
func NewAppointmentAdapter(pool *pgxpool.Pool) AppointmentAdapter {
	return AppointmentAdapter{pool: pool}
}

// CheckedIn indica si la cita de la mascota y la organización está en check-in.
func (a AppointmentAdapter) CheckedIn(ctx context.Context, appointmentID, petID, orgID uuid.UUID) (bool, error) {
	var status string
	err := a.pool.QueryRow(ctx, `
		SELECT status FROM read_scheduling.appointments
		 WHERE id = $1 AND pet_id = $2 AND organization_id = $3`, appointmentID, petID, orgID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, app.ErrAppointmentNotFound{}
	}
	return status == "checked_in", err
}
