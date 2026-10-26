package domain

import (
	"time"

	"github.com/google/uuid"
)

// Comandos del agregado Organization (docs/phase-1/02 §4.1). Los de admin
// (verify/reject/suspend/reactivate) existen desde F1; sus endpoints HTTP
// llegan en M1.4 (API-008).

// AddressInput es la dirección entrante.
type AddressInput struct {
	Country    string
	Region     *string
	City       string
	Line1      string
	Line2      *string
	PostalCode *string
}

// RegisterOrganization crea la organización (pendiente de verificación).
type RegisterOrganization struct {
	OrganizationID uuid.UUID
	Type           string
	LegalName      string
	DisplayName    string
	TaxCountry     string
	TaxValue       string
	Address        AddressInput
	ContactEmail   string
	ContactPhone   string
	Website        string
	FounderID      uuid.UUID
	Now            time.Time
}

// CommandName implementa bus.Command.
func (RegisterOrganization) CommandName() string { return "partnernetwork.RegisterOrganization" }

// UpdateOrganizationProfile edita el perfil. Cambiar razón social o tax_id
// en org activa emite OrganizationVerificationReset (PRD-012).
type UpdateOrganizationProfile struct {
	OrganizationID uuid.UUID
	LegalName      *string
	DisplayName    *string
	TaxCountry     *string
	TaxValue       *string
	Address        *AddressInput
	ContactEmail   *string
	ContactPhone   *string
	Website        *string
	Now            time.Time
}

// CommandName implementa bus.Command.
func (UpdateOrganizationProfile) CommandName() string {
	return "partnernetwork.UpdateOrganizationProfile"
}

// VerifyOrganization: SuperAdmin verifica (M1.4 HTTP).
type VerifyOrganization struct {
	OrganizationID uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (VerifyOrganization) CommandName() string { return "partnernetwork.VerifyOrganization" }

// RejectOrganization: motivo obligatorio.
type RejectOrganization struct {
	OrganizationID uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (RejectOrganization) CommandName() string { return "partnernetwork.RejectOrganization" }

// SuspendOrganization: motivo obligatorio.
type SuspendOrganization struct {
	OrganizationID uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (SuspendOrganization) CommandName() string { return "partnernetwork.SuspendOrganization" }

// ReactivateOrganization.
type ReactivateOrganization struct {
	OrganizationID uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (ReactivateOrganization) CommandName() string { return "partnernetwork.ReactivateOrganization" }

// InviteMember emite una invitación (O1, O4). Re-invitar al mismo email
// revoca la anterior. app genera InvitationID y el token (su hash va al read
// model) y resuelve ExistingUserID por email con UserLookup: si ese usuario
// ya es miembro activo → ALREADY_MEMBER.
type InviteMember struct {
	OrganizationID uuid.UUID
	InvitationID   uuid.UUID
	Email          string
	Role           string
	ExistingUserID uuid.UUID // uuid.Nil si el email no tiene cuenta
	Now            time.Time
}

// CommandName implementa bus.Command.
func (InviteMember) CommandName() string { return "partnernetwork.InviteMember" }

// RevokeInvitation revoca una invitación pendiente.
type RevokeInvitation struct {
	OrganizationID uuid.UUID
	InvitationID   uuid.UUID
}

// CommandName implementa bus.Command.
func (RevokeInvitation) CommandName() string { return "partnernetwork.RevokeInvitation" }

// AcceptInvitation acepta la invitación. app resuelve el token (hash en
// read_partnernetwork.invitations) → InvitationID; el dominio valida estado,
// vigencia, email y verificación.
type AcceptInvitation struct {
	OrganizationID uuid.UUID
	InvitationID   uuid.UUID
	UserID         uuid.UUID
	ActorEmail     string
	EmailVerified  bool
	Now            time.Time
}

// CommandName implementa bus.Command.
func (AcceptInvitation) CommandName() string { return "partnernetwork.AcceptInvitation" }

// ChangeMemberRole cambia el rol (O1, O2).
type ChangeMemberRole struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           string
}

// CommandName implementa bus.Command.
func (ChangeMemberRole) CommandName() string { return "partnernetwork.ChangeMemberRole" }

// RemoveMember retira un miembro (O2).
type RemoveMember struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Reason         string
}

// CommandName implementa bus.Command.
func (RemoveMember) CommandName() string { return "partnernetwork.RemoveMember" }
