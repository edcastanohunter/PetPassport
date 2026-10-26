package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

var (
	orgID   = uuid.MustParse("01930000-0000-7000-8000-0000000000f1")
	founder = uuid.MustParse("01930000-0000-7000-8000-0000000000aa")
	vet     = uuid.MustParse("01930000-0000-7000-8000-0000000000bb")
	admin2  = uuid.MustParse("01930000-0000-7000-8000-0000000000cc")
	nowOrg  = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	invA    = uuid.MustParse("01930000-0000-7000-8000-0000000000e1")
	invB    = uuid.MustParse("01930000-0000-7000-8000-0000000000e2")
)

func newOrg() *Organization { return &Organization{} }

func clinicRegistered() OrganizationRegistered {
	return OrganizationRegistered{
		OrganizationID: orgID, Type: OrgTypeClinic,
		LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxID:      TaxIDPayload{Country: "CO", Value: "900123456-8"},
		Address:    AddressPayload{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		Contact:    ContactPayload{Email: "contacto@clinicasur.co", Phone: "+573001234567"},
		PublicCode: "7K3MQ2", FounderID: founder,
	}
}

func registerCmd() RegisterOrganization {
	return RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900123456-8",
		Address:      AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567",
		FounderID: founder, Now: nowOrg,
	}
}

// --- O1: rol válido para el tipo ---------------------------------------------

func TestOrg_O1_RoleValidForType(t *testing.T) {
	given := []any{clinicRegistered()}
	// veterinarian válido en clínica (InvitationID lo genera app).
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "vet@sur.co", Role: "veterinarian", Now: nowOrg}).
		ThenAssert(func(t testing.TB, events []any) {
			if len(events) != 1 {
				t.Fatalf("eventos: %d", len(events))
			}
			e, ok := events[0].(MemberInvited)
			if !ok || e.Role != RoleVeterinarian || e.PII == nil || e.PII.Email != "vet@sur.co" ||
				!e.ExpiresAt.Equal(nowOrg.Add(InvitationTTL)) || e.InvitationID != invA {
				t.Fatalf("MemberInvited: %+v", events[0])
			}
		})
	// pharmacist NO válido en clínica.
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "f@sur.co", Role: "pharmacist", Now: nowOrg}).
		ThenError(ErrRoleNotAllowedForType)
	// clinic_admin NO válido en farmacia.
	farm := OrganizationRegistered{
		OrganizationID: orgID, Type: OrgTypePharmacy, LegalName: "Farmacia Central", DisplayName: "Farma Central",
		TaxID:      TaxIDPayload{Country: "CO", Value: "901000000-9"},
		Address:    AddressPayload{Country: "CO", City: "Bogotá", Line1: "Cra 7 # 1-1"},
		Contact:    ContactPayload{Email: "hola@farma.co", Phone: "+573100000000"},
		PublicCode: "0189AB", FounderID: founder,
	}
	testkit.Scenario[*Organization](t, newOrg).Given(farm).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "a@farma.co", Role: "clinic_admin", Now: nowOrg}).
		ThenError(ErrRoleNotAllowedForType)
}

// --- O2: siempre hay al menos un admin activo --------------------------------

func TestOrg_O2_LastAdminProtected(t *testing.T) {
	// Degradar al único admin: rechazado.
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(ChangeMemberRole{OrganizationID: orgID, UserID: founder, Role: "veterinarian"}).
		ThenError(ErrLastAdmin)
	// Retirar al único admin: rechazado.
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(RemoveMember{OrganizationID: orgID, UserID: founder}).
		ThenError(ErrLastAdmin)
	// Con un segundo admin: se puede degradar el primero.
	joined := []any{
		clinicRegistered(),
		MemberJoined{OrganizationID: orgID, UserID: admin2, Role: RoleClinicAdmin},
	}
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(ChangeMemberRole{OrganizationID: orgID, UserID: founder, Role: "veterinarian"}).
		Then(MemberRoleChanged{OrganizationID: orgID, UserID: founder, From: RoleClinicAdmin, To: RoleVeterinarian})
}

// --- O3: el tipo es inmutable -------------------------------------------------

