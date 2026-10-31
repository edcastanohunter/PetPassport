package main

// Fixture de Clinical Records para la suite por rutas (PP-F2-SEC-001): un
// borrador de orgActive sobre la mascota del tutor (vínculo activo con
// orgActive), con vet_linked como responsable. Los comandos fallan tras authz
// (fakes): lo que se prueba es la decisión y su traducción a 403/404.

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	clinicalapp "github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	clinicaldomain "github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	encID     = uuid.MustParse("01930000-0000-7000-8000-00000000e001")
	attID     = uuid.MustParse("01930000-0000-7000-8000-00000000e002")
	jobID     = uuid.MustParse("01930000-0000-7000-8000-00000000e003")
	vetLinked = uuid.NewSHA1(uuid.NameSpaceOID, []byte("vet_linked"))
)

var errSuite = errors.New("no disponible en la suite")

type fakeClinicalCommands struct{}

func (fakeClinicalCommands) Dispatch(context.Context, bus.Command) (bus.Result, error) {
	return bus.Result{}, errSuite
}

func (fakeClinicalCommands) VerifyEncounter(context.Context, uuid.UUID, string) (clinicalapp.VerifyView, error) {
	return clinicalapp.VerifyView{}, errSuite
}

type fakeClinicalQueries struct{}

func (fakeClinicalQueries) ListByOrg(context.Context, clinicalapp.EncounterFilter) ([]clinicalapp.EncounterListItem, *clinicalapp.Cursor, error) {
	return nil, nil, nil
}

func (fakeClinicalQueries) ListByPet(context.Context, uuid.UUID, []uuid.UUID, bool, clinicalapp.Cursor, int) ([]clinicalapp.EncounterListItem, *clinicalapp.Cursor, error) {
	return nil, nil, nil
}

func (fakeClinicalQueries) Get(_ context.Context, id uuid.UUID) (clinicalapp.EncounterView, error) {
	if id != encID {
		return clinicalapp.EncounterView{}, &kernel.NotFoundError{Resource: "encounter"}
	}
	vet := vetLinked
	now := time.Now()
	return clinicalapp.EncounterView{ID: encID, Version: 1, PetID: petID, OrganizationID: orgActive, Type: "consultation",
		Status: "draft", StartedAt: now, Reason: "control", AttendingVetID: &vet, LastActivityAt: now, CreatedAt: now, UpdatedAt: now}, nil
}

func (fakeClinicalQueries) Attachments(context.Context, uuid.UUID) ([]clinicalapp.AttachmentView, error) {
	return []clinicalapp.AttachmentView{{ID: attID, ContentType: "pdf", SizeBytes: 10, VisibleToGuardian: true}}, nil
}

func (fakeClinicalQueries) WeightHistory(context.Context, uuid.UUID, []uuid.UUID) ([]clinicalapp.WeightPoint, *clinicalapp.WeightTrend, error) {
	return nil, nil, nil
}

func (fakeClinicalQueries) Timeline(context.Context, clinicalapp.TimelineFilter) ([]clinicalapp.TimelineItem, *clinicalapp.Cursor, error) {
	return nil, nil, nil
}

func (fakeClinicalQueries) Summary(context.Context, uuid.UUID, []uuid.UUID) (clinicalapp.SummaryView, error) {
	return clinicalapp.SummaryView{}, nil
}

func (fakeClinicalQueries) Access(_ context.Context, id uuid.UUID) (clinicalapp.EncounterAccess, error) {
	if id != encID {
		return clinicalapp.EncounterAccess{}, &kernel.NotFoundError{Resource: "encounter"}
	}
	vet := vetLinked
	return clinicalapp.EncounterAccess{ID: encID, PetID: petID, OrganizationID: orgActive, Status: "draft",
		AttendingVetID: &vet, GuardianID: tutorID}, nil
}

// fakeClinicalPets: vínculo activo solo con orgActive (orgSusp está
// suspendida: no cuenta), sin custodias.
type fakeClinicalPets struct{}

func (fakeClinicalPets) CanOrgAccess(_ context.Context, _ uuid.UUID, org uuid.UUID) (clinicalapp.AccessLevel, error) {
	if org == orgActive {
		return clinicalapp.AccessLinked, nil
	}
	return clinicalapp.AccessNone, nil
}

func (fakeClinicalPets) GuardianOf(context.Context, uuid.UUID) (uuid.UUID, error) {
	return tutorID, nil
}

func (fakeClinicalPets) PetSummary(context.Context, uuid.UUID) (clinicalapp.PetSummary, error) {
	return clinicalapp.PetSummary{PetID: petID, Name: "Luna", Species: "dog"}, nil
}

func (fakeClinicalPets) LinkedOrgIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{orgActive}, nil
}

