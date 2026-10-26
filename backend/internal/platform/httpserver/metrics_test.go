package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// Las alertas de INF-007 filtran por clase de estado (status=~"5.."): la
// etiqueta debe ser el código numérico, también cuando el handler no llama a
// WriteHeader.
func TestHTTPMetrics_StatusLabelIsNumeric(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newHTTPMetrics(reg)
	for _, h := range []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		func(http.ResponseWriter, *http.Request) {},
	} {
		m.middleware(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range families {
		if f.GetName() != "http_server_requests_total" {
			continue
		}
		for _, metric := range f.GetMetric() {
			for _, l := range metric.GetLabel() {
				if l.GetName() == "status" {
					got[l.GetValue()] += metric.GetCounter().GetValue()
				}
			}
		}
	}
	if len(got) != 2 || got["200"] != 2 || got["503"] != 1 {
		t.Fatalf("status: %v", got)
	}
}
