package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

var (
	metricDegradedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "search",
		Name:      "degraded_requests_total",
		Help:      "Total de peticiones de búsqueda que degradaron a PostgreSQL.",
	})
)

// CircuitBreaker implementa el patrón circuit breaker para OpenSearch (ARQ-004).
// 5 fallos en 30 s -> abierto durante 60 s.
type CircuitBreaker struct {
	mu           sync.Mutex
	failures     []time.Time
	threshold    int
	window       time.Duration
	cooldown     time.Duration
	openUntil    time.Time
	forceDegrade bool
}

// NewCircuitBreaker crea un nuevo CircuitBreaker para búsqueda.
func NewCircuitBreaker(threshold int, window, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if window <= 0 {
		window = 30 * time.Second
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	return &CircuitBreaker{
		threshold: threshold,
		window:    window,
		cooldown:  cooldown,
	}
}

// Allow comprueba si la petición puede dirigirse a OpenSearch.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.forceDegrade {
		return false
	}

	now := time.Now()
	return !now.Before(cb.openUntil)
}

// RecordSuccess registra un éxito y resetea la ventana de fallos si estaba cerrado.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = nil
}

// RecordFailure registra un fallo y abre el circuito si se supera el umbral.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-cb.window)

	var valid []time.Time
	for _, t := range cb.failures {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	cb.failures = valid

	if len(valid) >= cb.threshold {
		cb.openUntil = now.Add(cb.cooldown)
		cb.failures = nil
	}
}

// SetForceDegrade permite forzar el modo degradado (útil para pruebas y caos).
func (cb *CircuitBreaker) SetForceDegrade(force bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.forceDegrade = force
}

// FallbackBackend define los métodos que PostgreSQL debe implementar para el modo degradado.
type FallbackBackend interface {
	SearchPatientsPG(ctx context.Context, req PatientSearchRequest, principal auth.Principal) (*PatientSearchResponse, error)
	SearchClinicalPG(ctx context.Context, req ClinicalSearchRequest, principal auth.Principal) (*ClinicalSearchResponse, error)
	SuggestPatientsPG(ctx context.Context, orgID uuid.UUID, q string, principal auth.Principal) ([]SuggestPatientItem, error)
	SuggestMedicationsPG(ctx context.Context, q string, species string, limit int) ([]SuggestMedicationItem, error)
}

// ResilientSearchService coordina la búsqueda con OpenSearch y la degradación automática a PostgreSQL.
type ResilientSearchService struct {
	primary  *QueryService
	fallback FallbackBackend
	breaker  *CircuitBreaker
}

// NewResilientSearchService crea una instancia de ResilientSearchService.
func NewResilientSearchService(primary *QueryService, fallback FallbackBackend, breaker *CircuitBreaker) *ResilientSearchService {
	if breaker == nil {
		breaker = NewCircuitBreaker(5, 30*time.Second, 60*time.Second)
	}
	return &ResilientSearchService{
		primary:  primary,
		fallback: fallback,
		breaker:  breaker,
	}
}

// SearchPatients ejecuta la búsqueda de pacientes con degradación automática.
func (s *ResilientSearchService) SearchPatients(ctx context.Context, req PatientSearchRequest, principal auth.Principal) (*PatientSearchResponse, error) {
	if s.breaker.Allow() {
		res, err := s.primary.SearchPatients(ctx, req, principal)
		if err == nil {
			s.breaker.RecordSuccess()
			return res, nil
		}
		s.breaker.RecordFailure()
	}

	// Degradación a PostgreSQL
	metricDegradedRequests.Inc()
	if s.fallback != nil {
		res, err := s.fallback.SearchPatientsPG(ctx, req, principal)
		if err == nil {
			res.Mode = "degraded"
			return res, nil
		}
		return nil, err
	}
	return nil, fmt.Errorf("search unavailable and fallback not configured")
}

// SearchClinical ejecuta la búsqueda clínica con degradación automática.
func (s *ResilientSearchService) SearchClinical(ctx context.Context, req ClinicalSearchRequest, principal auth.Principal) (*ClinicalSearchResponse, error) {
	if s.breaker.Allow() {
		res, err := s.primary.SearchClinical(ctx, req, principal)
		if err == nil {
			s.breaker.RecordSuccess()
			return res, nil
		}
		s.breaker.RecordFailure()
	}

	// Degradación a PostgreSQL
	metricDegradedRequests.Inc()
	if s.fallback != nil {
		res, err := s.fallback.SearchClinicalPG(ctx, req, principal)
		if err == nil {
			res.Mode = "degraded"
			return res, nil
		}
		return nil, err
	}
	return nil, fmt.Errorf("search unavailable and fallback not configured")
}

// SuggestPatients ejecuta el autocompletado con degradación automática.
func (s *ResilientSearchService) SuggestPatients(ctx context.Context, orgID uuid.UUID, q string, principal auth.Principal) ([]SuggestPatientItem, string, error) {
	if s.breaker.Allow() {
		items, err := s.primary.SuggestPatients(ctx, orgID, q, principal)
		if err == nil {
			s.breaker.RecordSuccess()
			return items, "full", nil
		}
		s.breaker.RecordFailure()
	}

	metricDegradedRequests.Inc()
	if s.fallback != nil {
		items, err := s.fallback.SuggestPatientsPG(ctx, orgID, q, principal)
		return items, "degraded", err
	}
	return nil, "degraded", fmt.Errorf("search unavailable and fallback not configured")
}

// SuggestMedications ejecuta el autocompletado de medicamentos con degradación a PostgreSQL (ARQ-004, API-003).
func (s *ResilientSearchService) SuggestMedications(ctx context.Context, q string, species string, limit int) ([]SuggestMedicationItem, string, error) {
	if s.breaker.Allow() {
		items, err := s.primary.SuggestMedications(ctx, q, species, limit)
		if err == nil {
			s.breaker.RecordSuccess()
			return items, "full", nil
		}
		s.breaker.RecordFailure()
	}

	metricDegradedRequests.Inc()
	if s.fallback != nil {
		items, err := s.fallback.SuggestMedicationsPG(ctx, q, species, limit)
		return items, "degraded", err
	}
	return nil, "degraded", fmt.Errorf("search unavailable and fallback not configured")
}

// Breaker devuelve el circuit breaker subyacente.
func (s *ResilientSearchService) Breaker() *CircuitBreaker {
	return s.breaker
}
