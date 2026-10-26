//go:build integration

// Administración (PP-F1-API-008, PRD-017/018) y mínimo privilegio en BD
// (SEC-010) contra Postgres real: cola de verificación con fundador →
// verificación por el handler HTTP (authz + motivo + audit_log en la misma
// tx) → búsqueda y suspensión de usuario (la cuenta queda bloqueada en el
// siguiente request) → auditoría filtrable. Los privilegios de pp_app_writer
// se comprueban con un rol LOGIN real (los tests corren como owner).
package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

type recordingKeycloak struct{ calls []string }

func (k *recordingKeycloak) SetEnabled(_ context.Context, id uuid.UUID, enabled bool) error {
	if enabled {
		k.calls = append(k.calls, "enable")
	} else {
		k.calls = append(k.calls, "disable")
	}
	return nil
}

func (k *recordingKeycloak) LogoutAllSessions(context.Context, uuid.UUID) error {
	k.calls = append(k.calls, "logout")
	return nil
}

func (k *recordingKeycloak) SendVerifyEmail(context.Context, uuid.UUID) error { return nil }

func TestAdmin_VerificationQueueSuspensionAndAudit(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	beto := e.user(t, "Beto Vet", "beto@example.com")
	ana := e.user(t, "Ana Tutora", "ana@example.com")
	superAdmin := e.user(t, "Root Admin", "root@example.com")
	admin := auth.Principal{UserID: superAdmin, EmailVerified: true, GlobalRoles: []string{"pet_owner", "super_admin"}}
	adminCtx := auth.WithPrincipal(ctx, admin)

	res, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900123456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567", FounderID: beto,
	})
	if err != nil {
		t.Fatal(err)
	}
	org := res.AggregateID
	h := orghttp.NewOrgHandlers(e.orgs, nil, nil, nil, logger).WithIPHashSalt("integration-salt")

	// PRD-017: la cola pendiente incluye la organización con su fundador.
	out, err := h.AdminListOrganizations(adminCtx, gen.AdminListOrganizationsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	page := out.(gen.AdminListOrganizations200JSONResponse)
	if len(page.Data) != 1 || page.Data[0].Id != openapi_types.UUID(org) || page.Data[0].Founder == nil ||
		page.Data[0].Founder.Email != "beto@example.com" || page.Data[0].MemberCount == nil || *page.Data[0].MemberCount != 1 {
		t.Fatalf("cola: %+v", page)
	}
	q := "sur"
	out, _ = h.AdminListOrganizations(adminCtx, gen.AdminListOrganizationsRequestObject{Params: gen.AdminListOrganizationsParams{Q: &q}})
	if len(out.(gen.AdminListOrganizations200JSONResponse).Data) != 1 {
		t.Fatal("búsqueda por nombre")
	}
	active := gen.OrgStatus("active")
	out, _ = h.AdminListOrganizations(adminCtx, gen.AdminListOrganizationsRequestObject{Params: gen.AdminListOrganizationsParams{Status: &active}})
	if len(out.(gen.AdminListOrganizations200JSONResponse).Data) != 0 {
		t.Fatal("no hay organizaciones activas todavía")
	}

	// Un no admin recibe 403 y no escribe nada.
	_, err = h.AdminVerifyOrganization(auth.WithPrincipal(ctx, auth.Principal{UserID: ana, GlobalRoles: []string{"pet_owner"}}),
		gen.AdminVerifyOrganizationRequestObject{OrgId: org, Body: &gen.AdminOptionalReasonRequest{}})
	if p := problem.From(err); p.Status != 403 {
		t.Fatalf("no admin: %+v", p)
	}

	// Verificación desde el handler: org activa, email al contacto y audit_log.
	reason := "RUT y cámara de comercio verificados"
	vres, err := h.AdminVerifyOrganization(adminCtx, gen.AdminVerifyOrganizationRequestObject{
		OrgId: org, Body: &gen.AdminOptionalReasonRequest{Reason: &reason},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := vres.(gen.AdminVerifyOrganization200JSONResponse); v.Body.Status != "active" || v.Body.Founder == nil {
		t.Fatalf("verificada: %+v", v.Body)
	}
	if e.mail.lastTo() != "contacto@clinicasur.co" {
		t.Fatalf("email de verificación: %q", e.mail.lastTo())
	}
	// Transición inválida (ya activa) → 409 y sin auditoría adicional.
	_, err = h.AdminVerifyOrganization(adminCtx, gen.AdminVerifyOrganizationRequestObject{OrgId: org, Body: &gen.AdminOptionalReasonRequest{}})
	if p := problem.From(err); p.Status != 409 {
		t.Fatalf("doble verificación: %+v", p)
	}
	// Suspensión con motivo corto → 422 REASON_REQUIRED.
	_, err = h.AdminSuspendOrganization(adminCtx, gen.AdminSuspendOrganizationRequestObject{OrgId: org, Body: &gen.AdminReasonRequest{Reason: "corto"}})
	if p := problem.From(err); p.Code != problem.ReasonRequired {
		t.Fatalf("motivo corto: %+v", p)
	}
	if _, err := h.AdminSuspendOrganization(adminCtx, gen.AdminSuspendOrganizationRequestObject{
		OrgId: org, Body: &gen.AdminReasonRequest{Reason: "Denuncia de ejercicio ilegal"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orgQ.SummaryByCode(ctx, mustOrg(t, e, org).PublicCode); err == nil {
		t.Fatal("una organización suspendida no aparece en la búsqueda por código (PRD-017)")
	}

	// PRD-018: búsqueda y suspensión de usuario.
	repo := identityinfra.NewPostgresRepository(e.pool)
	kc := &recordingKeycloak{}
	adminSvc := identityapp.NewAdminService(identityapp.AdminOptions{
		Repo: repo, Directory: repo, Keycloak: kc, Pets: e.petQ, Audit: identityinfra.NewAuditLog(e.pool), Logger: logger,
	})
	users, _, err := adminSvc.SearchUsers(ctx, admin, identityapp.AccountSearch{Query: "BETO@"})
	if err != nil || len(users) != 1 || users[0].Account.ID != beto {
		t.Fatalf("búsqueda por email: %+v %v", users, err)
	}
	users, _, _ = adminSvc.SearchUsers(ctx, admin, identityapp.AccountSearch{Query: "%"})
	if len(users) != 0 {
		t.Fatal("los comodines de ILIKE deben escaparse")
	}
	all, next, _ := adminSvc.SearchUsers(ctx, admin, identityapp.AccountSearch{Limit: 2})
	if len(all) != 2 || next == nil {
		t.Fatalf("paginación: %d next=%v", len(all), next)
	}
	rest, next2, _ := adminSvc.SearchUsers(ctx, admin, identityapp.AccountSearch{Limit: 2, Cursor: next})
	if len(rest) != 1 || next2 != nil || rest[0].Account.ID == all[0].Account.ID || rest[0].Account.ID == all[1].Account.ID {
		t.Fatalf("segunda página: %+v", rest)
	}

	if _, err := adminSvc.SuspendUser(ctx, admin, identityapp.AdminUserCommand{UserID: ana, Reason: "Suplantación de identidad"}); err != nil {
		t.Fatal(err)
	}
	st, err := e.identity.Resolve(ctx, auth.Claims{Subject: ana.String(), Email: "ana@example.com", EmailVerified: true})
	if err != nil || !st.Suspended {
		t.Fatalf("la cuenta debe quedar suspendida en el siguiente request: %+v %v", st, err)
	}
	if len(kc.calls) != 2 || kc.calls[0] != "disable" || kc.calls[1] != "logout" {
		t.Fatalf("keycloak: %v", kc.calls)
	}

	// Auditoría: 3 entradas admin (verify, suspend org, suspend user) sin PII.
	rows, _, err := adminSvc.AuditEntries(ctx, admin, audit.Filter{ActorID: &superAdmin})
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, r := range rows {
		actions[r.Action] = true
		if r.Reason == nil || len(*r.Reason) < 10 {
			t.Fatalf("toda acción admin lleva motivo: %+v", r)
		}
	}
	for _, a := range []string{"admin.org.verify", "admin.org.suspend", "admin.user.suspend"} {
		if !actions[a] {
			t.Fatalf("falta %s en %v", a, actions)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("entradas: %d", len(rows))
	}
	byTarget, _, _ := adminSvc.AuditEntries(ctx, admin, audit.Filter{TargetID: &org})
	if len(byTarget) != 2 {
		t.Fatalf("filtro por objetivo: %d", len(byTarget))
	}
}

func mustOrg(t *testing.T, e *orgEnv, id uuid.UUID) struct{ PublicCode string } {
	t.Helper()
	v, err := e.orgQ.GetOrg(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return struct{ PublicCode string }{v.PublicCode}
}

// TestLeastPrivilege_WriterGrants (SEC-010, DAT-001): con un rol LOGIN
// miembro de pp_app_writer, el Event Store puede versionar es.streams pero
// no mutar es.events ni platform.audit_log.
func TestLeastPrivilege_WriterGrants(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `CREATE ROLE pp_test_writer LOGIN PASSWORD 'writer' IN ROLE pp_app_writer`); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(e.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("pp_test_writer", "writer")
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	stream := uuid.New()
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	denied := func(sql string, args ...any) {
		t.Helper()
		_, err := conn.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("%s: esperaba permission denied (42501), obtuvo %v", sql, err)
		}
	}
	mustExec(`INSERT INTO es.streams (stream_id, stream_type) VALUES ($1, 'test')`, stream)
	mustExec(`UPDATE es.streams SET version = version + 1 WHERE stream_id = $1`, stream)
	mustExec(`INSERT INTO es.events (event_id, stream_id, stream_type, stream_version, event_type, payload, metadata, occurred_at)
	          VALUES ($1, $2, 'test', 1, 'Test', '{}', '{}', now())`, uuid.New(), stream)
	denied(`UPDATE es.events SET payload = '{}' WHERE stream_id = $1`, stream)
	denied(`DELETE FROM es.events WHERE stream_id = $1`, stream)
	denied(`TRUNCATE es.events`)
	mustExec(`INSERT INTO platform.audit_log (id, action) VALUES ($1, 'test.action')`, uuid.New())
	denied(`UPDATE platform.audit_log SET reason = 'x'`)
	denied(`DELETE FROM platform.audit_log`)
	denied(`CREATE TABLE es.intrusa (id int)`)
}

func (m *mailbox) lastTo() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return ""
	}
	return m.sent[len(m.sent)-1].To
}
