package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Eventos Partner Network v1 (PP-F1-DOM-008, docs/phase-1/02 §4.3).
// Email de invitación viaja solo en pii{} (ADR-002). Nunca se renombra un
// campo persistido: schema_version+1 y upcaster (ARQ-007).

// AddressPayload es la dirección en eventos (estructura cerrada DAT-008).
type AddressPayload struct {
	Country    string  `json:"country"`
	Region     *string `json:"region,omitempty"`
	City       string  `json:"city"`
	Line1      string  `json:"line1"`
	Line2      *string `json:"line2,omitempty"`
	PostalCode *string `json:"postal_code,omitempty"`
}

// TaxIDPayload es la identificación fiscal en eventos.
type TaxIDPayload struct {
	Country string `json:"country"`
	Value   string `json:"value"`
}

// ContactPayload es el contacto en eventos.
type ContactPayload struct {
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Website string `json:"website,omitempty"`
}

// OrganizationRegistered: nace el agregado con el fundador como admin (O2).
type OrganizationRegistered struct {
	OrganizationID uuid.UUID        `json:"organization_id"`
	Type           OrganizationType `json:"type"`
	LegalName      string           `json:"legal_name"`
	DisplayName    string           `json:"display_name"`
	TaxID          TaxIDPayload     `json:"tax_id"`
	Address        AddressPayload   `json:"address"`
	Contact        ContactPayload   `json:"contact"`
	PublicCode     string           `json:"public_code"`
	FounderID      uuid.UUID        `json:"founder_id"`
}

// EventName implementa eventstore.Event.
func (OrganizationRegistered) EventName() string { return "OrganizationRegistered" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationRegistered) SchemaVersion() int { return 1 }

// ProfileChanges son los campos cambiados del perfil.
type ProfileChanges struct {
	LegalName   *string         `json:"legal_name,omitempty"`
	DisplayName *string         `json:"display_name,omitempty"`
	TaxID       *TaxIDPayload   `json:"tax_id,omitempty"`
	Address     *AddressPayload `json:"address,omitempty"`
	Contact     *ContactPayload `json:"contact,omitempty"`
	Website     *string         `json:"website,omitempty"`
}

// OrganizationProfileUpdated: solo campos cambiados.
type OrganizationProfileUpdated struct {
	OrganizationID uuid.UUID      `json:"organization_id"`
	Changes        ProfileChanges `json:"changes"`
}

// EventName implementa eventstore.Event.
func (OrganizationProfileUpdated) EventName() string { return "OrganizationProfileUpdated" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationProfileUpdated) SchemaVersion() int { return 1 }

// OrganizationVerificationReset: cambio de razón social/tax en org activa
// la devuelve a pending_verification (PRD-012).
type OrganizationVerificationReset struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Cause          string    `json:"cause"`
}

// EventName implementa eventstore.Event.
func (OrganizationVerificationReset) EventName() string { return "OrganizationVerificationReset" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationVerificationReset) SchemaVersion() int { return 1 }

// OrganizationVerified: SuperAdmin valida la existencia legal.
type OrganizationVerified struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Reason         string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (OrganizationVerified) EventName() string { return "OrganizationVerified" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationVerified) SchemaVersion() int { return 1 }

// OrganizationRejected: rechazo con motivo obligatorio.
type OrganizationRejected struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Reason         string    `json:"reason"`
}

// EventName implementa eventstore.Event.
func (OrganizationRejected) EventName() string { return "OrganizationRejected" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationRejected) SchemaVersion() int { return 1 }

// OrganizationSuspended: los miembros pierden acceso a pacientes.
type OrganizationSuspended struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Reason         string    `json:"reason"`
}

// EventName implementa eventstore.Event.
func (OrganizationSuspended) EventName() string { return "OrganizationSuspended" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationSuspended) SchemaVersion() int { return 1 }

// OrganizationReactivated.
type OrganizationReactivated struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Reason         string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (OrganizationReactivated) EventName() string { return "OrganizationReactivated" }

// SchemaVersion implementa eventstore.Event.
func (OrganizationReactivated) SchemaVersion() int { return 1 }

// InvitationPII encapsula el email invitado (ADR-002).
type InvitationPII struct {
	Email kernel.Email `json:"email"`
}

// MemberInvited: invitación emitida.
type MemberInvited struct {
	OrganizationID uuid.UUID      `json:"organization_id"`
	InvitationID   uuid.UUID      `json:"invitation_id"`
	Role           OrgRole        `json:"role"`
	ExpiresAt      time.Time      `json:"expires_at"`
	PII            *InvitationPII `json:"pii,omitempty"`
}

// EventName implementa eventstore.Event.
func (MemberInvited) EventName() string { return "MemberInvited" }

// SchemaVersion implementa eventstore.Event.
func (MemberInvited) SchemaVersion() int { return 1 }

// InvitationRevoked: invitación pendiente revocada o re-emitida.
type InvitationRevoked struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	InvitationID   uuid.UUID `json:"invitation_id"`
}

// EventName implementa eventstore.Event.
func (InvitationRevoked) EventName() string { return "InvitationRevoked" }

// SchemaVersion implementa eventstore.Event.
func (InvitationRevoked) SchemaVersion() int { return 1 }

// MemberJoined: aceptación de invitación (o fundador en el registro).
type MemberJoined struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	Role           OrgRole   `json:"role"`
	InvitationID   uuid.UUID `json:"invitation_id,omitempty"`
}

// EventName implementa eventstore.Event.
func (MemberJoined) EventName() string { return "MemberJoined" }

// SchemaVersion implementa eventstore.Event.
func (MemberJoined) SchemaVersion() int { return 1 }

// MemberRoleChanged: con from/to para auditoría (02 §4.3).
type MemberRoleChanged struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	From           OrgRole   `json:"from"`
	To             OrgRole   `json:"to"`
}

// EventName implementa eventstore.Event.
func (MemberRoleChanged) EventName() string { return "MemberRoleChanged" }

// SchemaVersion implementa eventstore.Event.
func (MemberRoleChanged) SchemaVersion() int { return 1 }

// MemberRemoved.
type MemberRemoved struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	Reason         string    `json:"reason,omitempty"`
}

// EventName implementa eventstore.Event.
func (MemberRemoved) EventName() string { return "MemberRemoved" }

// SchemaVersion implementa eventstore.Event.
func (MemberRemoved) SchemaVersion() int { return 1 }

// PIISubjects: el sujeto de la PII es el invitado (por su email; el UserID
// puede no existir aún). El cifrado se activa en F5 (SEC-009).
func (e MemberInvited) PIISubjects() []uuid.UUID { return nil }

// ErrUnknownEvent/ErrUnknownCommand se declaran en errors.go.