func TestOrg_O3_TypeImmutable(t *testing.T) {
	// No existe comando que cambie el tipo: la prueba ejercita el replay y
	// verifica que los comandos de edición nunca alteran Type.
	s := testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(UpdateOrganizationProfile{OrganizationID: orgID, DisplayName: ptrStr("Clínica Norte"), Now: nowOrg}).
		Then(OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{DisplayName: ptrStr("Clínica Norte")}})
	if s.Agg().Type() != OrgTypeClinic {
		t.Fatalf("tipo cambió: %v", s.Agg().Type())
	}
}

// --- O4: máximo una invitación pendiente por email ----------------------------

func TestOrg_O4_SinglePendingInvitationPerEmail(t *testing.T) {
	first := MemberInvited{OrganizationID: orgID, InvitationID: invA,
		Role: RoleVeterinarian, ExpiresAt: nowOrg.Add(InvitationTTL), PII: &InvitationPII{Email: "vet@sur.co"}}
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "vet@sur.co", Role: "veterinarian", Now: nowOrg}).
		ThenAssert(func(t testing.TB, events []any) {
			if len(events) != 1 {
				t.Fatalf("eventos: %d", len(events))
			}
			e, ok := events[0].(MemberInvited)
			if !ok || e.Role != RoleVeterinarian || e.PII == nil || e.PII.Email != "vet@sur.co" {
				t.Fatalf("MemberInvited: %+v", events[0])
			}
		})
	// Segunda invitación al mismo email (normalizada): revoca la anterior y
	// emite una nueva (O4).
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered(), first).
		When(InviteMember{OrganizationID: orgID, InvitationID: invB, Email: "VET@sur.co", Role: "receptionist", Now: nowOrg}).
		ThenAssert(func(t testing.TB, events []any) {
			if len(events) != 2 {
				t.Fatalf("eventos: %d", len(events))
			}
			revoked, ok := events[0].(InvitationRevoked)
			if !ok || revoked.InvitationID != first.InvitationID {
				t.Fatalf("revocación: %+v", events[0])
			}
			inv, ok := events[1].(MemberInvited)
			if !ok || inv.Role != RoleReceptionist || inv.InvitationID != invB ||
				inv.PII == nil || inv.PII.Email != "vet@sur.co" {
				t.Fatalf("nueva invitación: %+v", events[1])
			}
		})
}

// --- O5: PublicCode único (reserva; formato y generación aquí) ----------------

func TestOrg_O5_PublicCodeFormat(t *testing.T) {
	for _, s := range []string{"7K3MQ2", "0189AB"} {
		if _, err := NewPublicCode(s); err != nil {
			t.Errorf("NewPublicCode(%q): %v", s, err)
		}
	}
	for _, s := range []string{"7K3MQ", "7K3MQ23", "7K3MQI", "7k3mq2 ", "7K3MQO"} {
		if _, err := NewPublicCode(s); err == nil {
			t.Errorf("NewPublicCode(%q) debe fallar", s)
		}
	}
	seen := map[PublicCode]bool{}
	for i := 0; i < 100; i++ {
		code, err := GeneratePublicCode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewPublicCode(string(code)); err != nil {
			t.Fatalf("código generado inválido: %q", code)
		}
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Fatalf("generación poco aleatoria: %d únicos de 100", len(seen))
	}
}

// Comportamiento adicional (PRD-011..014).

func TestOrg_RegisterValidation(t *testing.T) {
	bad := []RegisterOrganization{
		{Type: "hospital", LegalName: "X", DisplayName: "Y", TaxCountry: "CO", TaxValue: "900123456-8",
			Address: AddressInput{Country: "CO", City: "B", Line1: "C"}, ContactEmail: "a@b.co", ContactPhone: "+573000000000", FounderID: founder},
		{Type: "clinic", LegalName: "A", DisplayName: "B", TaxCountry: "CO", TaxValue: "900123456-8",
			Address: AddressInput{Country: "CO", City: "", Line1: "C"}, ContactEmail: "a@b.co", ContactPhone: "+573000000000", FounderID: founder},
		{Type: "clinic", LegalName: "A", DisplayName: "B", TaxCountry: "CO", TaxValue: "900123456-8",
			Address: AddressInput{Country: "CO", City: "B", Line1: "C"}, ContactEmail: "no-email", ContactPhone: "+573000000000", FounderID: founder},
	}
	for _, cmd := range bad {
		testkit.Scenario[*Organization](t, newOrg).When(cmd).ThenError(&kernel.ValidationError{})
	}
	// NIT con dígito de control inválido.
	testkit.Scenario[*Organization](t, newOrg).
		When(RegisterOrganization{Type: "clinic", LegalName: "A", DisplayName: "B", TaxCountry: "CO", TaxValue: "900123456-2",
			Address: AddressInput{Country: "CO", City: "B", Line1: "C"}, ContactEmail: "a@b.co", ContactPhone: "+573000000000", FounderID: founder}).
		ThenError(&kernel.ValidationError{})
}

