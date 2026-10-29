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

func newSuiteClinicalHandlers(idem *idempotency.Store, logger *slog.Logger) *clinicalhttp.ClinicalHandlers {
	return clinicalhttp.NewClinicalHandlers(clinicalhttp.Deps{
		Commands: fakeClinicalCommands{}, Queries: fakeClinicalQueries{}, Pets: fakeClinicalPets{},
		Documents: fakeDocuments{}, Auditor: fakeAuditor{}, Idem: idem, Logger: logger,
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
}

func init() {
	for k, v := range clinicalMatrix {
		matrix[k] = v
	}
}
