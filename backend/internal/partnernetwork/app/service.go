package app

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CommandContext viaja en el contexto desde http hasta los handlers (DOM-012),
// mismo contrato que petregistry. ExpectedVersion transporta If-Match.
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

// PrincipalInfo desacopla el handler de platform/auth.
type PrincipalInfo struct {
	UserID        uuid.UUID
	Email         string
	EmailVerified bool
	Roles         []string
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

// maxPublicCodeAttempts acota los reintentos ante colisión de código (32^6
// combinaciones: una colisión ya es improbable).
const maxPublicCodeAttempts = 5

// Service registra los handlers de Partner Network en el bus.
type Service struct {
	repo        Repository
	uow         TxRunner
	projects    ProjectionRunner
	codes       PublicCodeReservations
	tokens      InvitationTokens
	queries     Queries
	users       UserLookup
	memberships MembershipInvalidator
	notifier    notify.Enqueuer
	auditor     Auditor
	ids         kernel.IDGenerator
	tokenGen    kernel.TokenGenerator
	clock       kernel.Clock
	logger      *slog.Logger
	bus         *bus.Bus
}

// Options construye el servicio. Users, Memberships y Notifier son opcionales
// (nil en pruebas de dominio/app).
type Options struct {
	Repo        Repository
	UoW         TxRunner
	Projects    ProjectionRunner
	Codes       PublicCodeReservations
	Tokens      InvitationTokens
	Queries     Queries
	Users       UserLookup
	Memberships MembershipInvalidator
	Notifier    notify.Enqueuer // encola los correos con token (ARQ-014); el SMTP es del worker
	Auditor     Auditor         // obligatorio para los comandos admin (WithAdminAudit)
	IDs         kernel.IDGenerator
	TokenGen    kernel.TokenGenerator
	Clock       kernel.Clock
	Logger      *slog.Logger
}

// NewService construye el servicio.
func NewService(opts Options) *Service {
	if opts.IDs == nil {
		opts.IDs = kernel.RealIDGenerator{}
	}
	if opts.TokenGen == nil {
		opts.TokenGen = kernel.RealTokenGenerator{}
	}
	if opts.Clock == nil {
		opts.Clock = kernel.RealClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{
		repo: opts.Repo, uow: opts.UoW, projects: opts.Projects, codes: opts.Codes, tokens: opts.Tokens,
		queries: opts.Queries, users: opts.Users, memberships: opts.Memberships, notifier: opts.Notifier, auditor: opts.Auditor,
		ids: opts.IDs, tokenGen: opts.TokenGen,
		clock: opts.Clock, logger: opts.Logger,
	}
}

// RegisterCommands registra los handlers en el bus (arranque).
func (s *Service) RegisterCommands(b *bus.Bus) {
	if b == nil {
		panic("partnernetwork: bus nil")
	}
	s.bus = b
	b.Register(domain.RegisterOrganization{}, s.handleRegister)
	b.Register(domain.UpdateOrganizationProfile{}, s.handleUpdateProfile)
	b.Register(domain.VerifyOrganization{}, s.handleVerify)
	b.Register(domain.RejectOrganization{}, s.handleReject)
	b.Register(domain.SuspendOrganization{}, s.handleSimple)
	b.Register(domain.ReactivateOrganization{}, s.handleSimple)
	b.Register(domain.InviteMember{}, s.handleInvite)
	b.Register(domain.RevokeInvitation{}, s.handleSimple)
	b.Register(domain.AcceptInvitation{}, s.handleAccept)
	b.Register(domain.ChangeMemberRole{}, s.handleSimple)
	b.Register(domain.RemoveMember{}, s.handleSimple)
}

// Dispatch ejecuta un comando por el bus (fachada para http).
func (s *Service) Dispatch(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	if s.bus == nil {
		return bus.Result{}, errors.New("partnernetwork: comandos no registrados")
	}
	return s.bus.Dispatch(ctx, cmd)
}

// Queries expone el lado de consulta.
func (s *Service) Queries() Queries { return s.queries }

// Now expone el reloj inyectado (consultas con caducidad derivada).
func (s *Service) Now() time.Time { return s.clock.Now() }

func (s *Service) metadata(ctx context.Context) eventstore.Metadata {
	cc := commandContext(ctx)
	source := cc.Source
	if source == "" {
		source = eventstore.SourceAPI
	}
	actor := cc.Principal.UserID.String()
	if cc.Principal.UserID == uuid.Nil {
		actor = eventstore.SourceSystem
	}
	return eventstore.Metadata{
		ActorID: actor, ActorRoles: cc.Principal.Roles,
		CorrelationID: cc.CorrelationID, CausationID: cc.CausationID, Source: source,
		IPHash: cc.IPHash, UserAgentHash: cc.UserAgentHash, IdempotencyKey: cc.IdempotencyKey,
	}
}

// outcome son los efectos posteriores al commit de un comando.
type outcome struct {
	result bus.Result
	org    *domain.Organization
	events []any
}

// execute corre el flujo transaccional: load → If-Match → handle → append →
// project. Los efectos (invalidación de caché, emails) se aplican después
// del commit en afterCommit.
func (s *Service) execute(ctx context.Context, orgID uuid.UUID, fn func(o *domain.Organization) ([]any, error)) (outcome, error) {
	var out outcome
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		o, err := s.repo.Load(ctx, orgID)
		if err != nil {
			return err
		}
		if cc := commandContext(ctx); cc.ExpectedVersion > 0 && o.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: o.Version()}
		}
		events, err := fn(o)
		if err != nil {
			return err
		}
		out = outcome{result: bus.Result{AggregateID: orgID, Version: o.Version()}, org: o, events: events}
		if len(events) == 0 {
			return nil
		}
		stored, err := s.repo.Save(ctx, o, s.metadata(ctx))
		if err != nil {
			return err
		}
		if err := s.projects.Apply(ctx, stored); err != nil {
			return err
		}
		out.result.Version = versionOf(stored)
		return s.recordAdminAudit(ctx, orgID)
	})
	if err != nil {
		return outcome{}, mapErr(err)
	}
	s.afterCommit(out)
	return out, nil
}