func TestOrg_RegisterEmitsFounderAdmin(t *testing.T) {
	o := newOrg()
	o.SetPendingPublicCode("7K3MQ2")
	events, err := o.HandleCommand(registerCmd())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("eventos: %d", len(events))
	}
	reg := events[0].(OrganizationRegistered)
	join := events[1].(MemberJoined)
	if reg.PublicCode != "7K3MQ2" || reg.FounderID != founder || join.UserID != founder || join.Role != RoleClinicAdmin {
		t.Fatalf("registro: %+v %+v", reg, join)
	}
}

func TestOrg_UpdateProfileResetsVerification_PRD012(t *testing.T) {
	// Org activa: cambiar legal_name emite reset.
	active := []any{clinicRegistered(), OrganizationVerified{OrganizationID: orgID}}
	testkit.Scenario[*Organization](t, newOrg).Given(active...).
		When(UpdateOrganizationProfile{OrganizationID: orgID, LegalName: ptrStr("Clínica Sur S.A.S."), Now: nowOrg}).
		Then(
			OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{LegalName: ptrStr("Clínica Sur S.A.S.")}},
			OrganizationVerificationReset{OrganizationID: orgID, Cause: "legal_name_change"},
		)
	// Org pendiente: no hay reset.
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(UpdateOrganizationProfile{OrganizationID: orgID, LegalName: ptrStr("Clínica Sur S.A.S."), Now: nowOrg}).
		Then(OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{LegalName: ptrStr("Clínica Sur S.A.S.")}})
	// Sin cambios reales: sin evento.
	testkit.Scenario[*Organization](t, newOrg).Given(clinicRegistered()).
		When(UpdateOrganizationProfile{OrganizationID: orgID, DisplayName: ptrStr("Clínica Sur"), Now: nowOrg}).
		ThenNoEvents()
	// Rechazada: no editable.
	testkit.Scenario[*Organization](t, newOrg).
		Given(clinicRegistered(), OrganizationRejected{OrganizationID: orgID, Reason: "no existe"}).
		When(UpdateOrganizationProfile{OrganizationID: orgID, DisplayName: ptrStr("X"), Now: nowOrg}).
		ThenError(ErrOrgRejected)
}

func TestOrg_StatusTransitions(t *testing.T) {
	pending := []any{clinicRegistered()}
	// Motivo corto en reject: 422.
	testkit.Scenario[*Organization](t, newOrg).Given(pending...).
		When(RejectOrganization{OrganizationID: orgID, Reason: "corto"}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Organization](t, newOrg).Given(pending...).
		When(RejectOrganization{OrganizationID: orgID, Reason: "no pudimos verificar la existencia legal"}).
		Then(OrganizationRejected{OrganizationID: orgID, Reason: "no pudimos verificar la existencia legal"})
	// Suspend requiere activa; reactivate requiere suspendida.
	testkit.Scenario[*Organization](t, newOrg).Given(pending...).
		When(SuspendOrganization{OrganizationID: orgID, Reason: "motivo suficientemente largo"}).
		ThenError(ErrInvalidStatusTransition)
	activated := []any{clinicRegistered(), OrganizationVerified{OrganizationID: orgID}}
	testkit.Scenario[*Organization](t, newOrg).Given(activated...).
		When(SuspendOrganization{OrganizationID: orgID, Reason: "motivo suficientemente largo"}).
		Then(OrganizationSuspended{OrganizationID: orgID, Reason: "motivo suficientemente largo"})
	suspended := append(activated, OrganizationSuspended{OrganizationID: orgID, Reason: "motivo suficientemente largo"})
	testkit.Scenario[*Organization](t, newOrg).Given(suspended...).
		When(ReactivateOrganization{OrganizationID: orgID}).
		Then(OrganizationReactivated{OrganizationID: orgID})
	// Invitar en suspendida: rechazado.
	testkit.Scenario[*Organization](t, newOrg).Given(suspended...).
		When(InviteMember{OrganizationID: orgID, InvitationID: invA, Email: "x@sur.co", Role: "veterinarian", Now: nowOrg}).
		ThenError(ErrOrgSuspended)
	// Suspendida: el perfil sigue siendo editable (02 §4.1: solo rejected bloquea).
	testkit.Scenario[*Organization](t, newOrg).Given(suspended...).
		When(UpdateOrganizationProfile{OrganizationID: orgID, DisplayName: ptrStr("Clínica Norte"), Now: nowOrg}).
		Then(OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{DisplayName: ptrStr("Clínica Norte")}})
	// Verificar dos veces: transición inválida.
	testkit.Scenario[*Organization](t, newOrg).Given(activated...).
		When(VerifyOrganization{OrganizationID: orgID}).
		ThenError(ErrInvalidStatusTransition)
	// Comandos sobre org inexistente: 404.
	testkit.Scenario[*Organization](t, newOrg).
		When(VerifyOrganization{OrganizationID: orgID}).
		ThenError(&kernel.NotFoundError{})
}

