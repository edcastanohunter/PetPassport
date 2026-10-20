package http_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

const aud = "petpassport-api"

type handlers struct {
	httpserver.SystemHandlers
	*identityhttp.Handlers
}

type env struct {
	t        *testing.T
	h        http.Handler
	jwks     *testkit.JWKS
	repo     *testkit.MemoryIdentityRepo
	contract *testkit.Contract
	userID   uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	j := testkit.NewJWKS(t)
	verifier, err := auth.NewVerifier(context.Background(), auth.VerifierConfig{Issuer: j.Issuer, Audience: aud, AllowedAZP: []string{"petpassport-web"}})
	if err != nil {
		t.Fatal(err)
	}
	repo := testkit.NewMemoryIdentityRepo()
	svc := identityapp.NewService(repo, slog.New(slog.NewTextHandler(discard{}, nil)))
	spec, _ := gen.GetSpec()
	guard, err := auth.NewGuard(spec, auth.GuardOptions{Verifier: verifier, Resolver: svc, Logger: slog.New(slog.NewTextHandler(discard{}, nil)), BasePath: "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	s := httpserver.New(":0", ":0", httpserver.Options{
		Logger: slog.New(slog.NewTextHandler(discard{}, nil)), Registry: prometheus.NewRegistry(),
		Health: httpserver.NewHealth(), CORSOrigins: []string{"http://localhost:5173"},
		Handlers: handlers{Handlers: identityhttp.NewHandlers(svc, "salt")}, Guard: guard.Middleware,
	})
	return &env{t: t, h: s.Public.Handler, jwks: j, repo: repo, contract: testkit.NewContract(t), userID: uuid.Must(uuid.NewV7())}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func (e *env) token(verified bool) string {
	return e.jwks.Token(e.t, testkit.Claims{Subject: e.userID.String(), Email: "ana@example.com", EmailVerified: verified, Name: "Ana", Audience: aud, Roles: []string{"pet_owner"}})
}

func (e *env) call(method, path, token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequestWithContext(context.Background(), method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.RemoteAddr = "203.0.113.9:4567"
	return e.contract.Do(e.h, r)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("json: %v: %s", err, rec.Body.String())
	}
	return v
}

const onboardingBody = `{"display_name":"Ana Tutora","country":"co","locale":"es","phone":"+57 300 123 4567","accept_terms_version":"2026-10-draft","accept_privacy_version":"2026-10-draft"}`

func TestLegalCurrent_Public(t *testing.T) {
	e := newEnv(t)
	rec := e.call(http.MethodGet, "/api/v1/legal/current", "", "")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	v := decode[gen.LegalCurrent](t, rec)
	if v.TermsVersion != "2026-10-draft" || v.TermsUrl != "/legal/terms" {
		t.Fatalf("%+v", v)
	}
}

func TestMe_FullFlow(t *testing.T) {
	e := newEnv(t)
	// 1. GET /me provoca JIT; email sin verificar aceptado.
	rec := e.call(http.MethodGet, "/api/v1/me", e.token(false), "")
	if rec.Code != 200 {
		t.Fatalf("GET /me: %d %s", rec.Code, rec.Body.String())
	}
	me := decode[gen.Me](t, rec)
	if !me.OnboardingRequired || !me.ConsentOutdated || me.EmailVerified || me.Status != "active" || uuid.UUID(me.Id) != e.userID {
		t.Fatalf("me inicial: %+v", me)
	}
	// 2. Onboarding con email sin verificar → 403 EMAIL_NOT_VERIFIED.
	rec = e.call(http.MethodPost, "/api/v1/me/onboarding", e.token(false), onboardingBody)
	if rec.Code != 403 || decode[gen.Problem](t, rec).Code != "EMAIL_NOT_VERIFIED" {
		t.Fatalf("onboarding sin verificar: %d %s", rec.Code, rec.Body.String())
	}
	// 3. PATCH antes del onboarding → 403 ONBOARDING_REQUIRED.
	rec = e.call(http.MethodPatch, "/api/v1/me", e.token(true), `{"display_name":"X"}`)
	if rec.Code != 403 || decode[gen.Problem](t, rec).Code != "ONBOARDING_REQUIRED" {
		t.Fatalf("patch sin onboarding: %d %s", rec.Code, rec.Body.String())
	}
	// 4. Onboarding OK.
	rec = e.call(http.MethodPost, "/api/v1/me/onboarding", e.token(true), onboardingBody)
	if rec.Code != 200 {
		t.Fatalf("onboarding: %d %s", rec.Code, rec.Body.String())
	}
	me = decode[gen.Me](t, rec)
	if me.OnboardingRequired || me.ConsentOutdated || *me.Country != "CO" || *me.Phone != "+573001234567" || me.Consent == nil {
		t.Fatalf("me tras onboarding: %+v", me)
	}
	// 5. PATCH: cambia nombre, añade dirección, borra teléfono con null.
	rec = e.call(http.MethodPatch, "/api/v1/me", e.token(true),
		`{"display_name":"Ana María","phone":null,"address":{"country":"CO","city":"Bogotá","line1":"Calle 1 # 2-3"},"emergency_contact":{"name":"Luis","phone":"+57 300 987 6543","relationship":"hermano"}}`)
	if rec.Code != 200 {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	me = decode[gen.Me](t, rec)
	if me.DisplayName != "Ana María" || me.Phone != nil || me.Address == nil || me.Address.City != "Bogotá" || me.EmergencyContact == nil || me.EmergencyContact.Phone != "+573009876543" {
		t.Fatalf("me tras patch: %+v", me)
	}
	// 6. Consentimiento: versión no vigente → 422; vigente → 204.
	rec = e.call(http.MethodPost, "/api/v1/me/consent", e.token(true), `{"terms_version":"2020-01","privacy_version":"2026-10-draft"}`)
	if rec.Code != 422 || decode[gen.Problem](t, rec).Code != "CONSENT_VERSION_OUTDATED" {
		t.Fatalf("consent caducado: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.call(http.MethodPost, "/api/v1/me/consent", e.token(true), `{"terms_version":"2026-10-draft","privacy_version":"2026-10-draft"}`)
	if rec.Code != 204 {
		t.Fatalf("consent: %d %s", rec.Code, rec.Body.String())
	}
	// 7. Auditoría: onboarding, patch, consent; sin PII en details.
	if len(e.repo.Audit) != 3 {
		t.Fatalf("audit entries = %d", len(e.repo.Audit))
	}
	for _, a := range e.repo.Audit {
		b, _ := json.Marshal(a.Details)
		if strings.Contains(string(b), "Ana") || strings.Contains(string(b), "300") || a.IPHash == nil || a.CorrelationID == nil {
			t.Fatalf("audit con PII o sin metadatos: %+v", a)
		}
	}
}

func TestMe_SchemaValidation(t *testing.T) {
	e := newEnv(t)
	_ = e.call(http.MethodGet, "/api/v1/me", e.token(true), "")
	cases := map[string]string{
		"campo desconocido":    `{"display_name":"A","country":"CO","locale":"es","accept_terms_version":"1","accept_privacy_version":"1","x":1}`,
		"locale fuera de enum": `{"display_name":"A","country":"CO","locale":"fr","accept_terms_version":"1","accept_privacy_version":"1"}`,
		"falta requerido":      `{"display_name":"A"}`,
		"json inválido":        `{`,
		"nombre con control":   "{\"display_name\":\"A\\u0001\",\"country\":\"CO\",\"locale\":\"es\",\"accept_terms_version\":\"1\",\"accept_privacy_version\":\"1\"}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := e.call(http.MethodPost, "/api/v1/me/onboarding", e.token(true), body)
			if rec.Code != 422 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			p := decode[gen.Problem](t, rec)
			if p.Code != "VALIDATION_FAILED" || (p.Detail != nil && strings.Contains(*p.Detail, "\u0001")) {
				t.Fatalf("%+v", p)
			}
		})
	}
	// Validación semántica (VO) con errors[] por campo.
	rec := e.call(http.MethodPost, "/api/v1/me/onboarding", e.token(true),
		`{"display_name":"A","country":"CO","locale":"es","phone":"+1 2","accept_terms_version":"2026-10-draft","accept_privacy_version":"2026-10-draft"}`)
	p := decode[gen.Problem](t, rec)
	if rec.Code != 422 || p.Errors == nil || len(*p.Errors) != 1 || (*p.Errors)[0].Field != "phone" {
		t.Fatalf("%d %+v", rec.Code, p)
	}
	// PATCH vacío (minProperties) y Content-Type incorrecto.
	rec = e.call(http.MethodPost, "/api/v1/me/onboarding", e.token(true), onboardingBody)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := e.call(http.MethodPatch, "/api/v1/me", e.token(true), `{}`); rec.Code != 422 {
		t.Fatalf("patch vacío: %d", rec.Code)
	}
	rec = e.call(http.MethodPatch, "/api/v1/me", e.token(true), `{"display_name":"A","x":1}`)
	p = decode[gen.Problem](t, rec)
	if rec.Code != 422 || p.Errors == nil || len(*p.Errors) == 0 || (*p.Errors)[0].Code != "UNKNOWN_FIELD" {
		t.Fatalf("campo desconocido: %d %+v", rec.Code, p)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/api/v1/me", strings.NewReader("display_name=A"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Authorization", "Bearer "+e.token(true))
	rec = e.contract.Do(e.h, r)
	if rec.Code != 415 || decode[gen.Problem](t, rec).Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("content-type: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMe_Unauthenticated(t *testing.T) {
	e := newEnv(t)
	rec := e.call(http.MethodGet, "/api/v1/me", "", "")
	if rec.Code != 401 || decode[gen.Problem](t, rec).Code != "UNAUTHENTICATED" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Cache-Control")
	}
}
