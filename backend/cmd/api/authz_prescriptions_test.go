package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identitydomain "github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	prescriptionapp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	prescriptiondomain "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	prescriptionhttp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/http"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var rxID = uuid.MustParse("01930000-0000-7000-8000-00000000e004")

type fakePrescriptionRepo struct{}

func (fakePrescriptionRepo) Load(_ context.Context, id uuid.UUID) (*prescriptiondomain.Prescription, error) {
	rx := prescriptiondomain.NewPrescription()
	rx.ID = id
	rx.PetID = petID
	rx.GuardianID = tutorID
	rx.Country = "CO"
	rx.Status = prescriptiondomain.StatusConfirmed
	return rx, nil
}

func (fakePrescriptionRepo) Save(_ context.Context, _ *prescriptiondomain.Prescription, _ eventstore.Metadata) ([]eventstore.StoredEvent, error) {
	return nil, nil
}

func (fakePrescriptionRepo) ContentHash(_ context.Context, _ uuid.UUID, _ int64) (string, error) {
	return "abc123hash", nil
}

type fakePrescriptionUoW struct{}

func (fakePrescriptionUoW) Do(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakePrescriptionQueries struct{}

func (fakePrescriptionQueries) GetByID(_ context.Context, id uuid.UUID) (*prescriptionapp.ReadPrescriptionDetail, error) {
	now := time.Now()
	vu := now.Add(30 * 24 * time.Hour).Format("2006-01-02")
	return &prescriptionapp.ReadPrescriptionDetail{
		ReadPrescriptionSummary: prescriptionapp.ReadPrescriptionSummary{
			ID:          id,
			Version:     1,
			PetID:       petID,
			GuardianID:  tutorID,
			Country:     "CO",
			Source:      "issued",
			Status:      "issued",
			IssuerID:    &vetLinked,
			IssuerOrgID: &orgActive,
			IssuedAt:    &now,
			ValidUntil:  &vu,
			ItemsCount:  1,
		},
		Items: []prescriptiondomain.PrescriptionItem{
			{
				ItemID:       "item-1",
				NameSnapshot: "Amoxicilina",
				Dose:         prescriptiondomain.DoseVO{Value: 250, Unit: "mg"},
				Route:        "oral",
				Frequency:    "cada 12 horas",
				QuantityToDispense: prescriptiondomain.QuantityVO{
					Value: 1,
					Unit:  "pack",
				},
			},
		},
	}, nil
}

func (fakePrescriptionQueries) ListByOrg(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID, _ *uuid.UUID, _ int) ([]prescriptionapp.ReadPrescriptionSummary, error) {
	return nil, nil
}

func (fakePrescriptionQueries) ListByGuardian(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ string, _ int) ([]prescriptionapp.ReadPrescriptionSummary, error) {
	return nil, nil
}

func (fakePrescriptionQueries) ListByEncounter(_ context.Context, _ uuid.UUID) ([]prescriptionapp.ReadPrescriptionSummary, error) {
	return nil, nil
}

func (fakePrescriptionQueries) Archive(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (fakePrescriptionQueries) RecordVerification(_ context.Context, _ uuid.UUID, _ []byte, _ string) error {
	return nil
}

func (fakePrescriptionQueries) SetPDFObjectKey(_ context.Context, _ uuid.UUID, _ string, _ int) error {
	return nil
}

func (fakePrescriptionQueries) GetImage(_ context.Context, _ uuid.UUID, _ int) (*prescriptiondomain.ImageRef, error) {
	return &prescriptiondomain.ImageRef{
		ObjectKey:   "prescriptions/test.jpg",
		ContentType: "image/jpeg",
		Page:        1,
	}, nil
}

func (fakePrescriptionQueries) AddImage(_ context.Context, _ uuid.UUID, _ prescriptiondomain.ImageRef) error {
	return nil
}

func (fakePrescriptionQueries) GetExtractionJob(_ context.Context, id uuid.UUID) (*prescriptionapp.ExtractionJobDTO, error) {
	return &prescriptionapp.ExtractionJobDTO{
		PrescriptionID: id,
		Status:         "done",
		Attempt:        1,
		Extraction: &prescriptiondomain.Extraction{
			Legibility: 0.95,
			Header:     prescriptiondomain.ExtractedHeader{},
			Items:      []prescriptiondomain.ExtractedItem{},
		},
	}, nil
}

func (fakePrescriptionQueries) FindExpiredPrescriptions(_ context.Context, _ time.Time) ([]uuid.UUID, error) {
	return nil, nil
}

type fakeCredentialCheck struct{}

func (fakeCredentialCheck) Verified(_ context.Context, _ uuid.UUID, country string) (*prescriptiondomain.CredentialRef, bool, error) {
	return &prescriptiondomain.CredentialRef{
		Country:        country,
		RegistryNumber: "TP-12345",
		VerifiedAt:     time.Now(),
	}, true, nil
}

type fakeRegulatoryRules struct{}

func (fakeRegulatoryRules) ForCountry(_ context.Context, country string) (*prescriptiondomain.RegulatoryRule, error) {
	return &prescriptiondomain.RegulatoryRule{
		Country:                        country,
		DefaultValidityDays:            30,
		MaxValidityDays:                180,
		MaxRefills:                     5,
		RequiresCredentialVerification: true,
	}, nil
}

type fakeEncounterPlanReader struct{}

func (fakeEncounterPlanReader) SignedPlanItems(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID) ([]prescriptiondomain.EncounterPlanItem, bool, error) {
	return nil, true, nil
}

type fakePetAccessChecker struct{}

func (fakePetAccessChecker) HasActiveAccess(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	return true, nil
}

type fakeCredentialStore struct{}

func (fakeCredentialStore) ListByUserID(_ context.Context, _ uuid.UUID) ([]identitydomain.ProfessionalCredential, error) {
	return nil, nil
}

func (fakeCredentialStore) GetByID(_ context.Context, id uuid.UUID) (*identitydomain.ProfessionalCredential, error) {
	return &identitydomain.ProfessionalCredential{
		ID:             id,
		UserID:         vetLinked,
		Country:        "CO",
		Kind:           "veterinarian",
		RegistryNumber: "TP-12345",
		Issuer:         "COMVEZCOL",
		IssuedAt:       time.Now(),
		Status:         identitydomain.CredentialStatusVerified,
	}, nil
}

func (fakeCredentialStore) GetVerifiedByUserAndCountry(_ context.Context, _ uuid.UUID, _ string) (*identitydomain.ProfessionalCredential, error) {
	return nil, nil
}

func (fakeCredentialStore) Create(_ context.Context, _ *identitydomain.ProfessionalCredential) error {
	return nil
}

func (fakeCredentialStore) Update(_ context.Context, _ *identitydomain.ProfessionalCredential) error {
	return nil
}

func (fakeCredentialStore) Delete(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (fakeCredentialStore) ListByOrgMembers(_ context.Context, _ []uuid.UUID) ([]identityapp.MemberCredentialSummary, error) {
	return nil, nil
}

func (fakeCredentialStore) ListByOrg(_ context.Context, _ uuid.UUID) ([]identityapp.MemberCredentialSummary, error) {
	return nil, nil
}

func (fakeCredentialStore) ListByStatus(_ context.Context, _ string) ([]identitydomain.ProfessionalCredential, error) {
	return nil, nil
}

type fakeEncounterAccessReader struct{}

func (fakeEncounterAccessReader) GetEncounterAccess(_ context.Context, id uuid.UUID) (*authz.EncounterResource, error) {
	return &authz.EncounterResource{
		ID:             id,
		PetID:          petID,
		GuardianID:     tutorID,
		OrganizationID: orgActive,
		Status:         "signed",
	}, nil
}

type fakePetReader struct{}

func (fakePetReader) GetPetInfo(_ context.Context, _ uuid.UUID) (string, string, uuid.UUID, error) {
	return "Luna", "canine", tutorID, nil
}

type fakeStorage struct{}

func (fakeStorage) Put(_ context.Context, _ string, _ string, _ io.Reader, _ int64) error {
	return nil
}

func (fakeStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (fakeStorage) PresignGet(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://storage.local/presigned-url", nil
}

func newSuitePrescriptionHandlers(logger *slog.Logger) *prescriptionhttp.PrescriptionHandlers {
	queries := fakePrescriptionQueries{}
	svc := prescriptionapp.NewService(prescriptionapp.Options{
		Repo:          fakePrescriptionRepo{},
		UoW:           fakePrescriptionUoW{},
		Queries:       queries,
		Credentials:   fakeCredentialCheck{},
		Rules:         fakeRegulatoryRules{},
		PlanReader:    fakeEncounterPlanReader{},
		AccessChecker: fakePetAccessChecker{},
		PetReader:     fakePetReader{},
		Clock:         kernel.RealClock{},
		Logger:        logger,
	})
	return prescriptionhttp.NewPrescriptionHandlers(svc, fakeStorage{}).
		WithEncounterReader(fakeEncounterAccessReader{}).
		WithPetReader(fakePetReader{})
}

func newSuiteCredentialHandlers(base *identityhttp.Handlers, logger *slog.Logger) *identityhttp.CredentialHandlers {
	store := fakeCredentialStore{}
	svc := identityapp.NewCredentialService(store, logger)
	return identityhttp.NewCredentialHandlers(svc, base)
}

// prescriptionsMatrix: columnas = tutor vetL admL recL vetU vetS phAd str sup.
var prescriptionsMatrix = map[string][9]int{
	// prescription:issue — solo veterinario de clínica activa o super_admin
	"draftPrescription":       {403, ok, 403, 403, 403, 403, 403, 403, ok},
	"updatePrescriptionDraft": {403, ok, 403, 403, 403, 403, 403, 403, ok},
	"issuePrescription":       {403, ok, 403, 403, 403, 403, 403, 403, ok},
	// prescription:revoke — veterinario o admin de la clínica activa o super_admin
	"revokePrescription": {403, ok, ok, 403, 403, 403, 403, 403, ok},
	// prescription:read — tutor de la mascota, miembros de la clínica emisora o super_admin
	"getPrescription":    {ok, ok, ok, ok, 403, 403, 403, 403, ok},
	"getPrescriptionPDF": {ok, ok, ok, ok, 403, 403, 403, 403, ok},
	// prescription:list — miembros de la clínica activa o super_admin
	"listOrgPrescriptions": {403, ok, ok, ok, 403, 403, 403, 403, ok},
	// encounter:read — tutor, miembros de la clínica activa o super_admin
	"listEncounterPrescriptions": {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	// org:read — miembros de la clínica activa o super_admin
	"listOrgMembersCredentials": {403, ok, ok, ok, 403, ok, ok, 403, ok},
	// admin:user:list — solo super_admin
	"listAdminCredentials":  {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"verifyAdminCredential": {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"rejectAdminCredential": {403, 403, 403, 403, 403, 403, 403, 403, ok},

	// prescription:upload (tutor de la mascota o super_admin) (PP-F4-API-003)
	"uploadExternalPrescription":  {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"addPrescriptionImage":        {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"retryPrescriptionExtraction": {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"confirmExternalPrescription": {ok, 403, 403, 403, 403, 403, 403, 403, ok},
	"reopenExternalPrescription":  {ok, 403, 403, 403, 403, 403, 403, 403, ok},

	// prescription:read (tutor de la mascota, miembros vinculados o super_admin) (PP-F4-API-003)
	"getPrescriptionExtraction": {ok, ok, ok, ok, 403, 403, 403, 403, ok},
	"getPrescriptionImage":      {ok, ok, ok, ok, 403, 403, 403, 403, ok},
}

func init() {
	for k, v := range prescriptionsMatrix {
		matrix[k] = v
	}
}