func TestOrg_InvitationLifecycle_PRD013(t *testing.T) {
	invited := MemberInvited{OrganizationID: orgID, InvitationID: invA,
		Role: RoleVeterinarian, ExpiresAt: nowOrg.Add(InvitationTTL), PII: &InvitationPII{Email: "vet@sur.co"}}
	given := []any{clinicRegistered(), invited}
	accept := func(email string, verified bool, at time.Time) AcceptInvitation {
		return AcceptInvitation{OrganizationID: orgID, InvitationID: invA, UserID: vet, ActorEmail: email, EmailVerified: verified, Now: at}
	}

	// Aceptar con email equivocado.
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(accept("otro@sur.co", true, nowOrg)).ThenError(ErrInvitationEmailMismatch)
	// Sin email verificado.
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(accept("vet@sur.co", false, nowOrg)).ThenError(ErrEmailNotVerified)
	// Caducada (7 días).
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(accept("vet@sur.co", true, nowOrg.Add(InvitationTTL))).ThenError(ErrInvitationTokenInvalid)
	// Válida (email normalizado) → MemberJoined con invitation_id.
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(accept("VET@sur.co", true, nowOrg)).
		Then(MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleVeterinarian, InvitationID: invA})
	// Invitación inexistente → inválida (HTTP 404 sin distinguir).
	testkit.Scenario[*Organization](t, newOrg).Given(given...).
		When(AcceptInvitation{OrganizationID: orgID, InvitationID: invB, UserID: vet, ActorEmail: "vet@sur.co", EmailVerified: true, Now: nowOrg}).
		ThenError(ErrInvitationTokenInvalid)
	// Ya aceptada por el mismo usuario: idempotente.
	joined := append(append([]any{}, given...), MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleVeterinarian, InvitationID: invA})
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(accept("vet@sur.co", true, nowOrg)).ThenNoEvents()
	// Ya miembro: invitar de nuevo a su email → ALREADY_MEMBER.
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(InviteMember{OrganizationID: orgID, InvitationID: invB, Email: "vet@sur.co", Role: "receptionist", ExistingUserID: vet, Now: nowOrg}).
		ThenError(ErrAlreadyMember)
	// Revocar y luego aceptar: inválida.
	revoked := append(append([]any{}, given...), InvitationRevoked{OrganizationID: orgID, InvitationID: invA})
	testkit.Scenario[*Organization](t, newOrg).Given(revoked...).
		When(accept("vet@sur.co", true, nowOrg)).ThenError(ErrInvitationTokenInvalid)
	// Revocar dos veces: la segunda falla.
	testkit.Scenario[*Organization](t, newOrg).Given(revoked...).
		When(RevokeInvitation{OrganizationID: orgID, InvitationID: invA}).
		ThenError(ErrInvitationNotFound)
	// Pendiente con org suspendida: no se acepta.
	suspended := append(append([]any{}, given...), OrganizationVerified{OrganizationID: orgID},
		OrganizationSuspended{OrganizationID: orgID, Reason: "motivo suficientemente largo"})
	testkit.Scenario[*Organization](t, newOrg).Given(suspended...).
		When(accept("vet@sur.co", true, nowOrg)).ThenError(ErrInvitationTokenInvalid)
}

