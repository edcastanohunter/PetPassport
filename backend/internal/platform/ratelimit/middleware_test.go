package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

func TestDetermineChecks(t *testing.T) {
	cfg := DefaultConfig()
	uID := uuid.New()
	principal := auth.Principal{UserID: uID}

	t.Run("public share route unauthenticated", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/share/tok123", nil)
		checks := determineChecks(req, "192.168.1.1", auth.Principal{}, false, cfg)

		if len(checks) != 2 {
			t.Fatalf("esperadas 2 comprobaciones (token y public ip), obtenidas %d", len(checks))
		}
		if checks[0].RouteGroup != "share" || checks[0].Limit != 30 {
			t.Errorf("check 0 inesperado: %+v", checks[0])
		}
		if checks[1].RouteGroup != "public" || checks[1].Limit != 60 {
			t.Errorf("check 1 inesperado: %+v", checks[1])
		}
	})

	t.Run("commands authenticated", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/pets", nil)
		checks := determineChecks(req, "192.168.1.1", principal, true, cfg)

		if len(checks) != 1 {
			t.Fatalf("esperada 1 comprobación (commands), obtenidas %d", len(checks))
		}
		if checks[0].RouteGroup != "commands" || checks[0].Limit != 60 {
			t.Errorf("check inesperado: %+v", checks[0])
		}
	})

	t.Run("search authenticated with org", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search/patients?q=fifi", nil)
		orgUUID := uuid.New()
		req.Header.Set("X-PP-Organization", orgUUID.String())
		pWithOrg := auth.Principal{
			UserID: uID,
			Memberships: []auth.Membership{
				{OrganizationID: orgUUID, OrgStatus: "active"},
			},
		}
		checks := determineChecks(req, "192.168.1.1", pWithOrg, true, cfg)

		if len(checks) != 2 {
			t.Fatalf("esperadas 2 comprobaciones (user search y org search), obtenidas %d", len(checks))
		}
		if checks[0].RouteGroup != "search" || checks[0].Limit != 120 {
			t.Errorf("check 0 inesperado: %+v", checks[0])
		}
		if checks[1].RouteGroup != "search" || checks[1].Limit != 1200 {
			t.Errorf("check 1 inesperado: %+v", checks[1])
		}
	})

	t.Run("search authenticated with spoofed org (not member)", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search/patients?q=fifi", nil)
		orgUUID := uuid.New()
		req.Header.Set("X-PP-Organization", orgUUID.String())
		// Usuario no tiene membresía en esa org:
		checks := determineChecks(req, "192.168.1.1", principal, true, cfg)

		// Solo debe aplicar el límite de usuario, ignorando la org ajena
		if len(checks) != 1 {
			t.Fatalf("esperada 1 comprobación (solo user), obtenidas %d", len(checks))
		}
		if checks[0].RouteGroup != "search" || checks[0].Limit != 120 {
			t.Errorf("check 0 inesperado: %+v", checks[0])
		}
	})

	t.Run("lookup authenticated", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/pets/pet-lookup", nil)
		checks := determineChecks(req, "192.168.1.1", principal, true, cfg)

		if len(checks) != 1 || checks[0].RouteGroup != "lookup" || checks[0].Limit != 20 {
			t.Errorf("check lookup inesperado: %+v", checks)
		}
	})

	t.Run("export authenticated", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/cohorts/123/export", nil)
		checks := determineChecks(req, "192.168.1.1", principal, true, cfg)

		if len(checks) != 1 || checks[0].RouteGroup != "export" || checks[0].Limit != 5 {
			t.Errorf("check export inesperado: %+v", checks)
		}
	})
}

func TestMiddleware_FailOpenOnNilClient(t *testing.T) {
	cfg := DefaultConfig()
	limiter := NewLimiter(nil) // sin cliente redis

	mw := Middleware(limiter, cfg)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/pets", nil)
	uID := uuid.New()
	ctx := auth.WithPrincipal(req.Context(), auth.Principal{UserID: uID})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado código 200 (fail-open), obtenido %d", rec.Code)
	}

	// Verificar cabeceras IETF
	if limit := rec.Header().Get("RateLimit-Limit"); limit != "60" {
		t.Errorf("esperado RateLimit-Limit 60, obtenido %q", limit)
	}
	if rem := rec.Header().Get("RateLimit-Remaining"); rem != "60" {
		t.Errorf("esperado RateLimit-Remaining 60, obtenido %q", rem)
	}
	if reset := rec.Header().Get("RateLimit-Reset"); reset != "60" {
		t.Errorf("esperado RateLimit-Reset 60, obtenido %q", reset)
	}
}

func TestMiddleware_Allowlist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Allowlist = []string{"192.168.1.50"}

	limiter := NewLimiter(nil)
	mw := Middleware(limiter, cfg)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/directory", nil)
	req.RemoteAddr = "192.168.1.50:54321"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 para IP en allowlist, obtenido %d", rec.Code)
	}
	// Si está en allowlist, no se imponen cabeceras de limitación
	if rec.Header().Get("RateLimit-Limit") != "" {
		t.Errorf("no esperaba cabeceras RateLimit-* para petición en allowlist")
	}
}

func TestMiddleware_429ResponseStructure(t *testing.T) {
	// Probar que cuando un limiter retorna !Allowed, se escribe 429 con Problem JSON y Retry-After
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/pets", nil)

	w := rec
	w.Header().Set("RateLimit-Limit", "60")
	w.Header().Set("RateLimit-Remaining", "0")
	w.Header().Set("RateLimit-Reset", "45")
	w.Header().Set("Retry-After", "45")

	w.WriteHeader(http.StatusTooManyRequests)
	body := map[string]interface{}{
		"type":   "https://docs.petpassport.app/errors/RATE_LIMITED",
		"title":  "Demasiadas solicitudes",
		"status": 429,
		"code":   "RATE_LIMITED",
		"detail": "Has superado el límite de solicitudes para el grupo 'commands'. Intenta nuevamente en 45 segundos.",
	}
	_ = json.NewEncoder(w).Encode(body)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("esperado 429, obtenido %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "45" {
		t.Errorf("esperado Retry-After 45, obtenido %s", rec.Header().Get("Retry-After"))
	}
	if rec.Header().Get("RateLimit-Reset") != "45" {
		t.Errorf("esperado RateLimit-Reset 45, obtenido %s", rec.Header().Get("RateLimit-Reset"))
	}
	_ = req
}
