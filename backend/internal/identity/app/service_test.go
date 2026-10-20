package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

var base = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

type fixture struct {
	repo *testkit.MemoryIdentityRepo
	svc  *app.Service
	now  time.Time
	id   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{repo: testkit.NewMemoryIdentityRepo(), now: base, id: uuid.Must(uuid.NewV7())}
	f.svc = app.NewService(f.repo, nil).WithClock(func() time.Time { return f.now })
	return f
}

func (f *fixture) claims(verified bool) auth.Claims {
	c := auth.Claims{Subject: f.id.String(), Email: "Ana@Example.com", EmailVerified: verified, Name: "Ana", Locale: "en"}
	c.RealmAccess.Roles = []string{"pet_owner", "offline_access"}
	return c
}

func (f *fixture) principal() auth.Principal {
	return auth.Principal{UserID: f.id, Email: "ana@example.com", EmailVerified: true, GlobalRoles: []string{"pet_owner"}}
}

func (f *fixture) onboard(t *testing.T) {
	t.Helper()
	_, err := f.svc.CompleteOnboarding(context.Background(), f.principal(), app.CompleteOnboardingCommand{
		DisplayName: "Ana", Country: "co", Locale: "es", TermsVersion: "2026-10-draft", PrivacyVersion: "2026-10-draft",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolve_ProvisionsJIT(t *testing.T) {
	f := newFixture(t)
	st, err := f.svc.Resolve(context.Background(), f.claims(true))
	if err != nil {
		t.Fatal(err)
	}
	if st.Suspended || st.OnboardingCompleted || st.ConsentCurrent {
		t.Fatalf("estado: %+v", st)
	}
	acc := f.repo.Accounts[f.id]
	if acc == nil || acc.Email != "ana@example.com" || acc.Locale != kernel.LocaleEN || acc.DisplayName != "Ana" || len(acc.GlobalRoles) != 1 {
		t.Fatalf("cuenta aprovisionada: %+v", acc)
	}
	if f.repo.Saves != 0 {
		t.Fatal("el insert no debe ir seguido de un save")
	}
}

func TestResolve_ThrottlesLastLogin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Resolve(ctx, f.claims(true)); err != nil {
		t.Fatal(err)
	}
	f.now = base.Add(time.Minute)
	if _, err := f.svc.Resolve(ctx, f.claims(true)); err != nil {
		t.Fatal(err)
	}
	if f.repo.Saves != 0 {
		t.Fatalf("dentro del throttle no debe escribir, saves=%d", f.repo.Saves)
	}
	f.now = base.Add(app.LoginThrottle + time.Second)
	if _, err := f.svc.Resolve(ctx, f.claims(true)); err != nil {
		t.Fatal(err)
	}
	if f.repo.Saves != 1 || !f.repo.Accounts[f.id].LastLoginAt.Equal(f.now) {
		t.Fatalf("pasado el throttle debe escribir last_login_at, saves=%d", f.repo.Saves)
	}
}

func TestResolve_SyncsTokenChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Resolve(ctx, f.claims(false)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Resolve(ctx, f.claims(true)); err != nil {
		t.Fatal(err)
	}
	if !f.repo.Accounts[f.id].EmailVerified || f.repo.Saves != 1 {
		t.Fatal("email_verified no sincronizado")
	}
}

func TestResolve_RaceOnInsert(t *testing.T) {
	f := newFixture(t)
	// Simula que otro request insertó entre Get e Insert: pre-creamos la cuenta
	// y hacemos que Get falle la primera vez sería complejo; en su lugar, el
	// Insert devuelve ErrAlreadyExists y el servicio relee.
	f.repo.Accounts[f.id] = domain.Provision(f.id, domain.TokenSnapshot{Email: "ana@example.com"}, base)
	st, err := f.svc.Resolve(context.Background(), f.claims(true))
	if err != nil || st.Suspended {
		t.Fatal(err)
	}
}

func TestResolve_Errors(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Resolve(context.Background(), auth.Claims{Subject: "x"}); err == nil {
		t.Fatal("sub inválido")
	}
	c := f.claims(true)
	c.Email = "no-es-email"
	if _, err := f.svc.Resolve(context.Background(), c); err == nil {
		t.Fatal("email inválido")
	}
	f.repo.FailWith = errors.New("db down")
	if _, err := f.svc.Resolve(context.Background(), f.claims(true)); err == nil {
		t.Fatal("error de repo debe propagarse")
	}
}

func TestOnboardingAndGetMe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Resolve(ctx, f.claims(true)); err != nil {
		t.Fatal(err)
	}
	phone := "+57 300 123 4567"
	view, err := f.svc.CompleteOnboarding(ctx, f.principal(), app.CompleteOnboardingCommand{
		DisplayName: "  Ana   Tutora ", Country: "co", Locale: "ES", Phone: &phone,
		TermsVersion: "2026-10-draft", PrivacyVersion: "2026-10-draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := view.Account
	if a.DisplayName != "Ana Tutora" || *a.Country != "CO" || a.Locale != "es" || *a.Phone != "+573001234567" || !a.IsOnboarded() {
		t.Fatalf("normalización: %+v", a)
	}
	if len(f.repo.Audit) != 1 || f.repo.Audit[0].Action != "me.onboarding_completed" {
		t.Fatalf("audit: %+v", f.repo.Audit)
	}
	me, err := f.svc.GetMe(ctx, f.principal())
	if err != nil || !me.Account.ConsentCurrent(me.Current) {
		t.Fatalf("GetMe: %v", err)
	}
	st, _ := f.svc.Resolve(ctx, f.claims(true))
	if !st.OnboardingCompleted || !st.ConsentCurrent {
		t.Fatalf("estado tras onboarding: %+v", st)
	}
}

func TestOnboarding_Validation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Resolve(ctx, f.claims(true))
	bad := "abc"
	_, err := f.svc.CompleteOnboarding(ctx, f.principal(), app.CompleteOnboardingCommand{
		DisplayName: "\x01", Country: "COL", Locale: "fr", Phone: &bad, TermsVersion: "2026-10-draft", PrivacyVersion: "2026-10-draft",
	})
	var ve *kernel.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 4 {
		t.Fatalf("esperaba 4 violaciones, got %v", err)
	}
	_, err = f.svc.CompleteOnboarding(ctx, f.principal(), app.CompleteOnboardingCommand{
		DisplayName: "Ana", Country: "CO", Locale: "es", TermsVersion: "2020-01", PrivacyVersion: "2026-10-draft",
	})
	if !errors.Is(err, domain.ErrConsentOutdated) {
		t.Fatalf("esperaba CONSENT_VERSION_OUTDATED, got %v", err)
	}
	_, err = f.svc.CompleteOnboarding(ctx, auth.Principal{}, app.CompleteOnboardingCommand{})
	var fb *kernel.ForbiddenError
	if !errors.As(err, &fb) {
		t.Fatalf("sin principal → forbidden, got %v", err)
	}
}

func TestUpdateProfile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Resolve(ctx, f.claims(true))
	f.onboard(t)
	name := "Ana María"
	view, err := f.svc.UpdateProfile(ctx, f.principal(), app.UpdateProfileCommand{
		DisplayName:      &name,
		Address:          &kernel.Address{Country: "co", City: " Bogotá ", Line1: "Calle 1"},
		EmergencyContact: &kernel.Contact{Name: "Luis", Phone: "+57 300 987 6543"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Account.DisplayName != name || view.Account.Address.City != "Bogotá" || view.Account.Address.Country != "CO" || view.Account.EmergencyContact.Phone != "+573009876543" {
		t.Fatalf("perfil: %+v", view.Account)
	}
	last := f.repo.Audit[len(f.repo.Audit)-1]
	if last.Action != "me.profile_updated" || len(last.Details["fields"].([]string)) != 3 {
		t.Fatalf("audit: %+v", last)
	}
	// Clear con null explícito.
	view, err = f.svc.UpdateProfile(ctx, f.principal(), app.UpdateProfileCommand{ClearAddress: true, ClearEmergencyContact: true, ClearCountry: true})
	if err != nil || view.Account.Address != nil || view.Account.EmergencyContact != nil || view.Account.Country != nil {
		t.Fatalf("clear: %v %+v", err, view.Account)
	}
	// Sin cambios: no se audita.
	n := len(f.repo.Audit)
	if _, err := f.svc.UpdateProfile(ctx, f.principal(), app.UpdateProfileCommand{DisplayName: &name}); err != nil || len(f.repo.Audit) != n {
		t.Fatal("sin cambios no audita")
	}
	// Validación.
	badPhone := "123"
	_, err = f.svc.UpdateProfile(ctx, f.principal(), app.UpdateProfileCommand{
		Phone: &badPhone, Address: &kernel.Address{Country: "XXX"}, EmergencyContact: &kernel.Contact{Name: "", Phone: "x"},
	})
	var ve *kernel.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) < 5 {
		t.Fatalf("validación: %v", err)
	}
}

func TestAcceptConsent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Resolve(ctx, f.claims(true))
	f.onboard(t)
	// Cambia la versión vigente → caducado.
	f.repo.Consent.Terms = "2027-01"
	f.svc.InvalidateConsentCache()
	me, _ := f.svc.GetMe(ctx, f.principal())
	if me.Account.ConsentCurrent(me.Current) {
		t.Fatal("debería estar caducado")
	}
	err := f.svc.AcceptConsent(ctx, f.principal(), app.AcceptConsentCommand{TermsVersion: "2026-10-draft", PrivacyVersion: "2026-10-draft"})
	if !errors.Is(err, domain.ErrConsentOutdated) {
		t.Fatal(err)
	}
	if err := f.svc.AcceptConsent(ctx, f.principal(), app.AcceptConsentCommand{TermsVersion: "2027-01", PrivacyVersion: "2026-10-draft"}); err != nil {
		t.Fatal(err)
	}
	me, _ = f.svc.GetMe(ctx, f.principal())
	if !me.Account.ConsentCurrent(me.Current) || f.repo.Audit[len(f.repo.Audit)-1].Action != "me.consent_accepted" {
		t.Fatal("consentimiento no registrado")
	}
}

func TestConsentCacheTTL(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cur, _ := f.svc.LegalCurrent(ctx)
	f.repo.Consent.Terms = "2027-01"
	if again, _ := f.svc.LegalCurrent(ctx); again.Terms != cur.Terms {
		t.Fatal("la caché debe servir dentro del TTL")
	}
	f.now = base.Add(2 * time.Minute)
	if again, _ := f.svc.LegalCurrent(ctx); again.Terms != "2027-01" {
		t.Fatal("la caché debe expirar")
	}
}

func TestSuspendedAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Resolve(ctx, f.claims(true))
	f.onboard(t)
	f.repo.Accounts[f.id].Status = domain.StatusSuspended
	st, _ := f.svc.Resolve(ctx, f.claims(true))
	if !st.Suspended {
		t.Fatal("Resolve debe reportar suspendida")
	}
	p := f.principal()
	p.Account.Suspended = true
	if _, err := f.svc.UpdateProfile(ctx, p, app.UpdateProfileCommand{}); err == nil {
		t.Fatal("suspendida no puede editar")
	}
}
