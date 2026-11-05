package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETagMiddleware(t *testing.T) {
	handler := etagMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"42"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))

	t.Run("no If-None-Match gives 200 with body", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("ETag") != `W/"42"` {
			t.Errorf("expected ETag header, got %s", rec.Header().Get("ETag"))
		}
		if rec.Body.String() != `{"hello":"world"}` {
			t.Errorf("expected body, got %s", rec.Body.String())
		}
	})

	t.Run("matching If-None-Match gives 304 with no body", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
		req.Header.Set("If-None-Match", `"42"`)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotModified {
			t.Fatalf("expected 304, got %d", rec.Code)
		}
		if rec.Header().Get("ETag") != `W/"42"` {
			t.Errorf("expected ETag header preserved, got %s", rec.Header().Get("ETag"))
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body for 304, got %d bytes: %s", rec.Body.Len(), rec.Body.String())
		}
	})

	t.Run("matching If-None-Match weak gives 304", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
		req.Header.Set("If-None-Match", `W/"42"`)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotModified {
			t.Fatalf("expected 304, got %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body, got %s", rec.Body.String())
		}
	})

	t.Run("wildcard If-None-Match gives 304", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
		req.Header.Set("If-None-Match", `*`)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotModified {
			t.Fatalf("expected 304, got %d", rec.Code)
		}
	})

	t.Run("non-matching If-None-Match gives 200 with body", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
		req.Header.Set("If-None-Match", `"99"`)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Body.String() != `{"hello":"world"}` {
			t.Errorf("expected body, got %s", rec.Body.String())
		}
	})

	t.Run("POST request ignores If-None-Match", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/resource", nil)
		req.Header.Set("If-None-Match", `"42"`)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Body.String() != `{"hello":"world"}` {
			t.Errorf("expected body, got %s", rec.Body.String())
		}
	})
}
