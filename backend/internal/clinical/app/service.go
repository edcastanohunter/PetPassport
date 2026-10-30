package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CommandContext viaja en el contexto desde http hasta los handlers del bus
// (DOM-012). ExpectedVersion transporta el If-Match: si no coincide con la
// versión del agregado tras Load → *kernel.ConcurrencyError (409).
type CommandContext struct {
	Principal       PrincipalInfo
	CorrelationID   string
	CausationID     string
	Source          string
	IPHash          string
	UserAgentHash   string
	IdempotencyKey  string
	ExpectedVersion int64
}

// PrincipalInfo desacopla el handler de platform/auth (solo lo necesario).
type PrincipalInfo struct {
	UserID     uuid.UUID
	Email      string
	Roles      []string
	SuperAdmin bool
}

type cmdCtxKey struct{}

// WithCommandContext adjunta el contexto del comando.
func WithCommandContext(ctx context.Context, cc CommandContext) context.Context {
	return context.WithValue(ctx, cmdCtxKey{}, cc)
}

// CommandContextFrom devuelve el contexto del comando o uno vacío.
func CommandContextFrom(ctx context.Context) CommandContext {
	cc, _ := ctx.Value(cmdCtxKey{}).(CommandContext)
	return cc
}

// CommandContextOf es el alias usado por http.
func CommandContextOf(ctx context.Context) CommandContext { return CommandContextFrom(ctx) }

// Options son las dependencias del servicio.
type Options struct {
	Repo         Repository
	UoW          TxRunner
	Projects     ProjectionRunner
	PetAccess    PetAccess
	Membership   Membership
	Appointments AppointmentAccess
	Queries      EncounterQueries
	IDs          kernel.IDGenerator
	Clock        kernel.Clock
	Logger       *slog.Logger
}

// Service ejecuta los casos de uso de Clinical Records.
type Service struct {
	repo         Repository
	uow          TxRunner
	projects     ProjectionRunner
	petAccess    PetAccess
	membership   Membership
	appointments AppointmentAccess
	queries      EncounterQueries
	ids          kernel.IDGenerator
	clock        kernel.Clock
	logger       *slog.Logger
	bus          *bus.Bus
}

// NewService construye el servicio. Los puertos opcionales (queries, citas)
// pueden ser nil en pruebas de dominio.
func NewService(opts Options) *Service {
	if opts.IDs == nil {
		opts.IDs = kernel.RealIDGenerator{}
	}
	if opts.Clock == nil {
		opts.Clock = kernel.RealClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{
		repo: opts.Repo, uow: opts.UoW, projects: opts.Projects,
		petAccess: opts.PetAccess, membership: opts.Membership, appointments: opts.Appointments,
		queries: opts.Queries, ids: opts.IDs, clock: opts.Clock, logger: opts.Logger,
	}
}

// Queries devuelve el lado de lectura (fachada para http).
func (s *Service) Queries() EncounterQueries { return s.queries }

// RegisterCommands registra los handlers en el bus (arranque).
func (s *Service) RegisterCommands(b *bus.Bus) {
	if b == nil {
		panic("clinical: bus nil")
	}
	s.bus = b
	b.Register(domain.OpenEncounter{}, s.handleOpen)
	b.Register(domain.UpdateEncounterDetails{}, s.handleUpdateDetails)
	b.Register(domain.RecordVitals{}, s.handleVitals)
	b.Register(domain.UpdateNarrative{}, s.handleNarrative)
	b.Register(domain.SetDiagnoses{}, s.handleDiagnoses)
	b.Register(domain.SetPlan{}, s.handlePlan)
	b.Register(domain.RecommendFollowUp{}, s.handleFollowUp)
	b.Register(domain.ClearFollowUp{}, s.handleClearFollowUp)
	b.Register(domain.AssignAttendingVet{}, s.handleAssignVet)
	b.Register(domain.AddAttachment{}, s.handleAddAttachment)
	b.Register(domain.RemoveAttachment{}, s.handleRemoveAttachment)
	b.Register(domain.SetTutorVisibility{}, s.handleVisibility)
	b.Register(domain.SignEncounter{}, s.handleSign)
	b.Register(domain.AddAddendum{}, s.handleAddendum)
	b.Register(domain.DiscardEncounter{}, s.handleDiscard)
}

// Dispatch ejecuta un comando por el bus (fachada para http).
func (s *Service) Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	if s.bus == nil {
		return bus.Result{}, errors.New("clinical: comandos no registrados")
	}
	return s.bus.Dispatch(ctx, cmd)
}

