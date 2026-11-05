package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// RUMEvent representa una métrica Web Vitals reportada por el cliente (PP-F3-FE-010).
type RUMEvent struct {
	Name   string  `json:"name"`   // LCP, INP, CLS, TTFB
	Value  float64 `json:"value"`  // ms (o ratio para CLS)
	Route  string  `json:"route"`  // /app, /org/:id/clinical, etc.
	Rating string  `json:"rating"` // good, needs-improvement, poor
}

// RUMPayload es el lote de eventos recibido en /client-events.
type RUMPayload struct {
	Events []RUMEvent `json:"events"`
}

type rumMetrics struct {
	duration *prometheus.HistogramVec
	events   *prometheus.CounterVec
}

func newRUMMetrics(reg prometheus.Registerer) *rumMetrics {
	m := &rumMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "rum_web_vitals_duration_seconds",
			Help: "Duración de Web Vitals reportados por RUM desde el cliente frontend (PP-F3-FE-010)",
			// Cubre desde 20 ms hasta 10 s (INP, TTFB, LCP y CLS escalado)
			Buckets: []float64{.02, .05, .1, .2, .3, .5, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 8.0},
		}, []string{"metric", "route"}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rum_events_total",
			Help: "Total de eventos RUM recibidos por métrica, ruta y rating",
		}, []string{"metric", "route", "rating"}),
	}
	if reg != nil {
		reg.MustRegister(m.duration, m.events)
	}
	return m
}

// clientEventsHandler recibe eventos RUM del frontend y registra métricas Prometheus.
func clientEventsHandler(metrics *rumMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var payload RUMPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			// Fail-safe para no bloquear al cliente ni generar errores 5xx
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		for _, ev := range payload.Events {
			name := strings.ToUpper(strings.TrimSpace(ev.Name))
			route := cleanRoute(ev.Route)
			rating := strings.ToLower(strings.TrimSpace(ev.Rating))
			if rating == "" {
				rating = "unknown"
			}

			// Value: convertir ms a segundos para LCP, INP, TTFB
			valSec := ev.Value / 1000.0
			if name == "CLS" {
				valSec = ev.Value // CLS es adimensional (0.0 - 1.0+)
			}

			if metrics != nil {
				metrics.duration.WithLabelValues(name, route).Observe(valSec)
				metrics.events.WithLabelValues(name, route, rating).Inc()
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func cleanRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return "unknown"
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	return route
}
