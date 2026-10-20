package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
)

// Requirement describe qué estado de cuenta exige una acción.
type Requirement struct {
	Public               bool
	AllowUnverified      bool
	AllowNotOnboarded    bool
	AllowConsentOutdated bool
}

// defaultPolicy: acciones de cuenta propia y onboarding. Cualquier acción no
// listada exige email verificado, onboarding completo y consentimiento vigente.
var defaultPolicy = map[string]Requirement{
	"public":              {Public: true},
	"me:read":             {AllowUnverified: true, AllowNotOnboarded: true, AllowConsentOutdated: true},
	"onboarding:complete": {AllowNotOnboarded: true, AllowConsentOutdated: true},
	"me:update":           {AllowConsentOutdated: true},
}

// Guard es el middleware de autenticación por ruta. Lee la acción de cada
// operación del contrato (`x-pp-action`) y aplica la política correspondiente;
// el Principal resultante queda en el contexto para los handlers y para authz.
type Guard struct {
	verifier    *Verifier
	resolver    AccountResolver
	memberships MembershipLoader
	logger      *slog.Logger
	actions     map[string]string // "GET /api/v1/me" -> "me:read"
	policy      map[string]Requirement
}

// GuardOptions configura el Guard.
type GuardOptions struct {
	Verifier    *Verifier // nil = autenticación no disponible (503 en rutas protegidas)
	Resolver    AccountResolver
	Memberships MembershipLoader // opcional
	Logger      *slog.Logger
	BasePath    string // prefijo del router donde se monta el contrato, p. ej. /api/v1
	Policy      map[string]Requirement
}

// NewGuard valida que toda operación del contrato declare `x-pp-action`.
func NewGuard(spec *openapi3.T, o GuardOptions) (*Guard, error) {
	if o.Resolver == nil {
		return nil, errors.New("auth: resolver obligatorio")
	}
	if o.Policy == nil {
		o.Policy = defaultPolicy
	}
	g := &Guard{verifier: o.Verifier, resolver: o.Resolver, memberships: o.Memberships, logger: o.Logger, actions: map[string]string{}, policy: o.Policy}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-pp-action"]
			action, _ := raw.(string)
			if !ok || action == "" {
				return nil, fmt.Errorf("auth: %s %s no declara x-pp-action", method, path)
			}
			g.actions[strings.ToUpper(method)+" "+o.BasePath+path] = action
		}
	}
	return g, nil
}

// ActionFor devuelve la acción declarada para método+patrón de ruta.
func (g *Guard) ActionFor(method, pattern string) (string, bool) {
	a, ok := g.actions[strings.ToUpper(method)+" "+pattern]
	return a, ok
}

// Middleware aplica la política de la ruta enrutada por chi.
func (g *Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pattern := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern = rc.RoutePattern()
		}
		action, known := g.ActionFor(r.Method, pattern)
		if !known {
			// Ruta fuera del contrato: deny by default.
			problem.Write(w, r, problem.New(problem.Forbidden, "Prohibido", "Operación no declarada."))
			return
		}
		req, ok := g.policy[action]
		if !ok {
			req = Requirement{}
		}
		if req.Public {
			next.ServeHTTP(w, r)
			return
		}
		if g.verifier == nil {
			problem.Write(w, r, problem.New(problem.Unavailable, "No disponible", "La autenticación no está configurada."))
			return
		}
		raw, ok := BearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthenticated(w, r)
			return
		}
		claims, err := g.verifier.Verify(r.Context(), raw)
		if err != nil {
			g.logger.WarnContext(r.Context(), "token rejected", "reason", err.Error(), "route", pattern)
			unauthenticated(w, r)
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			unauthenticated(w, r)
			return
		}
		reqctx.From(r.Context()).SetActor(userID.String())
		state, err := g.resolver.Resolve(r.Context(), claims)
		if err != nil {
			g.logger.ErrorContext(r.Context(), "account resolve failed", "error", err.Error())
			problem.Write(w, r, problem.New(problem.Internal, "Error interno", "No se pudo resolver la cuenta."))
			return
		}
		if state.Suspended {
			problem.Write(w, r, problem.New(problem.AccountSuspended, "Cuenta suspendida", "La cuenta está suspendida."))
			return
		}
		if !req.AllowUnverified && !claims.EmailVerified {
			problem.Write(w, r, problem.New(problem.EmailNotVerified, "Email no verificado", "Verifica tu email para continuar."))
			return
		}
		if !req.AllowNotOnboarded && !state.OnboardingCompleted {
			problem.Write(w, r, problem.New(problem.OnboardingRequired, "Onboarding pendiente", "Completa el onboarding para continuar."))
			return
		}
		if !req.AllowConsentOutdated && !state.ConsentCurrent {
			p := problem.New(problem.ConsentVersionOutdated, "Consentimiento pendiente", "Acepta los términos vigentes para continuar.")
			p.Status = http.StatusForbidden
			problem.Write(w, r, p)
			return
		}
		p := Principal{
			UserID: userID, Email: claims.Email, EmailVerified: claims.EmailVerified,
			GlobalRoles: claims.Roles(), SessionID: claims.SessionID, Account: state,
			AuthTime: time.Unix(claims.IssuedAt, 0),
		}
		if g.memberships != nil {
			ms, err := g.memberships.Memberships(r.Context(), userID)
			if err != nil {
				g.logger.ErrorContext(r.Context(), "memberships load failed", "error", err.Error())
				problem.Write(w, r, problem.New(problem.Internal, "Error interno", "No se pudieron cargar las membresías."))
				return
			}
			p.Memberships = ms
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

func unauthenticated(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="petpassport", error="invalid_token"`)
	problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere un token válido."))
}
