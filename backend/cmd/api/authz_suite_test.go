package main

// Suite de autorización por rutas (PP-F1-SEC-005, quality gate):
//
//  1. TestRoutes_EveryHandlerCallsAuthz recorre TODAS las operaciones del
//     contrato OpenAPI, invoca su handler con un principal y falla si el
//     handler no llama a authz.Can con la x-pp-action declarada (sonda de
//     decisiones de platform/authz).
//  2. TestRoutes_AuthzMatrix ejecuta cada operación de mascotas, vínculos y
//     organizaciones con cada persona (tutor / vinculado / no vinculado / org
//     suspendida / farmacia / ajeno / super_admin) y comprueba el código:
//     403 o 404 cuando la política deniega; cualquier otro (la ejecución pasa
//     de authz y sigue contra fakes) cuando permite. Una fila por operación ×
//     persona; una operación nueva sin fila hace fallar la prueba.
//
// Los handlers corren contra fakes de lectura: el objetivo es la decisión de
// autorización y su traducción a HTTP (404 no visible / 403 visible sin
// permiso), no la persistencia (cubierta en test/integration).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	orgapp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

// ------------------------------------------------------------------ fixture

var (
	tutorID   = uuid.MustParse("01930000-0000-7000-8000-00000000a001")
	petID     = uuid.MustParse("01930000-0000-7000-8000-00000000b001")
	orgActive = uuid.MustParse("01930000-0000-7000-8000-00000000c001") // clínica activa con vínculo
	orgSusp   = uuid.MustParse("01930000-0000-7000-8000-00000000c002") // clínica suspendida con vínculo
	orgOther  = uuid.MustParse("01930000-0000-7000-8000-00000000c003") // clínica activa sin vínculo
	orgPharma = uuid.MustParse("01930000-0000-7000-8000-00000000c004") // farmacia activa
)

type persona struct {
	name    string
	p       auth.Principal
	homeOrg uuid.UUID // org usada en las rutas /organizations/{orgId}
}

func member(org uuid.UUID, orgType, role, status string) auth.Membership {
	return auth.Membership{OrganizationID: org, OrgType: orgType, Role: role, OrgStatus: status}
}

func newPersona(name string, homeOrg uuid.UUID, roles []string, ms ...auth.Membership) persona {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
	if name == "tutor" {
		id = tutorID
	}
	return persona{name: name, homeOrg: homeOrg, p: auth.Principal{
		UserID: id, Email: name + "@example.com", EmailVerified: true, GlobalRoles: roles, Memberships: ms,
	}}
}

var personas = []persona{
	newPersona("tutor", orgActive, []string{"pet_owner"}),
	newPersona("vet_linked", orgActive, []string{"pet_owner"}, member(orgActive, "clinic", "veterinarian", "active")),
	newPersona("admin_linked", orgActive, []string{"pet_owner"}, member(orgActive, "clinic", "clinic_admin", "active")),
	newPersona("recep_linked", orgActive, []string{"pet_owner"}, member(orgActive, "clinic", "receptionist", "active")),
	newPersona("vet_unlinked", orgActive, []string{"pet_owner"}, member(orgOther, "clinic", "veterinarian", "active")),
	newPersona("vet_suspended", orgSusp, []string{"pet_owner"}, member(orgSusp, "clinic", "veterinarian", "suspended")),
	newPersona("pharmacy_admin", orgPharma, []string{"pet_owner"}, member(orgPharma, "pharmacy", "pharmacy_admin", "active")),
	newPersona("stranger", orgActive, []string{"pet_owner"}),
	newPersona("super_admin", orgActive, []string{"pet_owner", "super_admin"}),
}

// fakePetQueries: una mascota del tutor con vínculo activo a orgActive y a
// orgSusp (esta última marcada org_suspended por la reacción DOM-011).
type fakePetQueries struct{}

func (fakePetQueries) ListPets(context.Context, uuid.UUID, string, petapp.PetCursor, int) ([]petapp.PetListItem, *petapp.PetCursor, error) {
	return nil, nil, nil
}

