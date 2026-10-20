package problem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/pets/x", nil)
	req.Header.Set("X-Correlation-ID", "abc")
	Write(rec, req, New(NotFound, "No encontrado", "El recurso no existe."))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != NotFound || p.CorrelationID != "abc" || p.Instance != "/api/v1/pets/x" || p.Type != TypeBase+"NOT_FOUND" {
		t.Fatalf("unexpected body: %+v", p)
	}
}
