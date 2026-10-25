package authz

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Métricas de autorización (SEC-005, SEC-007): decisiones por acción y
// resultado; las denegaciones también se registran en audit_log cuando el
// actor es super_admin (M1.4). Sin InitMetrics no se registran (pruebas).
var (
	decisions   *prometheus.CounterVec
	deniedTotal *prometheus.CounterVec
	metricsMu   sync.Mutex
)

// InitMetrics registra los contadores de decisiones en el registro de
// telemetría. Idempotente: el segundo registro sobre el mismo registro falla
// solo si se llama con otro Registerer (arranque único en la práctica).
func InitMetrics(reg prometheus.Registerer) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	if decisions != nil {
		return
	}
	decisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "authz_decisions_total",
		Help: "Decisiones de autorización por acción y resultado.",
	}, []string{"action", "decision"})
	deniedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "authz_denied_total",
		Help: "Denegaciones de autorización por acción (IAM-010).",
	}, []string{"action"})
	reg.MustRegister(decisions, deniedTotal)
}

// probe es la sonda de decisiones: la inyecta el test de cobertura de rutas
// (SEC-005) para verificar que cada handler llama a authz.Can con la
// `x-pp-action` declarada en el contrato. Mecanismo; suite completa en M1.3.
var (
	probeMu sync.RWMutex
	probe   func(p auth.Principal, action Action, d Decision)
)

// SetProbe instala la sonda de decisiones (solo pruebas).
func SetProbe(fn func(p auth.Principal, action Action, d Decision)) {
	probeMu.Lock()
	defer probeMu.Unlock()
	probe = fn
}

func notify(p auth.Principal, action Action, d Decision) Decision {
	result := "allow"
	if !d.Allow {
		result = "deny"
	}
	metricsMu.Lock()
	if decisions != nil {
		decisions.WithLabelValues(string(action), result).Inc()
	}
	if !d.Allow && deniedTotal != nil {
		deniedTotal.WithLabelValues(string(action)).Inc()
	}
	metricsMu.Unlock()
	probeMu.RLock()
	fn := probe
	probeMu.RUnlock()
	if fn != nil {
		fn(p, action, d)
	}
	return d
}
