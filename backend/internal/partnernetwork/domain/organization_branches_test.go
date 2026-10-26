package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

// Ramas adicionales del agregado (cobertura de dominio ≥ 90 %, QA-002).

func TestOrg_CommandNamesAndSchemaVersions(t *testing.T) {
	cmds := []interface{ CommandName() string }{
		RegisterOrganization{}, UpdateOrganizationProfile{}, VerifyOrganization{}, RejectOrganization{},
		SuspendOrganization{}, ReactivateOrganization{}, InviteMember{}, RevokeInvitation{},
		AcceptInvitation{}, ChangeMemberRole{}, RemoveMember{},
	}
	seen := map[string]bool{}
	for _, c := range cmds {
		n := c.CommandName()
		if !strings.HasPrefix(n, "partnernetwork.") || seen[n] {
			t.Errorf("nombre de comando inválido o repetido: %q", n)
		}
		seen[n] = true
	}
	events := []interface {
		EventName() string
		SchemaVersion() int
	}{
		OrganizationRegistered{}, OrganizationProfileUpdated{}, OrganizationVerificationReset{}, OrganizationVerified{},
		OrganizationRejected{}, OrganizationSuspended{}, OrganizationReactivated{}, MemberInvited{},
		InvitationRevoked{}, MemberJoined{}, MemberRoleChanged{}, MemberRemoved{},
	}
	for _, e := range events {
		if e.SchemaVersion() != 1 || e.EventName() == "" {
			t.Errorf("%T: schema_version=%d", e, e.SchemaVersion())
		}
	}
	if (MemberInvited{}).PIISubjects() != nil {
		t.Error("MemberInvited no tiene sujeto con UserID (el invitado se identifica por email)")
	}
}

func TestOrg_Accessors(t *testing.T) {
	inv := MemberInvited{OrganizationID: orgID, InvitationID: invA, Role: RoleVeterinarian,
		ExpiresAt: nowOrg.Add(InvitationTTL), PII: &InvitationPII{Email: "vet@sur.co"}}
	s := testkit.Scenario[*Organization](t, newOrg).
		Given(clinicRegistered(), inv, MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleVeterinarian}).
		When(RemoveMember{OrganizationID: orgID, UserID: vet}).
		Then(MemberRemoved{OrganizationID: orgID, UserID: vet})
	o := s.Agg()
	if o.PublicCode() != "7K3MQ2" || o.DisplayName() != "Clínica Sur" || o.ContactEmail() != "contacto@clinicasur.co" || o.FounderID() != founder {
		t.Fatalf("accesores: %+v", o)
	}
	if got := o.PendingInvitations(); len(got) != 1 || got[0].ID != invA {
		t.Fatalf("pendientes: %+v", got)
	}
	if i, ok := o.Invitation(invA); !ok || i.Email != "vet@sur.co" {
		t.Fatalf("Invitation: %+v", i)
	}
	if ids := o.MemberIDs(); len(ids) != 1 || ids[0] != founder {
		t.Fatalf("MemberIDs tras retirar: %v", ids)
	}
}

func TestOrg_UpdateProfileBranches(t *testing.T) {
	active := []any{clinicRegistered(), OrganizationVerified{OrganizationID: orgID}}
	// Cambio de tax_id en activa → reset con causa tax_id_change.
	testkit.Scenario[*Organization](t, newOrg).Given(active...).
		When(UpdateOrganizationProfile{OrganizationID: orgID, TaxValue: ptrStr("800000001-1"), Now: nowOrg}).
		Then(
			OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{TaxID: &TaxIDPayload{Country: "CO", Value: "800000001-1"}}},
			OrganizationVerificationReset{OrganizationID: orgID, Cause: "tax_id_change"},
		)
	// Dirección, contacto y web: sin reset (no son datos legales).
	region, line2, postal := "Antioquia", "Of. 201", "050021"
	s := testkit.Scenario[*Organization](t, newOrg).Given(active...).
		When(UpdateOrganizationProfile{OrganizationID: orgID, Now: nowOrg,
			Address:      &AddressInput{Country: "CO", Region: &region, City: "Envigado", Line1: "Cra 43A # 1-50", Line2: &line2, PostalCode: &postal},
			ContactEmail: ptrStr("Hola@Sur.co"), Website: ptrStr("https://sur.co")})
	s.ThenAssert(func(t testing.TB, events []any) {
		if len(events) != 1 {
			t.Fatalf("no debe reiniciar la verificación: %d eventos", len(events))
		}
		ch := events[0].(OrganizationProfileUpdated).Changes
		if ch.Address == nil || ch.Address.City != "Envigado" || *ch.Address.Region != "Antioquia" ||
			ch.Contact == nil || ch.Contact.Email != "hola@sur.co" || ch.Website == nil || *ch.Website != "https://sur.co" {
			t.Fatalf("cambios: %+v", ch)
		}
	})
	if a := s.Agg(); a.address.City != "Envigado" || a.address.PostalCode != "050021" || a.contact.Website != "https://sur.co" {
		t.Fatalf("estado tras replay: %+v %+v", a.address, a.contact)
	}
	// Validaciones por campo.
	for _, c := range []UpdateOrganizationProfile{
		{OrganizationID: orgID, LegalName: ptrStr("X")},
		{OrganizationID: orgID, DisplayName: ptrStr("Y")},
		{OrganizationID: orgID, TaxValue: ptrStr("900123456-7")},
		{OrganizationID: orgID, Address: &AddressInput{Country: "CO", City: "", Line1: "x"}},
		{OrganizationID: orgID, ContactPhone: ptrStr("123")},
		{OrganizationID: orgID, Website: ptrStr("http://inseguro.co")},
	} {
		testkit.Scenario[*Organization](t, newOrg).Given(active...).When(c).ThenError(&kernel.ValidationError{})
	}
	// Org inexistente.
	testkit.Scenario[*Organization](t, newOrg).
		When(UpdateOrganizationProfile{OrganizationID: orgID, DisplayName: ptrStr("Nueva")}).
		ThenError(&kernel.NotFoundError{})
}

