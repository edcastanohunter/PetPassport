package infra

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ProcessRules es el nombre estable del PM (clave del checkpoint, 03 §9).
const ProcessRules = "notifications.rules"

// Rules es el process manager de reglas de notificación (NOT-001). Escribe en
// notifications.* dentro de la tx del outbox y nunca despacha comandos.
type Rules struct{ clock kernel.Clock }

var _ process.ProcessManager = (*Rules)(nil)

// NewRules construye el PM.
func NewRules(clock kernel.Clock) *Rules {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &Rules{clock: clock}
}

// Name implementa ProcessManager.
func (*Rules) Name() string { return ProcessRules }

// Handles implementa ProcessManager.
func (*Rules) Handles() []string { return app.HandledEvents }

// Emits implementa ProcessManager: no emite comandos.
func (*Rules) Emits() []string { return nil }

// Handle implementa ProcessManager: planifica con el directorio de la tx y
// aplica las acciones. Reentregas del mismo evento no duplican nada: las
// entregas y los programados son únicos por dedup_key.
func (r *Rules) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	acts, err := app.NewPlanner(NewDirectory(tx), r.clock).Plan(ctx, e)
	if err != nil {
		return err
	}
	if err := NewStore(tx).Apply(ctx, acts); err != nil {
		return fmt.Errorf("%s: %s: %w", ProcessRules, e.Type, err)
	}
	return nil
}
