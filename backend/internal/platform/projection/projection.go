// Package projection ejecuta los proyectores de read models (PP-F1-ARQ-005).
// En F1 el Runner es síncrono: se invoca dentro de la misma transacción que
// el Append del comando y un fallo revierte todo (consistencia fuerte,
// explícitamente aceptada en ADR-002). Los proyectores deben ser idempotentes
// para poder ejecutarse asíncronos y en rebuild a partir de F2 sin cambios.
package projection

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// Projector proyecta un evento sobre un read model.
type Projector interface {
	Name() string      // "petregistry.pets"
	Handles() []string // tipos de evento
	Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error
}

// Runner llama a los proyectores suscritos a cada evento.
type Runner struct {
	byType map[string][]Projector
	names  map[string]bool
	order  []Projector // orden de registro (rebuild --sync)

	duration *prometheus.HistogramVec
}

// NewRunner construye el runner.
func NewRunner() *Runner {
	return &Runner{byType: map[string][]Projector{}, names: map[string]bool{}}
}

// Instrument registra projection_duration_seconds{projector} (INF-007).
// Sin llamar = sin métricas.
func (r *Runner) Instrument(reg prometheus.Registerer) *Runner {
	r.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "projection_duration_seconds",
		Help:    "Duración de cada proyección de evento, por proyector.",
		Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25},
	}, []string{"projector"})
	reg.MustRegister(r.duration)
	return r
}

// Register suscribe un proyector. Pánico si el nombre ya existe.
func (r *Runner) Register(p Projector) {
	if r.names[p.Name()] {
		panic("projection: proyector ya registrado: " + p.Name())
	}
	r.names[p.Name()] = true
	r.order = append(r.order, p)
	for _, t := range p.Handles() {
		r.byType[t] = append(r.byType[t], p)
	}
}

// Apply proyecta los eventos recién añadidos en la misma tx del comando.
// Un error detiene el comando entero (ARQ-005).
func (r *Runner) Apply(ctx context.Context, tx pgx.Tx, events []eventstore.StoredEvent) error {
	for _, e := range events {
		for _, p := range r.byType[e.Type] {
			start := time.Now()
			err := p.Project(ctx, tx, e)
			if r.duration != nil {
				r.duration.WithLabelValues(p.Name()).Observe(time.Since(start).Seconds())
			}
			if err != nil {
				return fmt.Errorf("projection %s: %s@%d: %w", p.Name(), e.Type, e.SchemaVersion, err)
			}
		}
	}
	return nil
}
