package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type mockProvQueries struct {
	pets        map[uuid.UUID]PetView
	claims      map[string]ClaimView
	petClaims   map[uuid.UUID]ClaimView
	provList    []ProvisionalPetView
	matchingPet *MatchingPetView
}

func (m *mockProvQueries) ListPets(context.Context, uuid.UUID, string, PetCursor, int) ([]PetListItem, *PetCursor, error) {
	return nil, nil, nil
}

func (m *mockProvQueries) GetPet(_ context.Context, petID uuid.UUID) (PetView, error) {
	p, ok := m.pets[petID]
	if !ok {
		return PetView{}, &kernel.NotFoundError{Resource: "pet"}
	}
	return p, nil
}

func (m *mockProvQueries) ListPatients(context.Context, uuid.UUID, string, string, PatientCursor, int) ([]PatientRow, *PatientCursor, error) {
	return nil, nil, nil
}

func (m *mockProvQueries) TransferByTokenHash(context.Context, []byte, time.Time) (TransferView, error) {
	return TransferView{}, nil
}

func (m *mockProvQueries) ClaimByTokenHash(_ context.Context, hash []byte, _ time.Time) (ClaimView, error) {
	c, ok := m.claims[string(hash)]
	if !ok {
		return ClaimView{}, &kernel.NotFoundError{Resource: "claim"}
	}
	return c, nil
}

func (m *mockProvQueries) ClaimByPetID(_ context.Context, petID uuid.UUID) (ClaimView, error) {
	c, ok := m.petClaims[petID]
	if !ok {
		return ClaimView{}, &kernel.NotFoundError{Resource: "claim"}
	}
	return c, nil
}

func (m *mockProvQueries) ListProvisionalPets(_ context.Context, _ uuid.UUID, _ string) ([]ProvisionalPetView, error) {
	return m.provList, nil
}

func (m *mockProvQueries) FindMatchingPetByMicrochip(_ context.Context, _ uuid.UUID, _ string) (*MatchingPetView, error) {
	return m.matchingPet, nil
}

type noopProjection struct{}

func (noopProjection) Apply(context.Context, []eventstore.StoredEvent) error { return nil }

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

func setupProvService(now time.Time) (*Service, *memPetRepo, *mockProvQueries) {
	repo := &memPetRepo{streams: make(map[uuid.UUID][]any)}
	queries := &mockProvQueries{
		pets:      make(map[uuid.UUID]PetView),
		claims:    make(map[string]ClaimView),
		petClaims: make(map[uuid.UUID]ClaimView),
	}

	b := bus.New()
	svc := NewService(Options{
		Repo:     repo,
		UoW:      txDirect{},
		Projects: noopProjection{},
		Queries:  queries,
		Clock:    testClock{now: now},
	})
	svc.RegisterCommands(b)
	return svc, repo, queries
}

