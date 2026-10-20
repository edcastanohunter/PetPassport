package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func newTestServers(t *testing.T) (*Servers, *Health) {
	t.Helper()
	h := NewHealth()
	s := New(":0", ":0", Options{
		Logger:      slog.New(slog.NewTextHandler(testWriter{t}, nil)),
		Registry:    prometheus.NewRegistry(),
		Health:      h,
		CORSOrigins: []string{"http://localhost:5173"},
	})
	return s, h
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func TestPublicHealthzAndHeaders(t *testing.T) {
	s, _ := newTestServers(t)
	rec := httptest.NewRecorder()
	s.Public.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, h := range []string{"X-Request-ID", "X-Correlation-ID", "X-Content-Type-Options"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestCorrelationIDPropagation(t *testing.T) {
	s, _ := newTestServers(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/healthz", nil)
	req.Header.Set("X-Correlation-ID", "0192f3a2-2222-7aaa-bbbb-dddddddddddd")
	rec := httptest.NewRecorder()
	s.Public.Handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Correlation-ID"); got != "0192f3a2-2222-7aaa-bbbb-dddddddddddd" {
		t.Fatalf("correlation id not propagated: %q", got)
	}
	// Un valor no UUID se reemplaza.
	req.Header.Set("X-Correlation-ID", "<script>")
	rec = httptest.NewRecorder()
	s.Public.Handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Correlation-ID"); got == "<script>" || got == "" {
		t.Fatalf("invalid correlation id accepted: %q", got)
	}
}

func TestNotFoundIsProblemJSON(t *testing.T) {
	s, _ := newTestServers(t)
	rec := httptest.NewRecorder()
	s.Public.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status=%d ct=%s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestReadiness(t *testing.T) {
	s, h := newTestServers(t)
	rec := httptest.NewRecorder()
	s.Internal.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before SetReady, got %d", rec.Code)
	}
	h.Register("db", func(context.Context) error { return nil })
	h.SetReady(true)
	rec = httptest.NewRecorder()
	s.Internal.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	h.Register("jwks", func(context.Context) error { return errors.New("not loaded") })
	rec = httptest.NewRecorder()
	s.Internal.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with failing dependency, got %d", rec.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	s, _ := newTestServers(t)
	rec := httptest.NewRecorder()
	s.Internal.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
