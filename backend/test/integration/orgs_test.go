//go:build integration

// Partner Network + vínculos + transferencia de extremo a extremo contra
// Postgres con dos (y tres) usuarios (PP-F1-PRD-010..016, DOM-006..008,
// DOM-011, IAM-008, DAT-006): organización pendiente → verificada → vínculo
// por la tutora → la clínica ve al paciente → invitación aceptada y miembro
// retirado (pierde acceso en el siguiente request) → suspensión oculta
// pacientes → transferencia de tutela por token. Rebuild idempotente.
package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	orgapp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

// mailbox captura las notificaciones que los servicios encolan (ARQ-014) para
// extraer los tokens de los enlaces.
type mailbox struct {
	mu   sync.Mutex
	sent []notify.Notification
}

func (m *mailbox) Enqueue(_ context.Context, n notify.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, n)
	return nil
}

var tokenRe = regexp.MustCompile(`/(?:invitations|app/transfers)/([A-Za-z0-9_-]{20,})`)

func (m *mailbox) lastToken(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.sent) - 1; i >= 0; i-- {
		if strings.EqualFold(m.sent[i].To, to) {
			if g := tokenRe.FindStringSubmatch(m.sent[i].Payload["link_path"]); g != nil {
				return g[1]
			}
		}
	}
	t.Fatalf("sin email con token para %s", to)
	return ""
}

type orgEnv struct {
	pool     *db.Pool
	unit     *db.UnitOfWork
	store    *eventstore.Store
	registry *eventstore.Registry
	runner   *projection.Runner
	identity *identityapp.Service
	orgs     *orgapp.Service
	orgQ     *orginfra.Queries
	pets     *petapp.Service
	petQ     *petinfra.Queries
	cache    *auth.MembershipCache
	mail     *mailbox
}