func TestProvisionalPet_FullFlow(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc, repo, mq := setupProvService(now)

	orgID := uuid.MustParse("01930000-0000-7000-8000-000000000001")
	vetID := uuid.MustParse("01930000-0000-7000-8000-000000000002")
	tutorID := uuid.MustParse("01930000-0000-7000-8000-000000000003")
	tutorEmail := "tutor@example.com"

	ctx := WithCommandContext(context.Background(), CommandContext{
		Principal: PrincipalInfo{UserID: vetID},
	})

	// 1. Registrar mascota provisional
	cmd := domain.RegisterProvisionalPet{
		PetID:         uuid.MustParse("01930000-0000-7000-8000-000000000010"),
		OrgID:         orgID,
		CreatedBy:     vetID,
		Name:          "Max",
		Species:       "dog",
		Sex:           "male",
		Neutered:      "no",
		GuardianEmail: tutorEmail,
		Now:           now,
	}

	res, expiresAt, token, err := svc.RegisterProvisional(ctx, cmd, true)
	if err != nil {
		t.Fatalf("RegisterProvisional falló: %v", err)
	}
	if res.AggregateID != cmd.PetID {
		t.Fatalf("AggregateID no coincide: got %s, want %s", res.AggregateID, cmd.PetID)
	}
	if expiresAt == nil || token == "" {
		t.Fatalf("esperaba token y expiresAt, got token=%q, exp=%v", token, expiresAt)
	}

	// Verificar stream de eventos
	stream := repo.streams[cmd.PetID]
	if len(stream) != 2 {
		t.Fatalf("esperaba 2 eventos (ProvisionalPetRegistered + CareLinkGranted), got %d", len(stream))
	}
	if _, ok := stream[0].(domain.ProvisionalPetRegistered); !ok {
		t.Fatalf("evento 0 no es ProvisionalPetRegistered: %T", stream[0])
	}
	if _, ok := stream[1].(domain.CareLinkGranted); !ok {
		t.Fatalf("evento 1 no es CareLinkGranted: %T", stream[1])
	}

	// 2. Simular lectura en read model y preview
	tokenHash := kernel.HashToken(token)
	mq.claims[string(tokenHash)] = ClaimView{
		TokenHash:      tokenHash,
		PetID:          cmd.PetID,
		OrganizationID: orgID,
		ContactHash:    hashContact(tutorEmail),
		ContactKind:    "email",
		Status:         "pending",
		ExpiresAt:      *expiresAt,
		CreatedAt:      now,
	}
	mq.petClaims[cmd.PetID] = mq.claims[string(tokenHash)]
	mq.pets[cmd.PetID] = PetView{
		ID:          cmd.PetID,
		Name:        "Max",
		Species:     "dog",
		Sex:         "male",
		Provisional: true,
	}

	preview, err := svc.GetClaimPreview(ctx, token, tutorID)
	if err != nil {
		t.Fatalf("GetClaimPreview falló: %v", err)
	}
	if preview.Pet.Name != "Max" {
		t.Fatalf("preview pet name = %s, want Max", preview.Pet.Name)
	}

	// 3. Reenviar invitación (ResendClaim)
	if err := svc.ResendClaim(ctx, orgID, cmd.PetID); err != nil {
		t.Fatalf("ResendClaim falló: %v", err)
	}

	// 4. Aceptar reclamación como tutor autenticado (modo directo)
	tutorCtx := WithCommandContext(context.Background(), CommandContext{
		Principal: PrincipalInfo{UserID: tutorID, Email: tutorEmail},
	})

	claimRes, err := svc.AcceptClaim(tutorCtx, token, nil)
	if err != nil {
		t.Fatalf("AcceptClaim falló: %v", err)
	}
	if claimRes.AggregateID != cmd.PetID {
		t.Fatalf("AggregateID no coincide: got %s", claimRes.AggregateID)
	}

	// Verificar evento PetClaimed en el agregado
	stream = repo.streams[cmd.PetID]
	var foundClaimed bool
	for _, e := range stream {
		if c, ok := e.(domain.PetClaimed); ok {
			foundClaimed = true
			if c.GuardianID != tutorID {
				t.Fatalf("GuardianID reclamado incorrecto: %s", c.GuardianID)
			}
		}
	}
	if !foundClaimed {
		t.Fatalf("no se encontró evento PetClaimed en el stream")
	}

	// 5. Test importación CSV
	csvData := `name,species,contact,breed,microchip
Luna,cat,luna@example.com,Siames,123456789012345
Thor,dog,thor@example.com,Labrador,987654321098765
`
	importRes, err := svc.ImportProvisionalPetsCSV(ctx, orgID, vetID, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ImportProvisionalPetsCSV falló: %v", err)
	}
	if importRes.Total != 2 || importRes.Created != 2 {
		t.Fatalf("esperaba 2 creados de 2, got total=%d, created=%d, errs=%v", importRes.Total, importRes.Created, importRes.Errors)
	}
}
