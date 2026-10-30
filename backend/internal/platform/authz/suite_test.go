package authz

import (
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Suite por tabla de SEC-005 a nivel de decisión: cada Action × persona ×
// relación (tutor / vinculado / no vinculado / org suspendida). Falla si una
// acción con política no tiene fila: 100 % de las acciones existentes. La
// traducción a HTTP (403/404) se prueba por rutas en cmd/api.

var (
	sTutor   = uuid.MustParse("01930000-0000-7000-8000-00000000a001")
	sPet     = uuid.MustParse("01930000-0000-7000-8000-00000000b001")
	sLinked  = uuid.MustParse("01930000-0000-7000-8000-00000000c001")
	sSusp    = uuid.MustParse("01930000-0000-7000-8000-00000000c002")
	sOther   = uuid.MustParse("01930000-0000-7000-8000-00000000c003")
	sPharma  = uuid.MustParse("01930000-0000-7000-8000-00000000c004")
	sUnknown = uuid.MustParse("01930000-0000-7000-8000-00000000c005")
)

type suitePersona struct {
	name string
	p    auth.Principal
	org  uuid.UUID // org sobre la que actúa en acciones org:*
}

func mem(org uuid.UUID, typ, role, status string) auth.Membership {
	return auth.Membership{OrganizationID: org, OrgType: typ, Role: role, OrgStatus: status}
}

func sp(name string, org uuid.UUID, roles []string, ms ...auth.Membership) suitePersona {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
	if name == "tutor" {
		id = sTutor
	}
	return suitePersona{name: name, org: org, p: auth.Principal{UserID: id, GlobalRoles: roles, Memberships: ms}}
}

var suitePersonas = []suitePersona{
	sp("tutor", sLinked, []string{"pet_owner"}),
	sp("vet_linked", sLinked, nil, mem(sLinked, "clinic", "veterinarian", "active")),
	sp("admin_linked", sLinked, nil, mem(sLinked, "clinic", "clinic_admin", "active")),
	sp("recep_linked", sLinked, nil, mem(sLinked, "clinic", "receptionist", "active")),
	sp("vet_unlinked", sLinked, nil, mem(sOther, "clinic", "veterinarian", "active")),
	sp("vet_suspended", sSusp, nil, mem(sSusp, "clinic", "veterinarian", "suspended")),
	sp("pharmacy_admin", sPharma, nil, mem(sPharma, "pharmacy", "pharmacy_admin", "active")),
	sp("pharmacist", sPharma, nil, mem(sPharma, "pharmacy", "pharmacist", "active")),
	sp("pending_admin", sUnknown, nil, mem(sUnknown, "clinic", "clinic_admin", "pending_verification")),
	sp("stranger", sLinked, []string{"pet_owner"}),
	sp("super_admin", sLinked, []string{"super_admin"}),
}

// La mascota está vinculada a sLinked y a sSusp (org suspendida).
var suitePet = PetResource{ID: sPet, GuardianID: sTutor, ActiveCareLinkOrgIDs: []uuid.UUID{sLinked, sSusp}}

// Recursos de Clinical Records (SEC-001): el mismo encuentro en borrador,
// firmado y en custodia (vínculo revocado, se conserva lo firmado).
var (
	sVetLinked  = uuid.NewSHA1(uuid.NameSpaceOID, []byte("vet_linked"))
	sEncDraft   = uuid.MustParse("01930000-0000-7000-8000-00000000e001")
	sEncSigned  = uuid.MustParse("01930000-0000-7000-8000-00000000e002")
	sEncCustody = uuid.MustParse("01930000-0000-7000-8000-00000000e003")
)

func encRes(id uuid.UUID, org uuid.UUID, status string, linked bool) EncounterResource {
	return EncounterResource{
		ID: id, PetID: sPet, OrganizationID: org, GuardianID: sTutor,
		AttendingVetID: &sVetLinked, Status: status, PetLinked: linked,
	}
}

func encDraftRes(suitePersona) Resource  { return encRes(sEncDraft, sLinked, "draft", true) }
func encSignedRes(suitePersona) Resource { return encRes(sEncSigned, sLinked, "signed", true) }
func encNotLinkedRes(suitePersona) Resource {
	return encRes(sEncDraft, sLinked, "draft", false)
}
func encCustodyRes(suitePersona) Resource { return encRes(sEncCustody, sOther, "signed", false) }

// Consulta firmada por sOther sobre una mascota vinculada a sLinked: las
// clínicas con vínculo activo leen las firmadas de otras clínicas (PRD-009).
func encOtherClinicRes(suitePersona) Resource {
	r := encRes(sEncCustody, sOther, "signed", false)
	r.LinkedOrgIDs = []uuid.UUID{sLinked, sSusp}
	return r
}

func recordRes(suitePersona) Resource {
	return PetRecordResource{PetID: sPet, GuardianID: sTutor, LinkedOrgIDs: []uuid.UUID{sLinked, sSusp}}
}

func recordCustodyRes(suitePersona) Resource {
	return PetRecordResource{
		PetID: sPet, GuardianID: sTutor,
		LinkedOrgIDs: []uuid.UUID{sLinked, sSusp}, CustodyOrgIDs: []uuid.UUID{sOther},
	}
}

// Y = permitido, N = denegado. Columnas en el orden de suitePersonas.
const (
	Y = true
	N = false
)

type suiteRow struct {
	resource func(sp suitePersona) Resource
	want     [11]bool
}

func petRes(suitePersona) Resource { return suitePet }
func noRes(suitePersona) Resource  { return nil }
func orgRes(s suitePersona) Resource {
	return OrgResource{ID: s.org}
}
func orgMembersRes(s suitePersona) Resource {
	return OrgResource{ID: s.org, MembersOnly: true}
}

var suite = map[Action][]suiteRow{
	//                                tutor vetL admL recL vetU vetS phAd phar pend  str  sup
	MeRead:             {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	MeUpdate:           {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OnboardingComplete: {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	PetRegister:        {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	PetRead:            {{petRes, [11]bool{Y, Y, Y, Y, N, N, N, N, N, N, Y}}},
	PetGuardianContact: {{petRes, [11]bool{Y, Y, Y, Y, N, N, N, N, N, N, Y}}},
	PetUpdate:          {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetMicrochip:       {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetPhoto:           {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetDeceased:        {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetTransfer:        {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetCareLinkGrant:   {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetCareLinkRevoke:  {{petRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, Y}}},
	PetTransferRead:    {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	PetTransferAccept:  {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OrgRegister:        {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OrgLookup:          {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OrgInvitationRead:  {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OrgInvitationJoin:  {{noRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}}},
	OrgRead: {
		{orgRes, [11]bool{Y, Y, Y, Y, Y, Y, Y, Y, Y, Y, Y}},        // vista pública mínima
		{orgMembersRes, [11]bool{N, Y, Y, Y, N, Y, Y, Y, Y, N, Y}}, // miembros
	},
	OrgUpdate:       {{orgRes, [11]bool{N, N, Y, N, N, N, Y, N, Y, N, Y}}},
	OrgMemberInvite: {{orgRes, [11]bool{N, N, Y, N, N, N, Y, N, Y, N, Y}}},
	OrgMemberRole:   {{orgRes, [11]bool{N, N, Y, N, N, N, Y, N, Y, N, Y}}},
	OrgMemberRemove: {{orgRes, [11]bool{N, N, Y, N, N, N, Y, N, Y, N, Y}}},
	OrgPatientsList: {{orgRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, Y}}},

	// Administración de plataforma (API-008): solo super_admin.
	AdminOrgList:        {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminOrgVerify:      {{orgRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminOrgReject:      {{orgRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminOrgSuspend:     {{orgRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminOrgReactivate:  {{orgRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminUserList:       {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminUserSuspend:    {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminUserReactivate: {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminAuditRead:      {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},
	AdminCatalogWrite:   {{noRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}}},

	// Clinical Records (SEC-001, docs/phase-2/08 §2). tutor, vetL, admL,
	// recL, vetU, vetS, phAd, phar, pend, stranger, super.
	RecordRead: {
		{recordRes, [11]bool{Y, Y, Y, Y, N, N, N, N, N, N, Y}},
		// La custodia (vínculo revocado) conserva lectura de lo propio.
		{recordCustodyRes, [11]bool{Y, Y, Y, Y, Y, N, N, N, N, N, Y}},
	},
	EncounterList: {{encDraftRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, Y}}},
	EncounterOpen: {
		{encDraftRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, N}},
		{encNotLinkedRes, [11]bool{N, N, N, N, N, N, N, N, N, N, N}},
	},
	EncounterRead: {
		{encDraftRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, Y}},
		{encSignedRes, [11]bool{Y, Y, Y, Y, N, N, N, N, N, N, Y}},
		// Otra clínica no ve el encuentro aunque esté firmado.
		{encCustodyRes, [11]bool{Y, N, N, N, Y, N, N, N, N, N, Y}},
		{encOtherClinicRes, [11]bool{Y, Y, Y, Y, Y, N, N, N, N, N, Y}},
	},
	EncounterEdit: {
		{encDraftRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, N}},
		{encSignedRes, [11]bool{N, N, N, N, N, N, N, N, N, N, N}},
	},
	EncounterClinicalEdit: {
		{encDraftRes, [11]bool{N, Y, N, N, N, N, N, N, N, N, N}},
		{encSignedRes, [11]bool{N, N, N, N, N, N, N, N, N, N, N}},
	},
	EncounterSign: {
		{encDraftRes, [11]bool{N, Y, N, N, N, N, N, N, N, N, N}},
		{encSignedRes, [11]bool{N, Y, N, N, N, N, N, N, N, N, N}},
	},
	EncounterDiscard: {
		{encDraftRes, [11]bool{N, Y, Y, N, N, N, N, N, N, N, Y}},
		{encSignedRes, [11]bool{N, N, N, N, N, N, N, N, N, N, Y}},
	},
	// Carnet de vacunación: la custodia no escribe; el tutor declara y comparte.
	CardRecord:  {{recordCustodyRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, N}}},
	CardVerify:  {{recordCustodyRes, [11]bool{N, Y, Y, Y, N, N, N, N, N, N, N}}},
	CardVoid:    {{recordCustodyRes, [11]bool{Y, Y, Y, Y, N, N, N, N, N, N, Y}}},
	CardDeclare: {{recordCustodyRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, N}}},
	ShareManage: {{recordCustodyRes, [11]bool{Y, N, N, N, N, N, N, N, N, N, N}}},
}

func TestSuite_EveryActionHasRows(t *testing.T) {
	for _, a := range Actions() {
		if _, ok := suite[a]; !ok {
			t.Errorf("acción %q sin filas en la suite de autorización (SEC-005)", a)
		}
	}
}

func TestSuite_Matrix(t *testing.T) {
	for action, rows := range suite {
		for ri, row := range rows {
			for i, who := range suitePersonas {
				d := Can(who.p, action, row.resource(who))
				if d.Allow != row.want[i] {
					t.Errorf("%s[fila %d] como %s: allow=%v (%s), esperaba %v", action, ri, who.name, d.Allow, d.Reason, row.want[i])
				}
			}
		}
	}
}

func TestSuite_DeniesWithoutPrincipalOrSuspendedAccount(t *testing.T) {
	suspended := auth.Principal{UserID: sTutor, Account: auth.AccountState{Suspended: true}, GlobalRoles: []string{"super_admin"}}
	for action, rows := range suite {
		res := rows[0].resource(suitePersonas[0])
		if Can(auth.Principal{}, action, res).Allow {
			t.Errorf("%s sin principal debe denegar", action)
		}
		if Can(suspended, action, res).Allow {
			t.Errorf("%s con cuenta suspendida debe denegar", action)
		}
	}
}

func TestSuite_SuspendedOrgReason(t *testing.T) {
	d := Can(suitePersonas[5].p, OrgPatientsList, OrgResource{ID: sSusp})
	if d.Allow || d.Reason != ReasonOrgSuspended {
		t.Fatalf("org suspendida: %+v", d)
	}
}

func TestSuite_WrongResourceTypeDenies(t *testing.T) {
	super := suitePersonas[len(suitePersonas)-1].p
	for _, a := range []Action{PetRead, PetUpdate, OrgRead, OrgUpdate, OrgPatientsList} {
		if Can(super, a, "no es un recurso").Allow {
			t.Errorf("%s con recurso de tipo incorrecto debe denegar", a)
		}
	}
	if Can(super, OrgUpdate, OrgResource{}).Allow {
		t.Error("OrgResource sin ID debe denegar")
	}
}
