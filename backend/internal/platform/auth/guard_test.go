package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type fakeResolver struct {
	state auth.AccountState
	err   error
	calls int
}

func (f *fakeResolver) Resolve(context.Context, auth.Claims) (auth.AccountState, error) {
	f.calls++
	return f.state, f.err
}

const subject = "0192f3a2-2222-7aaa-bbbb-dddddddddddd"

// newGuardedRouter monta el contrato real con un handler trivial por ruta para
// probar la política por operación.
func newGuardedRouter(t *testing.T, j *testkit.JWKS, r *fakeResolver, verifier *auth.Verifier) http.Handler {
	t.Helper()
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.NewGuard(spec, auth.GuardOptions{
		Verifier: verifier, Resolver: r, Logger: slog.New(slog.NewTextHandler(testWriter{t}, nil)), BasePath: "/api/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := chi.NewRouter()
	ok := func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFrom(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"user_id": p.UserID.String(), "roles": p.GlobalRoles})
	}
	mux.Route("/api/v1", func(api chi.Router) {
		api.With(g.Middleware).Get("/healthz", ok)
		api.With(g.Middleware).Get("/me", ok)
		api.With(g.Middleware).Patch("/me", ok)
		api.With(g.Middleware).Post("/me/onboarding", ok)
		api.With(g.Middleware).Post("/me/consent", ok)
		api.With(g.Middleware).Get("/legal/current", ok)
		api.With(g.Middleware).Get("/fuera-del-contrato", ok)
	})
	return mux
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func do(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func TestGuard_RequiresActionOnEveryOperation(t *testing.T) {
	spec, _ := gen.GetSpec()
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if a, _ := op.Extensions["x-pp-action"].(string); a == "" {
				t.Errorf("%s %s sin x-pp-action", method, path)
			}
		}
	}
}

func TestGuard_PublicAndUnknownRoutes(t *testing.T) {
	j := testkit.NewJWKS(t)
	h := newGuardedRouter(t, j, &fakeResolver{}, newVerifier(t, j))
	if rec := do(h, http.MethodGet, "/api/v1/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("healthz público: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/v1/legal/current", ""); rec.Code != http.StatusOK {
		t.Fatalf("legal público: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/v1/fuera-del-contrato", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("ruta fuera del contrato debe ser 403, got %d", rec.Code)
	}
}

func TestGuard_Unauthenticated(t *testing.T) {
	j := testkit.NewJWKS(t)
	h := newGuardedRouter(t, j, &fakeResolver{}, newVerifier(t, j))
	rec := do(h, http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusUnauthorized || code(t, rec) != "UNAUTHENTICATED" {
		t.Fatalf("sin token: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("falta WWW-Authenticate")
	}
	rec = do(h, http.MethodGet, "/api/v1/me", "garbage")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token inválido: %d", rec.Code)
	}
	rec = do(h, http.MethodGet, "/api/v1/me", j.Token(t, testkit.Claims{Subject: "no-uuid", Audience: aud}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sub no uuid: %d", rec.Code)
	}
}

func TestGuard_NoVerifierReturns503(t *testing.T) {
	j := testkit.NewJWKS(t)
	h := newGuardedRouter(t, j, &fakeResolver{}, nil)
	rec := do(h, http.MethodGet, "/api/v1/me", j.Token(t, testkit.Claims{Subject: subject, Audience: aud}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/v1/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("público sigue disponible: %d", rec.Code)
	}
}

func TestGuard_AccountStatePolicy(t *testing.T) {
	j := testkit.NewJWKS(t)
	verified := j.Token(t, testkit.Claims{Subject: subject, Audience: aud, EmailVerified: true, Roles: []string{"pet_owner"}})
	unverified := j.Token(t, testkit.Claims{Subject: subject, Audience: aud, EmailVerified: false})
	full := auth.AccountState{OnboardingCompleted: true, ConsentCurrent: true}

	cases := []struct {
		name   string
		state  auth.AccountState
		token  string
		method string
		path   string
		want   int
		code   string
	}{
		{"me:read acepta no verificado", auth.AccountState{}, unverified, "GET", "/api/v1/me", 200, ""},
		{"me:read acepta sin onboarding", auth.AccountState{}, verified, "GET", "/api/v1/me", 200, ""},
		{"onboarding exige verificado", auth.AccountState{}, unverified, "POST", "/api/v1/me/onboarding", 403, "EMAIL_NOT_VERIFIED"},
		{"onboarding acepta sin onboarding", auth.AccountState{}, verified, "POST", "/api/v1/me/onboarding", 200, ""},
		{"me:update exige onboarding", auth.AccountState{}, verified, "PATCH", "/api/v1/me", 403, "ONBOARDING_REQUIRED"},
		{"me:update acepta consentimiento caducado", auth.AccountState{OnboardingCompleted: true}, verified, "POST", "/api/v1/me/consent", 200, ""},
		{"suspendida", auth.AccountState{Suspended: true, OnboardingCompleted: true, ConsentCurrent: true}, verified, "GET", "/api/v1/me", 403, "ACCOUNT_SUSPENDED"},
		{"todo en orden", full, verified, "PATCH", "/api/v1/me", 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeResolver{state: tc.state}
			h := newGuardedRouter(t, j, r, newVerifier(t, j))
			rec := do(h, tc.method, tc.path, tc.token)
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if tc.code != "" && code(t, rec) != tc.code {
				t.Fatalf("code = %s, want %s", code(t, rec), tc.code)
			}
			if r.calls != 1 {
				t.Fatalf("resolver llamado %d veces", r.calls)
			}
		})
	}
}

func TestGuard_PrincipalInContext(t *testing.T) {
	j := testkit.NewJWKS(t)
	h := newGuardedRouter(t, j, &fakeResolver{state: auth.AccountState{OnboardingCompleted: true, ConsentCurrent: true}}, newVerifier(t, j))
	tok := j.Token(t, testkit.Claims{Subject: subject, Audience: aud, EmailVerified: true, Roles: []string{"pet_owner"}})
	rec := do(h, http.MethodGet, "/api/v1/me", tok)
	var body struct {
		UserID string   `json:"user_id"`
		Roles  []string `json:"roles"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.UserID != subject || len(body.Roles) != 1 || body.Roles[0] != "pet_owner" {
		t.Fatalf("principal: %+v", body)
	}
}

func TestGuard_ResolverErrorIs500(t *testing.T) {
	j := testkit.NewJWKS(t)
	h := newGuardedRouter(t, j, &fakeResolver{err: errors.New("db down")}, newVerifier(t, j))
	rec := do(h, http.MethodGet, "/api/v1/me", j.Token(t, testkit.Claims{Subject: subject, Audience: aud}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestGuard_RequiresResolver(t *testing.T) {
	spec, _ := gen.GetSpec()
	if _, err := auth.NewGuard(spec, auth.GuardOptions{}); err == nil {
		t.Fatal("esperaba error sin resolver")
	}
}
