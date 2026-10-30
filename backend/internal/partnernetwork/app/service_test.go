package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ----------------------------------------------------------------- fakes

type memRepo struct {
	mu      sync.Mutex
	streams map[uuid.UUID][]any
}

func (r *memRepo) Load(_ context.Context, id uuid.UUID) (*domain.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := &domain.Organization{}
	for _, e := range r.streams[id] {
		if err := o.ApplyEvent(e); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (r *memRepo) Save(_ context.Context, o *domain.Organization, _ eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := o.UncommittedEvents()
	base := o.Version() - int64(len(pending))
	out := make([]eventstore.StoredEvent, 0, len(pending))
	for i, e := range pending {
		r.streams[o.ID()] = append(r.streams[o.ID()], e)
		out = append(out, eventstore.StoredEvent{StreamID: o.ID(), StreamVersion: base + int64(i) + 1, Type: e.(eventstore.Event).EventName()})
	}
	o.MarkCommitted()
	return out, nil
}

type directUoW struct{}

func (directUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// captureProjector guarda el hash de token visto en el contexto (DAT-006).
type captureProjector struct{ hashes map[uuid.UUID][]byte }

func (p *captureProjector) Apply(ctx context.Context, events []eventstore.StoredEvent) error {
	if m, ok := ctx.Value(tokenHashKey{}).(map[uuid.UUID][]byte); ok {
		for k, v := range m {
			p.hashes[k] = v
		}
	}
	return nil
}

type fakeCodes struct {
	collisions int
	reserved   []string
}

func (c *fakeCodes) Reserve(_ context.Context, code string, _ uuid.UUID) error {
	if c.collisions > 0 {
		c.collisions--
		return ErrPublicCodeTaken
	}
	c.reserved = append(c.reserved, code)
	return nil
}

// fakeMailer captura lo que el servicio encola (ARQ-014).
type fakeMailer struct {
	sent []notify.Notification
	fail bool
}

func (m *fakeMailer) Enqueue(_ context.Context, n notify.Notification) error {
	m.sent = append(m.sent, n)
	if m.fail {
		return errors.New("cola caída")
	}
	return nil
}

type fakeInvalidator struct{ ids []uuid.UUID }

func (f *fakeInvalidator) InvalidateUsers(ids []uuid.UUID) { f.ids = append(f.ids, ids...) }

type fakeUsers map[string]uuid.UUID

func (u fakeUsers) FindByEmail(_ context.Context, email string) (uuid.UUID, bool) {
	id, ok := u[strings.ToLower(email)]
	return id, ok
}

func (u fakeUsers) Info(context.Context, uuid.UUID) (UserInfo, bool) { return UserInfo{}, false }

type fakeTokens struct{ err error }

func (t fakeTokens) Rotate(context.Context, uuid.UUID, uuid.UUID, []byte, time.Time) (string, string, error) {
	return "vet@sur.co", "veterinarian", t.err
}

type fakeQueries struct {
	Queries
	invitation InvitationView
	invErr     error
}

func (q fakeQueries) InvitationByTokenHash(context.Context, []byte, time.Time) (InvitationView, error) {
	return q.invitation, q.invErr
}

func (q fakeQueries) GetOrg(_ context.Context, id uuid.UUID) (OrgView, error) {
	return OrgView{ID: id, DisplayName: "Clínica Sur"}, nil
}

type fixedTokens struct{}

func (fixedTokens) NewToken() string { return "token-fijo-de-prueba-123456" }

type env struct {
	svc   *Service
	repo  *memRepo
	proj  *captureProjector
	codes *fakeCodes
	mail  *fakeMailer
	inv   *fakeInvalidator
}

var (
	founder = uuid.MustParse("01930000-0000-7000-8000-0000000000aa")
	vetUser = uuid.MustParse("01930000-0000-7000-8000-0000000000bb")
	now     = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return now }

func newEnv(t *testing.T, q Queries) *env {
	t.Helper()
	e := &env{
		repo: &memRepo{streams: map[uuid.UUID][]any{}}, proj: &captureProjector{hashes: map[uuid.UUID][]byte{}},
		codes: &fakeCodes{}, mail: &fakeMailer{}, inv: &fakeInvalidator{},
	}
	e.svc = NewService(Options{
		Repo: e.repo, UoW: directUoW{}, Projects: e.proj, Codes: e.codes, Tokens: fakeTokens{},
		Queries: q, Users: fakeUsers{"miembro@sur.co": vetUser}, Memberships: e.inv, Notifier: e.mail,
		TokenGen: fixedTokens{}, Clock: fixedClock{},
	})
	e.svc.RegisterCommands(bus.New())
	return e
}

func (e *env) register(t *testing.T) uuid.UUID {
	t.Helper()
	res, err := e.svc.Dispatch(context.Background(), domain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900123456-8",
		Address:      domain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567", FounderID: founder,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.AggregateID
}

// ----------------------------------------------------------------- tests

func TestService_Register_PublicCodeRetryAndFounderInvalidated(t *testing.T) {
	e := newEnv(t, nil)
	e.codes.collisions = 2
	id := e.register(t)
	if len(e.codes.reserved) != 1 {
		t.Fatalf("reservas: %v", e.codes.reserved)
	}
	o, _ := e.repo.Load(context.Background(), id)
	if string(o.PublicCode()) != e.codes.reserved[0] || o.Status() != domain.OrgPendingVerification {
		t.Fatalf("org: code=%s status=%s", o.PublicCode(), o.Status())
	}
	if len(e.inv.ids) != 1 || e.inv.ids[0] != founder {
		t.Fatalf("invalidación del fundador: %v", e.inv.ids)
	}
}

func TestService_Register_GivesUpAfterMaxCollisions(t *testing.T) {
	e := newEnv(t, nil)
	e.codes.collisions = maxPublicCodeAttempts
	_, err := e.svc.Dispatch(context.Background(), domain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Sur", DisplayName: "Clínica Sur", TaxCountry: "CO", TaxValue: "900123456-8",
		Address:      domain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 1"},
		ContactEmail: "a@sur.co", ContactPhone: "+573001234567", FounderID: founder,
	})
	if !errors.Is(err, ErrPublicCodeTaken) {
		t.Fatalf("err: %v", err)
	}
}

func TestService_Invite_TokenHashToProjectorAndEmail(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	res, err := e.svc.Dispatch(context.Background(), domain.InviteMember{OrganizationID: org, Email: "nuevo@sur.co", Role: "veterinarian"})
	if err != nil {
		t.Fatal(err)
	}
	hash := e.proj.hashes[res.AggregateID]
	if string(hash) != string(kernel.HashToken("token-fijo-de-prueba-123456")) {
		t.Fatal("el proyector no recibió el hash del token de la invitación")
	}
	if len(e.mail.sent) != 1 || e.mail.sent[0].To != "nuevo@sur.co" || e.mail.sent[0].Template != "org_invitation" ||
		e.mail.sent[0].Payload["link_path"] != "/invitations/token-fijo-de-prueba-123456" ||
		len(e.mail.sent[0].Secret) != 1 || e.mail.sent[0].Secret[0] != "link_path" {
		t.Fatalf("notificación: %+v", e.mail.sent)
	}
}

func TestService_Invite_AlreadyMemberByEmail(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	e.repo.streams[org] = append(e.repo.streams[org], domain.MemberJoined{OrganizationID: org, UserID: vetUser, Role: domain.RoleVeterinarian})
	_, err := e.svc.Dispatch(context.Background(), domain.InviteMember{OrganizationID: org, Email: "MIEMBRO@sur.co", Role: "receptionist"})
	if !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("err: %v", err)
	}
}

func TestService_MemberChangesInvalidateCache_DOM011(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	e.repo.streams[org] = append(e.repo.streams[org], domain.MemberJoined{OrganizationID: org, UserID: vetUser, Role: domain.RoleVeterinarian})
	e.inv.ids = nil
	if _, err := e.svc.Dispatch(context.Background(), domain.ChangeMemberRole{OrganizationID: org, UserID: vetUser, Role: "receptionist"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Dispatch(context.Background(), domain.RemoveMember{OrganizationID: org, UserID: vetUser}); err != nil {
		t.Fatal(err)
	}
	if len(e.inv.ids) != 2 || e.inv.ids[0] != vetUser || e.inv.ids[1] != vetUser {
		t.Fatalf("invalidaciones: %v", e.inv.ids)
	}
}

func TestService_StatusChangeInvalidatesAllMembersAndMails(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	e.repo.streams[org] = append(e.repo.streams[org], domain.MemberJoined{OrganizationID: org, UserID: vetUser, Role: domain.RoleVeterinarian})
	e.inv.ids = nil
	if _, err := e.svc.Dispatch(context.Background(), domain.VerifyOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}
	if len(e.inv.ids) != 2 {
		t.Fatalf("debe invalidar a todos los miembros: %v", e.inv.ids)
	}
	if len(e.mail.sent) != 0 {
		t.Fatalf("verificar no encola desde el comando (lo hace el PM): %+v", e.mail.sent)
	}
	e.mail.fail = true // un fallo al encolar no revierte el comando
	if _, err := e.svc.Dispatch(context.Background(), domain.SuspendOrganization{OrganizationID: org, Reason: "motivo suficientemente largo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Dispatch(context.Background(), domain.ReactivateOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}
	o, _ := e.repo.Load(context.Background(), org)
	if o.Status() != domain.OrgActive {
		t.Fatalf("estado: %s", o.Status())
	}
}

func TestService_RejectDoesNotMail(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	if _, err := e.svc.Dispatch(context.Background(), domain.RejectOrganization{OrganizationID: org, Reason: "no adjuntó el RUT vigente"}); err != nil {
		t.Fatal(err)
	}
	if len(e.mail.sent) != 0 {
		t.Fatalf("rechazar no encola desde el comando (lo hace el PM): %+v", e.mail.sent)
	}
}

func TestService_IfMatchMismatch(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	ctx := WithCommandContext(context.Background(), CommandContext{ExpectedVersion: 99})
	_, err := e.svc.Dispatch(ctx, domain.UpdateOrganizationProfile{OrganizationID: org, DisplayName: ptr("Clínica Norte")})
	var ce *kernel.ConcurrencyError
	if !errors.As(err, &ce) {
		t.Fatalf("err: %v", err)
	}
}

func TestService_AcceptInvitationToken(t *testing.T) {
	invID := uuid.MustParse("01930000-0000-7000-8000-0000000000e1")
	q := fakeQueries{}
	e := newEnv(t, &q)
	org := e.register(t)
	e.repo.streams[org] = append(e.repo.streams[org], domain.MemberInvited{OrganizationID: org, InvitationID: invID,
		Role: domain.RoleVeterinarian, ExpiresAt: now.Add(domain.InvitationTTL), PII: &domain.InvitationPII{Email: "vet@sur.co"}})
	q.invitation = InvitationView{ID: invID, OrganizationID: org}
	ctx := WithCommandContext(context.Background(), CommandContext{Principal: PrincipalInfo{UserID: vetUser, Email: "vet@sur.co", EmailVerified: true}})
	e.inv.ids = nil
	if _, err := e.svc.AcceptInvitationToken(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if len(e.inv.ids) != 1 || e.inv.ids[0] != vetUser {
		t.Fatalf("invalidación tras unirse: %v", e.inv.ids)
	}
	// Segunda aceptación del mismo usuario: idempotente.
	if _, err := e.svc.AcceptInvitationToken(ctx, "tok"); err != nil {
		t.Fatalf("idempotencia: %v", err)
	}
	// Otra persona con el mismo enlace ya usado → 404 sin distinguir.
	other := WithCommandContext(context.Background(), CommandContext{Principal: PrincipalInfo{UserID: uuid.New(), Email: "vet@sur.co", EmailVerified: true}})
	var nf *kernel.NotFoundError
	if _, err := e.svc.AcceptInvitationToken(other, "tok"); !errors.As(err, &nf) {
		t.Fatalf("token usado: %v", err)
	}
	// Token desconocido → 404.
	q.invErr = &kernel.NotFoundError{Resource: "invitation"}
	if _, err := e.svc.AcceptInvitationToken(ctx, "otro"); !errors.As(err, &nf) {
		t.Fatalf("token desconocido: %v", err)
	}
}

func TestService_ResendInvitation(t *testing.T) {
	e := newEnv(t, fakeQueries{})
	org := uuid.New()
	if err := e.svc.ResendInvitation(context.Background(), org, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if len(e.mail.sent) != 1 || e.mail.sent[0].To != "vet@sur.co" || e.mail.sent[0].Template != "org_invitation" {
		t.Fatalf("reenvío: %+v", e.mail.sent)
	}
	e.svc.tokens = fakeTokens{err: ErrResendLimit}
	if err := e.svc.ResendInvitation(context.Background(), org, uuid.New()); !errors.Is(err, ErrResendLimit) {
		t.Fatalf("límite: %v", err)
	}
}

func TestInvitationTokenHashContext(t *testing.T) {
	id := uuid.New()
	ctx := WithInvitationTokenHash(context.Background(), id, []byte{1})
	if string(InvitationTokenHashFrom(ctx, id)) != "\x01" || InvitationTokenHashFrom(context.Background(), id) != nil {
		t.Fatal("hash en contexto")
	}
}

func ptr(s string) *string { return &s }
