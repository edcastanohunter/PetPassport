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

// CardOptions son las dependencias del servicio del carnet.
type CardOptions struct {
	Repo       CardRepository
	UoW        TxRunner
	Projects   ProjectionRunner
	PetAccess  PetAccess
	Membership Membership
	Catalog    Catalog
	Clock      kernel.Clock
	Logger     *slog.Logger
}

// CardService ejecuta los casos de uso del carnet (DOM-006).
type CardService struct {
	repo       CardRepository
	uow        TxRunner
	projects   ProjectionRunner
	petAccess  PetAccess
	membership Membership
	catalog    Catalog
	clock      kernel.Clock
	logger     *slog.Logger
	bus        *bus.Bus
}

// NewCardService construye el servicio.
func NewCardService(o CardOptions) *CardService {
	if o.Clock == nil {
		o.Clock = kernel.RealClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &CardService{
		repo: o.Repo, uow: o.UoW, projects: o.Projects, petAccess: o.PetAccess,
		membership: o.Membership, catalog: o.Catalog, clock: o.Clock, logger: o.Logger,
	}
}

// RegisterCommands registra los handlers en el bus (arranque).
func (s *CardService) RegisterCommands(b *bus.Bus) {
	if b == nil {
		panic("clinical: bus nil")
	}
	s.bus = b
	b.Register(domain.RecordEntry{}, s.handleRecord)
	b.Register(domain.DeclareEntry{}, s.handleDeclare)
	b.Register(domain.VerifyEntry{}, s.handleVerify)
	b.Register(domain.DisputeEntry{}, s.handleDispute)
	b.Register(domain.VoidEntry{}, s.handleVoid)
	b.Register(domain.AdjustNextDue{}, s.handleAdjust)
	b.Register(domain.SyncSpecies{}, s.handleSync)
}

// Dispatch ejecuta un comando por el bus (fachada para http).
func (s *CardService) Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	if s.bus == nil {
		return bus.Result{}, errors.New("clinical: comandos de carnet no registrados")
	}
	return s.bus.Dispatch(ctx, cmd)
}

// resolveActor completa lo que no puede venir del cliente: identidad, flags
// de super_admin y tutor, y el acceso de la organización a la mascota. orgID
// es la organización con la que actúa el principal (nil-uuid si ninguna).
func (s *CardService) resolveActor(ctx context.Context, petID, orgID uuid.UUID) (domain.CardActor, bool, error) {
	cc := CommandContextFrom(ctx)
	a := domain.CardActor{UserID: cc.Principal.UserID, SuperAdmin: cc.Principal.SuperAdmin}
	guardian, err := s.petAccess.GuardianOf(ctx, petID)
	if err != nil {
		return a, false, err
	}
	a.Guardian = guardian == a.UserID
	if orgID == uuid.Nil {
		return a, false, nil
	}
	role, active, err := s.membership.RoleOf(ctx, a.UserID, orgID)
	if err != nil {
		return a, false, err
	}
	switch role {
	case OrgRole(domain.RoleVeterinarian), OrgRole(domain.RoleClinicAdmin), OrgRole(domain.RoleReceptionist):
	default:
		active = false
	}
	if !active {
		return a, false, &kernel.ForbiddenError{Reason: "sin membresía clínica activa en la organización"}
	}
	a.OrganizationID = orgID
	level, err := s.petAccess.CanOrgAccess(ctx, petID, orgID)
	if err != nil {
		return a, false, err
	}
	return a, level == AccessLinked, nil
}

func (s *CardService) metadata(ctx context.Context, orgID uuid.UUID) eventstore.Metadata {
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
		ActorID: actor, ActorRoles: cc.Principal.Roles, CorrelationID: cc.CorrelationID,
		CausationID: cc.CausationID, Source: source, IPHash: cc.IPHash,
		UserAgentHash: cc.UserAgentHash, IdempotencyKey: cc.IdempotencyKey,
	}
	if orgID != uuid.Nil {
		meta.ActorOrgID = orgID.String()
	}
	return meta
}

