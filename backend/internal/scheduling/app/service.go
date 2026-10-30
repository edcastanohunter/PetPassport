package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Options son las dependencias del servicio de Scheduling.
type Options struct {
	Appointments AppointmentRepository
	Agendas      AgendaRepository
	AgendaReader AgendaReader
	Queries      AppointmentQueries
	Reservations Reservations
	UoW          TxRunner
	Projects     ProjectionRunner
	Pets         PetAccess
	Membership   Membership
	Clock        kernel.Clock
	Logger       *slog.Logger
}

// Service ejecuta los casos de uso de Scheduling (DOM-010/011).
type Service struct {
	appts        AppointmentRepository
	agendas      AgendaRepository
	agendaReader AgendaReader
	queries      AppointmentQueries
	reservations Reservations
	uow          TxRunner
	projects     ProjectionRunner
	pets         PetAccess
	membership   Membership
	clock        kernel.Clock
	logger       *slog.Logger
	bus          *bus.Bus
}

// NewService construye el servicio.
func NewService(o Options) *Service {
	if o.Clock == nil {
		o.Clock = kernel.RealClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{
		appts: o.Appointments, agendas: o.Agendas, agendaReader: o.AgendaReader, queries: o.Queries,
		reservations: o.Reservations, uow: o.UoW, projects: o.Projects, pets: o.Pets,
		membership: o.Membership, clock: o.Clock, logger: o.Logger,
	}
}

// RegisterCommands registra los handlers en el bus (api y worker).
func (s *Service) RegisterCommands(b *bus.Bus) {
	if b == nil {
		panic("scheduling: bus nil")
	}
	s.bus = b
	b.Register(RequestAppointment{}, s.handleRequest)
	b.Register(CreateAppointment{}, s.handleCreate)
	b.Register(ConfirmAppointment{}, s.handleConfirm)
	b.Register(ProposeAlternative{}, s.handlePropose)
	b.Register(AcceptProposal{}, s.handleAccept)
	b.Register(RejectRequest{}, s.handleReject)
	b.Register(RescheduleAppointment{}, s.handleReschedule)
	b.Register(CancelAppointment{}, s.handleCancel)
	b.Register(CheckInAppointment{}, s.handleCheckIn)
	b.Register(MarkAppointmentNoShow{}, s.handleNoShow)
	b.Register(CompleteAppointment{}, s.handleComplete)
	b.Register(ExpireProposal{}, s.handleExpire)
	b.Register(AutoNoShow{}, s.handleAutoNoShow)
	b.Register(CompleteOnSign{}, s.handleCompleteOnSign)
	b.Register(SaveAgenda{}, s.handleSaveAgenda)
	b.Register(AddAgendaException{}, s.handleAddException)
	b.Register(RemoveAgendaException{}, s.handleRemoveException)
}

// Dispatch ejecuta un comando por el bus (fachada para http).
func (s *Service) Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	if s.bus == nil {
		return bus.Result{}, errors.New("scheduling: comandos no registrados")
	}
	return s.bus.Dispatch(ctx, cmd)
}

// ------------------------------------------------------------- apoyo

// actor devuelve el usuario que ejecuta el comando (uuid.Nil = sistema).
func actor(ctx context.Context) uuid.UUID { return CommandContextFrom(ctx).Principal.UserID }

func (s *Service) metadata(ctx context.Context, orgID uuid.UUID) eventstore.Metadata {
	cc := CommandContextFrom(ctx)
	meta := eventstore.Metadata{
		ActorID: cc.Principal.UserID.String(), ActorRoles: nil, CorrelationID: cc.CorrelationID,
		CausationID: cc.CausationID, Source: eventstore.SourceAPI, IPHash: cc.IPHash,
		UserAgentHash: cc.UserAgentHash, IdempotencyKey: cc.IdempotencyKey,
	}
	if cc.Principal.UserID == uuid.Nil {
		meta.ActorID, meta.Source = eventstore.SourceSystem, eventstore.SourceSystem
		if o, ok := bus.SystemOriginFrom(ctx); ok {
			meta.CorrelationID, meta.CausationID = o.CorrelationID, o.CausationID
		}
	}
	if orgID != uuid.Nil {
		meta.ActorOrgID = orgID.String()
	}
	return meta
}

