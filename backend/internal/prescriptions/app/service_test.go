package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type mockRepo struct {
	prescriptions map[uuid.UUID]*domain.Prescription
	savedEvents   map[uuid.UUID][]eventstore.StoredEvent
	contentHashes map[uuid.UUID]string
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		prescriptions: make(map[uuid.UUID]*domain.Prescription),
		savedEvents:   make(map[uuid.UUID][]eventstore.StoredEvent),
		contentHashes: make(map[uuid.UUID]string),
	}
}

func (m *mockRepo) Load(ctx context.Context, id uuid.UUID) (*domain.Prescription, error) {
	rx, ok := m.prescriptions[id]
	if !ok {
		return nil, domain.ErrPrescriptionNotFound
	}
	return rx, nil
}

func (m *mockRepo) Save(ctx context.Context, p *domain.Prescription, meta eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	m.prescriptions[p.ID] = p
	var stored []eventstore.StoredEvent
	for _, e := range p.UncommittedEvents() {
		ev, _ := e.(eventstore.Event)
		stored = append(stored, eventstore.StoredEvent{
			EventID:       uuid.New(),
			StreamID:      p.ID,
			StreamVersion: p.Version,
			Type:          ev.EventName(),
			SchemaVersion: ev.SchemaVersion(),
		})
	}
	p.MarkCommitted()
	m.savedEvents[p.ID] = append(m.savedEvents[p.ID], stored...)
	return stored, nil
}

func (m *mockRepo) ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error) {
	if h, ok := m.contentHashes[id]; ok {
		return h, nil
	}
	return "hash_test_12345", nil
}

type mockUoW struct{}

func (mockUoW) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type mockRules struct{}

func (mockRules) ForCountry(ctx context.Context, country string) (*domain.RegulatoryRule, error) {
	return &domain.RegulatoryRule{
		Country:                        country,
		DefaultValidityDays:            30,
		MaxValidityDays:                180,
		MaxRefills:                     5,
		ControlledOrderable:            false,
		RequiresCredentialVerification: true,
	}, nil
}

type mockCredentials struct {
	verified map[uuid.UUID]*domain.CredentialRef
}

func (m *mockCredentials) Verified(ctx context.Context, userID uuid.UUID, country string) (*domain.CredentialRef, bool, error) {
	if ref, ok := m.verified[userID]; ok {
		return ref, true, nil
	}
	return nil, false, nil
}

type mockAccessChecker struct {
	allowed map[string]bool
}

func (m *mockAccessChecker) HasActiveAccess(ctx context.Context, orgID uuid.UUID, petID uuid.UUID) (bool, error) {
	return m.allowed[orgID.String()+":"+petID.String()], nil
}

type mockPlanReader struct {
	items map[uuid.UUID][]domain.EncounterPlanItem
}

func (m *mockPlanReader) SignedPlanItems(ctx context.Context, encounterID uuid.UUID, petID uuid.UUID, orgID uuid.UUID) ([]domain.EncounterPlanItem, bool, error) {
	if items, ok := m.items[encounterID]; ok {
		return items, true, nil
	}
	return nil, false, nil
}

type mockPetReader struct{}

func (mockPetReader) GetPetInfo(ctx context.Context, petID uuid.UUID) (string, string, uuid.UUID, error) {
	return "Toby", "Canino", uuid.New(), nil
}

func samplePrescriptionItem(catID *uuid.UUID, name string) domain.PrescriptionItem {
	dur := 7
	return domain.PrescriptionItem{
		ItemID:       uuid.New().String(),
		CatalogID:    catID,
		NameSnapshot: name,
		Dose: domain.DoseVO{
			Value: 500,
			Unit:  "mg",
		},
		Route:        "oral",
		Frequency:    "cada 12 horas",
		DurationDays: &dur,
		QuantityToDispense: domain.QuantityVO{
			Value: 1,
			Unit:  "pack",
		},
		SubstitutionAllowed: true,
		Instructions:        "Tomar después de las comidas",
	}
}