// metadata construye los metadatos del evento (DOM-012), incluido el org del
// actor (SEC-001: el rol es el de la organización del encuentro).
func (s *Service) metadata(ctx context.Context, orgID uuid.UUID) eventstore.Metadata {
	cc := CommandContextFrom(ctx)
	source := cc.Source
	if source == "" {
		source = eventstore.SourceAPI
	}
	actor := cc.Principal.UserID.String()
	if cc.Principal.UserID == uuid.Nil {
		actor = eventstore.SourceSystem
	}
	meta := eventstore.Metadata{
		ActorID: actor, ActorRoles: cc.Principal.Roles,
		CorrelationID: cc.CorrelationID, CausationID: cc.CausationID, Source: source,
		IPHash: cc.IPHash, UserAgentHash: cc.UserAgentHash, IdempotencyKey: cc.IdempotencyKey,
	}
	if orgID != uuid.Nil {
		meta.ActorOrgID = orgID.String()
	}
	return meta
}

// actorFor resuelve el rol del principal en la organización del encuentro.
// Sin membresía activa → ForbiddenError (authz ya lo habría denegado; es una
// segunda barrera, SEC-001).
func (s *Service) actorFor(ctx context.Context, orgID uuid.UUID) (domain.Actor, error) {
	cc := CommandContextFrom(ctx)
	actor := domain.Actor{UserID: cc.Principal.UserID, SuperAdmin: cc.Principal.SuperAdmin}
	if orgID == uuid.Nil {
		return actor, nil
	}
	if s.membership == nil {
		return actor, &kernel.ForbiddenError{Reason: "sin puerto de membresías"}
	}
	role, active, err := s.membership.RoleOf(ctx, cc.Principal.UserID, orgID)
	if err != nil {
		return actor, err
	}
	if !active && !actor.SuperAdmin {
		return actor, &kernel.ForbiddenError{Reason: "sin membresía activa en la organización"}
	}
	actor.OrgRole = string(role)
	return actor, nil
}

// run carga el agregado, resuelve el actor, construye y ejecuta el comando, y
// persiste + proyecta. build recibe el contexto de la transacción (para leer
// es.events en la misma tx), el agregado cargado y el actor resuelto.
func (s *Service) run(ctx context.Context, id uuid.UUID, build func(ctx context.Context, enc *domain.Encounter, actor domain.Actor) (any, error)) (bus.Result, error) {
	var res bus.Result
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		enc, err := s.repo.Load(ctx, id)
		if err != nil {
			return err
		}
		cc := CommandContextFrom(ctx)
		if cc.ExpectedVersion > 0 && enc.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: enc.Version()}
		}
		actor, err := s.actorFor(ctx, enc.OrganizationID())
		if err != nil {
			return err
		}
		cmd, err := build(ctx, enc, actor)
		if err != nil {
			return err
		}
		events, err := enc.HandleCommand(cmd)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			res = bus.Result{AggregateID: id, Version: enc.Version()}
			return nil
		}
		stored, err := s.repo.Save(ctx, enc, s.metadata(ctx, enc.OrganizationID()))
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

func versionOf(events []eventstore.StoredEvent) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].StreamVersion
}

// ------------------------------------------------------------------- open

