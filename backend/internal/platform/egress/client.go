package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrEgressDenied     = errors.New("egress: host no permitido en allowlist")
	ErrSSRFBlocked      = errors.New("egress: dirección IP privada o restringida (SSRF bloqueado)")
	ErrCircuitOpen      = errors.New("egress: circuit breaker abierto por fallos consecutivos")
	ErrResponseTooLarge = errors.New("egress: tamaño de respuesta excede el límite permitido")
)

// Config define las opciones del cliente de egress seguro (PP-F4-ARQ-006, SEC-004).
type Config struct {
	AllowedHosts      []string
	ConnectTimeout    time.Duration
	RequestTimeout    time.Duration
	MaxResponseBytes  int64
	CircuitThreshold  int           // fallos consecutivos para abrir circuito (defecto 5)
	CircuitResetAfter time.Duration // tiempo para reintentar tras circuito abierto (defecto 5 min)
}

// CircuitState mantiene el estado del circuit breaker para un host específico.
type CircuitState struct {
	failures  int
	lastError time.Time
	isOpen    bool
}

// Client gestiona peticiones HTTP salientes seguras con anti-SSRF y allowlist (PP-F4-ARQ-006, SEC-008).
type Client struct {
	cfg           Config
	httpClient    *http.Client
	mu            sync.RWMutex
	circuitBreaks map[string]*CircuitState
}

// NewClient inicializa un cliente de egress validado.
func NewClient(cfg Config) *Client {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 3 * time.Second
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 60 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 5 << 20 // 5 MB para IA
	}
	if cfg.CircuitThreshold == 0 {
		cfg.CircuitThreshold = 5
	}
	if cfg.CircuitResetAfter == 0 {
		cfg.CircuitResetAfter = 5 * time.Minute
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Comprobación anti-SSRF de la IP resuelta
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolución DNS fallida: %w", err)
			}
			for _, ip := range ips {
				if isRestrictedIP(ip) {
					return nil, fmt.Errorf("%w: %s (%s)", ErrSSRFBlocked, host, ip.String())
				}
			}

			dialer := &net.Dialer{
				Timeout: cfg.ConnectTimeout,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		ResponseHeaderTimeout: cfg.RequestTimeout,
	}

	c := &Client{
		cfg:           cfg,
		circuitBreaks: make(map[string]*CircuitState),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.RequestTimeout,
		},
	}
	return c
}

// HTTPClient devuelve el *http.Client subyacente para usar con SDKs externos.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// CheckAllowed valida si una URL está permitida por allowlist y no tiene el circuito abierto.
func (c *Client) CheckAllowed(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if !c.isHostAllowed(host) {
		return fmt.Errorf("%w: %s", ErrEgressDenied, host)
	}

	if c.isCircuitOpen(host) {
		return fmt.Errorf("%w: %s", ErrCircuitOpen, host)
	}

	return nil
}

// RecordSuccess registra una petición exitosa y reinicia el circuit breaker.
func (c *Client) RecordSuccess(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	host = strings.ToLower(host)
	if cs, ok := c.circuitBreaks[host]; ok {
		cs.failures = 0
		cs.isOpen = false
	}
}

// RecordFailure registra un fallo HTTP y potencialmente abre el circuit breaker.
func (c *Client) RecordFailure(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	host = strings.ToLower(host)
	cs, ok := c.circuitBreaks[host]
	if !ok {
		cs = &CircuitState{}
		c.circuitBreaks[host] = cs
	}
	cs.failures++
	cs.lastError = time.Now()
	if cs.failures >= c.cfg.CircuitThreshold {
		cs.isOpen = true
	}
}

func (c *Client) isHostAllowed(host string) bool {
	if len(c.cfg.AllowedHosts) == 0 {
		return false
	}
	for _, allowed := range c.cfg.AllowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "*" || allowed == host {
			return true
		}
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // .example.com
			if strings.HasSuffix(host, suffix) {
				return true
			}
		}
	}
	return false
}

func (c *Client) isCircuitOpen(host string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cs, ok := c.circuitBreaks[strings.ToLower(host)]
	if !ok || !cs.isOpen {
		return false
	}
	// Comprobar si ya expiró el tiempo de enfriamiento
	if time.Since(cs.lastError) > c.cfg.CircuitResetAfter {
		return false
	}
	return true
}

func isRestrictedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// AWS / GCP / Cloud metadata 169.254.169.254
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}
