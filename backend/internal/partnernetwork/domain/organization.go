package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// StreamType es el tipo de stream del agregado (org-<OrganizationID>).
const StreamType = "Organization"

// InvitationTTL es la caducidad de una invitación (PRD-013).
const InvitationTTL = 7 * 24 * time.Hour

// MaxResends24h es el máximo de reenvíos por invitación en 24 h (API-007).
const MaxResends24h = 3

// Organization es la raíz de agregado (PP-F1-DOM-006). Rehidratable por replay.
type Organization struct {
	id           uuid.UUID
	version      int64
	orgType      OrganizationType
	legalName    LegalName
	displayName  DisplayName
	taxID        TaxID
	address      kernel.Address
	contact      OrgContact
	publicCode   PublicCode
	status       OrgStatus
	statusReason string
	founderID    uuid.UUID
	members      map[uuid.UUID]Member
	invitations  map[uuid.UUID]Invitation
	uncommitted  []any

	// pendingPublicCode lo fija app tras reservarlo (O5); no se persiste.
	pendingPublicCode string
}

// ID devuelve el identificador.
func (o *Organization) ID() uuid.UUID { return o.id }

// Version devuelve la versión del stream.
func (o *Organization) Version() int64 { return o.version }

// Status devuelve el estado.
func (o *Organization) Status() OrgStatus { return o.status }

// Type devuelve el tipo (inmutable, O3).
func (o *Organization) Type() OrganizationType { return o.orgType }

// PublicCode devuelve el código público.
func (o *Organization) PublicCode() PublicCode { return o.publicCode }

// DisplayName devuelve el nombre comercial.
func (o *Organization) DisplayName() string { return string(o.displayName) }

// ContactEmail devuelve el email de contacto.
func (o *Organization) ContactEmail() string { return string(o.contact.Email) }

// FounderID devuelve el fundador.
func (o *Organization) FounderID() uuid.UUID { return o.founderID }

// Members devuelve los miembros activos.
func (o *Organization) Members() []Member {
	out := make([]Member, 0, len(o.members))
	for _, m := range o.members {
		if m.Status == MemberStatusActive {
			out = append(out, m)
		}
	}
	return out
}

// PendingInvitations devuelve las invitaciones pendientes.
func (o *Organization) PendingInvitations() []Invitation {
	out := make([]Invitation, 0, len(o.invitations))
	for _, inv := range o.invitations {
		if inv.Status == InvitationStatusPending {
			out = append(out, inv)
		}
	}
	return out
}

// Invitation devuelve una invitación por id (reenvío y previsualización).
func (o *Organization) Invitation(id uuid.UUID) (Invitation, bool) {
	inv, ok := o.invitations[id]
	return inv, ok
}

// MemberIDs devuelve los usuarios con membresía activa (invalidación de la
// caché de membresías cuando cambia el estado de la org, DOM-011).
func (o *Organization) MemberIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(o.members))
	for id, m := range o.members {
		if m.Status == MemberStatusActive {
			out = append(out, id)
		}
	}
	return out
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (o *Organization) UncommittedEvents() []any { return o.uncommitted }

// MarkCommitted limpia los eventos tras el Append.
func (o *Organization) MarkCommitted() { o.uncommitted = nil }