func (s *Service) handleOpen(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.OpenEncounter)
	if c.EncounterID == uuid.Nil {
		c.EncounterID = s.ids.NewID()
	}
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	if c.PetID == uuid.Nil || c.OrganizationID == uuid.Nil {
		return bus.Result{}, &kernel.ValidationError{Fields: []kernel.FieldViolation{
			{Field: "pet_id", Code: "INVALID"},
		}}
	}
	// SEC-001: solo miembros de una clínica activa abren consultas.
	actor, err := s.actorFor(ctx, c.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	switch actor.OrgRole {
	case domain.RoleVeterinarian, domain.RoleClinicAdmin, domain.RoleReceptionist:
	default:
		if !actor.SuperAdmin {
			return bus.Result{}, &kernel.ForbiddenError{Reason: "rol no clínico"}
		}
	}
	c.Actor = actor
	if s.petAccess == nil {
		return bus.Result{}, errors.New("clinical: PetAccess no configurado")
	}
	level, err := s.petAccess.CanOrgAccess(ctx, c.PetID, c.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.PetLinked = level == AccessLinked
	if c.AppointmentID != nil {
		if s.appointments == nil {
			return bus.Result{}, &kernel.NotFoundError{Resource: "appointment"}
		}
		ok, err := s.appointments.CheckedIn(ctx, *c.AppointmentID, c.PetID)
		if err != nil {
			var nf ErrAppointmentNotFound
			if errors.As(err, &nf) {
				return bus.Result{}, domain.ErrAppointmentNotCheckedIn
			}
			return bus.Result{}, err
		}
		c.AppointmentCheckedIn = ok
	}
	if c.AttendingVetID != nil {
		if !actor.SuperAdmin {
			role, active, err := s.membership.RoleOf(ctx, *c.AttendingVetID, c.OrganizationID)
			if err != nil {
				return bus.Result{}, err
			}
			c.AttendingVetEligible = active && role == OrgRole(domain.RoleVeterinarian)
		} else {
			c.AttendingVetEligible = true
		}
	}
	var res bus.Result
	err = s.uow.Do(ctx, func(ctx context.Context) error {
		enc, err := s.repo.Load(ctx, c.EncounterID)
		if err != nil {
			return err
		}
		if enc.Version() != 0 {
			return domain.ErrEncounterAlreadyOpened
		}
		if _, err := enc.HandleCommand(c); err != nil {
			return err
		}
		stored, err := s.repo.Save(ctx, enc, s.metadata(ctx, c.OrganizationID))
		if err != nil {
			return err
		}
		res = bus.Result{AggregateID: c.EncounterID, Version: versionOf(stored)}
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return res, nil
}

// ---------------------------------------------------------------- handlers

func (s *Service) handleUpdateDetails(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UpdateEncounterDetails)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleVitals(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RecordVitals)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleNarrative(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UpdateNarrative)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleDiagnoses(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.SetDiagnoses)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handlePlan(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.SetPlan)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleFollowUp(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RecommendFollowUp)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleClearFollowUp(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.ClearFollowUp)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleAssignVet(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AssignAttendingVet)
	return s.run(ctx, c.EncounterID, func(ctx context.Context, enc *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		if c.UserID != uuid.Nil && s.membership != nil && !a.SuperAdmin {
			role, active, err := s.membership.RoleOf(ctx, c.UserID, enc.OrganizationID())
			if err != nil {
				return nil, err
			}
			c.Eligible = active && role == OrgRole(domain.RoleVeterinarian)
		} else {
			c.Eligible = true
		}
		return c, nil
	})
}

func (s *Service) handleAddAttachment(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AddAttachment)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleRemoveAttachment(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RemoveAttachment)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleVisibility(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.SetTutorVisibility)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleSign(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.SignEncounter)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.run(ctx, c.EncounterID, func(ctx context.Context, enc *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		// E8: el hash se calcula en el repositorio sobre los eventos ya
		// persistidos hasta la versión actual (la firma no entra en él).
		hash, err := s.repo.ContentHash(ctx, c.EncounterID, enc.Version())
		if err != nil {
			return nil, err
		}
		c.StreamVersion = enc.Version()
		c.ContentHash = hash
		return c, nil
	})
}

func (s *Service) handleAddendum(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AddAddendum)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

func (s *Service) handleDiscard(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.DiscardEncounter)
	return s.run(ctx, c.EncounterID, func(_ context.Context, _ *domain.Encounter, a domain.Actor) (any, error) {
		c.Actor = a
		return c, nil
	})
}

// ------------------------------------------------------------------ read

// Get devuelve la vista completa de un encuentro (HTTP filtra visibilidad).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (EncounterView, error) {
	if s.queries == nil {
		return EncounterView{}, errors.New("clinical: consultas no configuradas")
	}
	return s.queries.Get(ctx, id)
}

// Access devuelve la vista de autorización (404 si no existe).
func (s *Service) Access(ctx context.Context, id uuid.UUID) (EncounterAccess, error) {
	if s.queries == nil {
		return EncounterAccess{}, errors.New("clinical: consultas no configuradas")
	}
	return s.queries.Access(ctx, id)
}

// VerifyEncounter recomputa el hash desde el event store (API-013). No
// devuelve datos clínicos ni PII.
func (s *Service) VerifyEncounter(ctx context.Context, id uuid.UUID, hash string) (VerifyView, error) {
	var out VerifyView
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		enc, err := s.repo.Load(ctx, id)
		if err != nil {
			return err
		}
		sig := enc.Signature()
		if enc.Version() == 0 || sig == nil {
			return &kernel.NotFoundError{Resource: "encounter"}
		}
		computed, err := s.repo.ContentHash(ctx, id, sig.StreamVersion)
		if err != nil {
			return err
		}
		out = VerifyView{
			Valid:    computed == sig.ContentHash && (hash == "" || strings.EqualFold(hash, sig.ContentHash)),
			SignedAt: sig.SignedAt, StreamVersion: sig.StreamVersion,
			ContentHash: sig.ContentHash, Organization: enc.OrganizationID(), PetID: enc.PetID(),
		}
		return nil
	})
	if err != nil {
		return VerifyView{}, err
	}
	if s.membership != nil {
		name, err := s.membership.OrganizationName(ctx, out.Organization)
		if err != nil {
			return VerifyView{}, err
		}
		out.OrganizationName = name
	}
	return out, nil
}