// run carga el carnet, construye y ejecuta el comando y persiste + proyecta.
func (s *CardService) run(ctx context.Context, petID, orgID uuid.UUID,
	build func(ctx context.Context, card *domain.VaccinationCard) (any, error)) (bus.Result, error) {
	var res bus.Result
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		card, err := s.repo.Load(ctx, petID)
		if err != nil {
			return err
		}
		cc := CommandContextFrom(ctx)
		if cc.ExpectedVersion > 0 && card.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: card.Version()}
		}
		cmd, err := build(ctx, card)
		if err != nil {
			return err
		}
		events, err := card.HandleCommand(cmd)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			res = bus.Result{AggregateID: petID, Version: card.Version()}
			return nil
		}
		stored, err := s.repo.Save(ctx, card, s.metadata(ctx, orgID))
		if err != nil {
			return err
		}
		res = bus.Result{AggregateID: petID, Version: versionOf(stored)}
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return res, nil
}

// pet devuelve especie y nacimiento de la mascota (puerto PetAccess).
func (s *CardService) pet(ctx context.Context, petID uuid.UUID) (string, *kernel.CivilDate, error) {
	ps, err := s.petAccess.PetSummary(ctx, petID)
	if err != nil {
		return "", nil, err
	}
	var birth *kernel.CivilDate
	if ps.BirthDate != nil {
		if d, err := kernel.NewCivilDate(*ps.BirthDate); err == nil {
			birth = &d
		}
	}
	return ps.Species, birth, nil
}

// resolveProduct copia el nombre del catálogo (DOM-007) y comprueba que la
// vacuna aplica a la especie.
func (s *CardService) resolveProduct(ctx context.Context, in domain.EntryInput, species string) (domain.EntryInput, *VaccineRef, error) {
	if in.Product.CatalogID == nil {
		return in, nil, nil
	}
	v, err := s.catalog.Vaccine(ctx, *in.Product.CatalogID)
	if err != nil {
		return in, nil, err
	}
	if !v.Active {
		return in, nil, &kernel.NotFoundError{Resource: "vaccine"}
	}
	ve := &kernel.ValidationError{}
	if in.Kind != string(domain.KindVaccine) {
		ve.Add("product.catalog_id", "ONLY_FOR_VACCINES")
	}
	if !containsFold(v.Species, species) {
		ve.Add("product.catalog_id", "SPECIES_MISMATCH")
	}
	if err := ve.ErrOrNil(); err != nil {
		return in, nil, err
	}
	in.Product.Name = v.Name
	return in, &v, nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// Propose calcula la propuesta de próxima dosis para previsualizarla
// (GET /card/next-due/proposal) o para fijar la entrada al registrarla.
func (s *CardService) Propose(ctx context.Context, petID uuid.UUID, in domain.EntryInput) (domain.Proposal, error) {
	species, birth, err := s.pet(ctx, petID)
	if err != nil {
		return domain.Proposal{}, err
	}
	in, vac, err := s.resolveProduct(ctx, in, species)
	if err != nil || vac == nil {
		return domain.Proposal{Explanation: "Sin protocolo para esta vacuna: indique la próxima dosis manualmente."}, err
	}
	date, err := kernel.NewCivilDate(in.AdministeredAt)
	if err != nil {
		return domain.Proposal{}, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "administered_at", Code: "INVALID"}}}
	}
	card, err := s.repo.Load(ctx, petID)
	if err != nil {
		return domain.Proposal{}, err
	}
	return s.propose(ctx, card, species, birth, vac, date)
}

func (s *CardService) propose(ctx context.Context, card *domain.VaccinationCard, species string,
	birth *kernel.CivilDate, vac *VaccineRef, date kernel.CivilDate) (domain.Proposal, error) {
	p, err := s.catalog.Protocol(ctx, DefaultCountry, species, vac.ID)
	if err != nil {
		return domain.Proposal{}, err
	}
	var proto *domain.Protocol
	if p != nil {
		proto = &domain.Protocol{VaccineID: p.VaccineID, InitialSeries: p.InitialSeries, BoosterIntervalMonths: p.BoosterIntervalMonths}
	}
	return domain.NextDueCalculator{}.Propose(card.Entries(), vac.ID, date, birth, proto), nil
}

