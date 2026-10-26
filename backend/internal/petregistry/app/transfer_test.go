package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type memPetRepo struct{ streams map[uuid.UUID][]any }

func (r *memPetRepo) Load(_ context.Context, id uuid.UUID) (*domain.Pet, error) {
	p := &domain.Pet{}
	for _, e := range r.streams[id] {
		if err := p.ApplyEvent(e); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (r *memPetRepo) Save(_ context.Context, p *domain.Pet, _ eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	pending := p.UncommittedEvents()
	base := p.Version() - int64(len(pending))
	out := make([]eventstore.StoredEvent, 0, len(pending))
	for i, e := range pending {
		r.streams[p.ID()] = append(r.streams[p.ID()], e)
		out = append(out, eventstore.StoredEvent{StreamID: p.ID(), StreamVersion: base + int64(i) + 1})
	}
	p.MarkCommitted()
	return out, nil
}

type txDirect struct{}

func (txDirect) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type hashCapture struct{ hashes map[uuid.UUID][]byte }

func (h *hashCapture) Apply(ctx context.Context, _ []eventstore.StoredEvent) error {
	if m, ok := ctx.Value(transferHashKey{}).(map[uuid.UUID][]byte); ok {
		for k, v := range m {
			h.hashes[k] = v
		}
	}
	return nil
}

type guardians map[uuid.UUID]GuardianContact

func (g guardians) Exists(_ context.Context, id uuid.UUID) bool { _, ok := g[id]; return ok }
func (g guardians) EmailOf(_ context.Context, id uuid.UUID) (string, bool) {
	c, ok := g[id]
	return c.Email, ok
}
func (g guardians) DisplayName(_ context.Context, id uuid.UUID) string { return g[id].DisplayName }
func (g guardians) Contact(_ context.Context, id uuid.UUID) (GuardianContact, bool) {
	c, ok := g[id]
	return c, ok
}

type orgs struct{ err error }

func (o orgs) ActiveClinic(_ context.Context, id uuid.UUID) (OrgSummary, error) {
	return OrgSummary{ID: id, Type: "clinic", Status: "active"}, o.err
}

type transfers struct {
	PetQueries
	view TransferView
	err  error
}

func (t transfers) TransferByTokenHash(context.Context, []byte, time.Time) (TransferView, error) {
	return t.view, t.err
}

type mails struct{ sent []mail.Message }

func (m *mails) Send(_ context.Context, msg mail.Message) error {
	m.sent = append(m.sent, msg)
	return errors.New("best-effort: este error no debe propagarse")
}

type tok struct{}

func (tok) NewToken() string { return "tok-transfer-abcdefghijk" }

type clk struct{}

func (clk) Now() time.Time { return tNow }

var (
	tNow     = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	tGuard   = uuid.MustParse("01930000-0000-7000-8000-00000000a001")
	tNewUser = uuid.MustParse("01930000-0000-7000-8000-00000000a002")
	tPet     = uuid.MustParse("01930000-0000-7000-8000-00000000b001")
	tOrg     = uuid.MustParse("01930000-0000-7000-8000-00000000c001")
)

type petEnv struct {
	svc   *Service
	repo  *memPetRepo
	proj  *hashCapture
	mails *mails
}

func newPetEnv(q PetQueries, orgErr error) *petEnv {
	e := &petEnv{
		repo:  &memPetRepo{streams: map[uuid.UUID][]any{tPet: {domain.PetRegistered{PetID: tPet, GuardianID: tGuard, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes"}}}},
		proj:  &hashCapture{hashes: map[uuid.UUID][]byte{}},
		mails: &mails{},
	}
	e.svc = NewService(Options{
		Repo: e.repo, UoW: txDirect{}, Projects: e.proj, Queries: q, Orgs: orgs{err: orgErr},
		Guardian: guardians{
			tGuard:   {DisplayName: "Ana", Email: "ana@example.com"},
			tNewUser: {DisplayName: "Beto", Email: "beto@example.com"},
		},
		Mailer: e.mails, BaseURL: "https://app.example", Tokens: tok{}, Clock: clk{},
	})
	e.svc.RegisterCommands(bus.New())
	return e
}

func TestTransfer_PRD010_InitiateMailsTokenAndProjectsHash(t *testing.T) {
	e := newPetEnv(nil, nil)
	if _, err := e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "Beto@Example.com"}); err != nil {
		t.Fatal(err)
	}
	if string(e.proj.hashes[tPet]) != string(kernel.HashToken("tok-transfer-abcdefghijk")) {
		t.Fatal("el proyector de pet_transfers no recibió el hash del token")
	}
	if len(e.mails.sent) != 1 || e.mails.sent[0].To != "Beto@Example.com" ||
		!strings.Contains(e.mails.sent[0].Body, "https://app.example/app/transfers/tok-transfer-abcdefghijk") ||
		!strings.Contains(e.mails.sent[0].Body, "2026-11-02") {
		t.Fatalf("email: %+v", e.mails.sent)
	}
	last := e.repo.streams[tPet][len(e.repo.streams[tPet])-1].(domain.GuardianshipTransferInitiated)
	if !last.ExpiresAt.Equal(tNow.Add(domain.TransferTTL)) {
		t.Fatalf("caducidad: %v", last.ExpiresAt)
	}
	// Mismo tutor (I4) y pendiente duplicada.
	if _, err := e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "otro@example.com"}); !errors.Is(err, domain.ErrTransferAlreadyPending) {
		t.Fatalf("pendiente duplicada: %v", err)
	}
}

func TestTransfer_SameGuardian_I4(t *testing.T) {
	e := newPetEnv(nil, nil)
	if _, err := e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "ANA@example.com"}); !errors.Is(err, domain.ErrSameGuardian) {
		t.Fatalf("I4: %v", err)
	}
}

