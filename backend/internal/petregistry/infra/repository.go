// Package infra contiene los adaptadores de Pet Registry: repositorio sobre
// el event store, proyector de read_petregistry.pets (+ reservas y vínculos
// en la misma tx), consultas sqlc y adaptadores de puertos a Identity.
package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Repository carga y persiste Pet desde el Event Store (ARQ-004). La tx del
// UoW viaja en el contexto; Append con expected version del agregado.
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

// RegisterEvents registra los eventos de Pet Registry v1 en el registry
// (arranque; validar sin versión es error).
func RegisterEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.PetRegistered{} })
	reg.Register(func() eventstore.Event { return domain.PetProfileUpdated{} })
	reg.Register(func() eventstore.Event { return domain.PetMicrochipAssigned{} })
	reg.Register(func() eventstore.Event { return domain.PetPhotoChanged{} })
	reg.Register(func() eventstore.Event { return domain.PetMarkedDeceased{} })
	reg.Register(func() eventstore.Event { return domain.PetDeceasedUndone{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkGranted{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkRevoked{} })
	reg.Register(func() eventstore.Event { return domain.GuardianshipTransferInitiated{} })
	reg.Register(func() eventstore.Event { return domain.GuardianshipTransferCancelled{} })
	reg.Register(func() eventstore.Event { return domain.PetGuardianshipTransferred{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkRequested{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkRequestApproved{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkRequestDeclined{} })
	reg.Register(func() eventstore.Event { return domain.CareLinkRequestExpired{} })
	reg.Register(func() eventstore.Event { return domain.PetAlertsUpdated{} })
}

// Load rehidrata por replay. Stream inexistente → agregado vacío (versión 0).
func (r *Repository) Load(ctx context.Context, id uuid.UUID) (*domain.Pet, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, err
	}
	pet := &domain.Pet{}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("petregistry load %s: %w", id, err)
		}
		if err := pet.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return pet, nil
}

// Save hace Append de los eventos no confirmados con la versión esperada
// (versión actual menos pendientes) y marca el agregado como confirmado.
func (r *Repository) Save(ctx context.Context, pet *domain.Pet, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := pet.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := pet.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("petregistry save: evento %T no implementa eventstore.Event", e)
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
	stored, err := r.store.Append(ctx, tx, pet.ID(), domain.StreamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("petregistry append: %w", err)
	}
	pet.MarkCommitted()
	return stored, nil
}
