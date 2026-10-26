package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Repository carga y persiste Organization desde el Event Store (ARQ-004).
type Repository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	ids   kernel.IDGenerator
	clock kernel.Clock
}

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

// RegisterEvents registra los eventos de Partner Network v1 en el registry.
func RegisterEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.OrganizationRegistered{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationProfileUpdated{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationVerificationReset{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationVerified{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationRejected{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationSuspended{} })
	reg.Register(func() eventstore.Event { return domain.OrganizationReactivated{} })
	reg.Register(func() eventstore.Event { return domain.MemberInvited{} })
	reg.Register(func() eventstore.Event { return domain.InvitationRevoked{} })
	reg.Register(func() eventstore.Event { return domain.MemberJoined{} })
	reg.Register(func() eventstore.Event { return domain.MemberRoleChanged{} })
	reg.Register(func() eventstore.Event { return domain.MemberRemoved{} })
}

// Load rehidrata por replay. Stream inexistente → agregado vacío (versión 0).
func (r *Repository) Load(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, err
	}
	o := &domain.Organization{}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("partnernetwork load %s: %w", id, err)
		}
		if err := o.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// Save hace Append con la versión esperada y marca el agregado confirmado.
func (r *Repository) Save(ctx context.Context, o *domain.Organization, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := o.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := o.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("partnernetwork save: evento %T no implementa eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID: r.ids.NewID(), Type: ev.EventName(), SchemaVersion: ev.SchemaVersion(),
			Payload: ev, Metadata: meta, OccurredAt: r.clock.Now(),
		}
	}
	stored, err := r.store.Append(ctx, tx, o.ID(), domain.StreamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("partnernetwork append: %w", err)
	}
	o.MarkCommitted()
	return stored, nil
}
