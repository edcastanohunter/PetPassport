// Package infra implementa los puertos de Scheduling sobre PostgreSQL y el
// Event Store: repositorios (con snapshots de la agenda), reservas con
// restricción de exclusión, proyecciones y consultas.
package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// agendaNamespace deriva el stream_id de la agenda (agenda-<Org>-<User>, DOM-011).
var agendaNamespace = uuid.MustParse("6f1d3c52-8a47-4e0b-b1c9-2d7e5a9f4c10")

// AgendaStreamID devuelve el stream determinista de la agenda de un profesional.
func AgendaStreamID(orgID, professionalID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(agendaNamespace, []byte("agenda-"+orgID.String()+"-"+professionalID.String()))
}

// El agregado de agenda implementa el contrato de snapshots (ARQ-004).
var _ eventstore.Snapshotter = (*domain.ProfessionalAgenda)(nil)

// RegisterEvents registra los eventos Scheduling v1 en el registro (arranque).
func RegisterEvents(reg *eventstore.Registry) {
	reg.Register(func() eventstore.Event { return domain.AppointmentRequested{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentCreated{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentConfirmed{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentAlternativeProposed{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentRejected{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentRescheduled{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentCancelled{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentCheckedIn{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentNoShow{} })
	reg.Register(func() eventstore.Event { return domain.AppointmentCompleted{} })
	reg.Register(func() eventstore.Event { return domain.AgendaDefined{} })
	reg.Register(func() eventstore.Event { return domain.AgendaWeeklyTemplateUpdated{} })
	reg.Register(func() eventstore.Event { return domain.AgendaSlotDurationsSet{} })
	reg.Register(func() eventstore.Event { return domain.AgendaExceptionAdded{} })
	reg.Register(func() eventstore.Event { return domain.AgendaExceptionRemoved{} })
	reg.Register(func() eventstore.Event { return domain.AgendaPoliciesSet{} })
	reg.Register(func() eventstore.Event { return domain.AgendaDeactivated{} })
	reg.Register(func() eventstore.Event { return domain.AgendaActivated{} })
}

type eventSource interface {
	UncommittedEvents() []any
	Version() int64
	MarkCommitted()
}

// base comparte la lógica de Append entre los dos agregados.
type base struct {
	store *eventstore.Store
	reg   *eventstore.Registry
	ids   kernel.IDGenerator
	clock kernel.Clock
}

func newBase(store *eventstore.Store, reg *eventstore.Registry, ids kernel.IDGenerator, clock kernel.Clock) base {
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return base{store: store, reg: reg, ids: ids, clock: clock}
}

func (b base) append(ctx context.Context, agg eventSource, streamID uuid.UUID, streamType string, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := agg.UncommittedEvents()
	if len(pending) == 0 {
		return nil, nil
	}
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	expected := agg.Version() - int64(len(pending))
	newEvents := make([]eventstore.NewEvent, len(pending))
	for i, e := range pending {
		ev, ok := e.(eventstore.Event)
		if !ok {
			return nil, fmt.Errorf("scheduling save: evento %T no implementa eventstore.Event", e)
		}
		newEvents[i] = eventstore.NewEvent{
			ID: b.ids.NewID(), Type: ev.EventName(), SchemaVersion: ev.SchemaVersion(),
			Payload: ev, Metadata: meta, OccurredAt: b.clock.Now(),
		}
	}
	stored, err := b.store.Append(ctx, tx, streamID, streamType, expected, newEvents)
	if err != nil {
		var ce *kernel.ConcurrencyError
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, fmt.Errorf("scheduling append: %w", err)
	}
	agg.MarkCommitted()
	return stored, nil
}

// AppointmentRepository carga y persiste Appointment (stream = appointment id).
type AppointmentRepository struct{ base }

var _ app.AppointmentRepository = (*AppointmentRepository)(nil)

// NewAppointmentRepository construye el repositorio.
func NewAppointmentRepository(store *eventstore.Store, reg *eventstore.Registry, ids kernel.IDGenerator, clock kernel.Clock) *AppointmentRepository {
	return &AppointmentRepository{newBase(store, reg, ids, clock)}
}

// Load rehidrata por replay. Stream inexistente → cita vacía (versión 0).
func (r *AppointmentRepository) Load(ctx context.Context, id uuid.UUID) (*domain.Appointment, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, id, 0)
	if err != nil {
		return nil, err
	}
	a := &domain.Appointment{}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("scheduling appointment load %s: %w", id, err)
		}
		if err := a.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Save hace Append de los eventos no confirmados con la versión esperada.
func (r *AppointmentRepository) Save(ctx context.Context, a *domain.Appointment, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	return r.append(ctx, a, a.ID(), domain.AppointmentStreamType, meta)
}

// AgendaRepository carga y persiste ProfessionalAgenda con snapshots
// (PP-F2-ARQ-004): Load = snapshot compatible + eventos posteriores; Save
// escribe un snapshot cuando la versión cruza un múltiplo del umbral.
type AgendaRepository struct {
	base
	snaps eventstore.SnapshotStore
}

var _ app.AgendaRepository = (*AgendaRepository)(nil)

// NewAgendaRepository construye el repositorio.
func NewAgendaRepository(store *eventstore.Store, reg *eventstore.Registry, ids kernel.IDGenerator, clock kernel.Clock) *AgendaRepository {
	return &AgendaRepository{base: newBase(store, reg, ids, clock)}
}

// Load rehidrata la agenda. Un snapshot ilegible o de otro schema se ignora y
// se rehidrata desde la versión 0: los snapshots nunca son fuente de verdad.
func (r *AgendaRepository) Load(ctx context.Context, orgID, professionalID uuid.UUID) (*domain.ProfessionalAgenda, error) {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return nil, err
	}
	stream := AgendaStreamID(orgID, professionalID)
	a := &domain.ProfessionalAgenda{}
	from := int64(0)
	if snap, err := r.snaps.Get(ctx, tx, stream); err == nil {
		if rerr := a.RestoreSnapshot(snap.State, snap.SchemaVersion); rerr == nil {
			a.SetVersion(snap.StreamVersion)
			from = snap.StreamVersion
		} else {
			a = &domain.ProfessionalAgenda{} // descartar estado parcial
		}
	} else if !errors.Is(err, eventstore.ErrNoSnapshot) {
		return nil, err
	}
	events, err := r.store.Load(ctx, tx, stream, from)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		decoded, err := r.reg.Decode(e)
		if err != nil {
			return nil, fmt.Errorf("scheduling agenda load %s: %w", stream, err)
		}
		if err := a.ApplyEvent(decoded); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Save hace Append y, si la versión cruzó un múltiplo del umbral, guarda el
// snapshot en la misma transacción (es derivable: borrarlo no pierde nada).
func (r *AgendaRepository) Save(ctx context.Context, a *domain.ProfessionalAgenda, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	before := a.Version() - int64(len(a.UncommittedEvents()))
	stored, err := r.append(ctx, a, AgendaStreamID(a.Organization(), a.Professional()), domain.AgendaStreamType, meta)
	if err != nil || len(stored) == 0 {
		return stored, err
	}
	if a.Version()/domain.SnapshotThreshold > before/domain.SnapshotThreshold {
		if err := r.writeSnapshot(ctx, a); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func (r *AgendaRepository) writeSnapshot(ctx context.Context, a *domain.ProfessionalAgenda) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	state, schema := a.SnapshotState()
	raw, err := jsonMarshal(state)
	if err != nil {
		return err
	}
	return r.snaps.Save(ctx, tx, eventstore.Snapshot{
		StreamID: AgendaStreamID(a.Organization(), a.Professional()), StreamVersion: a.Version(),
		SchemaVersion: schema, State: raw,
	})
}
