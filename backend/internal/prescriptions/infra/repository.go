package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// RegisterEvents registra los 13 tipos de eventos de Prescription v1 en el Registry.
func RegisterEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.PrescriptionDrafted{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionDraftUpdated{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionIssued{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionRevoked{} })
	reg.Register(func() eventstore.Event { return domain.ExternalPrescriptionUploaded{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionExtractionRecorded{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionReuploadRequested{} })
	reg.Register(func() eventstore.Event { return domain.ExternalPrescriptionConfirmed{} })
	reg.Register(func() eventstore.Event { return domain.ExternalPrescriptionReopened{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionPharmacistValidated{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionDispensed{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionExhausted{} })
	reg.Register(func() eventstore.Event { return domain.PrescriptionExpired{} })
}

// Repository persiste y rehidrata agregados Prescription sobre el Event Store (PP-F4-DOM-004).
type Repository struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	clock func() time.Time
}

// NewRepository construye el repositorio de prescripciones.
func NewRepository(store *eventstore.Store, reg *eventstore.Registry) *Repository {
	return &Repository{
		store: store,
		reg:   reg,
		clock: time.Now,
	}
}

// WithClock fija el reloj para pruebas.
func (r *Repository) WithClock(clock func() time.Time) *Repository {
	r.clock = clock
	return r
}

// Load rehidrata el agregado por replay completo desde es.events.
func (r *Repository) Load(ctx context.Context, id uuid.UUID) (*domain.Prescription, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrPrescriptionNotFound
	}
	rx := domain.NewPrescription()
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("prescription load %s: %w", id, err)
		}
		rx.Apply(decoded)
	}
	rx.MarkCommitted()
	return rx, nil
}

// Save persiste los eventos pendientes en el Event Store dentro de la tx del UoW.
func (r *Repository) Save(ctx context.Context, rx *domain.Prescription, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := rx.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := rx.Version - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("prescription save: %T no es eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID:            kernel.NewID(),
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
			Payload:       ev,
			Metadata:      meta,
			OccurredAt:    r.clock(),
		}
	}
	stored, err := r.store.Append(ctx, tx, rx.ID, domain.StreamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("prescription append: %w", err)
	}
	rx.MarkCommitted()
	return stored, nil
}

// ContentHash calcula SHA-256 de los eventos previos hasta upToVersion para la firma digital (PP-F4-ARQ-011).
func (r *Repository) ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `
		SELECT event_id, event_type, schema_version, payload
		  FROM es.events
		 WHERE stream_id = $1 AND stream_version <= $2
		 ORDER BY stream_version`, id, upToVersion)
	if err != nil {
		return "", fmt.Errorf("prescription content hash: %w", err)
	}
	defer rows.Close()

	var events []domain.HashableEvent
	for rows.Next() {
		var e domain.HashableEvent
		if err := rows.Scan(&e.EventID, &e.EventType, &e.SchemaVersion, &e.Payload); err != nil {
			return "", fmt.Errorf("scan hashable event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return domain.ContentHash(events)
}
