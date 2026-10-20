package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Checker verifica una dependencia para /readyz.
type Checker func(ctx context.Context) error

// Health mantiene el estado de liveness/readiness del proceso.
// Ver docs/phase-1/03 §8 (arranque/apagado) y 05 §3.7.
type Health struct {
	ready    atomic.Bool
	mu       sync.RWMutex
	checkers map[string]Checker
}

// NewHealth crea un Health no listo.
func NewHealth() *Health { return &Health{checkers: map[string]Checker{}} }

// Register añade una dependencia a comprobar en /readyz.
func (h *Health) Register(name string, c Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[name] = c
}

// SetReady marca el proceso como listo (o no) para recibir tráfico.
func (h *Health) SetReady(v bool) { h.ready.Store(v) }

// Liveness responde 200 si el proceso está vivo.
func (h *Health) Liveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readiness responde 200 si el proceso está marcado listo y todas las
// dependencias responden; 503 con detalle en caso contrario.
func (h *Health) Readiness(w http.ResponseWriter, r *http.Request) {
	type dep struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	resp := struct {
		Status string         `json:"status"`
		Checks map[string]dep `json:"checks"`
	}{Status: "ok", Checks: map[string]dep{}}

	if !h.ready.Load() {
		resp.Status = "not_ready"
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for name, c := range h.checkers {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err := c(ctx)
		cancel()
		if err != nil {
			resp.Status = "not_ready"
			resp.Checks[name] = dep{Status: "fail", Error: err.Error()}
			continue
		}
		resp.Checks[name] = dep{Status: "ok"}
	}
	code := http.StatusOK
	if resp.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