func TestService_DraftAndIssuePrescription(t *testing.T) {
	repo := newMockRepo()
	uow := mockUoW{}
	rules := mockRules{}
	creds := &mockCredentials{verified: make(map[uuid.UUID]*domain.CredentialRef)}
	access := &mockAccessChecker{allowed: make(map[string]bool)}

	vetID := uuid.New()
	orgID := uuid.New()
	petID := uuid.New()
	access.allowed[orgID.String()+":"+petID.String()] = true

	svc := NewService(Options{
		Repo:          repo,
		UoW:           uow,
		Rules:         rules,
		Credentials:   creds,
		AccessChecker: access,
		PetReader:     mockPetReader{},
	})

	ctx := context.Background()
	item := samplePrescriptionItem(nil, "Amoxicilina 500mg")
	item.FreeText = &item.NameSnapshot

	// 1. Fallo por credencial requerida (R1)
	cmd := DraftIssuedPrescriptionCmd{
		PetID:          petID,
		IssuerOrgID:    orgID,
		Country:        "CO",
		Items:          []domain.PrescriptionItem{item},
		RefillsAllowed: 1,
	}
	_, err := svc.DraftIssuedPrescription(ctx, cmd, vetID)
	if err != domain.ErrCredentialRequired {
		t.Fatalf("expected ErrCredentialRequired, got: %v", err)
	}

	// 2. Configurar credencial y emitir borrador
	creds.verified[vetID] = &domain.CredentialRef{
		Country:        "CO",
		RegistryNumber: "VET-123",
		VerifiedAt:     time.Now(),
	}

	rxID, err := svc.DraftIssuedPrescription(ctx, cmd, vetID)
	if err != nil {
		t.Fatalf("draft failed: %v", err)
	}
	if rxID == uuid.Nil {
		t.Fatalf("expected valid rx id")
	}

	rx, err := repo.Load(ctx, rxID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if rx.Status != domain.StatusDraft {
		t.Errorf("expected status draft, got: %v", rx.Status)
	}

	// 3. Update Draft
	item2 := samplePrescriptionItem(nil, "Ibuprofeno Vet 100mg")
	item2.FreeText = &item2.NameSnapshot
	updCmd := UpdateDraftCmd{
		PrescriptionID: rxID,
		Items:          []domain.PrescriptionItem{item, item2},
		RefillsAllowed: 2,
	}
	if err := svc.UpdateDraft(ctx, updCmd, vetID); err != nil {
		t.Fatalf("update draft failed: %v", err)
	}

	// 4. Issue Prescription
	issueCmd := IssuePrescriptionCmd{
		PrescriptionID: rxID,
		SignerName:     "Dr. Veterinario",
	}
	issuedID, err := svc.IssuePrescription(ctx, issueCmd, vetID)
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}
	if issuedID != rxID {
		t.Errorf("mismatched issued id")
	}

	rxIssued, _ := repo.Load(ctx, rxID)
	if rxIssued.Status != domain.StatusIssued {
		t.Errorf("expected status issued, got: %v", rxIssued.Status)
	}
	if rxIssued.Signature == nil || rxIssued.Signature.ContentHash == "" {
		t.Errorf("expected valid signature and content hash")
	}

	// 5. Revocar receta
	revokeCmd := RevokePrescriptionCmd{
		PrescriptionID: rxID,
		Reason:         "Diagnóstico actualizado tras ecografía",
	}
	if err := svc.RevokePrescription(ctx, revokeCmd, vetID); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	rxRevoked, _ := repo.Load(ctx, rxID)
	if rxRevoked.Status != domain.StatusRevoked {
		t.Errorf("expected status revoked, got: %v", rxRevoked.Status)
	}
}

func TestService_ImportFromEncounterPlan(t *testing.T) {
	repo := newMockRepo()
	uow := mockUoW{}
	rules := mockRules{}
	creds := &mockCredentials{verified: make(map[uuid.UUID]*domain.CredentialRef)}
	access := &mockAccessChecker{allowed: make(map[string]bool)}
	planReader := &mockPlanReader{items: make(map[uuid.UUID][]domain.EncounterPlanItem)}

	vetID := uuid.New()
	orgID := uuid.New()
	petID := uuid.New()
	encID := uuid.New()
	access.allowed[orgID.String()+":"+petID.String()] = true
	creds.verified[vetID] = &domain.CredentialRef{Country: "CO", RegistryNumber: "VET-123"}

	dur := 10
	planReader.items[encID] = []domain.EncounterPlanItem{
		{
			Name:         "Meloxicam 1mg",
			DoseValue:    1.0,
			DoseUnit:     "mg",
			Route:        "oral",
			Frequency:    "cada 24 horas",
			DurationDays: &dur,
			Instructions: "Dar con comida",
		},
	}

	svc := NewService(Options{
		Repo:          repo,
		UoW:           uow,
		Rules:         rules,
		Credentials:   creds,
		AccessChecker: access,
		PlanReader:    planReader,
		PetReader:     mockPetReader{},
	})

	ctx := context.Background()
	cmd := DraftIssuedPrescriptionCmd{
		PetID:       petID,
		IssuerOrgID: orgID,
		Country:     "CO",
		EncounterID: &encID,
		Items:       nil, // Vacío: se importa del plan firmado (PP-F4-DOM-013)
	}

	rxID, err := svc.DraftIssuedPrescription(ctx, cmd, vetID)
	if err != nil {
		t.Fatalf("draft from plan failed: %v", err)
	}

	rx, _ := repo.Load(ctx, rxID)
	if len(rx.Items) != 1 {
		t.Fatalf("expected 1 imported item, got %d", len(rx.Items))
	}
	if rx.Items[0].NameSnapshot != "Meloxicam 1mg" {
		t.Errorf("expected item Meloxicam 1mg, got: %s", rx.Items[0].NameSnapshot)
	}
}