func (s *CardService) handleRecord(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RecordEntry)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	actor, linked, err := s.resolveActor(ctx, c.PetID, c.Actor.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor, c.PetLinked = actor, linked
	species, birth, err := s.pet(ctx, c.PetID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Species, c.BirthDate = species, birth
	in, vac, err := s.resolveProduct(ctx, c.EntryInput, species)
	if err != nil {
		return bus.Result{}, err
	}
	c.EntryInput = in
	return s.run(ctx, c.PetID, actor.OrganizationID, func(ctx context.Context, card *domain.VaccinationCard) (any, error) {
		if vac != nil && c.NextDueDate == nil {
			if date, err := kernel.NewCivilDate(c.AdministeredAt); err == nil {
				pr, err := s.propose(ctx, card, species, birth, vac, date)
				if err != nil {
					return nil, err
				}
				c.Proposed = pr.NextDue
			}
		}
		return c, nil
	})
}

func (s *CardService) handleDeclare(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.DeclareEntry)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	actor, _, err := s.resolveActor(ctx, c.PetID, uuid.Nil)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor = actor
	species, birth, err := s.pet(ctx, c.PetID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Species, c.BirthDate = species, birth
	in, vac, err := s.resolveProduct(ctx, c.EntryInput, species)
	if err != nil {
		return bus.Result{}, err
	}
	c.EntryInput = in
	return s.run(ctx, c.PetID, uuid.Nil, func(ctx context.Context, card *domain.VaccinationCard) (any, error) {
		if vac != nil && c.NextDueDate == nil {
			if date, err := kernel.NewCivilDate(c.AdministeredAt); err == nil {
				pr, err := s.propose(ctx, card, species, birth, vac, date)
				if err != nil {
					return nil, err
				}
				c.Proposed = pr.NextDue
			}
		}
		return c, nil
	})
}

// petOf devuelve la mascota de una entrada del carnet. El comando solo trae
// EntryID; el HTTP resuelve PetID y lo pone en el contexto del comando.
type petKey struct{}

// WithPet adjunta la mascota objetivo al contexto (los comandos sobre una
// entrada solo llevan EntryID; el stream es vax-<PetID>).
func WithPet(ctx context.Context, petID uuid.UUID) context.Context {
	return context.WithValue(ctx, petKey{}, petID)
}

func petFrom(ctx context.Context) (uuid.UUID, error) {
	id, _ := ctx.Value(petKey{}).(uuid.UUID)
	if id == uuid.Nil {
		return uuid.Nil, errors.New("clinical: comando de carnet sin mascota en el contexto")
	}
	return id, nil
}

func (s *CardService) handleVerify(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.VerifyEntry)
	petID, err := petFrom(ctx)
	if err != nil {
		return bus.Result{}, err
	}
	actor, linked, err := s.resolveActor(ctx, petID, c.Actor.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor, c.PetLinked = actor, linked
	return s.run(ctx, petID, actor.OrganizationID, func(context.Context, *domain.VaccinationCard) (any, error) { return c, nil })
}

func (s *CardService) handleDispute(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.DisputeEntry)
	petID, err := petFrom(ctx)
	if err != nil {
		return bus.Result{}, err
	}
	actor, linked, err := s.resolveActor(ctx, petID, c.Actor.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor, c.PetLinked = actor, linked
	return s.run(ctx, petID, actor.OrganizationID, func(context.Context, *domain.VaccinationCard) (any, error) { return c, nil })
}

func (s *CardService) handleVoid(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.VoidEntry)
	petID, err := petFrom(ctx)
	if err != nil {
		return bus.Result{}, err
	}
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	actor, linked, err := s.resolveActor(ctx, petID, c.Actor.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor, c.PetLinked = actor, linked
	return s.run(ctx, petID, actor.OrganizationID, func(context.Context, *domain.VaccinationCard) (any, error) { return c, nil })
}

func (s *CardService) handleAdjust(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AdjustNextDue)
	petID, err := petFrom(ctx)
	if err != nil {
		return bus.Result{}, err
	}
	actor, linked, err := s.resolveActor(ctx, petID, c.Actor.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	c.Actor, c.PetLinked = actor, linked
	return s.run(ctx, petID, actor.OrganizationID, func(context.Context, *domain.VaccinationCard) (any, error) { return c, nil })
}

// handleSync es la reacción al cambio de especie de Pet (sin actor humano).
func (s *CardService) handleSync(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.SyncSpecies)
	return s.run(ctx, c.PetID, uuid.Nil, func(context.Context, *domain.VaccinationCard) (any, error) { return c, nil })
}
