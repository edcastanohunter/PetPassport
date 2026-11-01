package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
)

// Config define límites y listas blancas para el middleware de rate limit.
type Config struct {
	Allowlist []string // IPs, UserIDs o tokens exentos de limitación (ej: k6 en staging)

	// Límites configurables por entorno (valores por defecto según ARQ-007)
	UserSearchLimit   int64         // defecto: 120 / min
	OrgSearchLimit    int64         // defecto: 1200 / min
	CommandsLimit     int64         // defecto: 60 / min
	LookupLimit       int64         // defecto: 20 / hora
	ExportLimit       int64         // defecto: 5 / hora
	PublicIPLimit     int64         // defecto: 60 / min
	ShareTokenLimit   int64         // defecto: 30 / min

	SearchWindow      time.Duration // defecto: 1 minuto
	CommandsWindow    time.Duration // defecto: 1 minuto
	LookupWindow      time.Duration // defecto: 1 hora
	ExportWindow      time.Duration // defecto: 1 hora
	PublicWindow      time.Duration // defecto: 1 minuto
	ShareTokenWindow  time.Duration // defecto: 1 minuto
}

// DefaultConfig devuelve la configuración por defecto de ARQ-007.
func DefaultConfig() Config {
	return Config{
		UserSearchLimit:  120,
		OrgSearchLimit:   1200,
		CommandsLimit:    60,
		LookupLimit:      20,
		ExportLimit:      5,
		PublicIPLimit:    60,
		ShareTokenLimit:  30,
		SearchWindow:     time.Minute,
		CommandsWindow:   time.Minute,
		LookupWindow:     time.Hour,
		ExportWindow:     time.Hour,
		PublicWindow:     time.Minute,
		ShareTokenWindow: time.Minute,
	}
}

// Middleware construye el handler de rate limiting distribuido.
func Middleware(limiter *Limiter, cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := extractIP(r)
			principal, hasPrincipal := auth.PrincipalFrom(r.Context())

			// 1. Comprobar allowlist
			if isAllowlisted(cfg.Allowlist, clientIP, principal, hasPrincipal) {
				next.ServeHTTP(w, r)
				return
			}

			// 2. Determinar ámbito y reglas a evaluar
			checks := determineChecks(r, clientIP, principal, hasPrincipal, cfg)
			if len(checks) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// 3. Ejecutar comprobaciones
			var mostRestrictive Result
			first := true

			for _, chk := range checks {
				res, err := limiter.Check(r.Context(), chk.Key, chk.RouteGroup, chk.Limit, chk.Window)
				if err != nil {
					// En caso de error inesperado, permitir (fail-open)
					continue
				}

				if first || !res.Allowed || (res.Remaining < mostRestrictive.Remaining && mostRestrictive.Allowed) {
					mostRestrictive = res
					first = false
				}

				if !res.Allowed {
					// Si alguna regla bloquea, se detiene
					mostRestrictive = res
					break
				}
			}

			// 4. Fijar cabeceras estándar IETF
			w.Header().Set("RateLimit-Limit", strconv.FormatInt(mostRestrictive.Limit, 10))
			w.Header().Set("RateLimit-Remaining", strconv.FormatInt(mostRestrictive.Remaining, 10))
			w.Header().Set("RateLimit-Reset", strconv.FormatInt(mostRestrictive.ResetSeconds, 10))

			if !mostRestrictive.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(mostRestrictive.RetryAfterSeconds, 10))
				p := problem.New(
					problem.RateLimited,
					"Demasiadas solicitudes",
					fmt.Sprintf("Has superado el límite de solicitudes para el grupo '%s'. Intenta nuevamente en %d segundos.", mostRestrictive.RouteGroup, mostRestrictive.RetryAfterSeconds),
				)
				problem.Write(w, r, p)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type checkItem struct {
	Key        string
	RouteGroup string
	Limit      int64
	Window     time.Duration
}