func (fakePetQueries) GetPet(_ context.Context, id uuid.UUID) (petapp.PetView, error) {
	if id != petID {
		return petapp.PetView{}, &kernel.NotFoundError{Resource: "pet"}
	}
	return petapp.PetView{
		ID: petID, Version: 3, GuardianID: tutorID, GuardianDisplayName: "Tutor", Name: "Luna",
		Species: "dog", Sex: "female", Neutered: "yes", Status: "active",
		ActiveLinks: []petapp.CareLinkView{
			{OrganizationID: orgActive, DisplayName: "Clínica Sur", Status: "active", GrantedAt: time.Now()},
			{OrganizationID: orgSusp, DisplayName: "Clínica Cerrada", Status: "active", OrgSuspended: true, GrantedAt: time.Now()},
		},
	}, nil
}

func (fakePetQueries) ListPatients(context.Context, uuid.UUID, string, string, petapp.PatientCursor, int) ([]petapp.PatientRow, *petapp.PatientCursor, error) {
	return nil, nil, nil
}

func (fakePetQueries) TransferByTokenHash(context.Context, []byte, time.Time) (petapp.TransferView, error) {
	return petapp.TransferView{PetID: petID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type fakeOrgQueries struct{}

func orgView(id uuid.UUID) (orgapp.OrgView, bool) {
	types := map[uuid.UUID]string{orgActive: "clinic", orgSusp: "clinic", orgOther: "clinic", orgPharma: "pharmacy"}
	t, ok := types[id]
	if !ok {
		return orgapp.OrgView{}, false
	}
	status := "active"
	if id == orgSusp {
		status = "suspended"
	}
	return orgapp.OrgView{
		ID: id, Version: 2, Type: t, LegalName: "Org S.A.S.", DisplayName: "Org", City: "Medellín", Country: "CO",
		TaxID: orgdomain.TaxIDPayload{Country: "CO", Value: "900123456-8"}, PublicCode: "7K3MQ2", Status: status,
		Address: orgdomain.AddressPayload{Country: "CO", City: "Medellín", Line1: "Calle 1"},
		Contact: orgdomain.ContactPayload{Email: "org@example.com", Phone: "+573001234567"},
	}, true
}

func (fakeOrgQueries) GetOrg(_ context.Context, id uuid.UUID) (orgapp.OrgView, error) {
	v, ok := orgView(id)
	if !ok {
		return orgapp.OrgView{}, &kernel.NotFoundError{Resource: "organization"}
	}
	return v, nil
}

func (fakeOrgQueries) ListMembers(context.Context, uuid.UUID) ([]orgapp.MemberView, error) {
	return nil, nil
}

func (fakeOrgQueries) ListInvitations(context.Context, uuid.UUID, string, time.Time) ([]orgapp.InvitationView, error) {
	return nil, nil
}

func (fakeOrgQueries) InvitationByTokenHash(context.Context, []byte, time.Time) (orgapp.InvitationView, error) {
	return orgapp.InvitationView{ID: uuid.New(), OrganizationID: orgActive, Email: "x@example.com", Role: "veterinarian",
		Status: "pending", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (fakeOrgQueries) SummaryByCode(context.Context, string) (orgapp.OrgSummary, error) {
	return orgapp.OrgSummary{ID: orgActive, Type: "clinic", DisplayName: "Org", City: "Medellín", Country: "CO", Status: "active"}, nil
}

func (fakeOrgQueries) ActiveClinic(context.Context, uuid.UUID) (orgapp.OrgSummary, error) {
	return orgapp.OrgSummary{}, orgapp.ErrOrgNotEligible
}

func (fakeOrgQueries) MembershipsOf(context.Context, uuid.UUID) ([]orgapp.MembershipRow, error) {
	return nil, nil
}

type failingTokens struct{}

func (failingTokens) Rotate(context.Context, uuid.UUID, uuid.UUID, []byte, time.Time) (string, string, error) {
	return "", "", errors.New("tokens no disponibles en la suite")
}

// failingDB hace fallar el almacén de idempotencia DESPUÉS de authz.
type failingDB struct{}

func (failingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("db no disponible en la suite")
}

func (failingDB) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{} }

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("db no disponible en la suite") }

func newSuiteHandlers() apiHandlers {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	idem := idempotency.NewStore(failingDB{}, 0)
	identitySvc := identityapp.NewService(testkit.NewMemoryIdentityRepo(), logger)
	identityHandlers := identityhttp.NewHandlers(identitySvc, "salt")
	petSvc := petapp.NewService(petapp.Options{Queries: fakePetQueries{}, Logger: logger})
	orgSvc := orgapp.NewService(orgapp.Options{Queries: fakeOrgQueries{}, Tokens: failingTokens{}, Logger: logger})
	return apiHandlers{
		Handlers:    identityHandlers,
		PetHandlers: pethttp.NewPetHandlers(petSvc, fakePetQueries{}, idem, nil, logger),
		OrgHandlers: orghttp.NewOrgHandlers(orgSvc, idem, nil, identityHandlers, logger),
	}
}

// ---------------------------------------------------------- invocación

type operation struct {
	id     string // operationId
	method string // nombre del método Go (operationId capitalizado)
	action string // x-pp-action
}

func operations(t *testing.T) []operation {
	t.Helper()
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	var ops []operation
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			action, _ := op.Extensions["x-pp-action"].(string)
			r := []rune(op.OperationID)
			r[0] = unicode.ToLower(r[0])
			id := string(r)
			r[0] = unicode.ToUpper(r[0])
			ops = append(ops, operation{id: id, method: string(r), action: action})
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].id < ops[j].id })
	return ops
}

