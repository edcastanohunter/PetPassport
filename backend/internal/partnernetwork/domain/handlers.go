package domain

import (
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// HandleCommand ejecuta un comando y devuelve los eventos a persistir.
func (o *Organization) HandleCommand(cmd any) ([]any, error) {
	switch c := cmd.(type) {
	case RegisterOrganization:
		return o.handleRegister(c)
	case UpdateOrganizationProfile:
		return o.handleUpdateProfile(c)
	case VerifyOrganization:
		return o.handleTransition(c.OrganizationID, OrgPendingVerification, OrganizationVerified{
			OrganizationID: c.OrganizationID, Reason: strings.TrimSpace(c.Reason),
		}, true)
	case RejectOrganization:
		return o.handleTransition(c.OrganizationID, OrgPendingVerification, OrganizationRejected{
			OrganizationID: c.OrganizationID, Reason: strings.TrimSpace(c.Reason),
		}, false)
	case SuspendOrganization:
		return o.handleTransition(c.OrganizationID, OrgActive, OrganizationSuspended{
			OrganizationID: c.OrganizationID, Reason: strings.TrimSpace(c.Reason),
		}, false)
	case ReactivateOrganization:
		return o.handleTransition(c.OrganizationID, OrgSuspended, OrganizationReactivated{
			OrganizationID: c.OrganizationID, Reason: strings.TrimSpace(c.Reason),
		}, true)
	case InviteMember:
		return o.handleInvite(c)
	case RevokeInvitation:
		return o.handleRevokeInvitation(c)
	case AcceptInvitation:
		return o.handleAccept(c)
	case ChangeMemberRole:
		return o.handleRoleChange(c)
	case RemoveMember:
		return o.handleRemove(c)
	}
	return nil, &kernel.DomainError{Code: "UNKNOWN_COMMAND", Message: "comando desconocido para Organization"}
}

func (o *Organization) handleRegister(c RegisterOrganization) ([]any, error) {
	if o.version != 0 {
		return nil, kernel.NewDomainError("ORGANIZATION_ALREADY_REGISTERED", "La organización ya está registrada.")
	}
	ve := &kernel.ValidationError{}
	orgType, err := NewOrganizationType(c.Type)
	if err != nil {
		ve.Add("type", "INVALID")
	}
	legal, err := NewLegalName(c.LegalName)
	if err != nil {
		ve.Add("legal_name", "INVALID")
	}
	display, err := NewDisplayName(c.DisplayName)
	if err != nil {
		ve.Add("display_name", "INVALID")
	}
	tax, err := NewTaxID(c.TaxCountry, c.TaxValue)
	if err != nil {
		return nil, err // ya trae el FieldViolation correcto
	}
	addr, err := newAddress(c.Address)
	if err != nil {
		return nil, err
	}
	contact, err := NewOrgContact(c.ContactEmail, c.ContactPhone, c.Website)
	if err != nil {
		return nil, err // ValidationError por campo
	}
	if c.FounderID == uuid.Nil {
		ve.Add("founder_id", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	id := c.OrganizationID
	if id == uuid.Nil {
		id = kernel.NewID()
	}
	p := AddressPayload{Country: string(addr.Country), City: addr.City, Line1: addr.Line1}
	if addr.Region != "" {
		r := addr.Region
		p.Region = &r
	}
	if addr.Line2 != "" {
		l2 := addr.Line2
		p.Line2 = &l2
	}
	if addr.PostalCode != "" {
		pc := addr.PostalCode
		p.PostalCode = &pc
	}
	o.record(OrganizationRegistered{
		OrganizationID: id, Type: orgType, LegalName: string(legal), DisplayName: string(display),
		TaxID:      TaxIDPayload{Country: string(tax.Country), Value: tax.Value},
		Address:    p,
		Contact:    ContactPayload{Email: string(contact.Email), Phone: string(contact.Phone), Website: contact.Website},
		PublicCode: o.publicCodeForRegistration(), FounderID: c.FounderID,
	})
	// El fundador entra como admin (O2 se cumple desde el registro).
	o.record(MemberJoined{OrganizationID: id, UserID: c.FounderID, Role: adminRoleFor(orgType)})
	return o.uncommitted, nil
}

// publicCodeForRegistration devuelve el código fijado por app (reserva O5).
func (o *Organization) publicCodeForRegistration() string {
	return o.pendingPublicCode
}

func (o *Organization) handleUpdateProfile(c UpdateOrganizationProfile) ([]any, error) {
	if o.version == 0 {
		return nil, &kernel.NotFoundError{Resource: "organization"}
	}
	if o.status == OrgRejected {
		return nil, ErrOrgRejected
	}
	ve := &kernel.ValidationError{}
	ch := ProfileChanges{}
	if c.LegalName != nil {
		ln, err := NewLegalName(*c.LegalName)
		if err != nil {
			ve.Add("legal_name", "INVALID")
		} else if string(ln) != string(o.legalName) {
			v := string(ln)
			ch.LegalName = &v
		}
	}
	if c.DisplayName != nil {
		dn, err := NewDisplayName(*c.DisplayName)
		if err != nil {
			ve.Add("display_name", "INVALID")
		} else if string(dn) != string(o.displayName) {
			v := string(dn)
			ch.DisplayName = &v
		}
	}
	if c.TaxCountry != nil || c.TaxValue != nil {
		country := string(o.taxID.Country)
		value := o.taxID.Value
		if c.TaxCountry != nil {
			country = *c.TaxCountry
		}
		if c.TaxValue != nil {
			value = *c.TaxValue
		}
		t, err := NewTaxID(country, value)
		if err != nil {
			return nil, err
		}
		if t != o.taxID {
			ch.TaxID = &TaxIDPayload{Country: string(t.Country), Value: t.Value}
		}
	}
	if c.Address != nil {
		addr, err := newAddress(*c.Address)
		if err != nil {
			return nil, err
		}
		if addr != o.address {
			ch.Address = &AddressPayload{Country: string(addr.Country), City: addr.City, Line1: addr.Line1}
			if addr.Region != "" {
				r := addr.Region
				ch.Address.Region = &r
			}
			if addr.Line2 != "" {
				l2 := addr.Line2
				ch.Address.Line2 = &l2
			}
			if addr.PostalCode != "" {
				pc := addr.PostalCode
				ch.Address.PostalCode = &pc
			}
		}
	}
	if c.ContactEmail != nil || c.ContactPhone != nil {
		email := string(o.contact.Email)
		phone := string(o.contact.Phone)
		if c.ContactEmail != nil {
			email = *c.ContactEmail
		}
		if c.ContactPhone != nil {
			phone = *c.ContactPhone
		}
		con, err := NewOrgContact(email, phone, o.contact.Website)
		if err != nil {
			return nil, err
		}
		if con.Email != o.contact.Email || con.Phone != o.contact.Phone {
			ch.Contact = &ContactPayload{Email: string(con.Email), Phone: string(con.Phone)}
		}
	}
	if c.Website != nil {
		w := strings.TrimSpace(*c.Website)
		con, err := NewOrgContact(string(o.contact.Email), string(o.contact.Phone), w)
		if err != nil {
			return nil, err
		}
		if con.Website != o.contact.Website {
			ch.Website = &con.Website
		}
	}
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if ch == (ProfileChanges{}) {
		return nil, nil
	}
	o.record(OrganizationProfileUpdated{OrganizationID: o.id, Changes: ch})
	// PRD-012: cambiar razón social o identificación fiscal en una org activa
	// la devuelve a pending_verification.
	if o.status == OrgActive && (ch.LegalName != nil || ch.TaxID != nil) {
		cause := "profile_change"
		if ch.TaxID != nil {
			cause = "tax_id_change"
		} else if ch.LegalName != nil {
			cause = "legal_name_change"
		}
		o.record(OrganizationVerificationReset{OrganizationID: o.id, Cause: cause})
	}
	return o.uncommitted, nil
}

// handleTransition aplica verify/reject/suspend/reactivate con su precondición.
func (o *Organization) handleTransition(orgID uuid.UUID, want OrgStatus, evt any, reasonOptional bool) ([]any, error) {
	if o.version == 0 {
		return nil, &kernel.NotFoundError{Resource: "organization"}
	}
	if o.status != want {
		return nil, ErrInvalidStatusTransition
	}
	if !reasonOptional {
		switch e := evt.(type) {
		case OrganizationRejected:
			if len([]rune(e.Reason)) < 10 {
				return nil, reasonTooShort("reason")
			}
		case OrganizationSuspended:
			if len([]rune(e.Reason)) < 10 {
				return nil, reasonTooShort("reason")
			}
		}
	}
	o.record(evt)
	return o.uncommitted, nil
}

func reasonTooShort(field string) error {
	return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: field, Code: "TOO_SHORT"}}}
}

