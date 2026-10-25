// Package bus es el command bus in-process (PP-F1-ARQ-003). Cada módulo
// registra sus handlers al arrancar; el handler ejecuta el flujo completo de
// un comando: authz (en la capa http) → idempotency → UoW {load → handle →
// append → project → outbox}. Un comando toca un solo agregado.
package bus

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Command es un comando de dominio dirigido a un agregado.
type Command interface {
	CommandName() string
}

// Result es lo que devuelve un handler de comando (ARQ-003): HTTP responde
// 201/200 con {id, version} o el recurso proyectado.
type Result struct {
	AggregateID uuid.UUID
	Version     int64
}

// Handler ejecuta un comando.
type Handler func(ctx context.Context, cmd Command) (Result, error)

// Bus despacha comandos a sus handlers por nombre.
type Bus struct {
	handlers map[string]Handler
}

// New construye el bus.
func New() *Bus { return &Bus{handlers: map[string]Handler{}} }

// Register asocia el handler al comando. Pánico si ya está registrado
// (error de arranque).
func (b *Bus) Register(cmd Command, h Handler) {
	name := cmd.CommandName()
	if _, dup := b.handlers[name]; dup {
		panic("bus: comando ya registrado: " + name)
	}
	b.handlers[name] = h
}

// Dispatch ejecuta el handler del comando. Comando desconocido → error
// (error de programación: falta registro en el arranque del módulo).
func (b *Bus) Dispatch(ctx context.Context, cmd Command) (Result, error) {
	h, ok := b.handlers[cmd.CommandName()]
	if !ok {
		return Result{}, fmt.Errorf("bus: sin handler para %T", cmd)
	}
	return h(ctx, cmd)
}
