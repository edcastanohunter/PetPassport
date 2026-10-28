package projection

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// SyncResetter lo implementan los proyectores síncronos (read models
// primarios) para `rebuild --sync` (PP-F2-ARQ-006). ResetForRebuild vacía,
// dentro de tx, las tablas de las que el proyector es dueño y devuelve cómo
// restaurar tras el replay los datos operativos que no viajan en los eventos
// (hashes de token, contadores de reenvío…); restore nil = nada que restaurar.
type SyncResetter interface {
	ResetForRebuild(ctx context.Context, tx pgx.Tx) (restore func(context.Context, pgx.Tx) error, err error)
}

// EventSource es lo que el rebuild lee del Event Store (*eventstore.Store).
type EventSource interface {
	ReadRange(ctx context.Context, q eventstore.Querier, from, to int64, types []string, limit int) ([]eventstore.StoredEvent, error)
	Head(ctx context.Context, q eventstore.Querier) (int64, error)
}

// SyncRebuildOptions configura RebuildSync.
type SyncRebuildOptions struct {
	BatchSize int // eventos por lectura; 0 = 1000
	Logger    *slog.Logger
}

// SyncRebuildReport resume un rebuild síncrono.
type SyncRebuildReport struct {
	Projectors []string
	Head       int64 // última posición reproyectada
	Events     int   // eventos aplicados (de los tipos manejados)
	Duration   time.Duration
}

// Names devuelve los proyectores registrados, en orden de registro.
func (r *Runner) Names() []string {
	out := make([]string, len(r.order))
	for i, p := range r.order {
		out[i] = p.Name()
	}
	return out
}

// RebuildSync reconstruye todos los read models primarios a la vez
// (PP-F2-ARQ-006, solo para desastre, con la API en modo mantenimiento):
//
//  1. Una única transacción con el lock de orden total (ningún Append puede
//     confirmar mientras dura; los lectores siguen viendo el estado anterior
//     hasta el commit).
//  2. ResetForRebuild de cada proyector (guarda los datos operativos).
//  3. Replay de todos los eventos manejados hasta el head, en orden global y
//     con Apply: los proyectores primarios dependen entre sí (p. ej.
//     petregistry.org_reactions actualiza pet_care_links), así que se
//     reconstruyen juntos y en el mismo orden en que se proyectaron.
//  4. Restauración de los datos operativos y commit.
//
// Falla sin tocar nada si algún proyector registrado no implementa
// SyncResetter.
func (r *Runner) RebuildSync(ctx context.Context, db DB, src EventSource, opts SyncRebuildOptions) (SyncRebuildReport, error) {
	start := time.Now()
	report := SyncRebuildReport{Projectors: r.Names()}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	resetters := make([]SyncResetter, 0, len(r.order))
	var missing []string
	for _, p := range r.order {
		rs, ok := p.(SyncResetter)
		if !ok {
			missing = append(missing, p.Name())
			continue
		}
		resetters = append(resetters, rs)
	}
	if len(missing) > 0 {
		return report, fmt.Errorf("projection: rebuild --sync: sin ResetForRebuild: %s", strings.Join(missing, ", "))
	}
	types := make([]string, 0, len(r.byType))
	for t := range r.byType {
		types = append(types, t)
	}
	sort.Strings(types)

	tx, err := db.Begin(ctx)
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := eventstore.LockTotalOrder(ctx, tx); err != nil {
		return report, fmt.Errorf("projection: rebuild --sync: lock de orden total: %w", err)
	}
	head, err := src.Head(ctx, tx)
	if err != nil {
		return report, err
	}
	report.Head = head

	restores := make([]func(context.Context, pgx.Tx) error, 0, len(resetters))
	for i, rs := range resetters {
		restore, err := rs.ResetForRebuild(ctx, tx)
		if err != nil {
			return report, fmt.Errorf("projection: reset %s: %w", r.order[i].Name(), err)
		}
		if restore != nil {
			restores = append(restores, restore)
		}
	}

	log := opts.Logger.With("projection", "sync", "head", head)
	log.Info("rebuild --sync: replay")
	var from int64
	lastLog := time.Now()
	for from < head {
		batch, err := src.ReadRange(ctx, tx, from, head, types, opts.BatchSize)
		if err != nil {
			return report, err
		}
		if len(batch) == 0 {
			break
		}
		if err := r.Apply(ctx, tx, batch); err != nil {
			return report, err
		}
		from = batch[len(batch)-1].GlobalPosition
		report.Events += len(batch)
		if time.Since(lastLog) > 5*time.Second {
			log.Info("rebuild --sync: progreso", "global_position", from, "events", report.Events)
			lastLog = time.Now()
		}
	}
	for _, restore := range restores {
		if err := restore(ctx, tx); err != nil {
			return report, fmt.Errorf("projection: restaurar datos operativos: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return report, err
	}
	report.Duration = time.Since(start)
	log.Info("rebuild --sync: completado", "events", report.Events, "duration", report.Duration)
	return report, nil
}