func (fakeClinicalPets) CustodyOrgIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (fakeClinicalPets) Alerts(context.Context, uuid.UUID) ([]string, error) { return nil, nil }

type fakeDocuments struct{}

func (fakeDocuments) Job(context.Context, uuid.UUID) (clinicalapp.DocumentJobView, error) {
	return clinicalapp.DocumentJobView{ID: jobID, Kind: clinicalapp.DocumentJobKindEncounterPDF, RefID: encID, Version: 1, Status: "pending"}, nil
}

func (fakeDocuments) LatestForRef(context.Context, string, uuid.UUID) (clinicalapp.DocumentJobView, error) {
	return clinicalapp.DocumentJobView{ID: jobID, Kind: clinicalapp.DocumentJobKindEncounterPDF, RefID: encID, Version: 1, Status: "pending"}, nil
}

type fakeAuditor struct{}

func (fakeAuditor) RecordRead(context.Context, clinicalapp.ReadAudit) error { return nil }

// Carnet (M2.2): los comandos y el catálogo fallan o devuelven vacío tras authz.
type fakeCardCommands struct{}

func (fakeCardCommands) Dispatch(context.Context, bus.Command) (bus.Result, error) {
	return bus.Result{}, errSuite
}

func (fakeCardCommands) Propose(context.Context, uuid.UUID, clinicaldomain.EntryInput) (clinicaldomain.Proposal, error) {
	return clinicaldomain.Proposal{}, errSuite
}

type fakeCardQueries struct{}

func (fakeCardQueries) Card(_ context.Context, id uuid.UUID, _ bool) (clinicalapp.CardView, error) {
	return clinicalapp.CardView{PetID: id, Species: "dog"}, nil
}

func (fakeCardQueries) Entry(_ context.Context, _, id uuid.UUID) (clinicalapp.CardEntryView, error) {
	return clinicalapp.CardEntryView{ID: id, Kind: "vaccine", ProductName: "Rabia", AdministeredAt: "2026-10-01",
		Source: "recorded", Verification: "verified", Status: "active"}, nil
}

type fakeCatalog struct{}

func (fakeCatalog) Vaccine(context.Context, uuid.UUID) (clinicalapp.VaccineRef, error) {
	return clinicalapp.VaccineRef{}, errSuite
}

func (fakeCatalog) Vaccines(context.Context, string, string) ([]clinicalapp.VaccineRef, error) {
	return nil, nil
}

func (fakeCatalog) Protocol(context.Context, string, string, uuid.UUID) (*clinicalapp.ProtocolRef, error) {
	return nil, nil
}

func (fakeCatalog) Protocols(context.Context, string, string) ([]clinicalapp.ProtocolRef, error) {
	return nil, nil
}

type fakeCardDocs struct{}

func (fakeCardDocs) EnsureCardPDF(context.Context, uuid.UUID) (clinicalapp.DocumentJobView, error) {
	return clinicalapp.DocumentJobView{ID: uuid.New(), Status: "pending"}, nil
}

type fakeShares struct{}

func (fakeShares) Create(context.Context, clinicalapp.ShareRecord) error { return errSuite }
func (fakeShares) List(context.Context, uuid.UUID) ([]clinicalapp.ShareView, error) {
	return nil, nil
}
func (fakeShares) Revoke(context.Context, uuid.UUID, uuid.UUID, time.Time) error { return errSuite }
func (fakeShares) ByTokenHash(context.Context, []byte) (clinicalapp.ShareRecord, error) {
	return clinicalapp.ShareRecord{}, &kernel.NotFoundError{Resource: "share"}
}
func (fakeShares) RecordAccess(context.Context, uuid.UUID, time.Time) error { return nil }
func (fakeShares) PinFailed(context.Context, uuid.UUID, time.Time, int) (bool, error) {
	return false, nil
}
func (fakeShares) PinOK(context.Context, uuid.UUID) error { return nil }

func newSuiteClinicalHandlers(idem *idempotency.Store, logger *slog.Logger) *clinicalhttp.ClinicalHandlers {
	return clinicalhttp.NewClinicalHandlers(clinicalhttp.Deps{
		Commands: fakeClinicalCommands{}, Queries: fakeClinicalQueries{}, Pets: fakeClinicalPets{},
		Documents: fakeDocuments{}, Auditor: fakeAuditor{}, Idem: idem, Logger: logger,
		CardDeps: clinicalhttp.CardDeps{
			Card: fakeCardCommands{}, CardQueries: fakeCardQueries{}, Catalog: fakeCatalog{},
			Shares:   clinicalapp.NewShareService(fakeShares{}, fakeCardQueries{}, nil, nil, nil),
			CardDocs: fakeCardDocs{},
		},
	})
}

