package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CommandContext viaja en el contexto desde http hasta los handlers del bus:
// principal, correlación y metadatos de evento (DOM-012). ExpectedVersion
// transporta el If-Match del request: si no coincide con la versión del
// agregado tras Load → *kernel.ConcurrencyError (409 CONFLICT_VERSION).
type CommandContext struct {
	Principal       PrincipalInfo
	CorrelationID   string
	CausationID     string
	Source          string
	IPHash          string
	UserAgentHash   string
	IdempotencyKey  string
	ExpectedVersion int64 // 0 = sin If-Match
}

// PrincipalInfo desacopla el handler de platform/auth (solo lo necesario).
// Exportado para que http construya el CommandContext.
type PrincipalInfo struct {
	UserID     uuid.UUID
	Email      string
	Roles      []string
	SuperAdmin bool
}

type cmdCtxKey struct{}

// WithCommandContext adjunta el contexto de comando.
func WithCommandContext(ctx context.Context, cc CommandContext) context.Context {
	return context.WithValue(ctx, cmdCtxKey{}, cc)
}

func commandContext(ctx context.Context) CommandContext {
	if cc, ok := ctx.Value(cmdCtxKey{}).(CommandContext); ok {
		return cc
	}
	return CommandContext{}
}

// Service registra los handlers de Pet Registry en el bus y expone la fachada
// usada por http. Un comando, un agregado, una transacción (ARQ-003).
type Service struct {
	repo     Repository
	uow      TxRunner
	projects ProjectionRunner
	reserve  MicrochipReservations
	guardian GuardianLookup
	orgs     OrganizationLookup
	queries  PetQueries
	events   *eventstore.Store
	mailer   Mailer
	baseURL  string
	ids      kernel.IDGenerator
	tokens   kernel.TokenGenerator
	clock    kernel.Clock
	logger   *slog.Logger
	bus      *bus.Bus
}

// Options construye el servicio. Mailer es opcional (DOM-011, best-effort).
type Options struct {
	Repo     Repository
	UoW      TxRunner
	Projects ProjectionRunner
	Reserve  MicrochipReservations
	Guardian GuardianLookup
	Orgs     OrganizationLookup
	Queries  PetQueries
	Events   *eventstore.Store
	Mailer   Mailer
	BaseURL  string
	IDs      kernel.IDGenerator
	Tokens   kernel.TokenGenerator
	Clock    kernel.Clock
	Logger   *slog.Logger
}

// NewService construye el servicio y registra los comandos en el bus.
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
	if opts.Tokens == nil {
		opts.Tokens = kernel.RealTokenGenerator{}
	}
	s := &Service{
		repo: opts.Repo, uow: opts.UoW, projects: opts.Projects, reserve: opts.Reserve,
		guardian: opts.Guardian, orgs: opts.Orgs, queries: opts.Queries, events: opts.Events,
		mailer: opts.Mailer, baseURL: strings.TrimRight(opts.BaseURL, "/"),
		ids: opts.IDs, tokens: opts.Tokens, clock: opts.Clock, logger: opts.Logger,
	}
	return s
}

// RegisterCommands registra los handlers en el bus (arranque) y lo guarda
// para Dispatch. Pánico si el bus es nil.
func (s *Service) RegisterCommands(b *bus.Bus) {
	if b == nil {
		panic("petregistry: bus nil")
	}
	s.bus = b
	b.Register(domain.RegisterPet{}, s.handleRegisterPet)
	b.Register(domain.UpdatePetProfile{}, s.handleUpdatePetProfile)
	b.Register(domain.AssignMicrochip{}, s.handleAssignMicrochip)
	b.Register(domain.ChangePetPhoto{}, s.handleChangePetPhoto)
	b.Register(domain.MarkPetDeceased{}, s.handleMarkPetDeceased)
	b.Register(domain.UndoDeceased{}, s.handleUndoDeceased)
	b.Register(domain.GrantCareLink{}, s.handleGrantCareLink)
	b.Register(domain.RevokeCareLink{}, s.handleRevokeCareLink)
	b.Register(domain.InitiateGuardianshipTransfer{}, s.handleInitiateTransfer)
	b.Register(domain.CancelGuardianshipTransfer{}, s.handleCancelTransfer)
	b.Register(domain.AcceptGuardianshipTransfer{}, s.handleAcceptTransfer)
}

// Dispatch ejecuta un comando por el bus (fachada para http).
func (s *Service) Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	if s.bus == nil {
		return bus.Result{}, errors.New("petregistry: comandos no registrados")
	}
	return s.bus.Dispatch(ctx, cmd)
}

// metadata construye los metadatos del evento (DOM-012).
func (s *Service) metadata(ctx context.Context) eventstore.Metadata {
	cc := commandContext(ctx)
	source := cc.Source
	if source == "" {
		source = eventstore.SourceAPI
	}
	actor := cc.Principal.UserID.String()
	if actor == uuid.Nil.String() {
		actor = eventstore.SourceSystem
	}
	return eventstore.Metadata{
		ActorID:        actor,
		ActorRoles:     cc.Principal.Roles,
		CorrelationID:  cc.CorrelationID,
		CausationID:    cc.CausationID,
		Source:         source,
		IPHash:         cc.IPHash,
		UserAgentHash:  cc.UserAgentHash,
		IdempotencyKey: cc.IdempotencyKey,
	}
}