func newOrgEnv(t *testing.T) *orgEnv {
	t.Helper()
	url := testkit.Postgres(t)
	pool, err := db.Open(context.Background(), url, 8, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	registry := eventstore.NewRegistry()
	petinfra.RegisterEvents(registry)
	petinfra.RegisterUpcasters(registry)
	orginfra.RegisterEvents(registry)
	clinicalinfra.RegisterEvents(registry)
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	store := eventstore.NewStore(registry, pool, nil)
	unit := db.NewUnitOfWork(pool)
	runner := projection.NewRunner()
	clinicalinfra.RegisterSyncProjectors(runner, registry)
	commands := bus.New()
	box := &mailbox{}
	identitySvc := identityapp.NewService(identityinfra.NewPostgresRepository(pool), logger)

	users := orginfra.NewUserAdapter(identitySvc)
	orgQ := orginfra.NewQueries(pool.Pool, users)
	cache := auth.NewMembershipCache(orginfra.NewMembershipProvider(orgQ), time.Minute, 0)
	orgSvc := orgapp.NewService(orgapp.Options{
		Repo: orginfra.NewRepository(store, registry, nil, nil), UoW: unit, Projects: orginfra.NewSyncProjector(runner),
		Codes: orginfra.PublicCodes{}, Tokens: orginfra.NewInvitationTokens(pool.Pool), Queries: orgQ,
		Users: users, Memberships: cache, Notifier: box, Auditor: orginfra.TxAuditor{}, Logger: logger,
	})
	orgSvc.RegisterCommands(commands)
	orginfra.RegisterSyncProjectors(runner, registry)

	guardian := petinfra.NewGuardianAdapter(identitySvc)
	orgAdapter := petinfra.NewOrganizationAdapter(orgQ)
	petQ := petinfra.NewQueries(pool.Pool).WithGuardian(guardian)
	petSvc := petapp.NewService(petapp.Options{
		Repo: petinfra.NewRepository(store, registry, nil, nil), UoW: unit, Projects: petinfra.NewSyncProjector(runner),
		Reserve: petinfra.NewReservations(pool), Guardian: guardian, Orgs: orgAdapter, Queries: petQ, Events: store,
		Notifier: box, Logger: logger,
	})
	petSvc.RegisterCommands(commands)
	petinfra.RegisterSyncProjectors(runner, registry, guardian, orgAdapter)

	return &orgEnv{pool: pool, unit: unit, store: store, registry: registry, runner: runner, identity: identitySvc,
		orgs: orgSvc, orgQ: orgQ, pets: petSvc, petQ: petQ, cache: cache, mail: box}
}

// user aprovisiona la cuenta JIT (IAM-009) y devuelve su id.
func (e *orgEnv) user(t *testing.T, name, email string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := e.identity.Resolve(context.Background(), auth.Claims{
		Subject: id.String(), Email: email, EmailVerified: true, Name: name,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func orgCtx(id uuid.UUID, email string, roles ...string) context.Context {
	return orgapp.WithCommandContext(context.Background(), orgapp.CommandContext{
		Principal: orgapp.PrincipalInfo{UserID: id, Email: email, EmailVerified: true, Roles: roles},
	})
}

func petCtx(id uuid.UUID, email string) context.Context {
	return petapp.WithCommandContext(context.Background(), petapp.CommandContext{
		Principal: petapp.PrincipalInfo{UserID: id, Email: email, Roles: []string{"pet_owner"}},
	})
}

func TestOrgs_TwoUsersEndToEnd(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	ana := e.user(t, "Ana Tutora", "ana@example.com")
	beto := e.user(t, "Beto Vet", "beto@example.com")
	carla := e.user(t, "Carla Recepción", "carla@example.com")
	admin := uuid.Must(uuid.NewV7())

	// Ana registra a Luna (PRD-005).
	res, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RegisterPet{
		GuardianID: ana, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	luna := res.AggregateID

	// PRD-011: Beto registra la clínica → pendiente, código público, admin.
	res, err = e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900.123.456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567", FounderID: beto,
	})
	if err != nil {
		t.Fatal(err)
	}
	org := res.AggregateID
	view, err := e.orgQ.GetOrg(ctx, org)
	if err != nil || view.Status != "pending_verification" || len(view.PublicCode) != 6 || view.TaxID.Value != "900123456-8" {
		t.Fatalf("org registrada: %+v %v", view, err)
	}
	ms, _ := e.cache.Memberships(ctx, beto)
	if len(ms) != 1 || ms[0].Role != "clinic_admin" || ms[0].OrgStatus != "pending_verification" {
		t.Fatalf("membresía del fundador: %+v", ms)
	}
	// DAT-006: tax_id duplicado → 422 ALREADY_REGISTERED.
	_, err = e.orgs.Dispatch(orgCtx(carla, "carla@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Otra Clínica S.A.S.", DisplayName: "Otra", TaxCountry: "CO", TaxValue: "900123456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Bogotá", Line1: "Cra 7"},
		ContactEmail: "otra@example.com", ContactPhone: "+573001234568", FounderID: carla,
	})
	var ve *kernel.ValidationError
	if !errors.As(err, &ve) || ve.Fields[0].Code != "ALREADY_REGISTERED" {
		t.Fatalf("tax_id duplicado: %v", err)
	}

	// PRD-015: pendiente no recibe vínculos ni aparece por código (I5).
	if _, err := e.orgQ.SummaryByCode(ctx, view.PublicCode); err == nil {
		t.Fatal("una org pendiente no debe resolverse por código")
	}
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.GrantCareLink{PetID: luna, OrganizationID: org, ActorID: ana}); !errors.Is(err, petdomain.ErrOrganizationNotEligible) {
		t.Fatalf("I5 pendiente: %v", err)
	}

	// SuperAdmin verifica (HTTP en M1.4) → activa + email (DOM-011).
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.VerifyOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}
	if ms, _ := e.cache.Memberships(ctx, beto); ms[0].OrgStatus != "active" {
		t.Fatalf("la verificación debe invalidar la caché del admin: %+v", ms)
	}
	summary, err := e.orgQ.SummaryByCode(ctx, strings.ToLower(view.PublicCode))
	if err != nil || summary.ID != org || summary.City != "Medellín" {
		t.Fatalf("lookup por código: %+v %v", summary, err)
	}

	// Ana vincula (idempotente) y Beto ve al paciente (PRD-015, PRD-016).
	for i := 0; i < 2; i++ {
		if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.GrantCareLink{PetID: luna, OrganizationID: org, ActorID: ana}); err != nil {
			t.Fatal(err)
		}
	}
	pet, _ := e.petQ.GetPet(ctx, luna)
	if len(pet.ActiveLinks) != 1 || pet.ActiveLinks[0].DisplayName != "Clínica Sur" || pet.ActiveLinks[0].City != "Medellín" {
		t.Fatalf("vínculo proyectado: %+v", pet.ActiveLinks)
	}
	rows, _, err := e.petQ.ListPatients(ctx, org, "ana", "all", petapp.PatientCursor{}, 20)
	if err != nil || len(rows) != 1 || rows[0].PetID != luna || rows[0].GuardianDisplayName != "Ana Tutora" {
		t.Fatalf("pacientes (búsqueda por tutor): %+v %v", rows, err)
	}
	if rows, _, _ := e.petQ.ListPatients(ctx, org, "%", "all", petapp.PatientCursor{}, 20); len(rows) != 0 {
		t.Fatal("los comodines de ILIKE deben escaparse")
	}

	// PRD-013: invitación a Carla con token de un solo uso.
	if _, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.InviteMember{OrganizationID: org, Email: "carla@example.com", Role: "receptionist"}); err != nil {
		t.Fatal(err)
	}
	token := e.mail.lastToken(t, "carla@example.com")
	// Reenviar rota el token: el anterior deja de valer.
	invs, _ := e.orgQ.ListInvitations(ctx, org, "pending", time.Now())
	if len(invs) != 1 {
		t.Fatalf("invitaciones pendientes: %+v", invs)
	}
	if err := e.orgs.ResendInvitation(ctx, org, invs[0].ID); err != nil {
		t.Fatal(err)
	}
	newToken := e.mail.lastToken(t, "carla@example.com")
	if newToken == token {
		t.Fatal("el reenvío debe generar un token nuevo")
	}
	if _, err := e.orgs.AcceptInvitationToken(orgCtx(carla, "carla@example.com"), token); err == nil {
		t.Fatal("el token anterior al reenvío no debe valer")
	}
	// Beto (otro email) no puede aceptar la invitación de Carla.
	if _, err := e.orgs.AcceptInvitationToken(orgCtx(beto, "beto@example.com"), newToken); !errors.Is(err, orgdomain.ErrInvitationEmailMismatch) {
		t.Fatalf("email distinto: %v", err)
	}
	if _, err := e.orgs.AcceptInvitationToken(orgCtx(carla, "carla@example.com"), newToken); err != nil {
		t.Fatal(err)
	}
	if ms, _ := e.cache.Memberships(ctx, carla); len(ms) != 1 || ms[0].Role != "receptionist" {
		t.Fatalf("membresía de Carla: %+v", ms)
	}
	// Límite de reenvíos (API-007): 3 en 24 h.
	if _, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.InviteMember{OrganizationID: org, Email: "dani@example.com", Role: "veterinarian"}); err != nil {
		t.Fatal(err)
	}
	invs, _ = e.orgQ.ListInvitations(ctx, org, "pending", time.Now())
	for i := 0; i < orgdomain.MaxResends24h; i++ {
		if err := e.orgs.ResendInvitation(ctx, org, invs[0].ID); err != nil {
			t.Fatalf("reenvío %d: %v", i+1, err)
		}
	}
	if err := e.orgs.ResendInvitation(ctx, org, invs[0].ID); !errors.Is(err, orgapp.ErrResendLimit) {
		t.Fatalf("cuarto reenvío: %v", err)
	}

	// PRD-014: O2 protege al último admin; retirar a Carla invalida su caché.
	if _, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.ChangeMemberRole{OrganizationID: org, UserID: beto, Role: "veterinarian"}); !errors.Is(err, orgdomain.ErrLastAdmin) {
		t.Fatalf("O2: %v", err)
	}
	if _, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RemoveMember{OrganizationID: org, UserID: carla}); err != nil {
		t.Fatal(err)
	}
	if ms, _ := e.cache.Memberships(ctx, carla); len(ms) != 0 {
		t.Fatalf("Carla debe perder el acceso en el siguiente request: %+v", ms)
	}
	members, _ := e.orgQ.ListMembers(ctx, org)
	if len(members) != 1 || members[0].Email != "beto@example.com" || members[0].DisplayName != "Beto Vet" {
		t.Fatalf("miembros: %+v", members)
	}

	// PRD-012: editar razón social en org activa → vuelve a pendiente.
	v, _ := e.orgQ.GetOrg(ctx, org)
	upd := orgapp.WithCommandContext(ctx, orgapp.CommandContext{Principal: orgapp.PrincipalInfo{UserID: beto}, ExpectedVersion: v.Version})
	if _, err := e.orgs.Dispatch(upd, orgdomain.UpdateOrganizationProfile{OrganizationID: org, LegalName: ptrStr("Clínica Sur S.A.S.")}); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.orgQ.GetOrg(ctx, org); v.Status != "pending_verification" || v.StatusReason != "legal_name_change" {
		t.Fatalf("reinicio de verificación: %+v", v)
	}
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.VerifyOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}

	// DOM-011: suspensión → org_suspended; la clínica no ve pacientes.
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.SuspendOrganization{OrganizationID: org, Reason: "documentación vencida"}); err != nil {
		t.Fatal(err)
	}
	pet, _ = e.petQ.GetPet(ctx, luna)
	if !pet.ActiveLinks[0].OrgSuspended {
		t.Fatal("el vínculo debe marcarse org_suspended")
	}
	if rows, _, _ := e.petQ.ListPatients(ctx, org, "", "all", petapp.PatientCursor{}, 20); len(rows) != 0 {
		t.Fatal("org suspendida no lista pacientes")
	}
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.ReactivateOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}
	if rows, _, _ := e.petQ.ListPatients(ctx, org, "", "all", petapp.PatientCursor{}, 20); len(rows) != 1 {
		t.Fatal("tras reactivar vuelve a ver al paciente")
	}

	// PRD-010: transferencia de tutela de Ana a Beto por token.
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.InitiateGuardianshipTransfer{PetID: luna, ToEmail: "beto@example.com"}); err != nil {
		t.Fatal(err)
	}
	pet, _ = e.petQ.GetPet(ctx, luna)
	if pet.PendingTransfer == nil || pet.PendingTransfer.ToEmailMasked == "beto@example.com" {
		t.Fatalf("pendiente enmascarada: %+v", pet.PendingTransfer)
	}
	tt := e.mail.lastToken(t, "beto@example.com")
	if _, err := e.pets.AcceptTransferToken(petCtx(carla, "carla@example.com"), tt); !errors.Is(err, petdomain.ErrTransferEmailMismatch) {
		t.Fatalf("aceptar con otro email: %v", err)
	}
	if _, err := e.pets.AcceptTransferToken(petCtx(beto, "beto@example.com"), tt); err != nil {
		t.Fatal(err)
	}
	pet, _ = e.petQ.GetPet(ctx, luna)
	if pet.GuardianID != beto || pet.PendingTransfer != nil || len(pet.ActiveLinks) != 1 || pet.ID != luna {
		t.Fatalf("tras la transferencia: %+v", pet)
	}
	if items, _, _ := e.petQ.ListPets(ctx, ana, "all", petapp.PetCursor{}, 10); len(items) != 0 {
		t.Fatal("la mascota desaparece de la lista de Ana")
	}
	if _, err := e.pets.AcceptTransferToken(petCtx(beto, "beto@example.com"), tt); !errors.Is(err, petdomain.ErrTransferTokenInvalid) {
		t.Fatalf("token reusado: %v", err)
	}

	// ARQ-005: rebuild de todas las proyecciones = mismo estado.
	dump := func() string {
		var out []string
		for _, q := range []string{
			`SELECT to_jsonb(o) FROM read_partnernetwork.organizations o ORDER BY id`,
			`SELECT to_jsonb(m) FROM read_partnernetwork.org_members m ORDER BY organization_id, user_id`,
			`SELECT to_jsonb(i) FROM read_partnernetwork.invitations i ORDER BY id`,
			`SELECT to_jsonb(p) FROM read_petregistry.pets p ORDER BY id`,
			`SELECT to_jsonb(l) FROM read_petregistry.pet_care_links l ORDER BY pet_id, organization_id`,
			`SELECT to_jsonb(t) FROM read_petregistry.pet_transfers t ORDER BY token_hash`,
		} {
			r, err := e.pool.Query(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			for r.Next() {
				var s string
				if err := r.Scan(&s); err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
			r.Close()
		}
		return strings.Join(out, "\n")
	}
	before := dump()
	events, err := e.store.ReadAll(ctx, 0, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.unit.Do(ctx, func(c context.Context) error {
		tx, err := db.TxFrom(c)
		if err != nil {
			return err
		}
		return e.runner.Apply(c, tx, events)
	}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := dump(); before != after {
		t.Fatalf("rebuild no idempotente:\nantes:\n%s\ndespués:\n%s", before, after)
	}
}