func (o *Organization) handleInvite(c InviteMember) ([]any, error) {
	if o.version == 0 {
		return nil, &kernel.NotFoundError{Resource: "organization"}
	}
	if o.status == OrgRejected {
		return nil, ErrOrgRejected
	}
	if o.status == OrgSuspended {
		return nil, ErrOrgSuspended
	}
	email, err := kernel.NewEmail(c.Email)
	if err != nil {
		return nil, invalid("email")
	}
	role := OrgRole(strings.TrimSpace(c.Role))
	if !ValidRoleForType(o.orgType, role) { // O1
		return nil, ErrRoleNotAllowedForType
	}
	if m, ok := o.members[c.ExistingUserID]; ok && c.ExistingUserID != uuid.Nil && m.Status == MemberStatusActive {
		return nil, ErrAlreadyMember
	}
	if c.InvitationID == uuid.Nil {
		return nil, invalid("invitation_id")
	}
	// O4: como máximo una invitación pendiente por email; si existe, se
	// revoca y se re-emite (PRD-013).
	for _, inv := range o.invitations {
		if inv.Status == InvitationStatusPending && inv.Email == email {
			o.record(InvitationRevoked{OrganizationID: o.id, InvitationID: inv.ID})
		}
	}
	o.record(MemberInvited{
		OrganizationID: o.id, InvitationID: c.InvitationID, Role: role, ExpiresAt: c.Now.Add(InvitationTTL),
		PII: &InvitationPII{Email: email},
	})
	return o.uncommitted, nil
}