func determineChecks(r *http.Request, clientIP string, principal auth.Principal, hasPrincipal bool, cfg Config) []checkItem {
	var checks []checkItem
	path := r.URL.Path

	// A. Token de enlace (/share/{token})
	if strings.Contains(path, "/share/") {
		token := extractShareToken(path)
		if token != "" {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:token:%s:share", token),
				RouteGroup: "share",
				Limit:      cfg.ShareTokenLimit,
				Window:     cfg.ShareTokenWindow,
			})
		}
	}

	// B. Búsqueda y suggest
	if strings.Contains(path, "/search") || strings.Contains(path, "/suggest") {
		if hasPrincipal && principal.UserID != uuid.Nil {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:user:%s:search", principal.UserID),
				RouteGroup: "search",
				Limit:      cfg.UserSearchLimit,
				Window:     cfg.SearchWindow,
			})

			// Límite por organización únicamente si el principal es miembro activo de ella (evita DoS cross-tenant)
			orgHeader := r.Header.Get("X-PP-Organization")
			if orgHeader != "" {
				if parsedOrgID, err := uuid.Parse(orgHeader); err == nil {
					isMember := false
					for _, m := range principal.Memberships {
						if m.OrganizationID == parsedOrgID && m.OrgStatus == "active" {
							isMember = true
							break
						}
					}
					if isMember {
						checks = append(checks, checkItem{
							Key:        fmt.Sprintf("pp:rl:org:%s:search", parsedOrgID),
							RouteGroup: "search",
							Limit:      cfg.OrgSearchLimit,
							Window:     cfg.SearchWindow,
						})
					}
				}
			}
		}
		return checks
	}

	// C. Lookup (pet-lookup, organizations/lookup)
	if strings.Contains(path, "lookup") {
		if hasPrincipal && principal.UserID != uuid.Nil {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:user:%s:lookup", principal.UserID),
				RouteGroup: "lookup",
				Limit:      cfg.LookupLimit,
				Window:     cfg.LookupWindow,
			})
		}
		return checks
	}

	// D. Export (cohortes, export)
	if strings.Contains(path, "export") {
		if hasPrincipal && principal.UserID != uuid.Nil {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:user:%s:export", principal.UserID),
				RouteGroup: "export",
				Limit:      cfg.ExportLimit,
				Window:     cfg.ExportWindow,
			})
		}
		return checks
	}

	// E. Rutas públicas no autenticadas
	if !hasPrincipal {
		if strings.Contains(path, "/share") ||
			strings.Contains(path, "/verify") ||
			strings.Contains(path, "/unsubscribe") ||
			strings.Contains(path, "/directory") ||
			strings.Contains(path, "/legal") {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:ip:%s:public", clientIP),
				RouteGroup: "public",
				Limit:      cfg.PublicIPLimit,
				Window:     cfg.PublicWindow,
			})
		}
		return checks
	}

	// F. Comandos (POST, PUT, PATCH, DELETE) autenticados
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
		if principal.UserID != uuid.Nil {
			checks = append(checks, checkItem{
				Key:        fmt.Sprintf("pp:rl:user:%s:commands", principal.UserID),
				RouteGroup: "commands",
				Limit:      cfg.CommandsLimit,
				Window:     cfg.CommandsWindow,
			})
		}
	}

	return checks
}

func extractShareToken(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "share" && i+1 < len(parts) {
			token := parts[i+1]
			// Limpiar posibles sufijos como /unlock
			if slash := strings.Index(token, "/"); slash != -1 {
				token = token[:slash]
			}
			// Validar longitud mínima razonable de token para evitar spam de claves en Redis
			if len(token) >= 4 && len(token) <= 128 {
				return token
			}
		}
	}
	return ""
}

func extractIP(r *http.Request) string {
	// 1. Del contexto o RemoteAddr
	remote := reqctx.From(r.Context()).RemoteAddr
	if remote == "" {
		remote = r.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(remote); err == nil && host != "" {
		if net.ParseIP(host) != nil {
			return host
		}
	}

	// 2. Cabeceras estándar de proxy reverso
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		ip := strings.TrimSpace(xrip)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// El último hop añadido por el proxy de entrada
		ip := strings.TrimSpace(parts[len(parts)-1])
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	return "127.0.0.1"
}

func isAllowlisted(allowlist []string, ip string, p auth.Principal, hasP bool) bool {
	for _, item := range allowlist {
		if item == "" {
			continue
		}
		if item == ip {
			return true
		}
		if hasP && (item == p.UserID.String() || item == p.Email) {
			return true
		}
	}
	return false
}
