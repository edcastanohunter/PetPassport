package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

const (
	StreamTypeBillingAccount = "bacct"
	StreamTypeSubscription   = "sub"
)

// BillingAccountRepository persiste y rehidrata cuentas de facturación en el Event Store (PP-F5-DOM-003).
type BillingAccountRepository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	clock func() time.Time
}

// NewBillingAccountRepository construye el repositorio de cuentas.
func NewBillingAccountRepository(store *eventstore.Store, reg *eventstore.Registry) *BillingAccountRepository {
	return &BillingAccountRepository{
		store: store,
		reg:   reg,
		clock: time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (r *BillingAccountRepository) WithClock(clock func() time.Time) *BillingAccountRepository {
	r.clock = clock
	return r
}

// Load recupera una cuenta de facturación por ID rehidratando sus eventos.
func (r *BillingAccountRepository) Load(ctx context.Context, id uuid.UUID) (*domain.BillingAccount, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing account repo load: %w", err)
	}

	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("load stream for account %s: %w", id, err)
	}

	if len(events) == 0 {
		return nil, kernel.NewDomainError("BILLING_ACCOUNT_NOT_FOUND", "Cuenta de facturación no encontrada.")
	}

	acc := domain.NewBillingAccount()
	for _, se := range events {
		evt, err := r.reg.Decode(se)
		if err != nil {
			return nil, fmt.Errorf("decode event %s: %w", se.Type, err)
		}
		acc.Apply(evt)
	}
	return acc, nil
}

// Save persiste los eventos no confirmados de la cuenta.
func (r *BillingAccountRepository) Save(ctx context.Context, acc *domain.BillingAccount) ([]eventstore.StoredEvent, error) {
	pending := acc.Changes()
	if len(pending) == 0 {
		return nil, nil
	}

	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing account repo save: %w", err)
	}

	expectedVersion := acc.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("billing account save: %T no es eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            kernel.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			OccurredAt:    r.clock(),
		}
	}

	storedEvents, err := r.store.Append(ctx, tx, acc.ID(), StreamTypeBillingAccount, expectedVersion, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("append stream account %s: %w", acc.ID(), err)
	}

	return storedEvents, nil
}

// SubscriptionRepository persiste y rehidrata agregados Subscription (PP-F5-DOM-004).
type SubscriptionRepository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	clock func() time.Time
}

// NewSubscriptionRepository construye el repositorio de suscripciones.
func NewSubscriptionRepository(store *eventstore.Store, reg *eventstore.Registry) *SubscriptionRepository {
	return &SubscriptionRepository{
		store: store,
		reg:   reg,
		clock: time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (r *SubscriptionRepository) WithClock(clock func() time.Time) *SubscriptionRepository {
	r.clock = clock
	return r
}

// Load recupera una suscripción por ID rehidratando sus eventos.
func (r *SubscriptionRepository) Load(ctx context.Context, id uuid.UUID) (*domain.Subscription, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscription repo load: %w", err)
	}

	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("load stream for sub %s: %w", id, err)
	}

	if len(events) == 0 {
		return nil, kernel.NewDomainError("SUBSCRIPTION_NOT_FOUND", "Suscripción no encontrada.")
	}

	sub := domain.NewSubscription()
	for _, se := range events {
		evt, err := r.reg.Decode(se)
		if err != nil {
			return nil, fmt.Errorf("decode event %s: %w", se.Type, err)
		}
		sub.Apply(evt)
	}
	return sub, nil
}

// Save persiste los eventos pendientes de la suscripción.
func (r *SubscriptionRepository) Save(ctx context.Context, sub *domain.Subscription) ([]eventstore.StoredEvent, error) {
	pending := sub.Changes()
	if len(pending) == 0 {
		return nil, nil
	}

	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscription repo save: %w", err)
	}

	expectedVersion := sub.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("subscription save: %T no es eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            kernel.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			OccurredAt:    r.clock(),
		}
	}

	storedEvents, err := r.store.Append(ctx, tx, sub.ID(), StreamTypeSubscription, expectedVersion, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("append stream sub %s: %w", sub.ID(), err)
	}

	return storedEvents, nil
}