// ActiveAdminCount cuenta los admins activos (O2).
func (o *Organization) ActiveAdminCount() int {
	n := 0
	for _, m := range o.members {
		if m.Status == MemberStatusActive && IsAdminRole(m.Role) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Rehidratación
// ---------------------------------------------------------------------------

// ApplyEvent muta el estado al rehidratar. Evento desconocido → error.
func (o *Organization) ApplyEvent(evt any) error {
	switch e := evt.(type) {
	case OrganizationRegistered:
		o.id = e.OrganizationID
		o.orgType = e.Type
		o.legalName = LegalName(e.LegalName)
		o.displayName = DisplayName(e.DisplayName)
		o.taxID = TaxID{Country: kernel.CountryCode(e.TaxID.Country), Value: e.TaxID.Value}
		o.address = addressFromPayload(e.Address)
		o.contact = OrgContact{Email: kernel.Email(e.Contact.Email), Phone: kernel.Phone(e.Contact.Phone), Website: e.Contact.Website}
		o.publicCode = PublicCode(e.PublicCode)
		o.status = OrgPendingVerification
		o.founderID = e.FounderID
		o.members = map[uuid.UUID]Member{
			e.FounderID: {UserID: e.FounderID, Role: adminRoleFor(e.Type), Status: MemberStatusActive, JoinedAt: time.Time{}},
		}
		o.invitations = map[uuid.UUID]Invitation{}
	case OrganizationProfileUpdated:
		o.applyChanges(e.Changes)
	case OrganizationVerificationReset:
		o.status = OrgPendingVerification
		o.statusReason = e.Cause
	case OrganizationVerified:
		o.status = OrgActive
		o.statusReason = e.Reason
	case OrganizationRejected:
		o.status = OrgRejected
		o.statusReason = e.Reason
	case OrganizationSuspended:
		o.status = OrgSuspended
		o.statusReason = e.Reason
	case OrganizationReactivated:
		o.status = OrgActive
		o.statusReason = e.Reason
	case MemberInvited:
		if e.PII == nil {
			return &kernel.DomainError{Code: "UNKNOWN_EVENT", Message: "invitación sin email"}
		}
		o.invitations[e.InvitationID] = Invitation{
			ID: e.InvitationID, Email: e.PII.Email, Role: e.Role, ExpiresAt: e.ExpiresAt,
			Status: InvitationStatusPending,
		}
	case InvitationRevoked:
		if inv, ok := o.invitations[e.InvitationID]; ok && inv.Status == InvitationStatusPending {
			inv.Status = InvitationStatusRevoked
			o.invitations[e.InvitationID] = inv
		}
	case MemberJoined:
		o.members[e.UserID] = Member{UserID: e.UserID, Role: e.Role, Status: MemberStatusActive}
		if e.InvitationID != uuid.Nil {
			if inv, ok := o.invitations[e.InvitationID]; ok {
				inv.Status = InvitationStatusAccepted
				o.invitations[e.InvitationID] = inv
			}
		}
	case MemberRoleChanged:
		if m, ok := o.members[e.UserID]; ok && m.Status == MemberStatusActive {
			m.Role = e.To
			o.members[e.UserID] = m
		}
	case MemberRemoved:
		if m, ok := o.members[e.UserID]; ok {
			m.Status = MemberStatusRemoved
			o.members[e.UserID] = m
		}
	default:
		return &kernel.DomainError{Code: "UNKNOWN_EVENT", Message: "evento desconocido para Organization"}
	}
	o.version++
	return nil
}

func adminRoleFor(t OrganizationType) OrgRole {
	if t == OrgTypeClinic {
		return RoleClinicAdmin
	}
	return RolePharmacyAdmin
}

func addressFromPayload(p AddressPayload) kernel.Address {
	a := kernel.Address{Country: kernel.CountryCode(p.Country), City: p.City, Line1: p.Line1}
	if p.Region != nil {
		a.Region = *p.Region
	}
	if p.Line2 != nil {
		a.Line2 = *p.Line2
	}
	if p.PostalCode != nil {
		a.PostalCode = *p.PostalCode
	}
	return a
}

func (o *Organization) applyChanges(c ProfileChanges) {
	if c.LegalName != nil {
		o.legalName = LegalName(*c.LegalName)
	}
	if c.DisplayName != nil {
		o.displayName = DisplayName(*c.DisplayName)
	}
	if c.TaxID != nil {
		o.taxID = TaxID{Country: kernel.CountryCode(c.TaxID.Country), Value: c.TaxID.Value}
	}
	if c.Address != nil {
		o.address = addressFromPayload(*c.Address)
	}
	if c.Contact != nil {
		o.contact.Email = kernel.Email(c.Contact.Email)
		o.contact.Phone = kernel.Phone(c.Contact.Phone)
	}
	if c.Website != nil {
		o.contact.Website = *c.Website
	}
}

func (o *Organization) record(evt any) {
	if err := o.ApplyEvent(evt); err != nil {
		panic("partnernetwork: aplicar evento recién emitido: " + err.Error())
	}
	o.uncommitted = append(o.uncommitted, evt)
}
