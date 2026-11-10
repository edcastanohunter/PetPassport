// Package f4metrics define y exporta las métricas Prometheus requeridas para la Fase 4 (PP-F4-INF-003).
package f4metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics agrupa todas las métricas de observabilidad de F4 (PP-F4-INF-003).
type Metrics struct {
	QuoteRequestsTotal       *prometheus.CounterVec   // quote_requests_total{status}
	QuoteFirstResponseSec    prometheus.Histogram     // quote_first_response_seconds
	QuoteToOrderRatio        prometheus.Gauge         // quote_to_order_ratio
	PartnerInvitationsTotal  *prometheus.CounterVec   // partner_invitations_total{org,channel,status}
	WebhookDeliveriesTotal   *prometheus.CounterVec   // webhook_deliveries_total{org,status}
	WebhookLatencySeconds    prometheus.Histogram     // webhook_latency_seconds
	PartnerAPIRequestsTotal  *prometheus.CounterVec   // partner_api_requests_total{org,route,status}
	EgressRequestsTotal      *prometheus.CounterVec   // egress_requests_total{dest,status}
	OCRJobsTotal             *prometheus.CounterVec   // ocr_jobs_total{stage,status}
	OCRStageDurationSeconds  *prometheus.HistogramVec // ocr_stage_duration_seconds{stage}
	AITokensTotal            *prometheus.CounterVec   // ai_tokens_total{model,kind}
	AICostUSDTotal           *prometheus.CounterVec   // ai_cost_usd_total{model}
	AIBudgetUsedRatio        prometheus.Gauge         // ai_budget_used_ratio
	OCRRefusalsTotal         prometheus.Counter       // ocr_refusals_total
	OCRFieldEditRatio        *prometheus.GaugeVec     // ocr_field_edit_ratio{field}
	PrescriptionsIssuedTotal prometheus.Counter       // prescriptions_issued_total
	PrescriptionVerifTotal   *prometheus.CounterVec   // prescription_verifications_total{via}
}

var (
	once     sync.Once
	instance *Metrics
)

// NewMetrics construye y registra el conjunto de métricas de F4 en el registro Prometheus suministrado.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		QuoteRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quote_requests_total",
			Help: "Total de solicitudes de cotización procesadas por estado.",
		}, []string{"status"}),

		QuoteFirstResponseSec: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "quote_first_response_seconds",
			Help:    "Tiempo en segundos hasta recibir la primera cotización para una solicitud.",
			Buckets: []float64{10, 30, 60, 120, 300, 600, 1800, 3600},
		}),

		QuoteToOrderRatio: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quote_to_order_ratio",
			Help: "Ratio de conversión entre solicitudes de cotización cerradas con pedido frente al total.",
		}),

		PartnerInvitationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "partner_invitations_total",
			Help: "Invitaciones de cotización despachadas por organización, canal y estado.",
		}, []string{"org", "channel", "status"}),

		WebhookDeliveriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "webhook_deliveries_total",
			Help: "Entregas de webhooks por organización y estado (sent, failed, queued).",
		}, []string{"org", "status"}),

		WebhookLatencySeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "webhook_latency_seconds",
			Help:    "Latencia en segundos de las llamadas a webhooks externos.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),

		PartnerAPIRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "partner_api_requests_total",
			Help: "Peticiones a la Partner API entrante por organización, ruta y código de respuesta.",
		}, []string{"org", "route", "status"}),

		EgressRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "egress_requests_total",
			Help: "Peticiones salientes a través de egress.Client por host de destino y estado.",
		}, []string{"dest", "status"}),

		OCRJobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ocr_jobs_total",
			Help: "Trabajos del pipeline OCR por etapa y estado.",
		}, []string{"stage", "status"}),

		OCRStageDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ocr_stage_duration_seconds",
			Help:    "Duración en segundos por etapa del pipeline OCR (preprocess, extract, match).",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		}, []string{"stage"}),

		AITokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ai_tokens_total",
			Help: "Tokens de IA consumidos por modelo y tipo (input, output).",
		}, []string{"model", "kind"}),

		AICostUSDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ai_cost_usd_total",
			Help: "Coste acumulado en USD facturado por el proveedor de IA.",
		}, []string{"model"}),

		AIBudgetUsedRatio: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ai_budget_used_ratio",
			Help: "Proporción de presupuesto mensual de IA consumido (0.0 a 1.0+).",
		}),

		OCRRefusalsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ocr_refusals_total",
			Help: "Número de negativas o refusals devueltos por el proveedor de IA.",
		}),

		OCRFieldEditRatio: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ocr_field_edit_ratio",
			Help: "Tasa de edición humana por campo extraído en recetas externas.",
		}, []string{"field"}),

		PrescriptionsIssuedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "prescriptions_issued_total",
			Help: "Número total de recetas emitidas en la plataforma.",
		}),

		PrescriptionVerifTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "prescription_verifications_total",
			Help: "Verificaciones criptográficas de recetas atendidas por canal (public, partner).",
		}, []string{"via"}),
	}

	if reg != nil {
		reg.MustRegister(
			m.QuoteRequestsTotal,
			m.QuoteFirstResponseSec,
			m.QuoteToOrderRatio,
			m.PartnerInvitationsTotal,
			m.WebhookDeliveriesTotal,
			m.WebhookLatencySeconds,
			m.PartnerAPIRequestsTotal,
			m.EgressRequestsTotal,
			m.OCRJobsTotal,
			m.OCRStageDurationSeconds,
			m.AITokensTotal,
			m.AICostUSDTotal,
			m.AIBudgetUsedRatio,
			m.OCRRefusalsTotal,
			m.OCRFieldEditRatio,
			m.PrescriptionsIssuedTotal,
			m.PrescriptionVerifTotal,
		)
	}

	return m
}

// Default devuelve el singleton global de métricas F4 registrado en prometheus.DefaultRegisterer.
func Default() *Metrics {
	once.Do(func() {
		instance = NewMetrics(prometheus.DefaultRegisterer)
	})
	return instance
}
