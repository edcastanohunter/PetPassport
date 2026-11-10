package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// RegisterEvents registra los 18 tipos de eventos de Pharmacy Orders v1 en el Registry (PP-F4-DOM-010).
func RegisterEvents(reg *eventstore.Registry) {
	// QuoteRequest events
	reg.Register(func() eventstore.Event { return domain.QuoteRequested{} })
	reg.Register(func() eventstore.Event { return domain.QuoteInvitationSent{} })
	reg.Register(func() eventstore.Event { return domain.QuoteInvitationFailed{} })
	reg.Register(func() eventstore.Event { return domain.QuoteReceived{} })
	reg.Register(func() eventstore.Event { return domain.QuoteDeclined{} })
	reg.Register(func() eventstore.Event { return domain.QuoteSelected{} })
	reg.Register(func() eventstore.Event { return domain.QuoteRequestCancelled{} })
	reg.Register(func() eventstore.Event { return domain.QuoteRequestExpired{} })
	reg.Register(func() eventstore.Event { return domain.QuoteRequestClosed{} })
	reg.Register(func() eventstore.Event { return domain.QuoteRequestReopened{} })

	// Order events
	reg.Register(func() eventstore.Event { return domain.OrderPlaced{} })
	reg.Register(func() eventstore.Event { return domain.OrderConfirmed{} })
	reg.Register(func() eventstore.Event { return domain.OrderRejected{} })
	reg.Register(func() eventstore.Event { return domain.OrderPreparing{} })
	reg.Register(func() eventstore.Event { return domain.OrderReady{} })
	reg.Register(func() eventstore.Event { return domain.OrderOutForDelivery{} })
	reg.Register(func() eventstore.Event { return domain.OrderDelivered{} })
	reg.Register(func() eventstore.Event { return domain.OrderCancelled{} })
	reg.Register(func() eventstore.Event { return domain.OrderRated{} })
}

// QuoteRequestRepository persiste y rehidrata agregados QuoteRequest sobre el Event Store (PP-F4-DOM-008).
type QuoteRequestRepository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	clock func() time.Time
}

// NewQuoteRequestRepository construye el repositorio de solicitudes de cotización.
func NewQuoteRequestRepository(store *eventstore.Store, reg *eventstore.Registry) *QuoteRequestRepository {
	return &QuoteRequestRepository{
		store: store,
		reg:   reg,
		clock: time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (r *QuoteRequestRepository) WithClock(clock func() time.Time) *QuoteRequestRepository {
	r.clock = clock
	return r
}

// Load rehidrata el agregado QuoteRequest desde su stream `qr-<id>`.
func (r *QuoteRequestRepository) Load(ctx context.Context, id uuid.UUID) (*domain.QuoteRequest, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("quote request repo load: %w", err)
	}

	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("load stream for qr %s: %w", id, err)
	}

	if len(events) == 0 {
		return nil, kernel.NewDomainError("QUOTE_REQUEST_NOT_FOUND", "Solicitud de cotización no encontrada.")
	}

	qr := domain.NewQuoteRequest()
	for _, se := range events {
		evt, err := r.reg.Decode(se)
		if err != nil {
			return nil, fmt.Errorf("decode event %s: %w", se.Type, err)
		}
		qr.Apply(evt)
	}
	qr.MarkCommitted()
	return qr, nil
}

// Save persiste los eventos pendientes del agregado con control de concurrencia optimista.
func (r *QuoteRequestRepository) Save(ctx context.Context, qr *domain.QuoteRequest) ([]eventstore.StoredEvent, error) {
	pending := qr.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}

	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("quote request repo save: %w", err)
	}

	expectedVersion := qr.Version - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("quote request save: %T no es eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            kernel.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			OccurredAt:    r.clock(),
		}
	}

	storedEvents, err := r.store.Append(ctx, tx, qr.ID, domain.StreamTypeQuoteRequest, expectedVersion, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("append stream qr %s: %w", qr.ID, err)
	}

	qr.MarkCommitted()
	return storedEvents, nil
}

// OrderRepository persiste y rehidrata agregados Order sobre el Event Store (PP-F4-DOM-009).
type OrderRepository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	clock func() time.Time
}

// NewOrderRepository construye el repositorio de pedidos.
func NewOrderRepository(store *eventstore.Store, reg *eventstore.Registry) *OrderRepository {
	return &OrderRepository{
		store: store,
		reg:   reg,
		clock: time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (r *OrderRepository) WithClock(clock func() time.Time) *OrderRepository {
	r.clock = clock
	return r
}

// Load rehidrata el agregado Order desde su stream `ord-<id>`.
func (r *OrderRepository) Load(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("order repo load: %w", err)
	}

	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("load stream for order %s: %w", id, err)
	}

	if len(events) == 0 {
		return nil, kernel.NewDomainError("ORDER_NOT_FOUND", "Pedido no encontrado.")
	}

	order := domain.NewOrder()
	for _, se := range events {
		evt, err := r.reg.Decode(se)
		if err != nil {
			return nil, fmt.Errorf("decode event %s: %w", se.Type, err)
		}
		order.Apply(evt)
	}
	order.MarkCommitted()
	return order, nil
}

// Save persiste los eventos pendientes de Order con control de concurrencia optimista.
func (r *OrderRepository) Save(ctx context.Context, order *domain.Order) ([]eventstore.StoredEvent, error) {
	pending := order.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}

	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, fmt.Errorf("order repo save: %w", err)
	}

	expectedVersion := order.Version - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("order save: %T no es eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            kernel.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			OccurredAt:    r.clock(),
		}
	}

	storedEvents, err := r.store.Append(ctx, tx, order.ID, domain.StreamTypeOrder, expectedVersion, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("append stream order %s: %w", order.ID, err)
	}

	order.MarkCommitted()
	return storedEvents, nil
}
