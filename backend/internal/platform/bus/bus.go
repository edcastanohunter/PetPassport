// Package bus es el command bus in-process (PP-F1-ARQ-003). Cada módulo
// registra sus handlers al arrancar; el handler ejecuta el flujo completo de
// un comando: authz (en la capa http) → idempotency → UoW {load → handle →
// append → project → outbox}. Un comando toca un solo agregado.
package bus

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

// Command es un comando de dominio dirigido a un agregado.
type Command interface {
	CommandName() string
}

// Result es lo que devuelve un handler de comando (ARQ-003): HTTP responde
// 201/200 con {id, version} o el recurso proyectado.
type Result struct {
	AggregateID uuid.UUID
	Version     int64
}

// Handler ejecuta un comando.
type Handler func(ctx context.Context, cmd Command) (Result, error)

// Bus despacha comandos a sus handlers por nombre.
type Bus struct {
	handlers map[string]Handler

	commands *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New construye el bus.
func New() *Bus { return &Bus{handlers: map[string]Handler{}} }

// Instrument registra commands_total{command,result} y
// command_duration_seconds{command} (INF-007). Sin llamar = sin métricas.
func (b *Bus) Instrument(reg prometheus.Registerer) *Bus {
	b.commands = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "commands_total",
		Help: "Comandos despachados por nombre y resultado (ok | error).",
	}, []string{"command", "result"})
	b.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "command_duration_seconds",
		Help:    "Duración del handler de comando (UoW completa).",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"command"})
	reg.MustRegister(b.commands, b.duration)
	return b
}

// Register asocia el handler al comando. Pánico si ya está registrado
// (error de arranque).
func (b *Bus) Register(cmd Command, h Handler) {
	name := cmd.CommandName()
	if _, dup := b.handlers[name]; dup {
		panic("bus: comando ya registrado: " + name)
	}
	b.handlers[name] = h
}

// SystemOrigin identifica un comando despachado por el worker con
// `Principal = system` (scheduler de comandos, PP-F2-ARQ-009). system no pasa
// por authz: solo puede despachar los comandos de scheduler.AllowedCommands
// (SEC-007). Los PM propagan correlation_id del evento origen y
// causation_id = event_id (INF-002).
type SystemOrigin struct {
	CorrelationID string
	CausationID   string
	JobID         uuid.UUID
}

type systemOriginKey struct{}

// WithSystemOrigin marca el contexto como despacho del sistema.
func WithSystemOrigin(ctx context.Context, o SystemOrigin) context.Context {
	return context.WithValue(ctx, systemOriginKey{}, o)
}

// SystemOriginFrom devuelve el origen de sistema, si el comando lo tiene.
// Los módulos lo usan para construir la metadata (actor "system", source "worker").
func SystemOriginFrom(ctx context.Context) (SystemOrigin, bool) {
	o, ok := ctx.Value(systemOriginKey{}).(SystemOrigin)
	return o, ok
}

// Dispatch ejecuta el handler del comando. Comando desconocido → error
// (error de programación: falta registro en el arranque del módulo).
func (b *Bus) Dispatch(ctx context.Context, cmd Command) (Result, error) {
	name := cmd.CommandName()
	h, ok := b.handlers[name]
	if !ok {
		return Result{}, fmt.Errorf("bus: sin handler para %T", cmd)
	}
	if b.commands == nil {
		return h(ctx, cmd)
	}
	start := time.Now()
	res, err := h(ctx, cmd)
	b.duration.WithLabelValues(name).Observe(time.Since(start).Seconds())
	result := "ok"
	if err != nil {
		result = "error"
	}
	b.commands.WithLabelValues(name, result).Inc()
	return res, err
}
