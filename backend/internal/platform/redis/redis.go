package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	goredis "github.com/redis/go-redis/v9"
)

var (
	// ErrCircuitOpen se devuelve cuando el circuit breaker de Redis está abierto.
	ErrCircuitOpen = errors.New("redis circuit breaker is open")

	metricCircuitState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "petpassport",
		Subsystem: "redis",
		Name:      "circuit_breaker_state",
		Help:      "Estado del circuit breaker de Redis (0=closed, 1=half-open, 2=open).",
	}, []string{"db"})

	metricOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "redis",
		Name:      "operations_total",
		Help:      "Operaciones sobre Redis por base de datos, operación y resultado.",
	}, []string{"db", "op", "status"})
)

// CircuitState representa el estado del circuit breaker.
type CircuitState int32

const (
	StateClosed CircuitState = iota
	StateHalfOpen
	StateOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// Config define la configuración de conexión a Redis (PP-F3-ARQ-005).
type Config struct {
	Addr         string        // PP_REDIS_ADDR (ej. "localhost:6379")
	Password     string        // PP_REDIS_PASSWORD
	DB           int           // 0 = caché, 1 = colas/asynq
	UseTLS       bool          // PP_REDIS_TLS
	DialTimeout  time.Duration // Default 200ms
	ReadTimeout  time.Duration // Default 100ms para caché, 1s para colas
	WriteTimeout time.Duration // Default 200ms para caché, 1s para colas
	PoolSize     int           // Max conns en pool (default 50)
	ClientName   string        // Identificador del cliente

	// Circuit breaker thresholds
	FailureThreshold int           // Fallos consecutivos para abrir (default 5)
	OpenDuration     time.Duration // Tiempo que permanece abierto (default 30s)
}

// Breaker implementa un circuit breaker simple y concurrente para Redis.
type Breaker struct {
	mu            sync.Mutex
	state         CircuitState
	failures      int
	threshold     int
	openDuration  time.Duration
	openedAt      time.Time
	dbName        string
	onStateChange func(from, to CircuitState)
}

// NewBreaker construye un circuit breaker para Redis.
func NewBreaker(dbName string, threshold int, openDuration time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if openDuration <= 0 {
		openDuration = 30 * time.Second
	}
	b := &Breaker{
		state:        StateClosed,
		threshold:    threshold,
		openDuration: openDuration,
		dbName:       dbName,
	}
	metricCircuitState.WithLabelValues(dbName).Set(float64(StateClosed))
	return b
}

// Allow comprueba si la petición puede ejecutarse según el estado del breaker.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if now.Sub(b.openedAt) >= b.openDuration {
			b.transition(StateHalfOpen)
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess registra una operación exitosa y cierra el breaker si estaba half-open.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures = 0
	if b.state != StateClosed {
		b.transition(StateClosed)
	}
}

// RecordFailure registra un error de Redis; si supera el umbral, abre el breaker.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	if b.state == StateHalfOpen || b.failures >= b.threshold {
		b.transition(StateOpen)
	}
}

func (b *Breaker) transition(to CircuitState) {
	from := b.state
	b.state = to
	if to == StateOpen {
		b.openedAt = time.Now()
	}
	metricCircuitState.WithLabelValues(b.dbName).Set(float64(to))
	if b.onStateChange != nil {
		b.onStateChange(from, to)
	}
}

// State devuelve el estado actual del breaker.
func (b *Breaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Client envuelve el cliente go-redis junto con su circuit breaker (PP-F3-ARQ-005).
type Client struct {
	rdb     *goredis.Client
	breaker *Breaker
	dbName  string
	closed  atomic.Bool
}

// NewClient crea un cliente Redis configurado según las especificaciones de F3.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 200 * time.Millisecond
	}
	if cfg.ReadTimeout <= 0 {
		if cfg.DB == 1 {
			cfg.ReadTimeout = 1 * time.Second
		} else {
			cfg.ReadTimeout = 100 * time.Millisecond
		}
	}
	if cfg.WriteTimeout <= 0 {
		if cfg.DB == 1 {
			cfg.WriteTimeout = 1 * time.Second
		} else {
			cfg.WriteTimeout = 200 * time.Millisecond
		}
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 50
	}

	opts := &goredis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
		ClientName:   cfg.ClientName,
	}
	if cfg.UseTLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rdb := goredis.NewClient(opts)
	dbName := fmt.Sprintf("db%d", cfg.DB)
	breaker := NewBreaker(dbName, cfg.FailureThreshold, cfg.OpenDuration)

	return &Client{
		rdb:     rdb,
		breaker: breaker,
		dbName:  dbName,
	}, nil
}

// RDB devuelve el cliente subyacente de go-redis.
func (c *Client) RDB() *goredis.Client {
	return c.rdb
}

// Breaker devuelve el circuit breaker del cliente.
func (c *Client) Breaker() *Breaker {
	return c.breaker
}

// Ping verifica la conexión a Redis registrando métricas y afectando al breaker.
func (c *Client) Ping(ctx context.Context) error {
	if !c.breaker.Allow() {
		metricOpsTotal.WithLabelValues(c.dbName, "ping", "circuit_open").Inc()
		return ErrCircuitOpen
	}
	err := c.rdb.Ping(ctx).Err()
	if err != nil {
		c.breaker.RecordFailure()
		metricOpsTotal.WithLabelValues(c.dbName, "ping", "error").Inc()
		return err
	}
	c.breaker.RecordSuccess()
	metricOpsTotal.WithLabelValues(c.dbName, "ping", "ok").Inc()
	return nil
}

// Do ejecuta una función sobre Redis protegida por el circuit breaker.
func (c *Client) Do(op string, fn func(rdb *goredis.Client) error) error {
	if !c.breaker.Allow() {
		metricOpsTotal.WithLabelValues(c.dbName, op, "circuit_open").Inc()
		return ErrCircuitOpen
	}
	err := fn(c.rdb)
	if err != nil {
		if !errors.Is(err, goredis.Nil) {
			c.breaker.RecordFailure()
			metricOpsTotal.WithLabelValues(c.dbName, op, "error").Inc()
		} else {
			metricOpsTotal.WithLabelValues(c.dbName, op, "nil").Inc()
		}
		return err
	}
	c.breaker.RecordSuccess()
	metricOpsTotal.WithLabelValues(c.dbName, op, "ok").Inc()
	return nil
}

// Close cierra la conexión al servidor Redis.
func (c *Client) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		return c.rdb.Close()
	}
	return nil
}
