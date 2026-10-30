//go:build integration

// Integración de M1.1: repositorio PostgreSQL de Identity, aprovisionamiento JIT
// y flujo HTTP /me contra Postgres real con JWT de prueba (PP-F1-DAT-004,
// DAT-007, IAM-009, API-003, QA-004).
package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	notificationshttp "github.com/edcastanohunter/petpassport/backend/internal/notifications/http"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	schedulinghttp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/http"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type handlers struct {
	httpserver.SystemHandlers
	*identityhttp.Handlers
	*identityhttp.AdminHandlers
	*pethttp.PetHandlers
	*orghttp.OrgHandlers
	*orghttp.StatsHandlers
	*clinicalhttp.ClinicalHandlers
	*schedulinghttp.SchedulingHandlers
	*notificationshttp.NotificationHandlers
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestIdentity_RepositoryRoundTrip(t *testing.T) {
	url := testkit.Postgres(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, url, 2, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := identityinfra.NewPostgresRepository(pool)

	cur, err := repo.CurrentConsent(ctx)
	if err != nil || cur.Terms != "2026-10-draft" || cur.PrivacyURL != "/legal/privacy" {
		t.Fatalf("consent: %+v %v", cur, err)
	}

	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC().Truncate(time.Microsecond)
	acc := domain.Provision(id, domain.TokenSnapshot{Email: "ana@example.com", EmailVerified: true, DisplayName: "Ana", Roles: []string{"pet_owner"}}, now)
	if err := repo.Insert(ctx, acc); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, acc); err != identityapp.ErrAlreadyExists {
		t.Fatalf("duplicado: %v", err)
	}
	phone := kernel.Phone("+573001234567")
	if err := acc.CompleteOnboarding(domain.OnboardingData{DisplayName: "Ana", Country: "CO", Locale: "es", Phone: &phone, TermsVersion: cur.Terms, PrivacyVersion: cur.Privacy}, cur, now); err != nil {
		t.Fatal(err)
	}
	acc.Address = &kernel.Address{Country: "CO", City: "Bogotá", Line1: "Calle 1"}
	acc.EmergencyContact = &kernel.Contact{Name: "Luis", Phone: "+573009876543"}
	actor := id
	entry := audit.Entry{ActorID: &actor, ActorRoles: []string{"pet_owner"}, Action: "me.profile_updated", TargetType: "user_account", TargetID: &actor, Details: map[string]any{"fields": []string{"address"}}}
	if err := repo.Save(ctx, acc, &entry); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "ana@example.com" || !got.IsOnboarded() || !got.ConsentCurrent(cur) || *got.Phone != phone ||
		got.Address.City != "Bogotá" || got.EmergencyContact.Name != "Luis" || got.GlobalRoles[0] != "pet_owner" {
		t.Fatalf("round trip: %+v", got)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform.audit_log WHERE actor_id = $1 AND action = 'me.profile_updated'`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit_log: %d %v", n, err)
	}
	// citext: el email es único sin distinguir mayúsculas.
	other := domain.Provision(uuid.Must(uuid.NewV7()), domain.TokenSnapshot{Email: "ANA@example.com"}, now)
	if err := repo.Insert(ctx, other); err != identityapp.ErrAlreadyExists {
		t.Fatalf("citext unique: %v", err)
	}
	if _, err := repo.Get(ctx, uuid.Must(uuid.NewV7())); err == nil {
		t.Fatal("esperaba not found")
	}
}

func TestIdentity_HTTPFlowAgainstPostgres(t *testing.T) {
	url := testkit.Postgres(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, url, 4, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	j := testkit.NewJWKS(t)
	verifier, err := auth.NewVerifier(ctx, auth.VerifierConfig{Issuer: j.Issuer, Audience: "petpassport-api"})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	svc := identityapp.NewService(identityinfra.NewPostgresRepository(pool), logger)
	spec, _ := gen.GetSpec()
	guard, err := auth.NewGuard(spec, auth.GuardOptions{Verifier: verifier, Resolver: svc, Logger: logger, BasePath: "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(":0", ":0", httpserver.Options{
		Logger: logger, Registry: prometheus.NewRegistry(), Health: httpserver.NewHealth(),
		Handlers: handlers{Handlers: identityhttp.NewHandlers(svc, "salt"), PetHandlers: pethttp.NewPetHandlers(nil, nil, nil, nil, nil)}, Guard: guard.Middleware,
	})
	contract := testkit.NewContract(t)
	userID := uuid.Must(uuid.NewV7())
	tok := j.Token(t, testkit.Claims{Subject: userID.String(), Email: "ana@example.com", EmailVerified: true, Name: "Ana", Audience: "petpassport-api", Roles: []string{"pet_owner"}})

	call := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequestWithContext(ctx, method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer "+tok)
		return contract.Do(srv.Public.Handler, r)
	}

	rec := call(http.MethodGet, "/api/v1/me", "")
	if rec.Code != 200 {
		t.Fatalf("GET /me: %d %s", rec.Code, rec.Body.String())
	}
	var me gen.Me
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if !me.OnboardingRequired || me.Email != "ana@example.com" {
		t.Fatalf("jit: %+v", me)
	}
	rec = call(http.MethodPost, "/api/v1/me/onboarding", `{"display_name":"Ana","country":"CO","locale":"es","accept_terms_version":"2026-10-draft","accept_privacy_version":"2026-10-draft"}`)
	if rec.Code != 200 {
		t.Fatalf("onboarding: %d %s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodPatch, "/api/v1/me", `{"phone":"+57 300 123 4567"}`)
	if rec.Code != 200 {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Phone == nil || *me.Phone != "+573001234567" || me.OnboardingRequired || me.ConsentOutdated {
		t.Fatalf("me: %+v", me)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform.audit_log WHERE actor_id = $1`, userID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit rows = %d (%v)", n, err)
	}
	var lastLogin *time.Time
	if err := pool.QueryRow(ctx, `SELECT last_login_at FROM identity.user_accounts WHERE id = $1`, userID).Scan(&lastLogin); err != nil || lastLogin == nil {
		t.Fatalf("last_login_at: %v", err)
	}
}