func TestOrg_RemoveAndRoleBranches_PRD014(t *testing.T) {
	joined := []any{
		clinicRegistered(),
		MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleVeterinarian},
	}
	// Rol inválido para el tipo.
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(ChangeMemberRole{OrganizationID: orgID, UserID: vet, Role: "pharmacist"}).
		ThenError(ErrRoleNotAllowedForType)
	// Rol igual: sin evento.
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(ChangeMemberRole{OrganizationID: orgID, UserID: vet, Role: "veterinarian"}).
		ThenNoEvents()
	// Miembro inexistente.
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(RemoveMember{OrganizationID: orgID, UserID: admin2}).
		ThenError(ErrMemberNotFound)
	// Retirar veterinario (no admin): OK.
	testkit.Scenario[*Organization](t, newOrg).Given(joined...).
		When(RemoveMember{OrganizationID: orgID, UserID: vet}).
		Then(MemberRemoved{OrganizationID: orgID, UserID: vet})
}

func TestOrg_Replay(t *testing.T) {
	o := newOrg()
	events := []any{
		clinicRegistered(),
		OrganizationVerified{OrganizationID: orgID},
		MemberJoined{OrganizationID: orgID, UserID: vet, Role: RoleVeterinarian},
		MemberRoleChanged{OrganizationID: orgID, UserID: vet, From: RoleVeterinarian, To: RoleReceptionist},
	}
	for _, e := range events {
		if err := o.ApplyEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	if o.ID() != orgID || o.Version() != 4 || o.Status() != OrgActive {
		t.Fatalf("estado: %+v", o)
	}
	if len(o.Members()) != 2 || o.ActiveAdminCount() != 1 {
		t.Fatalf("miembros: %+v", o.members)
	}
}

func TestOrg_TaxIDValidators_DOM007(t *testing.T) {
	valid := map[string][2]string{
		"NIT con DV":           {"CO", "900123456-8"},
		"NIT con puntos":       {"CO", "900.123.456-8"},
		"NIT DV por residuo 1": {"CO", "800000001-1"},
		"genérico":             {"MX", "ABC123456"},
		"genérico con guiones": {"ES", "B-12345678"},
	}
	for name, in := range valid {
		if _, err := NewTaxID(in[0], in[1]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	tax, _ := NewTaxID("co", " 900.123.456-8 ")
	if tax.Country != "CO" || tax.Value != "900123456-8" {
		t.Fatalf("normalización: %+v", tax)
	}
	invalidCases := map[string][2]string{
		"DV errado":         {"CO", "900123456-7"},
		"sin DV":            {"CO", "900123456"},
		"DV no numérico":    {"CO", "900123456-X"},
		"letras en base":    {"CO", "9001A3456-8"},
		"genérico corto":    {"MX", "AB1"},
		"genérico símbolos": {"MX", "ABC_12345"},
		"país inválido":     {"XXX", "ABC123456"},
	}
	for name, in := range invalidCases {
		if _, err := NewTaxID(in[0], in[1]); err == nil {
			t.Errorf("%s: debe fallar", name)
		}
	}
}

func TestOrg_ContactAndNames_DOM007(t *testing.T) {
	if _, err := NewOrgContact("A@B.co", "+57 300 123 4567", "https://sur.co"); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{{"no-email", "+573001234567", ""}, {"a@b.co", "123", ""}, {"a@b.co", "+573001234567", "http://inseguro.co"}} {
		if _, err := NewOrgContact(c[0], c[1], c[2]); err == nil {
			t.Errorf("contacto %v debe fallar", c)
		}
	}
	if _, err := NewLegalName("A"); err == nil {
		t.Error("legal_name de 1 carácter debe fallar")
	}
	long := make([]rune, 81)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := NewDisplayName(string(long)); err == nil {
		t.Error("display_name > 80 debe fallar")
	}
}

func TestOrg_ProfileContactKeepsWebsite(t *testing.T) {
	reg := clinicRegistered()
	reg.Contact.Website = "https://sur.co"
	s := testkit.Scenario[*Organization](t, newOrg).Given(reg).
		When(UpdateOrganizationProfile{OrganizationID: orgID, ContactPhone: ptrStr("+573009999999"), Now: nowOrg}).
		Then(OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{
			Contact: &ContactPayload{Email: "contacto@clinicasur.co", Phone: "+573009999999"},
		}})
	if s.Agg().contact.Website != "https://sur.co" {
		t.Fatalf("website perdido: %+v", s.Agg().contact)
	}
}

func ptrStr(s string) *string { return &s }
