package infra

import "github.com/prometheus/client_golang/prometheus"

// Metrics son las métricas de Notifications (NOT-005, NOT-002, INF-003).
type Metrics struct {
	Deliveries    *prometheus.CounterVec // notifications_deliveries_total{template,status}
	Latency       prometheus.Histogram   // notifications_delivery_latency_seconds
	DeadLetter    prometheus.Counter     // notifications_dead_letter_total
	ReminderDelay prometheus.Histogram   // reminder_dispatch_delay_seconds
}

// NewMetrics registra las métricas en reg (nil = sin registrar, para tests).
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_deliveries_total", Help: "Entregas procesadas por plantilla y estado.",
		}, []string{"template", "status"}),
		Latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "notifications_delivery_latency_seconds", Help: "Creación → envío.",
			Buckets: []float64{1, 5, 15, 60, 300, 1800, 7200},
		}),
		DeadLetter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "notifications_dead_letter_total", Help: "Entregas fallidas definitivamente.",
		}),
		ReminderDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "reminder_dispatch_delay_seconds", Help: "now - due_at al despachar un recordatorio.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 900},
		}),
	}
	if reg != nil {
		reg.MustRegister(m.Deliveries, m.Latency, m.DeadLetter, m.ReminderDelay)
	}
	return m
}