func TestOrg_RegisterBranches(t *testing.T) {
	// Registrar dos veces: rechazado.
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).When(registerCmd()).
		ThenError(&kernel.DomainError{Code: "ORGANIZATION_ALREADY_REGISTERED"})
	// Farmacia: el fundador entra como pharmacy_admin; dirección completa.
	region, line2, postal := "Cundinamarca", "Local 2", "110111"
	cmd := registerCmd()
	cmd.Type = "Pharmacy"
	cmd.TaxCountry, cmd.TaxValue = "MX", "abc123456"
	cmd.Address = AddressInput{Country: "MX", Region: &region, City: "CDMX", Line1: "Av. 1", Line2: &line2, PostalCode: &postal}
	o := newOrg()
	events, err := o.HandleCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	reg := events[0].(OrganizationRegistered)
	if reg.Type != OrgTypePharmacy || reg.TaxID.Value != "ABC123456" || *reg.Address.Line2 != "Local 2" ||
		events[1].(MemberJoined).Role != RolePharmacyAdmin {
		t.Fatalf("farmacia: %+v %+v", reg, events[1])
	}
	// Sin fundador o con dirección inválida.
	bad := registerCmd()
	bad.FounderID = uuid.Nil
	testkit.Scenario[*Organization](t, newOrg).When(bad).ThenError(&kernel.ValidationError{})
	bad = registerCmd()
	long := strings.Repeat("r", 121)
	bad.Address.Region = &long
	testkit.Scenario[*Organization](t, newOrg).When(bad).ThenError(&kernel.ValidationError{})
}

func TestOrg_InviteAndRevokeBranches(t *testing.T) {
	testkit.Scenario[*Organization](t, newOrg).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "a@b.co", Role: "veterinarian", Now: nowOrg}).
		ThenError(&kernel.NotFoundError{})
	rejected := []any{clinicRegistered(), OrganizationRejected{OrganizationID: orgID, Reason: "no verificable"}}
	testkit.Scenario[*Organization](t, newOrg).Given(rejected...).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "a@b.co", Role: "veterinarian", Now: nowOrg}).
		ThenError(ErrOrgRejected)
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "no-email", Role: "veterinarian", Now: nowOrg}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(InviteMember{OrganizationID: orgID, Email: "a@b.co", Role: "veterinarian", Now: nowOrg}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(RevokeInvitation{OrganizationID: orgID, InvitationID: invB}).
		ThenError(ErrInvitationNotFound)
	inv := MemberInvited{OrganizationID: orgID, InvitationID: invA, Role: RoleVeterinarian,
		ExpiresAt: nowOrg.Add(InvitationTTL), PII: &InvitationPII{Email: "vet@sur.co"}}
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered(), inv).
		When(RevokeInvitation{OrganizationID: orgID, InvitationID: invA}).
		Then(InvitationRevoked{OrganizationID: orgID, InvitationID: invA})
	// Un miembro ya activo no puede aceptar otra invitación de la misma org.
	joined := []any{clinicRegistered(), inv, MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleReceptionist}}
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(AcceptInvitation{OrganizationID: orgID, InvitationID: invA, UserID: vet, ActorEmail: "vet@sur.co", EmailVerified: true, Now: nowOrg}).
		ThenError(ErrAlreadyMember)
}

func TestOrg_UnknownCommandAndEvent(t *testing.T) {
	o := newOrg()
	if _, err := o.HandleCommand(struct{}{}); err == nil {
		t.Fatal("comando desconocido debe fallar")
	}
	if err := o.ApplyEvent(struct{}{}); err == nil {
		t.Fatal("evento desconocido debe fallar")
	}
	o = newOrg()
	_ = o.ApplyEvent(clinicRegistered())
	if err := o.ApplyEvent(MemberInvited{OrganizationID: orgID, InvitationID: invA}); err == nil {
		t.Fatal("MemberInvited sin pii debe fallar en el replay")
	}
	events, _ := newOrg().HandleCommand(registerCmd())
	o = newOrg()
	for _, e := range events {
		_ = o.ApplyEvent(e)
	}
	if len(o.UncommittedEvents()) != 0 {
		t.Fatal("replay no deja eventos sin confirmar")
	}
	o.MarkCommitted()
}