func TestTransfer_AcceptByTokenMailsBoth(t *testing.T) {
	q := transfers{view: TransferView{PetID: tPet}}
	e := newPetEnv(q, nil)
	if _, err := e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "beto@example.com"}); err != nil {
		t.Fatal(err)
	}
	e.mails.sent = nil
	ctx := WithCommandContext(context.Background(), CommandContext{Principal: PrincipalInfo{UserID: tNewUser, Email: "beto@example.com"}})
	if _, err := e.svc.AcceptTransferToken(ctx, "tok-transfer-abcdefghijk"); err != nil {
		t.Fatal(err)
	}
	p, _ := e.repo.Load(context.Background(), tPet)
	if p.GuardianID() != tNewUser || p.ID() != tPet {
		t.Fatalf("tutor tras aceptar: %v", p.GuardianID())
	}
	if len(e.mails.sent) != 2 || e.mails.sent[0].To != "ana@example.com" || e.mails.sent[1].To != "beto@example.com" {
		t.Fatalf("emails a ambos tutores: %+v", e.mails.sent)
	}
	// Reusar el token: ya no hay pendiente → TRANSFER_TOKEN_INVALID.
	if _, err := e.svc.AcceptTransferToken(ctx, "tok-transfer-abcdefghijk"); !errors.Is(err, domain.ErrTransferTokenInvalid) {
		t.Fatalf("token reusado: %v", err)
	}
}

func TestTransfer_AcceptWrongEmailAndUnknownToken(t *testing.T) {
	q := transfers{view: TransferView{PetID: tPet}}
	e := newPetEnv(q, nil)
	if _, err := e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "beto@example.com"}); err != nil {
		t.Fatal(err)
	}
	ctx := WithCommandContext(context.Background(), CommandContext{Principal: PrincipalInfo{UserID: uuid.New(), Email: "intruso@example.com"}})
	if _, err := e.svc.AcceptTransferToken(ctx, "tok"); !errors.Is(err, domain.ErrTransferEmailMismatch) {
		t.Fatalf("email distinto: %v", err)
	}
	e.svc.queries = transfers{err: &kernel.NotFoundError{Resource: "transfer"}}
	if _, err := e.svc.AcceptTransferToken(ctx, "nada"); !errors.Is(err, domain.ErrTransferTokenInvalid) {
		t.Fatalf("token desconocido: %v", err)
	}
}

func TestTransfer_Cancel(t *testing.T) {
	e := newPetEnv(nil, nil)
	if _, err := e.svc.Dispatch(context.Background(), domain.CancelGuardianshipTransfer{PetID: tPet}); !errors.Is(err, domain.ErrTransferNotPending) {
		t.Fatalf("sin pendiente: %v", err)
	}
	_, _ = e.svc.Dispatch(context.Background(), domain.InitiateGuardianshipTransfer{PetID: tPet, ToEmail: "beto@example.com"})
	if _, err := e.svc.Dispatch(context.Background(), domain.CancelGuardianshipTransfer{PetID: tPet}); err != nil {
		t.Fatal(err)
	}
}

func TestCareLink_I5_OrganizationLookup(t *testing.T) {
	e := newPetEnv(nil, ErrOrganizationNotEligible)
	if _, err := e.svc.Dispatch(context.Background(), domain.GrantCareLink{PetID: tPet, OrganizationID: tOrg, ActorID: tGuard}); !errors.Is(err, domain.ErrOrganizationNotEligible) {
		t.Fatalf("I5: %v", err)
	}
	infra := errors.New("db caída")
	e = newPetEnv(nil, infra)
	if _, err := e.svc.Dispatch(context.Background(), domain.GrantCareLink{PetID: tPet, OrganizationID: tOrg, ActorID: tGuard}); !errors.Is(err, infra) {
		t.Fatalf("error de infraestructura no debe disfrazarse de I5: %v", err)
	}
	e = newPetEnv(nil, nil)
	res, err := e.svc.Dispatch(context.Background(), domain.GrantCareLink{PetID: tPet, OrganizationID: tOrg, ActorID: tGuard})
	if err != nil || res.Version != 2 {
		t.Fatalf("grant: %v %+v", err, res)
	}
	res, err = e.svc.Dispatch(context.Background(), domain.GrantCareLink{PetID: tPet, OrganizationID: tOrg, ActorID: tGuard})
	if err != nil || res.Version != 0 {
		t.Fatalf("grant idempotente (sin evento): %v %+v", err, res)
	}
	if _, err := e.svc.Dispatch(context.Background(), domain.RevokeCareLink{PetID: tPet, OrganizationID: tOrg, ActorID: tGuard}); err != nil {
		t.Fatal(err)
	}
}
