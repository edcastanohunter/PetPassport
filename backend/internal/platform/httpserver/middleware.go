package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/reqctx"
)

// CorrelationIDFrom devuelve el X-Correlation-ID del request.
func CorrelationIDFrom(ctx context.Context) string { return reqctx.From(ctx).CorrelationID }

// RequestIDFrom devuelve el X-Request-ID del request.
func RequestIDFrom(ctx context.Context) string { return reqctx.From(ctx).RequestID }

// correlation asigna X-Request-ID (siempre nuevo) y X-Correlation-ID (entrante
// si es un UUID válido; nuevo si no) y los devuelve en la respuesta.
func correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.Must(uuid.NewV7()).String()
		corr := r.Header.Get("X-Correlation-ID")
		if _, err := uuid.Parse(corr); err != nil {
			corr = reqID
		}
		ctx := reqctx.With(r.Context(), &reqctx.Info{RequestID: reqID, CorrelationID: corr, RemoteAddr: r.RemoteAddr})
		w.Header().Set("X-Request-ID", reqID)
		w.Header().Set("X-Correlation-ID", corr)
		r.Header.Set("X-Correlation-ID", corr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoverer convierte panics en 500 problem+json y los registra sin cuerpo.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", rec, "stack", string(debug.Stack()),
						"request_id", RequestIDFrom(r.Context()), "correlation_id", CorrelationIDFrom(r.Context()))
					problem.Write(w, r, problem.New(problem.Internal, "Error interno", "Ocurrió un error inesperado."))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger emite una línea JSON por request sin PII (docs/phase-1/08 §8).
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			span := trace.SpanContextFromContext(r.Context())
			attrs := []any{
				"method", r.Method,
				"route", routePattern(r),
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
				"correlation_id", CorrelationIDFrom(r.Context()),
			}
			if actor := reqctx.From(r.Context()).ActorID(); actor != "" {
				attrs = append(attrs, "actor_id", actor)
			}
			if span.HasTraceID() {
				attrs = append(attrs, "trace_id", span.TraceID().String())
			}
			level := slog.LevelInfo
			if ww.Status() >= 500 {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "http request", toAttrs(attrs)...)
		})
	}
}

func toAttrs(kv []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, slog.Any(kv[i].(string), kv[i+1]))
	}
	return out
}

// routePattern devuelve el patrón de ruta de chi (sin valores de parámetros),
// para no meter identificadores en logs ni métricas.
func routePattern(r *http.Request) string {
	if p := chiRoutePattern(r); p != "" {
		return p
	}
	return "unmatched"
}

// securityHeaders aplica las cabeceras de docs/phase-1/08 §4 para respuestas de API.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// httpMetrics registra RED por ruta.
type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
}

func newHTTPMetrics(reg prometheus.Registerer) *httpMetrics {
	m := &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_server_requests_total", Help: "Requests HTTP por ruta, método y estado",
		}, []string{"route", "method", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_server_request_duration_seconds", Help: "Duración de requests HTTP",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"route", "method"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_server_in_flight_requests", Help: "Requests en curso",
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.inflight)
	return m
}

func (m *httpMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inflight.Inc()
		defer m.inflight.Dec()
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		route := routePattern(r)
		// status numérico ("200", "503"): las alertas filtran por clase (status=~"5..").
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // el handler escribió sin WriteHeader explícito
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(status)).Inc()
		m.duration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}
