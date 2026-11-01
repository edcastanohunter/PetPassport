package lease

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ErrLeaseLost se produce cuando la renovación periódica del lease falla.
	ErrLeaseLost = errors.New("lease: lost lease ownership")

	metricLeasesHeld = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "petpassport",
			Subsystem: "worker",
			Name:      "leases_held",
			Help:      "Número de leases de worker en posesión de esta réplica.",
		},
		[]string{"holder"},
	)

	metricTakeovers = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "petpassport",
			Subsystem: "worker",
			Name:      "lease_takeovers_total",
			Help:      "Total de retomas de lease tras expiración del holder anterior.",
		},
		[]string{"name"},
	)
)

// Info describe el estado actual de un lease.
type Info struct {
	Name       string    `json:"name"`
	Holder     string    `json:"holder"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Active     bool      `json:"active"`
}

// Manager coordina la adquisición, renovación y liberación de leases (PP-F3-ARQ-009).
type Manager struct {
	pool          *pgxpool.Pool
	holder        string
	ttl           time.Duration
	renewInterval time.Duration
	logger        *slog.Logger

	mu   sync.Mutex
	held map[string]context.CancelFunc
}

// NewManager crea un nuevo gestor de leases para este worker.
func NewManager(pool *pgxpool.Pool, holder string, logger *slog.Logger) *Manager {
	if holder == "" {
		holder = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	return &Manager{
		pool:          pool,
		holder:        holder,
		ttl:           30 * time.Second,
		renewInterval: 10 * time.Second,
		logger:        logger,
		held:          make(map[string]context.CancelFunc),
	}
}

// Holder devuelve el identificador de esta réplica de worker.
func (m *Manager) Holder() string {
	return m.holder
}

// HeldCount devuelve cuántos leases retiene actualmente este worker.
func (m *Manager) HeldCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.held)
}

// Acquire intenta adquirir el lease con el nombre dado.
// Si tiene éxito, devuelve true y un contexto derivado que se cancelará inmediatamente
// si la renovación falla o se pierde el lease.
func (m *Manager) Acquire(ctx context.Context, name string) (bool, context.Context, error) {
	m.mu.Lock()
	if _, alreadyHeld := m.held[name]; alreadyHeld {
		m.mu.Unlock()
		return true, ctx, nil
	}
	m.mu.Unlock()

	const q = `
INSERT INTO platform.worker_leases (name, holder, acquired_at, expires_at)
VALUES ($1, $2, now(), now() + $3::interval)
ON CONFLICT (name) DO UPDATE
SET holder = EXCLUDED.holder,
    acquired_at = EXCLUDED.acquired_at,
    expires_at = EXCLUDED.expires_at
WHERE platform.worker_leases.expires_at < now()
   OR platform.worker_leases.holder = EXCLUDED.holder
RETURNING (holder = $2), (acquired_at = now());
`
	ttlStr := fmt.Sprintf("%d milliseconds", m.ttl.Milliseconds())

	var acquired bool
	var isNewOrTakeover bool
	err := m.pool.QueryRow(ctx, q, name, m.holder, ttlStr).Scan(&acquired, &isNewOrTakeover)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("lease acquire query: %w", err)
	}

	if !acquired {
		return false, nil, nil
	}

	if isNewOrTakeover {
		metricTakeovers.WithLabelValues(name).Inc()
	}

	leaseCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.held[name] = cancel
	count := len(m.held)
	m.mu.Unlock()

	metricLeasesHeld.WithLabelValues(m.holder).Set(float64(count))
	if m.logger != nil {
		m.logger.InfoContext(ctx, "lease acquired", "name", name, "holder", m.holder)
	}

	// Iniciar gorutina de renovación periódica
	go m.renewLoop(leaseCtx, name, cancel)

	return true, leaseCtx, nil
}

// renewLoop renueva periódicamente el lease mientras esté activo.
func (m *Manager) renewLoop(ctx context.Context, name string, cancel context.CancelFunc) {
	ticker := time.NewTicker(m.renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, rCancel := context.WithTimeout(context.Background(), 5*time.Second)
			ok, err := m.renew(renewCtx, name)
			rCancel()

			if err != nil || !ok {
				if m.logger != nil {
					m.logger.Warn("lease renewal failed, stopping runner", "name", name, "error", err)
				}
				m.mu.Lock()
				delete(m.held, name)
				count := len(m.held)
				m.mu.Unlock()

				metricLeasesHeld.WithLabelValues(m.holder).Set(float64(count))
				cancel() // Detener inmediatamente el runner
				return
			}
		}
	}
}

func (m *Manager) renew(ctx context.Context, name string) (bool, error) {
	const q = `
UPDATE platform.worker_leases
SET expires_at = now() + $1::interval
WHERE name = $2 AND holder = $3
RETURNING true;
`
	ttlStr := fmt.Sprintf("%d milliseconds", m.ttl.Milliseconds())
	var ok bool
	err := m.pool.QueryRow(ctx, q, ttlStr, name, m.holder).Scan(&ok)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}

// Release libera voluntariamente un lease específico.
func (m *Manager) Release(ctx context.Context, name string) error {
	m.mu.Lock()
	if cancel, ok := m.held[name]; ok {
		cancel()
		delete(m.held, name)
	}
	count := len(m.held)
	m.mu.Unlock()

	metricLeasesHeld.WithLabelValues(m.holder).Set(float64(count))

	const q = `DELETE FROM platform.worker_leases WHERE name = $1 AND holder = $2`
	_, err := m.pool.Exec(ctx, q, name, m.holder)
	if m.logger != nil {
		m.logger.InfoContext(ctx, "lease released", "name", name, "holder", m.holder)
	}
	return err
}

// ReleaseAll libera todos los leases que retiene esta réplica.
func (m *Manager) ReleaseAll(ctx context.Context) error {
	m.mu.Lock()
	for _, cancel := range m.held {
		cancel()
	}
	m.held = make(map[string]context.CancelFunc)
	m.mu.Unlock()

	metricLeasesHeld.WithLabelValues(m.holder).Set(0)

	const q = `DELETE FROM platform.worker_leases WHERE holder = $1`
	_, err := m.pool.Exec(ctx, q, m.holder)
	return err
}

// ForceRelease fuerza la liberación de un lease independientemente de quién lo retenga.
func (m *Manager) ForceRelease(ctx context.Context, name string) error {
	const q = `DELETE FROM platform.worker_leases WHERE name = $1`
	_, err := m.pool.Exec(ctx, q, name)
	return err
}

// List devuelve todos los leases registrados en el sistema.
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	const q = `
SELECT name, holder, acquired_at, expires_at, (expires_at > now()) AS active
FROM platform.worker_leases
ORDER BY name;
`
	rows, err := m.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Info
	for rows.Next() {
		var item Info
		if err := rows.Scan(&item.Name, &item.Holder, &item.AcquiredAt, &item.ExpiresAt, &item.Active); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}
