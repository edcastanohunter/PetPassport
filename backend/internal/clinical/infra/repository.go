// Package infra contiene los adaptadores de Clinical Records: repositorio
// sobre el Event Store, proyecciones síncronas (read_clinical.encounters y
// adjuntos) y asíncronas (peso, custodia, timeline), consultas y puertos.
package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Repository carga y persiste Encounter desde el Event Store (ARQ-004).
type Repository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	ids   kernel.IDGenerator
	clock kernel.Clock
}

var _ app.Repository = (*Repository)(nil)

// NewRepository construye el repositorio.
func NewRepository(store *eventstore.Store, reg *eventstore.Registry, ids kernel.IDGenerator, clock kernel.Clock) *Repository {
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &Repository{store: store, reg: reg, ids: ids, clock: clock}
}

// RegisterEvents registra los eventos Encounter v1 (arranque).
func RegisterEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.EncounterOpened{} })
	reg.Register(func() eventstore.Event { return domain.EncounterDetailsUpdated{} })
	reg.Register(func() eventstore.Event { return domain.VitalsRecorded{} })
	reg.Register(func() eventstore.Event { return domain.EncounterNarrativeUpdated{} })
	reg.Register(func() eventstore.Event { return domain.DiagnosesSet{} })
	reg.Register(func() eventstore.Event { return domain.PlanSet{} })
	reg.Register(func() eventstore.Event { return domain.FollowUpRecommended{} })
	reg.Register(func() eventstore.Event { return domain.FollowUpCleared{} })
	reg.Register(func() eventstore.Event { return domain.AttendingVetAssigned{} })
	reg.Register(func() eventstore.Event { return domain.AttachmentAdded{} })
	reg.Register(func() eventstore.Event { return domain.AttachmentRemoved{} })
	reg.Register(func() eventstore.Event { return domain.AttachmentVisibilityChanged{} })
	reg.Register(func() eventstore.Event { return domain.EncounterSigned{} })
	reg.Register(func() eventstore.Event { return domain.EncounterAddendumAdded{} })
	reg.Register(func() eventstore.Event { return domain.EncounterDiscarded{} })
}

// Load rehidrata por replay. Stream inexistente → agregado vacío (versión 0).
func (r *Repository) Load(ctx context.Context, id uuid.UUID) (*domain.Encounter, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, err
	}
	enc := &domain.Encounter{}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("clinical load %s: %w", id, err)
		}
		if err := enc.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return enc, nil
}

// Save hace Append de los eventos no confirmados con la versión esperada.
func (r *Repository) Save(ctx context.Context, enc *domain.Encounter, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := enc.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := enc.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("clinical save: evento %T no implementa eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            r.ids.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			Metadata:      meta,
			OccurredAt:    r.clock.Now(),
		}
	}
	stored, err := r.store.Append(ctx, tx, enc.ID(), domain.StreamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("clinical append: %w", err)
	}
	enc.MarkCommitted()
	return stored, nil
}

// ContentHash calcula E8/ARQ-010 sobre es.events hasta upToVersion.
func (r *Repository) ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `
		SELECT event_id, event_type, schema_version, payload
		FROM es.events WHERE stream_id = $1 AND stream_version <= $2
		ORDER BY stream_version`, id, upToVersion)
	if err != nil {
		return "", fmt.Errorf("clinical content hash: %w", err)
	}
	defer rows.Close()
	var events []domain.HashableEvent
	for rows.Next() {
		var e domain.HashableEvent
		var raw []byte
		if err := rows.Scan(&e.EventID, &e.EventType, &e.SchemaVersion, &raw); err != nil {
			return "", err
		}
		e.Payload = json.RawMessage(raw)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return domain.ContentHash(events)
}