// Filas de Clinical (SEC-001, docs/phase-2/08 §2) sobre el borrador del
// fixture. Columnas: tutor vetL admL recL vetU vetS phAd str sup.
var clinicalMatrix = map[string][9]int{
	"listOrgEncounters": {403, ok, ok, ok, 403, 403, 403, 403, ok},
	"openEncounter":     {403, ok, ok, ok, 403, 403, 403, 403, 403},
	// El tutor no ve borradores; super_admin lee (auditado).
	"getEncounter":           {404, ok, ok, ok, 404, 404, 404, 404, ok},
	"getEncounterAttachment": {404, ok, ok, ok, 404, 404, 404, 404, ok},
	"getEncounterPdf":        {404, ok, ok, ok, 404, 404, 404, 404, ok},
	"getDocumentJob":         {404, ok, ok, ok, 404, 404, 404, 404, ok},
	// encounter:edit — cualquier rol clínico vinculado; super_admin ve pero no edita.
	"updateEncounter":           {404, ok, ok, ok, 404, 404, 404, 404, 403},
	"putEncounterVitals":        {404, ok, ok, ok, 404, 404, 404, 404, 403},
	"uploadEncounterAttachment": {404, ok, ok, ok, 404, 404, 404, 404, 403},
	"deleteEncounterAttachment": {404, ok, ok, ok, 404, 404, 404, 404, 403},
	// encounter:clinical_edit — solo veterinario (E4).
	"putEncounterDiagnoses":            {404, ok, 403, 403, 404, 404, 404, 404, 403},
	"putEncounterPlan":                 {404, ok, 403, 403, 404, 404, 404, 404, 403},
	"putEncounterFollowUp":             {404, ok, 403, 403, 404, 404, 404, 404, 403},
	"deleteEncounterFollowUp":          {404, ok, 403, 403, 404, 404, 404, 404, 403},
	"setEncounterAttachmentVisibility": {404, ok, 403, 403, 404, 404, 404, 404, 403},
	// encounter:sign — veterinario responsable (firma y adendas).
	"signEncounter":        {404, ok, 403, 403, 404, 404, 404, 404, 403},
	"addEncounterAddendum": {404, ok, 403, 403, 404, 404, 404, 404, 403},
	// encounter:discard — admin de la clínica, responsable o super_admin.
	"discardEncounter": {404, ok, ok, 403, 404, 404, 404, 404, ok},
	// record:read — tutor, clínicas vinculadas (activas) y super_admin.
	"getPetTimeline":      {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"listPetEncounters":   {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"getPetWeightHistory": {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"getPetSummary":       {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	// Carnet (M2.2). card:record / card:verify — clínica vinculada; el tutor
	// y super_admin ven pero no escriben; card:void también admite al tutor
	// (el dominio decide, V5) y a super_admin; card:declare y share:manage —
	// solo el tutor.
	"getPetCard":             {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"getPetCardPdf":          {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"recordCardEntry":        {403, ok, ok, ok, 404, 404, 404, 404, 403},
	"adjustCardNextDue":      {403, ok, ok, ok, 404, 404, 404, 404, 403},
	"getCardNextDueProposal": {403, ok, ok, ok, 404, 404, 404, 404, 403},
	"verifyCardEntry":        {403, ok, ok, ok, 404, 404, 404, 404, 403},
	"disputeCardEntry":       {403, ok, ok, ok, 404, 404, 404, 404, 403},
	"voidCardEntry":          {ok, ok, ok, ok, 404, 404, 404, 404, ok},
	"declareCardEntry":       {ok, 403, 403, 403, 404, 404, 404, 404, 403},
	"listPetShares":          {ok, 403, 403, 403, 404, 404, 404, 404, 403},
	"createPetShare":         {ok, 403, 403, 403, 404, 404, 404, 404, 403},
	"revokePetShare":         {ok, 403, 403, 403, 404, 404, 404, 404, 403},
	// Atajo desde la consulta: encounter:edit sobre el borrador del fixture.
	"recordEncounterVaccination": {404, ok, ok, ok, 404, 404, 404, 404, 403},
	// Catálogos: lectura para cualquier autenticado; escritura solo super_admin.
	"listVaccineCatalog":            {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"listVaccinationProtocols":      {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"createVaccine":                 {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"updateVaccine":                 {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"deactivateVaccine":             {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"createVaccinationProtocol":     {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"updateVaccinationProtocol":     {403, 403, 403, 403, 403, 403, 403, 403, ok},
	"deactivateVaccinationProtocol": {403, 403, 403, 403, 403, 403, 403, 403, ok},
}

func init() {
	for k, v := range clinicalMatrix {
		matrix[k] = v
	}
}
