// Package reqctx transporta los metadatos de un request (ids de correlación,
// dirección remota, actor) por el contexto. Lo rellena httpserver, lo completa
// auth (actor) y lo leen logs, auditoría y handlers. No contiene PII.
package reqctx

import (
	"context"
	"sync"
)

// Info son los metadatos del request. ActorID se fija cuando el guard autentica,
// por eso es mutable y seguro para concurrencia.
type Info struct {
	RequestID     string
	CorrelationID string
	RemoteAddr    string

	mu      sync.Mutex
	actorID string
}

type key struct{}

// With adjunta la info al contexto.
func With(ctx context.Context, i *Info) context.Context { return context.WithValue(ctx, key{}, i) }

// From devuelve la info; nunca nil (vacía si no hay).
func From(ctx context.Context) *Info {
	if i, ok := ctx.Value(key{}).(*Info); ok && i != nil {
		return i
	}
	return &Info{}
}

// SetActor registra el actor autenticado (id opaco, no PII).
func (i *Info) SetActor(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.actorID = id
}

// ActorID devuelve el actor o "" si el request es anónimo.
func (i *Info) ActorID() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.actorID
}
