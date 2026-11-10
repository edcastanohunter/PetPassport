package f4metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry/f4metrics"
)

func TestNewMetrics_RegistersAllF4Metrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := f4metrics.NewMetrics(reg)

	// Probar incrementos y escrituras en cada métrica
	m.QuoteRequestsTotal.WithLabelValues("open").Inc()
	m.QuoteFirstResponseSec.Observe(45.2)
	m.QuoteToOrderRatio.Set(0.72)
	m.PartnerInvitationsTotal.WithLabelValues("farmapet", "portal", "sent").Inc()
	m.WebhookDeliveriesTotal.WithLabelValues("farmapet", "sent").Inc()
	m.WebhookLatencySeconds.Observe(0.35)
	m.PartnerAPIRequestsTotal.WithLabelValues("farmapet", "/partner/v1/orders", "200").Inc()
	m.EgressRequestsTotal.WithLabelValues("api.anthropic.com", "200").Inc()
	m.OCRJobsTotal.WithLabelValues("extract", "done").Inc()
	m.OCRStageDurationSeconds.WithLabelValues("extract").Observe(1.8)
	m.AITokensTotal.WithLabelValues("claude-opus-5-5", "input").Add(500)
	m.AICostUSDTotal.WithLabelValues("claude-opus-5-5").Add(0.015)
	m.AIBudgetUsedRatio.Set(0.42)
	m.OCRRefusalsTotal.Inc()
	m.OCRFieldEditRatio.WithLabelValues("dosage").Set(0.12)
	m.PrescriptionsIssuedTotal.Inc()
	m.PrescriptionVerifTotal.WithLabelValues("public").Inc()

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("error recolectando métricas: %v", err)
	}

	names := make(map[string]bool)
	for _, metricFamily := range metrics {
		names[metricFamily.GetName()] = true
	}

	expected := []string{
		"quote_requests_total",
		"quote_first_response_seconds",
		"quote_to_order_ratio",
		"partner_invitations_total",
		"webhook_deliveries_total",
		"webhook_latency_seconds",
		"partner_api_requests_total",
		"egress_requests_total",
		"ocr_jobs_total",
		"ocr_stage_duration_seconds",
		"ai_tokens_total",
		"ai_cost_usd_total",
		"ai_budget_used_ratio",
		"ocr_refusals_total",
		"ocr_field_edit_ratio",
		"prescriptions_issued_total",
		"prescription_verifications_total",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("métrica requerida ausente en Prometheus: %s", name)
		}
	}
}