func (o *Organization) handleRevokeInvitation(c RevokeInvitation) ([]any, error) {
	inv, ok := o.invitations[c.InvitationID]
	if !ok || inv.Status != InvitationStatusPending {
		return nil, ErrInvitationNotFound
	}
	o.record(InvitationRevoked{OrganizationID: o.id, InvitationID: c.InvitationID})
	return o.uncommitted, nil
}

func (o *Organization) handleAccept(c AcceptInvitation) ([]any, error) {
	inv, ok := o.invitations[c.InvitationID]
	if !ok {
		return nil, ErrInvitationTokenInvalid
	}
	// Aceptar dos veces es idempotente (mismo usuario, ya miembro activo).
	if m, member := o.members[c.UserID]; member && m.Status == MemberStatusActive && inv.Status == InvitationStatusAccepted {
		return nil, nil
	}
	if inv.Status != InvitationStatusPending || !c.Now.Before(inv.ExpiresAt) {
		return nil, ErrInvitationTokenInvalid
	}
	actor, err := kernel.NewEmail(c.ActorEmail)
	if err != nil || actor != inv.Email {
		return nil, ErrInvitationEmailMismatch
	}
	if !c.EmailVerified {
		return nil, ErrEmailNotVerified
	}
	if o.status == OrgRejected || o.status == OrgSuspended {
		return nil, ErrInvitationTokenInvalid
	}
	if m, member := o.members[c.UserID]; member && m.Status == MemberStatusActive {
		return nil, ErrAlreadyMember
	}
	o.record(MemberJoined{OrganizationID: o.id, UserID: c.UserID, Role: inv.Role, InvitationID: inv.ID})
	return o.uncommitted, nil
}

func (o *Organization) handleRoleChange(c ChangeMemberRole) ([]any, error) {
	m, ok := o.members[c.UserID]
	if !ok || m.Status != MemberStatusActive {
		return nil, ErrMemberNotFound
	}
	role := OrgRole(strings.TrimSpace(c.Role))
	if !ValidRoleForType(o.orgType, role) { // O1
		return nil, ErrRoleNotAllowedForType
	}
	if role == m.Role {
		return nil, nil
	}
	if IsAdminRole(m.Role) && !IsAdminRole(role) && o.ActiveAdminCount() <= 1 { // O2
		return nil, ErrLastAdmin
	}
	o.record(MemberRoleChanged{OrganizationID: o.id, UserID: c.UserID, From: m.Role, To: role})
	return o.uncommitted, nil
}

func (o *Organization) handleRemove(c RemoveMember) ([]any, error) {
	m, ok := o.members[c.UserID]
	if !ok || m.Status != MemberStatusActive {
		return nil, ErrMemberNotFound
	}
	if IsAdminRole(m.Role) && o.ActiveAdminCount() <= 1 { // O2
		return nil, ErrLastAdmin
	}
	o.record(MemberRemoved{OrganizationID: o.id, UserID: c.UserID, Reason: strings.TrimSpace(c.Reason)})
	return o.uncommitted, nil
}

// SetPendingPublicCode fija el código para el registro (reservado por app, O5).
func (o *Organization) SetPendingPublicCode(code PublicCode) { o.pendingPublicCode = string(code) }
