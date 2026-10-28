// Package maintenance implementa el modo mantenimiento de la API
// (PP-F2-ARQ-006): mientras exista la fila de platform.maintenance, /api/v1
// responde 503 con Retry-After. Lo activa el worker durante `rebuild --sync`
// de las proyecciones primarias (solo para desastre) y se puede activar o
// desactivar a mano (`worker maintenance on|off`, RUNBOOK §15).
package maintenance

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

// DefaultTTL es lo que tarda cada instancia del api en ver un cambio de estado.
const DefaultTTL = 2 * time.Second

// RetryAfterSeconds se anuncia a los clientes mientras dura el mantenimiento.
const RetryAfterSeconds = 30

// DB es lo que el paquete necesita de pgxpool.Pool o pgx.Tx.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// State es el estado persistido.
type State struct {
	Active bool
	Reason string
	Since  time.Time
}

// Enable activa el modo (idempotente; actualiza el motivo).
func Enable(ctx context.Context, db DB, reason string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO platform.maintenance (reason) VALUES ($1)
		ON CONFLICT (singleton) DO UPDATE SET reason = EXCLUDED.reason`, reason)
	return err
}

// Disable desactiva el modo (idempotente).
func Disable(ctx context.Context, db DB) error {
	_, err := db.Exec(ctx, `DELETE FROM platform.maintenance`)
	return err
}

// Get lee el estado actual.
func Get(ctx context.Context, db DB) (State, error) {
	var s State
	err := db.QueryRow(ctx, `SELECT reason, since FROM platform.maintenance`).Scan(&s.Reason, &s.Since)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	s.Active = true
	return s, nil
}

// Gate cachea el estado durante ttl y lo aplica como middleware HTTP.
type Gate struct {
	db  DB
	ttl time.Duration
	now func() time.Time

	mu     sync.Mutex
	active bool
	until  time.Time
}

// NewGate construye el gate. ttl <= 0 usa DefaultTTL.
func NewGate(db DB, ttl time.Duration) *Gate {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Gate{db: db, ttl: ttl, now: time.Now}
}

// Active devuelve el estado, consultando la BD como mucho una vez por ttl. Si
// la consulta falla conserva el último estado conocido: un error transitorio
// de BD no debe convertirse en 503 para todos (ni sacar del mantenimiento).
func (g *Gate) Active(ctx context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if now.Before(g.until) {
		return g.active
	}
	if s, err := Get(ctx, g.db); err == nil {
		g.active = s.Active
	}
	g.until = now.Add(g.ttl)
	return g.active
}

// Middleware responde 503 UNAVAILABLE con Retry-After mientras el modo está
// activo. healthz queda fuera para no confundir mantenimiento con caída.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/healthz" && g.Active(r.Context()) {
			w.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds))
			problem.Write(w, r, problem.New(problem.Unavailable, "En mantenimiento",
				"El servicio está en mantenimiento. Inténtalo de nuevo en unos minutos."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
