// Package telemetry inicializa logs estructurados (slog JSON), trazas OpenTelemetry
// y métricas Prometheus. Ver docs/phase-1/09 §7 y 08 §8 (sin PII en logs).
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Options configura la telemetría del proceso.
type Options struct {
	ServiceName    string
	ServiceVersion string
	Env            string
	LogLevel       slog.Level
	OTLPEndpoint   string // vacío = sin exportación de trazas
}

// Telemetry agrupa los recursos a cerrar en el apagado.
type Telemetry struct {
	Logger   *slog.Logger
	Registry *prometheus.Registry
	tp       *sdktrace.TracerProvider
}

// Init configura logger, tracer provider y registro de métricas.
func Init(ctx context.Context, o Options) (*Telemetry, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: o.LogLevel})).
		With("service", o.ServiceName, "version", o.ServiceVersion, "env", o.Env)
	slog.SetDefault(logger)

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// NewSchemaless evita el conflicto de Schema URL con resource.Default().
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(o.ServiceName),
		semconv.ServiceVersion(o.ServiceVersion),
		attribute.String("deployment.environment.name", o.Env),
	))
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if o.OTLPEndpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(o.OTLPEndpoint))
		if err != nil {
			return nil, fmt.Errorf("otlp exporter: %w", err)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return &Telemetry{Logger: logger, Registry: reg, tp: tp}, nil
}

// Shutdown vacía los exportadores de trazas.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return t.tp.Shutdown(ctx)
}