func TestService_ExternalPrescriptionLifecycle(t *testing.T) {
	repo := newMockRepo()
	uow := mockUoW{}
	rules := mockRules{}

	svc := NewService(Options{
		Repo:      repo,
		UoW:       uow,
		Rules:     rules,
		PetReader: mockPetReader{},
	})

	ctx := context.Background()
	petID := uuid.New()
	guardianID := uuid.New()

	// 1. Upload external
	uploadCmd := UploadExternalPrescriptionCmd{
		PetID:   petID,
		Country: "CO",
		Images: []domain.ImageRef{
			{Page: 1, ObjectKey: "rx/page1.jpg", ContentType: "image/jpeg"},
		},
	}
	rxID, err := svc.UploadExternalPrescription(ctx, uploadCmd, guardianID)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	rx, _ := repo.Load(ctx, rxID)
	if rx.Status != domain.StatusDraft || rx.Source != domain.SourceExternal {
		t.Errorf("expected draft external rx")
	}

	// 2. Confirm external
	item := samplePrescriptionItem(nil, "Doxiciclina 100mg")
	item.FreeText = &item.NameSnapshot
	confirmCmd := ConfirmExternalPrescriptionCmd{
		PrescriptionID: rxID,
		Items:          []domain.PrescriptionItem{item},
		ValidUntil:     kernel.CivilDateOf(time.Now()).AddDays(30),
		RefillsAllowed: 1,
		Notes:          "Confirmada por el tutor",
	}
	if err := svc.ConfirmExternalPrescription(ctx, confirmCmd, guardianID); err != nil {
		t.Fatalf("confirm external failed: %v", err)
	}

	rxConfirmed, _ := repo.Load(ctx, rxID)
	if rxConfirmed.Status != domain.StatusConfirmed {
		t.Errorf("expected confirmed rx, got: %v", rxConfirmed.Status)
	}
}

func TestService_DispensationFlow(t *testing.T) {
	repo := newMockRepo()
	uow := mockUoW{}
	rules := mockRules{}
	creds := &mockCredentials{verified: make(map[uuid.UUID]*domain.CredentialRef)}
	access := &mockAccessChecker{allowed: make(map[string]bool)}

	vetID := uuid.New()
	orgID := uuid.New()
	petID := uuid.New()
	access.allowed[orgID.String()+":"+petID.String()] = true
	creds.verified[vetID] = &domain.CredentialRef{Country: "CO", RegistryNumber: "VET-123"}

	svc := NewService(Options{
		Repo:          repo,
		UoW:           uow,
		Rules:         rules,
		Credentials:   creds,
		AccessChecker: access,
		PetReader:     mockPetReader{},
	})

	ctx := context.Background()
	item := samplePrescriptionItem(nil, "Prednisolona 20mg")
	item.FreeText = &item.NameSnapshot
	item.QuantityToDispense = domain.QuantityVO{Value: 2, Unit: "pack"}

	rxID, _ := svc.DraftIssuedPrescription(ctx, DraftIssuedPrescriptionCmd{
		PetID:          petID,
		IssuerOrgID:    orgID,
		Country:        "CO",
		Items:          []domain.PrescriptionItem{item},
		RefillsAllowed: 0,
	}, vetID)

	_, _ = svc.IssuePrescription(ctx, IssuePrescriptionCmd{PrescriptionID: rxID}, vetID)

	// Dispensación parcial
	rx, _ := repo.Load(ctx, rxID)
	itemID := rx.Items[0].ItemID

	dispCmd := RecordDispensationCmd{
		PrescriptionID: rxID,
		PharmacyName:   "Farmapet",
		Lines: []domain.DispensationLine{
			{ItemID: itemID, Qty: 1},
		},
	}
	if err := svc.RecordDispensation(ctx, dispCmd, uuid.New()); err != nil {
		t.Fatalf("dispensation failed: %v", err)
	}

	rxDisp, _ := repo.Load(ctx, rxID)
	if rxDisp.ItemsRemaining[itemID] != 1 {
		t.Errorf("expected 1 remaining, got: %v", rxDisp.ItemsRemaining[itemID])
	}
	if rxDisp.Status != domain.StatusIssued {
		t.Errorf("expected status issued after partial dispensation")
	}

	// Dispensar el resto -> Agotada
	dispCmd2 := RecordDispensationCmd{
		PrescriptionID: rxID,
		PharmacyName:   "Farmapet",
		Lines: []domain.DispensationLine{
			{ItemID: itemID, Qty: 1},
		},
	}
	if err := svc.RecordDispensation(ctx, dispCmd2, uuid.New()); err != nil {
		t.Fatalf("second dispensation failed: %v", err)
	}

	rxExhausted, _ := repo.Load(ctx, rxID)
	if rxExhausted.Status != domain.StatusExhausted {
		t.Errorf("expected status exhausted, got: %v", rxExhausted.Status)
	}
}
