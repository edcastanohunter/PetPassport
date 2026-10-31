package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// cardNamespace deriva el stream_id del carnet de la mascota (vax-<PetID>,
// DOM-006): es distinto del id de la mascota para no colisionar con su stream.
var cardNamespace = uuid.MustParse("3c9a1e55-7d2b-4a0c-9f31-5b8e6d4c2a77")

// CardStreamID devuelve el stream del carnet de una mascota.
func CardStreamID(petID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(cardNamespace, []byte("vax-"+petID.String()))
}

// CardRepository carga y persiste VaccinationCard desde el Event Store.
type CardRepository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	ids   kernel.IDGenerator
	clock kernel.Clock
}

var _ app.CardRepository = (*CardRepository)(nil)

// NewCardRepository construye el repositorio.
func NewCardRepository(store *eventstore.Store, reg *eventstore.Registry, ids kernel.IDGenerator, clock kernel.Clock) *CardRepository {
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &CardRepository{store: store, reg: reg, ids: ids, clock: clock}
}

// RegisterCardEvents registra los eventos VaccinationCard v1 (arranque).
func RegisterCardEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.CardSpeciesSynced{} })
	reg.Register(func() eventstore.Event { return domain.VaccinationEntryRecorded{} })
	reg.Register(func() eventstore.Event { return domain.PreventiveEntryRecorded{} })
	reg.Register(func() eventstore.Event { return domain.VaccinationEntryDeclared{} })
	reg.Register(func() eventstore.Event { return domain.VaccinationEntryVerified{} })
	reg.Register(func() eventstore.Event { return domain.VaccinationEntryDisputed{} })
	reg.Register(func() eventstore.Event { return domain.VaccinationEntryVoided{} })
	reg.Register(func() eventstore.Event { return domain.NextDueAdjusted{} })
}

// Load rehidrata por replay. Stream inexistente → carnet vacío (versión 0).
func (r *CardRepository) Load(ctx context.Context, petID uuid.UUID) (*domain.VaccinationCard, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, CardStreamID(petID), 0)
	if err != nil {
		return nil, err
	}
	card := &domain.VaccinationCard{}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("clinical card load %s: %w", petID, err)
		}
		if err := card.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return card, nil
}

// Save hace Append de los eventos no confirmados con la versión esperada.
func (r *CardRepository) Save(ctx context.Context, card *domain.VaccinationCard, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := card.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := card.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("clinical card save: evento %T no implementa eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID: r.ids.NewID(), Type: ev.EventName(), SchemaVersion: ev.SchemaVersion(),
			Payload: ev, Metadata: meta, OccurredAt: r.clock.Now(),
		}
	}
	stored, err := r.store.Append(ctx, tx, CardStreamID(card.PetID()), domain.CardStreamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("clinical card append: %w", err)
	}
	card.MarkCommitted()
	return stored, nil
}
