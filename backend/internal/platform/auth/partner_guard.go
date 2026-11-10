package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
)

// PartnerStatusLoader consulta el estado de la integración del partner en la base de datos.
type PartnerStatusLoader interface {
	IsPartnerActive(ctx context.Context, orgID uuid.UUID) (bool, error)
}

// PartnerGuard valida access tokens para la Partner API (/partner/v1) (ARQ-005, PTN-002, SEC-002).
// Exige audiencia `petpassport-partner-api` y construye PartnerPrincipal en el request context.
type PartnerGuard struct {
	verifier *Verifier
	loader   PartnerStatusLoader
	logger   *slog.Logger
}

// PartnerGuardOptions configura el middleware de autenticación de partners.
type PartnerGuardOptions struct {
	Verifier *Verifier
	Loader   PartnerStatusLoader
	Logger   *slog.Logger
}

// NewPartnerGuard construye el guard para rutas de partners.
func NewPartnerGuard(opts PartnerGuardOptions) *PartnerGuard {
	return &PartnerGuard{
		verifier: opts.Verifier,
		loader:   opts.Loader,
		logger:   opts.Logger,
	}
}

// Middleware intercepta requests a /partner/v1 y aplica autenticación B2B client_credentials.
func (g *PartnerGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken, ok := BearerToken(r.Header.Get("Authorization"))
		if !ok || rawToken == "" {
			problem.Write(w, r, problem.New(problem.Unauthenticated, "No autenticado", "Se requiere cabecera Authorization: Bearer <token> con audiencia petpassport-partner-api."))
			return
		}

		if g.verifier == nil {
			problem.Write(w, r, problem.New(problem.Unavailable, "Servicio no disponible", "Verificador de identidad no disponible."))
			return
		}

		claims, err := g.verifier.Verify(r.Context(), rawToken)
		if err != nil {
			if g.logger != nil {
				g.logger.Warn("partner auth: token inválido", "err", err)
			}
			problem.Write(w, r, problem.New(problem.Unauthenticated, "Token inválido", "El token presentado no es válido para la Partner API."))
			return
		}

		// Extraer partner_org_id del token o claims
		orgIDStr := claims.PartnerOrgID
		if orgIDStr == "" {
			orgIDStr = claims.Subject
		}
		orgID, parseErr := uuid.Parse(orgIDStr)
		if parseErr != nil {
			problem.Write(w, r, problem.New(problem.Unauthenticated, "Token no contiene partner_org_id válido", "El token de partner carece de identificador de organización."))
			return
		}

		// Verificar que el partner esté activo en la plataforma si hay loader
		if g.loader != nil {
			active, checkErr := g.loader.IsPartnerActive(r.Context(), orgID)
			if checkErr != nil || !active {
				problem.Write(w, r, problem.New(problem.Forbidden, "Partner inactivo o suspendido", "La integración del partner no se encuentra activa."))
				return
			}
		}

		scopes := strings.Fields(claims.Scope)
		if len(scopes) == 0 {
			scopes = []string{"partner-api"}
		}
		// Construir PartnerPrincipal
		pp := PartnerPrincipal{
			OrgID:    orgID,
			ClientID: claims.AuthorizedParty,
			Scopes:   scopes,
		}

		// Registrar actor en reqctx para auditoría y logs (ARQ-010)
		reqctx.From(r.Context()).SetActor("partner:" + orgID.String())

		// Inyectar cabecera X-PP-Partner para correlación en Traefik y WAF (ARQ-010)
		w.Header().Set("X-PP-Partner", orgID.String())

		next.ServeHTTP(w, r.WithContext(WithPartnerPrincipal(r.Context(), pp)))
	})
}