// afterCommit invalida la caché de membresías de los usuarios afectados
// (DOM-011): miembros que cambian y, si cambia el estado de la org, todos.
func (s *Service) afterCommit(out outcome) {
	if s.memberships == nil || len(out.events) == 0 {
		return
	}
	var ids []uuid.UUID
	for _, e := range out.events {
		switch ev := e.(type) {
		case domain.MemberJoined:
			ids = append(ids, ev.UserID)
		case domain.MemberRoleChanged:
			ids = append(ids, ev.UserID)
		case domain.MemberRemoved:
			ids = append(ids, ev.UserID)
		case domain.OrganizationVerified, domain.OrganizationRejected, domain.OrganizationSuspended,
			domain.OrganizationReactivated, domain.OrganizationVerificationReset, domain.OrganizationProfileUpdated:
			ids = append(ids, out.org.MemberIDs()...)
		}
	}
	if len(ids) > 0 {
		s.memberships.InvalidateUsers(ids)
	}
}

// mapErr: la unicidad de tax_id (DAT-006) la traduce el adaptador de
// proyección a ValidationError; aquí solo pasan los errores tal cual.
func mapErr(err error) error { return err }

func versionOf(stored []eventstore.StoredEvent) int64 {
	if len(stored) == 0 {
		return 0
	}
	return stored[len(stored)-1].StreamVersion
}

// ------------------------------------------------------------------ handlers

// handleRegister reserva el PublicCode en la tx antes del Append (O5); ante
// colisión genera otro código y reintenta.
func (s *Service) handleRegister(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RegisterOrganization)
	if c.OrganizationID == uuid.Nil {
		c.OrganizationID = s.ids.NewID()
	}
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	for attempt := 1; ; attempt++ {
		var result bus.Result
		err := s.uow.Do(ctx, func(ctx context.Context) error {
			code, err := domain.GeneratePublicCode()
			if err != nil {
				return err
			}
			if err := s.codes.Reserve(ctx, string(code), c.OrganizationID); err != nil {
				return err
			}
			o := &domain.Organization{}
			o.SetPendingPublicCode(code)
			if _, err := o.HandleCommand(c); err != nil {
				return err
			}
			stored, err := s.repo.Save(ctx, o, s.metadata(ctx))
			if err != nil {
				return err
			}
			if err := s.projects.Apply(ctx, stored); err != nil {
				return err
			}
			result = bus.Result{AggregateID: c.OrganizationID, Version: versionOf(stored)}
			return nil
		})
		if errors.Is(err, ErrPublicCodeTaken) && attempt < maxPublicCodeAttempts {
			continue
		}
		if err != nil {
			return bus.Result{}, mapErr(err)
		}
		if s.memberships != nil {
			s.memberships.InvalidateUsers([]uuid.UUID{c.FounderID})
		}
		return result, nil
	}
}

func (s *Service) handleUpdateProfile(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UpdateOrganizationProfile)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	out, err := s.execute(ctx, c.OrganizationID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(c) })
	return out.result, err
}

// handleVerify: el aviso a la organización lo emite el PM de Notifications a
// partir de OrganizationVerified (ARQ-014): el comando no envía nada.
func (s *Service) handleVerify(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.VerifyOrganization)
	out, err := s.execute(ctx, c.OrganizationID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(c) })
	return out.result, err
}

// handleReject: el aviso con el motivo lo emite el PM de Notifications a
// partir de OrganizationRejected (ARQ-014).
func (s *Service) handleReject(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RejectOrganization)
	out, err := s.execute(ctx, c.OrganizationID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(c) })
	return out.result, err
}