func versionOf(stored []eventstore.StoredEvent) int64 {
	if len(stored) == 0 {
		return 0
	}
	return stored[len(stored)-1].StreamVersion
}

// requireStaff comprueba que el actor es miembro activo de clínica de la
// organización (404 si no: no se revela la existencia de la cita, ARQ-010).
func (s *Service) requireStaff(ctx context.Context, orgID uuid.UUID) error {
	role, active, err := s.membership.RoleOf(ctx, actor(ctx), orgID)
	if err != nil {
		return err
	}
	switch role {
	case "clinic_admin", "veterinarian", "receptionist":
	default:
		active = false
	}
	if !active {
		return &kernel.ForbiddenError{Reason: "sin membresía clínica activa en la organización"}
	}
	return nil
}

// professional resuelve A4 y A5 para un profesional: veterinario activo de la
// organización con agenda activa. Devuelve la agenda para tz y política.
func (s *Service) professional(ctx context.Context, orgID, profID uuid.UUID) (*domain.ProfessionalAgenda, bool, error) {
	role, active, err := s.membership.RoleOf(ctx, profID, orgID)
	if err != nil {
		return nil, false, err
	}
	if !active || role != "veterinarian" {
		return nil, false, nil
	}
	ag, err := s.agendaReader.Agenda(ctx, orgID, profID)
	var nf *kernel.NotFoundError
	if errors.As(err, &nf) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return ag, true, nil
}

// reserve aplica A5: el profesional debe tener agenda activa que cubra la
// franja y la reserva no puede solaparse (restricción de exclusión).
func (s *Service) reserve(ctx context.Context, ag *domain.ProfessionalAgenda, apptID, profID uuid.UUID, slot domain.Slot, expires *time.Time) error {
	if ag == nil || !ag.Covers(slot) {
		return domain.ErrSlotUnavailable
	}
	return s.reservations.Reserve(ctx, Reservation{
		AppointmentID: apptID, ProfessionalID: profID, Slot: slot, Provisional: expires != nil, ExpiresAt: expires,
	})
}

// execOpts controla cómo exec trata el stream: create exige que NO exista;
// orgID != nil exige que la cita pertenezca a esa organización (404 si no).
type execOpts struct {
	create bool
	orgID  *uuid.UUID
}

// exec carga la cita, ejecuta el comando de dominio, aplica los efectos sobre
// la reserva (misma tx, antes del Append) y persiste + proyecta.
func (s *Service) exec(ctx context.Context, id uuid.UUID, o execOpts,
	build func(ctx context.Context, a *domain.Appointment) (any, error),
	effect func(ctx context.Context, a *domain.Appointment, evs []any) error) (bus.Result, error) {
	var res bus.Result
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		a, err := s.appts.Load(ctx, id)
		if err != nil {
			return err
		}
		if o.create != (a.Version() == 0) {
			return &kernel.NotFoundError{Resource: "appointment"}
		}
		if o.orgID != nil && a.OrganizationID() != *o.orgID {
			return &kernel.NotFoundError{Resource: "appointment"}
		}
		cc := CommandContextFrom(ctx)
		if cc.ExpectedVersion > 0 && a.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: a.Version()}
		}
		cmd, err := build(ctx, a)
		if err != nil {
			return err
		}
		evs, err := a.HandleCommand(cmd)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			res = bus.Result{AggregateID: id, Version: a.Version()}
			return nil
		}
		if effect != nil {
			if err := effect(ctx, a, evs); err != nil {
				return err
			}
		}
		stored, err := s.appts.Save(ctx, a, s.metadata(ctx, a.OrganizationID()))
		if err != nil {
			return err
		}
		res = bus.Result{AggregateID: id, Version: versionOf(stored)}
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return res, nil
}

// release libera la reserva de la cita (terminal o cancelada).
func (s *Service) release(ctx context.Context, a *domain.Appointment, _ []any) error {
	return s.reservations.Release(ctx, a.ID())
}