// execute corre el flujo transaccional de un comando sobre el agregado:
// load → handle → append → project(sync) → outbox (dentro de repo.Save).
func (s *Service) execute(ctx context.Context, petID uuid.UUID, fn func(p *domain.Pet) ([]any, error)) ([]eventstore.StoredEvent, error) {
	var stored []eventstore.StoredEvent
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		pet, err := s.repo.Load(ctx, petID)
		if err != nil {
			return err
		}
		if cc := commandContext(ctx); cc.ExpectedVersion > 0 && pet.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: pet.Version()}
		}
		events, err := fn(pet)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			stored = nil
			return nil
		}
		stored, err = s.repo.Save(ctx, pet, s.metadata(ctx))
		if err != nil {
			return err
		}
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// ------------------------------------------------------------------ handlers

func (s *Service) handleRegisterPet(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RegisterPet)
	if c.PetID == uuid.Nil {
		c.PetID = s.ids.NewID()
	}
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	stored, err := s.execute(ctx, uuid.Nil, func(pet *domain.Pet) ([]any, error) {
		return pet.HandleCommand(c)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return bus.Result{AggregateID: c.PetID, Version: versionOf(stored)}, nil
}

func (s *Service) handleUpdatePetProfile(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UpdatePetProfile)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleChangePetPhoto(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.ChangePetPhoto)
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleMarkPetDeceased(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.MarkPetDeceased)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleUndoDeceased(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UndoDeceased)
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleGrantCareLink(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.GrantCareLink)
	// I5: solo organizaciones clínica + activa (puerto OrganizationLookup).
	if s.orgs == nil {
		return bus.Result{}, errors.New("petregistry: OrganizationLookup no configurado")
	}
	if _, err := s.orgs.ActiveClinic(ctx, c.OrganizationID); err != nil {
		if errors.Is(err, ErrOrganizationNotEligible) {
			return bus.Result{}, domain.ErrOrganizationNotEligible
		}
		return bus.Result{}, err
	}
	// Reintento único ante ErrConcurrency: GrantCareLink es idempotente por
	// naturaleza (ARQ-003).
	var res bus.Result
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		stored, err := s.execute(ctx, c.PetID, func(pet *domain.Pet) ([]any, error) {
			return pet.HandleCommand(c)
		})
		if err == nil {
			return bus.Result{AggregateID: c.PetID, Version: versionOf(stored)}, nil
		}
		lastErr = err
		var ce *kernel.ConcurrencyError
		if !errors.As(err, &ce) {
			break
		}
	}
	return res, lastErr
}

func (s *Service) handleRevokeCareLink(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RevokeCareLink)
	return s.simple(ctx, c.PetID, c)
}

// AssignMicrochip reserva el chip en la misma tx ANTES del Append (DAT-005):
// INSERT ... ON CONFLICT DO NOTHING; si el chip es de otra mascota →
// MICROCHIP_ALREADY_ASSIGNED sin revelar cuál (PRD-007). Si el agregado tenía
// otro chip, se libera su reserva.
func (s *Service) handleAssignMicrochip(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AssignMicrochip)
	chip, err := domain.NewMicrochip(c.Microchip)
	if err != nil {
		return bus.Result{}, err
	}
	var newVersion int64
	err = s.uow.Do(ctx, func(ctx context.Context) error {
		pet, err := s.repo.Load(ctx, c.PetID)
		if err != nil {
			return err
		}
		if cc := commandContext(ctx); cc.ExpectedVersion > 0 && pet.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: pet.Version()}
		}
		previous := ""
		if pet != nil && petHasMicrochip(pet) {
			previous = petMicrochip(pet)
		}
		if err := s.reserve.Reserve(ctx, string(chip), c.PetID); err != nil {
			return err
		}
		events, err := pet.HandleCommand(c)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			newVersion = pet.Version()
			return nil // idempotente: ya lo tenía
		}
		if previous != "" && previous != string(chip) {
			if err := s.reserve.Release(ctx, previous, c.PetID); err != nil {
				return err
			}
		}
		stored, err := s.repo.Save(ctx, pet, s.metadata(ctx))
		if err != nil {
			return err
		}
		newVersion = versionOf(stored)
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		if errors.Is(err, ErrMicrochipTaken) {
			return bus.Result{}, domain.ErrMicrochipAlreadyAssigned // sin revelar la otra mascota (PRD-007)
		}
		return bus.Result{}, err
	}
	return bus.Result{AggregateID: c.PetID, Version: newVersion}, nil
}

func (s *Service) simple(ctx context.Context, petID uuid.UUID, c any) (bus.Result, error) {
	stored, err := s.execute(ctx, petID, func(pet *domain.Pet) ([]any, error) {
		return pet.HandleCommand(c)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return bus.Result{AggregateID: petID, Version: versionOf(stored)}, nil
}

func versionOf(stored []eventstore.StoredEvent) int64 {
	if len(stored) == 0 {
		return 0
	}
	return stored[len(stored)-1].StreamVersion
}

// petHasMicrochip/petMicrochip evitan exponer internals del agregado; los
// getters mínimos viven en domain.
func petHasMicrochip(p *domain.Pet) bool { return p.MicrochipValue() != "" }

func petMicrochip(p *domain.Pet) string { return p.MicrochipValue() }

// Queries expone el lado de consulta (inyectado).
func (s *Service) Queries() PetQueries { return s.queries }