// handleInvite genera InvitationID y token (32 bytes); el hash viaja por el
// contexto hasta el proyector en la misma tx; el token en claro solo sale en
// el email (best-effort tras el commit).
func (s *Service) handleInvite(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.InviteMember)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	if c.InvitationID == uuid.Nil {
		c.InvitationID = s.ids.NewID()
	}
	if s.users != nil {
		if id, ok := s.users.FindByEmail(ctx, c.Email); ok {
			c.ExistingUserID = id
		}
	}
	token := s.tokenGen.NewToken()
	ctx = WithInvitationTokenHash(ctx, c.InvitationID, kernel.HashToken(token))
	out, err := s.execute(ctx, c.OrganizationID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(c) })
	if err != nil {
		return bus.Result{}, err
	}
	s.sendInvitation(ctx, c.InvitationID, out.org.DisplayName(), c.Email, c.Role, token)
	return bus.Result{AggregateID: c.InvitationID, Version: out.result.Version}, nil
}

func (s *Service) handleAccept(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AcceptInvitation)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	out, err := s.execute(ctx, c.OrganizationID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(c) })
	return out.result, err
}

func (s *Service) handleSimple(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	orgID, ok := orgIDOf(cmd)
	if !ok {
		return bus.Result{}, errors.New("partnernetwork: comando sin organización")
	}
	out, err := s.execute(ctx, orgID, func(o *domain.Organization) ([]any, error) { return o.HandleCommand(cmd) })
	return out.result, err
}

func orgIDOf(cmd bus.Command) (uuid.UUID, bool) {
	switch c := cmd.(type) {
	case domain.SuspendOrganization:
		return c.OrganizationID, true
	case domain.ReactivateOrganization:
		return c.OrganizationID, true
	case domain.RevokeInvitation:
		return c.OrganizationID, true
	case domain.ChangeMemberRole:
		return c.OrganizationID, true
	case domain.RemoveMember:
		return c.OrganizationID, true
	}
	return uuid.Nil, false
}

// --------------------------------------------------------- operaciones http

// AcceptInvitationToken resuelve el token (hash en el read model) y despacha
// AcceptInvitation. Token desconocido o caducado → *kernel.NotFoundError
// (404 sin distinguir, API-007).
func (s *Service) AcceptInvitationToken(ctx context.Context, token string) (bus.Result, error) {
	cc := commandContext(ctx)
	inv, err := s.queries.InvitationByTokenHash(ctx, kernel.HashToken(token), s.clock.Now())
	if err != nil {
		return bus.Result{}, err
	}
	res, err := s.Dispatch(ctx, domain.AcceptInvitation{
		OrganizationID: inv.OrganizationID, InvitationID: inv.ID, UserID: cc.Principal.UserID,
		ActorEmail: cc.Principal.Email, EmailVerified: cc.Principal.EmailVerified,
	})
	if errors.Is(err, domain.ErrInvitationTokenInvalid) {
		return bus.Result{}, &kernel.NotFoundError{Resource: "invitation"}
	}
	return res, err
}

// ResendInvitation rota el token (el anterior deja de valer) y reenvía el
// email. No emite evento: es entrega, no un hecho de dominio. Límite
// domain.MaxResends24h por 24 h → ErrResendLimit (429).
func (s *Service) ResendInvitation(ctx context.Context, orgID, invitationID uuid.UUID) error {
	org, err := s.queries.GetOrg(ctx, orgID)
	if err != nil {
		return err
	}
	token := s.tokenGen.NewToken()
	email, role, err := s.tokens.Rotate(ctx, orgID, invitationID, kernel.HashToken(token), s.clock.Now())
	if err != nil {
		return err
	}
	s.sendInvitation(ctx, invitationID, org.DisplayName, email, role, token)
	return nil
}

// sendInvitation encola el correo de invitación (regla member_invited). El
// token en claro solo existe aquí: no viaja en el evento (solo su hash), así
// que no puede resolverlo el PM; se encola con la clave de la rotación y se
// purga de la fila en cuanto el correo sale (ADR-026, propuesta). El fallo se
// registra sin PII y no revierte el comando ya confirmado: el administrador
// puede reenviar (reenvío rota el token).
func (s *Service) sendInvitation(ctx context.Context, invitationID uuid.UUID, orgName, email, role, token string) {
	if s.notifier == nil || strings.TrimSpace(email) == "" {
		return
	}
	err := s.notifier.Enqueue(context.WithoutCancel(ctx), notify.Notification{
		Rule: "member_invited", Template: "org_invitation", Category: "account",
		To: email, Locale: "es", SubjectID: invitationID,
		Occurrence: hex.EncodeToString(kernel.HashToken(token)[:8]),
		Payload: map[string]string{
			"org_name": orgName, "role": role, "link_path": "/invitations/" + url.PathEscape(token),
		},
		Secret: []string{"link_path"},
	})
	if err != nil {
		s.logger.WarnContext(ctx, "no se pudo encolar la invitación", "invitation_id", invitationID.String(), "error", err.Error())
	}
}