// call invoca el handler del contrato con una request sintética: ids de
// ruta del fixture, cuerpos vacíos no nulos. Devuelve el código HTTP que
// produciría (éxito → 2xx; error → status del problem+json).
func call(t *testing.T, h apiHandlers, op operation, who persona) (status int) {
	t.Helper()
	m := reflect.ValueOf(h).MethodByName(op.method)
	if !m.IsValid() {
		t.Fatalf("%s: el contrato declara %s pero no hay handler", op.id, op.method)
	}
	reqType := m.Type().In(1)
	req := reflect.New(reqType).Elem()
	for i := 0; i < reqType.NumField(); i++ {
		f, field := reqType.Field(i), req.Field(i)
		switch {
		case f.Name == "PetId":
			field.Set(reflect.ValueOf(petID).Convert(f.Type))
		case f.Name == "OrgId":
			field.Set(reflect.ValueOf(who.homeOrg).Convert(f.Type))
		case f.Name == "OrganizationId":
			field.Set(reflect.ValueOf(orgActive).Convert(f.Type))
		case f.Name == "UserId", f.Name == "InvitationId":
			field.Set(reflect.ValueOf(uuid.New()).Convert(f.Type))
		case f.Name == "Token":
			field.SetString("tok_tok_tok_tok_tok_tok_tok")
		case f.Name == "Body" && f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct &&
			!strings.Contains(f.Type.Elem().PkgPath(), "mime"):
			field.Set(reflect.New(f.Type.Elem()))
		}
	}
	ctx := auth.WithPrincipal(context.Background(), who.p)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s como %s: panic: %v", op.id, who.name, r)
		}
	}()
	out := m.Call([]reflect.Value{reflect.ValueOf(ctx), req})
	if errV := out[1]; !errV.IsNil() {
		return problem.From(errV.Interface().(error)).Status
	}
	return http.StatusOK
}

// probeRecorder registra las acciones evaluadas por authz.Can.
type probeRecorder struct {
	mu      sync.Mutex
	actions map[authz.Action]bool
}

func installProbe(t *testing.T) *probeRecorder {
	t.Helper()
	r := &probeRecorder{actions: map[authz.Action]bool{}}
	authz.SetProbe(func(_ auth.Principal, a authz.Action, _ authz.Decision) {
		r.mu.Lock()
		r.actions[a] = true
		r.mu.Unlock()
	})
	t.Cleanup(func() { authz.SetProbe(nil) })
	return r
}

func (r *probeRecorder) reset() {
	r.mu.Lock()
	r.actions = map[authz.Action]bool{}
	r.mu.Unlock()
}

