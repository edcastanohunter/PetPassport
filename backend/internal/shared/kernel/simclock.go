package kernel

import (
	"sync"
	"time"
)

// SimClock es un reloj simulado y avanzable para pruebas de process managers
// y del scheduler de comandos (PP-F2-QA-003). Seguro para concurrencia.
type SimClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewSimClock crea el reloj en el instante dado (UTC).
func NewSimClock(t time.Time) *SimClock { return &SimClock{now: t.UTC()} }

// Now implementa Clock.
func (c *SimClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance adelanta el reloj d.
func (c *SimClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set fija el instante.
func (c *SimClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.UTC()
}
