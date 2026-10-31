// Package app contiene los casos de uso de Scheduling y los puertos que
// consume de otros contextos (PP-F2-DOM-014, docs/phase-2/02 §6).
package app

import (
	"context"

	"github.com/google/uuid"
)

// PrincipalInfo desacopla el handler de platform/auth (solo lo necesario).
type PrincipalInfo struct {
	UserID     uuid.UUID
	SuperAdmin bool
}

// CommandContext es el contexto del request que el handler http adjunta al
// despachar un comando. Los comandos del sistema (scheduler de comandos) no
// lo traen: llevan bus.SystemOrigin.
type CommandContext struct {
	Principal       PrincipalInfo
	CorrelationID   string
	CausationID     string
	IPHash          string
	UserAgentHash   string
	IdempotencyKey  string
	ExpectedVersion int64
}

type cmdCtxKey struct{}

// WithCommandContext adjunta el contexto del comando.
func WithCommandContext(ctx context.Context, cc CommandContext) context.Context {
	return context.WithValue(ctx, cmdCtxKey{}, cc)
}

// CommandContextFrom devuelve el contexto del comando o uno vacío.
func CommandContextFrom(ctx context.Context) CommandContext {
	cc, _ := ctx.Value(cmdCtxKey{}).(CommandContext)
	return cc
}