func (r *probeRecorder) saw(a authz.Action) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actions[a]
}

// ------------------------------------------------------------------ tests

func TestRoutes_EveryHandlerCallsAuthz(t *testing.T) {
	h := newSuiteHandlers()
	rec := installProbe(t)
	super := personas[len(personas)-1]
	for _, op := range operations(t) {
		if op.action == "public" {
			continue
		}
		if op.action == "" {
			t.Errorf("%s no declara x-pp-action", op.id)
			continue
		}
		for _, who := range []persona{super, personas[0]} {
			rec.reset()
			call(t, h, op, who)
			if !rec.saw(authz.Action(op.action)) {
				t.Errorf("%s (%s como %s): el handler no llamó a authz.Can(%q)", op.id, op.method, who.name, op.action)
			}
		}
	}
}

// Resultado esperado por persona. ok = la política permite (el código no es
// 403/404 por authz); 403/404 = denegación con ese código.
const ok = 0

// Orden de columnas = orden de personas.
var matrix = map[string][9]int{
	//                       tutor vetL admL recL vetU vetS phAd  str  sup
	"listPets":            {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"createPet":           {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"getPet":              {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"updatePet":           {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"putPetMicrochip":     {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"putPetPhoto":         {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"deletePetPhoto":      {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"markPetDeceased":     {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"undoPetDeceased":     {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"initiatePetTransfer": {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"cancelPetTransfer":   {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"getPetTransfer":      {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"acceptPetTransfer":   {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"grantCareLink":       {ok, 404, 404, 404, 404, 404, 404, 404, ok},
	"revokeCareLink":      {ok, 404, 404, 404, 404, 404, 404, 404, ok},

	"lookupOrganization":      {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"createOrganization":      {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"getOrganization":         {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"updateOrganization":      {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"listOrganizationMembers": {403, ok, ok, ok, 403, ok, ok, 403, ok},
	"changeMemberRole":        {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"removeMember":            {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"createInvitation":        {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"listInvitations":         {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"revokeInvitation":        {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"resendInvitation":        {403, 403, ok, 403, 403, 403, ok, 403, ok},
	"getInvitation":           {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"acceptInvitation":        {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"listPatients":            {403, ok, ok, ok, 403, 403, 403, 403, ok},
	"getPatient":              {404, ok, ok, ok, 404, 403, 404, 404, ok},
}

// Operaciones de cuenta propia: su autorización (authenticatedOnly) se prueba
// por decisión en platform/authz; aquí solo se exige la cobertura de ruta.
var accountOps = map[string]bool{"getMe": true, "updateMe": true, "completeOnboarding": true, "acceptConsent": true}

func TestRoutes_AuthzMatrix(t *testing.T) {
	h := newSuiteHandlers()
	for _, op := range operations(t) {
		if op.action == "public" || accountOps[op.id] {
			continue
		}
		row, found := matrix[op.id]
		if !found {
			t.Errorf("%s (%s) sin fila en la matriz de autorización (SEC-005)", op.id, op.action)
			continue
		}
		for i, who := range personas {
			got := call(t, h, op, who)
			want := row[i]
			switch {
			case want == ok && (got == http.StatusForbidden || got == http.StatusNotFound || got == http.StatusUnauthorized):
				t.Errorf("%s como %s: esperaba permitido, obtuvo %d", op.id, who.name, got)
			case want != ok && got != want:
				t.Errorf("%s como %s: esperaba %d, obtuvo %d", op.id, who.name, want, got)
			}
		}
	}
}

// TestRoutes_SuspendedOrgCode: la denegación por org suspendida usa el código
// del catálogo ORGANIZATION_SUSPENDED (PRD-016).
func TestRoutes_SuspendedOrgCode(t *testing.T) {
	h := newSuiteHandlers()
	who := personas[5]
	_, err := h.ListPatients(auth.WithPrincipal(context.Background(), who.p),
		gen.ListPatientsRequestObject{OrgId: orgSusp})
	if p := problem.From(err); p.Code != problem.OrganizationSuspended || p.Status != http.StatusForbidden {
		t.Fatalf("problem: %+v", p)
	}
}
