package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type fakeKeycloak struct {
	calls []string
	fail  error
}

func (k *fakeKeycloak) SetEnabled(_ context.Context, id uuid.UUID, enabled bool) error {
	if k.fail != nil {
		return k.fail
	}
	if enabled {
		k.calls = append(k.calls, "enable "+id.String())
	} else {
		k.calls = append(k.calls, "disable "+id.String())
	}
	return nil
}

func (k *fakeKeycloak) LogoutAllSessions(_ context.Context, id uuid.UUID) error {
	k.calls = append(k.calls, "logout "+id.String())
	return nil
}

func (k *fakeKeycloak) SendVerifyEmail(context.Context, uuid.UUID) error { return nil }

type fakePets map[uuid.UUID]int

func (f fakePets) CountByGuardians(context.Context, []uuid.UUID) (map[uuid.UUID]int, error) {
	return f, nil
}

type fakeMemberships struct{}

func (fakeMemberships) Memberships(context.Context, uuid.UUID) ([]auth.Membership, error) {
	return []auth.Membership{{OrganizationID: uuid.New(), OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"}}, nil
}

type adminFixture struct {
	repo   *testkit.MemoryIdentityRepo
	kc     *fakeKeycloak
	svc    *app.AdminService
	admin  auth.Principal
	target uuid.UUID
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	f := &adminFixture{
		repo: testkit.NewMemoryIdentityRepo(), kc: &fakeKeycloak{}, target: uuid.Must(uuid.NewV7()),
		admin: auth.Principal{UserID: uuid.Must(uuid.NewV7()), GlobalRoles: []string{"super_admin"}},
	}
	snap := domain.TokenSnapshot{Email: "bea@example.com", EmailVerified: true, Roles: []string{"pet_owner"}}
	if err := f.repo.Insert(context.Background(), domain.Provision(f.target, snap, base)); err != nil {
		t.Fatal(err)
	}
	f.svc = app.NewAdminService(app.AdminOptions{
		Repo: f.repo, Keycloak: f.kc, Pets: fakePets{f.target: 2}, Memberships: fakeMemberships{},
		Now: func() time.Time { return base },
	})
	return f
}

func (f *adminFixture) cmd(reason string) app.AdminUserCommand {
	return app.AdminUserCommand{UserID: f.target, Reason: reason}
}

func TestAdmin_SuspendUser_PlatformKeycloakAndAudit(t *testing.T) {
	f := newAdminFixture(t)
	v, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("Uso fraudulento reportado"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Account.IsSuspended() || v.PetCount != 2 || len(v.Memberships) != 1 {
		t.Fatalf("vista: %+v", v)
	}
	stored, _ := f.repo.Get(context.Background(), f.target)
	if !stored.IsSuspended() || stored.StatusReason != "Uso fraudulento reportado" {
		t.Fatalf("persistido: %+v", stored)
	}
	want := []string{"disable " + f.target.String(), "logout " + f.target.String()}
	if len(f.kc.calls) != 2 || f.kc.calls[0] != want[0] || f.kc.calls[1] != want[1] {
		t.Fatalf("keycloak: %v", f.kc.calls)
	}
	if len(f.repo.Audit) != 1 {
		t.Fatalf("auditoría: %+v", f.repo.Audit)
	}
	e := f.repo.Audit[0]
	if e.Action != "admin.user.suspend" || e.Reason != "Uso fraudulento reportado" || *e.ActorID != f.admin.UserID ||
		*e.TargetID != f.target || e.TargetType != "user_account" {
		t.Fatalf("entrada: %+v", e)
	}
}

func TestAdmin_ReactivateUser_EnablesInKeycloak(t *testing.T) {
	f := newAdminFixture(t)
	if _, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("Uso fraudulento reportado")); err != nil {
		t.Fatal(err)
	}
	v, err := f.svc.ReactivateUser(context.Background(), f.admin, f.cmd("Revisión completada sin hallazgos"))
	if err != nil || v.Account.IsSuspended() {
		t.Fatalf("reactivate: %v %+v", err, v)
	}
	if last := f.kc.calls[len(f.kc.calls)-1]; last != "enable "+f.target.String() {
		t.Fatalf("keycloak: %v", f.kc.calls)
	}
	if len(f.repo.Audit) != 2 || f.repo.Audit[1].Action != "admin.user.reactivate" {
		t.Fatalf("auditoría: %+v", f.repo.Audit)
	}
}

func TestAdmin_ReasonRequired(t *testing.T) {
	f := newAdminFixture(t)
	_, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("corto"))
	if !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("motivo corto: %v", err)
	}
	if len(f.kc.calls) != 0 || len(f.repo.Audit) != 0 || f.repo.Saves != 0 {
		t.Fatal("sin motivo no debe haber efectos")
	}
}

