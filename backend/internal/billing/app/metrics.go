package app

import "github.com/prometheus/client_golang/prometheus"

// Metrics son las métricas de negocio de Billing (PP-F5-INF-003). Un *Metrics
// nil es válido y no registra nada (pruebas).
type Metrics struct {
	charges             *prometheus.CounterVec
	invoices            *prometheus.CounterVec
	webhooks            *prometheus.CounterVec
	discrepanciesOpened *prometheus.CounterVec
	discrepanciesOpen   prometheus.Gauge
	renewalsOverdue     prometheus.Gauge
	mrr                 *prometheus.GaugeVec
	invoicingUp         *prometheus.GaugeVec
}

// NewMetrics registra las métricas de Billing.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		charges: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "billing_charges_total",
			Help: "Resultados de cobro por estado (paid | failed | refunded | error) y proveedor.",
		}, []string{"status", "provider"}),
		invoices: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "billing_invoices_total",
			Help: "Facturas por estado (issued | accepted | rejected | contingency | credited | error).",
		}, []string{"status"}),
		webhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "billing_webhooks_total",
			Help: "Webhooks de pago recibidos por proveedor y resultado.",
		}, []string{"provider", "result"}),
		discrepanciesOpened: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "billing_discrepancies_opened_total",
			Help: "Discrepancias de conciliación abiertas por tipo.",
		}, []string{"kind"}),
		discrepanciesOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "billing_discrepancies_open",
			Help: "Discrepancias de conciliación sin resolver.",
		}),
		renewalsOverdue: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "billing_renewals_overdue",
			Help: "Suscripciones cuyo periodo venció hace más de una hora sin renovarse.",
		}),
		mrr: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "billing_mrr",
			Help: "Ingreso recurrente mensual de las suscripciones activas, por moneda.",
		}, []string{"currency"}),
		invoicingUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "billing_invoicing_provider_up",
			Help: "1 si la última llamada al proveedor de facturación respondió; 0 si estaba caído.",
		}, []string{"provider"}),
	}
	reg.MustRegister(m.charges, m.invoices, m.webhooks, m.discrepanciesOpened, m.discrepanciesOpen,
		m.renewalsOverdue, m.mrr, m.invoicingUp)
	return m
}

func (m *Metrics) charge(status, provider string) {
	if m != nil {
		m.charges.WithLabelValues(status, provider).Inc()
	}
}

func (m *Metrics) invoice(status string) {
	if m != nil {
		m.invoices.WithLabelValues(status).Inc()
	}
}

func (m *Metrics) webhook(provider, result string) {
	if m != nil {
		m.webhooks.WithLabelValues(provider, result).Inc()
	}
}

func (m *Metrics) discrepancy(kind string) {
	if m != nil {
		m.discrepanciesOpened.WithLabelValues(kind).Inc()
	}
}

// SetOpenDiscrepancies publica las discrepancias abiertas (alerta DiscrepanciesOpen).
func (m *Metrics) SetOpenDiscrepancies(n int) { m.setOpenDiscrepancies(n) }

func (m *Metrics) setOpenDiscrepancies(n int) {
	if m != nil {
		m.discrepanciesOpen.Set(float64(n))
	}
}

func (m *Metrics) invoicingProviderUp(provider string, up bool) {
	if m == nil {
		return
	}
	v := 0.0
	if up {
		v = 1
	}
	m.invoicingUp.WithLabelValues(provider).Set(v)
}

// SetRenewalsOverdue publica las renovaciones atrasadas (alerta RenewalJobStalled).
func (m *Metrics) SetRenewalsOverdue(n int) {
	if m != nil {
		m.renewalsOverdue.Set(float64(n))
	}
}

// SetMRR publica el MRR de una moneda.
func (m *Metrics) SetMRR(currency string, amount float64) {
	if m != nil {
		m.mrr.WithLabelValues(currency).Set(amount)
	}
}
