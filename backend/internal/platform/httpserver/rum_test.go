package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestClientEventsHandler(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newRUMMetrics(reg)
	h := clientEventsHandler(m)

	payload := `{
		"events": [
			{"name": "LCP", "value": 1250, "route": "/app/pets", "rating": "good"},
			{"name": "INP", "value": 85, "route": "/app/pets", "rating": "good"},
			{"name": "CLS", "value": 0.05, "route": "/app/pets", "rating": "good"},
			{"name": "TTFB", "value": 120, "route": "/app/pets", "rating": "good"}
		]
	}`

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/client-events", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rec.Code)
	}

	// Verify prometheus metrics were collected
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	foundRUM := false
	for _, mf := range metricFamilies {
		if *mf.Name == "rum_web_vitals_duration_seconds" {
			foundRUM = true
			if len(mf.Metric) != 4 {
				t.Errorf("expected 4 metrics for LCP, INP, CLS, TTFB, got %d", len(mf.Metric))
			}
		}
	}
	if !foundRUM {
		t.Error("expected rum_web_vitals_duration_seconds metric family in registry")
	}
}