func TestAdmin_CannotSuspendSelf(t *testing.T) {
	f := newAdminFixture(t)
	self := f.cmd("Motivo suficientemente largo")
	self.UserID = f.admin.UserID
	var ve *kernel.ValidationError
	if _, err := f.svc.SuspendUser(context.Background(), f.admin, self); !errors.As(err, &ve) {
		t.Fatalf("auto-suspensión: %v", err)
	}
}

func TestAdmin_KeycloakFailure_SuspendedInPlatformAnd503(t *testing.T) {
	f := newAdminFixture(t)
	f.kc.fail = errors.New("connection refused")
	_, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("Uso fraudulento reportado"))
	if !errors.Is(err, app.ErrIdentityProviderUnavailable) {
		t.Fatalf("esperaba UNAVAILABLE: %v", err)
	}
	stored, _ := f.repo.Get(context.Background(), f.target)
	if !stored.IsSuspended() {
		t.Fatal("la plataforma debe cortar el acceso aunque Keycloak falle")
	}
	f.kc.fail = nil
	if _, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("Uso fraudulento reportado")); err != nil {
		t.Fatalf("reintento idempotente: %v", err)
	}
}

func TestAdmin_KeycloakUnknownUserIsNotAnError(t *testing.T) {
	f := newAdminFixture(t)
	f.kc.fail = app.ErrKeycloakUserNotFound
	if _, err := f.svc.SuspendUser(context.Background(), f.admin, f.cmd("Cuenta huérfana sin Keycloak")); err != nil {
		t.Fatal(err)
	}
}

func TestAdmin_NonAdminForbidden(t *testing.T) {
	f := newAdminFixture(t)
	owner := auth.Principal{UserID: uuid.New(), GlobalRoles: []string{"pet_owner"}}
	var fb *kernel.ForbiddenError
	if _, err := f.svc.SuspendUser(context.Background(), owner, f.cmd("Motivo suficientemente largo")); !errors.As(err, &fb) {
		t.Fatalf("suspend: %v", err)
	}
	if _, _, err := f.svc.SearchUsers(context.Background(), owner, app.AccountSearch{}); !errors.As(err, &fb) {
		t.Fatalf("search: %v", err)
	}
	if _, _, err := f.svc.AuditEntries(context.Background(), owner, audit.Filter{}); !errors.As(err, &fb) {
		t.Fatalf("audit: %v", err)
	}
}

func TestAdmin_AuditRangeValidated(t *testing.T) {
	f := newAdminFixture(t)
	from, to := base, base.Add(-time.Hour)
	var ve *kernel.ValidationError
	if _, _, err := f.svc.AuditEntries(context.Background(), f.admin, audit.Filter{From: &from, To: &to}); !errors.As(err, &ve) {
		t.Fatalf("rango invertido: %v", err)
	}
}

type fakeDirectory struct {
	got  app.AccountSearch
	accs []*domain.UserAccount
}

func (d *fakeDirectory) SearchAccounts(_ context.Context, f app.AccountSearch) ([]*domain.UserAccount, *app.AccountCursor, error) {
	d.got = f
	return d.accs, &app.AccountCursor{CreatedAt: base, ID: d.accs[len(d.accs)-1].ID}, nil
}

type fakeAudit struct{ rows []audit.Row }

func (a fakeAudit) List(context.Context, audit.Filter) ([]audit.Row, *audit.Cursor, error) {
	return a.rows, nil, nil
}

func TestAdmin_SearchUsers_EnrichesRows(t *testing.T) {
	f := newAdminFixture(t)
	acc, _ := f.repo.Get(context.Background(), f.target)
	dir := &fakeDirectory{accs: []*domain.UserAccount{acc}}
	svc := app.NewAdminService(app.AdminOptions{
		Repo: f.repo, Directory: dir, Pets: fakePets{f.target: 3}, Memberships: fakeMemberships{},
		Audit: fakeAudit{rows: []audit.Row{{Action: "admin.user.suspend"}}},
	})
	views, next, err := svc.SearchUsers(context.Background(), f.admin, app.AccountSearch{Query: "  bea  ", Status: "active", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if dir.got.Query != "bea" || dir.got.Status != "active" || dir.got.Limit != 5 {
		t.Fatalf("filtro: %+v", dir.got)
	}
	if len(views) != 1 || views[0].PetCount != 3 || len(views[0].Memberships) != 1 || next == nil {
		t.Fatalf("vistas: %+v next=%v", views, next)
	}
	rows, _, err := svc.AuditEntries(context.Background(), f.admin, audit.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("audit: %v %v", rows, err)
	}
}
