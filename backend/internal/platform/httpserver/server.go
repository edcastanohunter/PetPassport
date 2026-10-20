// Package httpserver construye los dos servidores HTTP del proceso api:
// el público (/api/v1) y el interno (/healthz, /readyz, /metrics, pprof en dev),
// con los middlewares de docs/phase-1/03 §2 y 08 §4.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// MaxJSONBody es el límite de cuerpo JSON (docs/phase-1/05 §1).
const MaxJSONBody = 256 << 10

// Options configura los servidores.
type Options struct {
	Logger      *slog.Logger
	Registry    *prometheus.Registry
	Health      *Health
	CORSOrigins []string
	Dev         bool
	// Mount registra las rutas de negocio bajo /api/v1.
	Mount func(r chi.Router)
}

// Servers agrupa el servidor público y el interno.
type Servers struct {
	Public   *http.Server
	Internal *http.Server
}

// New construye ambos servidores sin arrancarlos.
func New(publicAddr, internalAddr string, o Options) *Servers {
	metrics := newHTTPMetrics(o.Registry)

	r := chi.NewRouter()
	r.Use(correlation)
	r.Use(recoverer(o.Logger))
	r.Use(otelhttp.NewMiddleware("api", otelhttp.WithFilter(func(r *http.Request) bool {
		return r.URL.Path != "/api/v1/healthz"
	})))
	r.Use(metrics.middleware)
	r.Use(requestLogger(o.Logger))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   o.CORSOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Idempotency-Key", "If-Match", "X-Correlation-ID"},
		ExposedHeaders:   []string{"ETag", "X-Correlation-ID", "X-Request-ID", "RateLimit-Limit", "RateLimit-Remaining", "RateLimit-Reset", "Retry-After"},
		AllowCredentials: false,
		MaxAge:           600,
	}))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(problem.NotFound, "No encontrado", "La ruta no existe."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		p := problem.New(problem.NotFound, "No encontrado", "La ruta no existe.")
		p.Status = http.StatusMethodNotAllowed
		problem.Write(w, r, p)
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.RequestSize(MaxJSONBody))
		mountGenerated(api)
		if o.Mount != nil {
			o.Mount(api)
		}
	})

	internal := chi.NewRouter()
	internal.Get("/healthz", o.Health.Liveness)
	internal.Get("/readyz", o.Health.Readiness)
	internal.Handle("/metrics", promhttp.HandlerFor(o.Registry, promhttp.HandlerOpts{}))
	if o.Dev {
		internal.HandleFunc("/debug/pprof/", pprof.Index)
		internal.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		internal.HandleFunc("/debug/pprof/profile", pprof.Profile)
		internal.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		internal.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	return &Servers{
		Public: &http.Server{
			Addr: publicAddr, Handler: r,
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
			WriteTimeout: 60 * time.Second, IdleTimeout: 180 * time.Second,
		},
		Internal: &http.Server{
			Addr: internalAddr, Handler: internal,
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
			WriteTimeout: 60 * time.Second, IdleTimeout: 180 * time.Second,
		},
	}
}

// Start arranca ambos servidores y devuelve un canal de errores fatales.
func (s *Servers) Start() <-chan error {
	errs := make(chan error, 2)
	go func() {
		if err := s.Public.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go func() {
		if err := s.Internal.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	return errs
}

// Shutdown apaga ambos servidores de forma ordenada.
func (s *Servers) Shutdown(ctx context.Context) error {
	return errors.Join(s.Public.Shutdown(ctx), s.Internal.Shutdown(ctx))
}

func chiRoutePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
