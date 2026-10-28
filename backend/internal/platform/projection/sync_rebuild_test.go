package projection

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

type plainProjector struct{ name string }

func (p plainProjector) Name() string      { return p.name }
func (p plainProjector) Handles() []string { return []string{"X"} }
func (plainProjector) Project(context.Context, pgx.Tx, eventstore.StoredEvent) error {
	return nil
}

type resettableProjector struct{ plainProjector }

func (resettableProjector) ResetForRebuild(context.Context, pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	return nil, nil
}

// Un proyector primario sin ResetForRebuild dejaría sus tablas sin vaciar:
// RebuildSync falla antes de abrir la transacción (db nil no se toca).
func TestRebuildSync_RequiresResetters(t *testing.T) {
	r := NewRunner()
	r.Register(resettableProjector{plainProjector{"a.ok"}})
	r.Register(plainProjector{"b.sin_reset"})
	if got := r.Names(); len(got) != 2 || got[0] != "a.ok" || got[1] != "b.sin_reset" {
		t.Fatalf("Names = %v (orden de registro)", got)
	}
	_, err := r.RebuildSync(context.Background(), nil, nil, SyncRebuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "b.sin_reset") || strings.Contains(err.Error(), "a.ok") {
		t.Fatalf("err = %v", err)
	}
}
