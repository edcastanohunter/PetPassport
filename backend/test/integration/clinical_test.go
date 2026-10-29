//go:build integration

// Clinical Records end-to-end contra Postgres (PP-F2 M2.1: PRD-001..006,
// DOM-003/005, ARQ-010/011/012, API-001/003/013, SEC-001/004/005): abrir →
// editar → firmar → adenda → PDF → verificación pública; custodia tras
// revocar el vínculo; alteración del read model detectada; proyecciones
// idempotentes.
package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	clinicalapp "github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	clinicaldomain "github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// memObjects es un ObjectStorage en memoria.
type memObjects struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *memObjects) Put(_ context.Context, key, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = b
	return nil
}

func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key, nil
}

// fakeRenderer evita depender del binario typst en CI de integración (el
// render real se prueba en infra cuando typst está disponible).
type fakeRenderer struct{ calls int }

func (f *fakeRenderer) Render(_ context.Context, _, data []byte, _ ...pdfrender.File) ([]byte, error) {
	f.calls++
	return append([]byte("%PDF-1.7 fake\n"), data...), nil
}

type clinicalEnv struct {
	*orgEnv
	svc      *clinicalapp.Service
	queries  *clinicalinfra.Queries
	access   *clinicalinfra.PetAccessAdapter
	handlers *clinicalhttp.ClinicalHandlers
	objects  *memObjects
}

func newClinicalEnv(t *testing.T) *clinicalEnv {
	t.Helper()
	e := newOrgEnv(t)
	queries := clinicalinfra.NewQueries(e.pool.Pool)
	pets := clinicalinfra.NewPetAccess(e.pool.Pool)
	svc := clinicalapp.NewService(clinicalapp.Options{
		Repo: clinicalinfra.NewRepository(e.store, e.registry, nil, nil), UoW: e.unit,
		Projects: clinicalinfra.NewSyncProjector(e.runner), PetAccess: pets,
		Membership: clinicalinfra.NewMembership(e.pool.Pool), Appointments: clinicalinfra.AppointmentAdapter{},
		Queries: queries, Logger: quietLogger(),
	})
	svc.RegisterCommands(bus.New())
	objects := &memObjects{objs: map[string][]byte{}}
	h := clinicalhttp.NewClinicalHandlers(clinicalhttp.Deps{
		Commands: svc, Queries: queries, Pets: pets,
		Documents: clinicalinfra.NewDocuments(e.pool.Pool), Uploads: clinicalinfra.NewUploads(e.pool.Pool),
		Freshness: clinicalinfra.NewFreshness(e.pool.Pool), Auditor: clinicalinfra.NewReadAuditor(e.pool.Pool),
		Idem: idempotency.NewStore(e.pool, 0), Objects: objects, Logger: quietLogger(),
	})
	return &clinicalEnv{orgEnv: e, svc: svc, queries: queries, access: pets, handlers: h, objects: objects}
}

func clinicalCtx(user uuid.UUID, expected int64) context.Context {
	return clinicalapp.WithCommandContext(context.Background(), clinicalapp.CommandContext{
		Principal: clinicalapp.PrincipalInfo{UserID: user}, ExpectedVersion: expected,
	})
}

// principalCtx construye el principal HTTP con las membresías actuales.
func (e *clinicalEnv) principalCtx(t *testing.T, user uuid.UUID, roles ...string) context.Context {
	t.Helper()
	ms, err := e.cache.Memberships(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return auth.WithPrincipal(context.Background(), auth.Principal{UserID: user, EmailVerified: true,
		GlobalRoles: append([]string{"pet_owner"}, roles...), Memberships: ms})
}

// clinicWithVet: Ana con Luna vinculada a una clínica activa con Beto
// (admin fundador) y Dani (veterinaria).
func (e *clinicalEnv) clinicWithVet(t *testing.T) (ana, dani, org, luna uuid.UUID) {
	t.Helper()
	ana = e.user(t, "Ana Tutora", "ana@example.com")
	beto := e.user(t, "Beto Admin", "beto@example.com")
	dani = e.user(t, "Dani Vet", "dani@example.com")
	admin := uuid.Must(uuid.NewV7())
	res, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RegisterPet{
		GuardianID: ana, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	luna = res.AggregateID
	res, err = e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sol S.A.S.", DisplayName: "Clínica Sol",
		TaxCountry: "CO", TaxValue: "900.123.456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Cali", Line1: "Calle 5 # 1-2"},
		ContactEmail: "hola@clinicasol.co", ContactPhone: "+573001112233", FounderID: beto,
	})
	if err != nil {
		t.Fatal(err)
	}
	org = res.AggregateID
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.VerifyOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.InviteMember{OrganizationID: org, Email: "dani@example.com", Role: "veterinarian"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orgs.AcceptInvitationToken(orgCtx(dani, "dani@example.com"), e.mail.lastToken(t, "dani@example.com")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.GrantCareLink{PetID: luna, OrganizationID: org, ActorID: ana}); err != nil {
		t.Fatal(err)
	}
	return ana, dani, org, luna
}

func (e *clinicalEnv) dispatch(t *testing.T, user uuid.UUID, expected int64, cmd bus.Command) bus.Result {
	t.Helper()
	r, err := e.svc.Dispatch(clinicalCtx(user, expected), cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd.CommandName(), err)
	}
	return r
}

func (e *clinicalEnv) asyncSubs() *projection.Subscriptions {
	s := projection.NewSubscriptions(e.pool, e.store, nil, projection.Options{
		Backoff:  []time.Duration{time.Millisecond},
		IdlePoll: 5 * time.Millisecond, BatchSize: 100, Logger: quietLogger(),
	})
	s.Register(clinicalinfra.NewTimelineProjector())
	s.Register(clinicalinfra.NewWeightProjector())
	s.Register(clinicalinfra.NewCustodyProjector())
	return s
}

// signedEncounter abre, completa y firma una consulta; devuelve id y versión.
func (e *clinicalEnv) signedEncounter(t *testing.T, dani, org, luna uuid.UUID) (uuid.UUID, int64) {
	t.Helper()
	enc := uuid.Must(uuid.NewV7())
	r := e.dispatch(t, dani, 0, clinicaldomain.OpenEncounter{
		EncounterID: enc, PetID: luna, OrganizationID: org, Type: "consultation", Reason: "Vómitos",
		StartedAt: time.Now().Add(-time.Hour), AttendingVetID: &dani,
	})
	w := 12.5
	r = e.dispatch(t, dani, r.Version, clinicaldomain.RecordVitals{EncounterID: enc, Vitals: clinicaldomain.Vitals{WeightKg: &w}})
	subj := "Vómitos desde ayer"
	r = e.dispatch(t, dani, r.Version, clinicaldomain.UpdateNarrative{EncounterID: enc, Subjective: &subj})
	r = e.dispatch(t, dani, r.Version, clinicaldomain.SetDiagnoses{EncounterID: enc, Diagnoses: []clinicaldomain.Diagnosis{
		{Text: "Gastroenteritis", Certainty: clinicaldomain.CertaintyPresumptive}}})
	r = e.dispatch(t, dani, r.Version, clinicaldomain.SignEncounter{EncounterID: enc})
	return enc, r.Version
}

func TestClinical_OpenEditSignAddendumPDFVerify(t *testing.T) {
	e := newClinicalEnv(t)
	ctx := context.Background()
	ana, dani, org, luna := e.clinicWithVet(t)
	enc, _ := e.signedEncounter(t, dani, org, luna)

	// Proyección síncrona: firmada con hash y PDF v1 encolado (ARQ-010).
	v, err := e.queries.Get(ctx, enc)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "signed" || v.Signature == nil || len(v.Signature.ContentHash) != 64 || v.AttendingVetName != "Dani Vet" {
		t.Fatalf("firmada: %+v", v)
	}
	if v.PDF == nil || v.PDF.Status != "pending" || v.PDF.Version != 1 {
		t.Fatalf("pdf encolado: %+v", v.PDF)
	}
	hash := v.Signature.ContentHash

	// E3: el cuerpo firmado no se edita.
	subj := "cambio"
	if _, err := e.svc.Dispatch(clinicalCtx(dani, 0), clinicaldomain.UpdateNarrative{EncounterID: enc, Subjective: &subj}); err == nil {
		t.Fatal("editar una consulta firmada debe fallar")
	}

	// Adenda (E9) → PDF v2.
	e.dispatch(t, dani, 0, clinicaldomain.AddAddendum{EncounterID: enc, Text: "Analítica normal", Reason: "lab_results"})
	v, _ = e.queries.Get(ctx, enc)
	if len(v.Addenda) != 1 || v.PDF.Version != 2 {
		t.Fatalf("adenda: %+v pdf %+v", v.Addenda, v.PDF)
	}

	// Worker de documentos: procesa v1 y v2 con renderer falso.
	renderer := &fakeRenderer{}
	pdf := clinicalinfra.NewPDFWorker(e.pool.Pool, e.objects, renderer, "https://app.test", nil, quietLogger())
	for {
		done, err := pdf.ProcessOne(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			break
		}
	}
	if renderer.calls != 2 {
		t.Fatalf("renders: %d", renderer.calls)
	}
	v, _ = e.queries.Get(ctx, enc)
	if v.PDF.Status != "ready" || v.PDF.ObjectKey != clinicalinfra.PDFKey(luna, enc, 2) {
		t.Fatalf("pdf listo: %+v", v.PDF)
	}
	if body := e.objects.objs[v.PDF.ObjectKey]; !bytes.Contains(body, []byte(hash)) || !bytes.Contains(body, []byte("https://app.test/verify/encounters/"+enc.String())) {
		t.Fatal("el PDF debe incluir el hash y la URL de verificación")
	}

	// GET /encounters/{id}/pdf → 302 a la prefirmada (tutor; firmado).
	resp, err := e.handlers.GetEncounterPdf(e.principalCtx(t, ana), gen.GetEncounterPdfRequestObject{EncounterId: enc})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := resp.(gen.GetEncounterPdf302Response); !ok || !strings.Contains(*r.Headers.Location, "/v2.pdf") {
		t.Fatalf("pdf: %#v", resp)
	}

	// Verificación pública (API-013): válida con el hash firmado.
	vr, err := e.handlers.VerifyEncounter(ctx, gen.VerifyEncounterRequestObject{EncounterId: enc, Params: gen.VerifyEncounterParams{Hash: &hash}})
	if err != nil {
		t.Fatal(err)
	}
	if out := vr.(gen.VerifyEncounter200JSONResponse); !out.Valid || *out.OrganizationName != "Clínica Sol" {
		t.Fatalf("verificación: %+v", out)
	}

	// SEC-004: alterar el read model no cambia el hash recomputado desde
	// es.events; el hash alterado no verifica.
	tampered := strings.Repeat("0", 64)
	if _, err := e.pool.Exec(ctx, `UPDATE read_clinical.encounters
		SET signature = jsonb_set(signature, '{content_hash}', to_jsonb($2::text)),
		    diagnoses = '[{"text":"otro","certainty":"confirmed"}]'
		WHERE id = $1`, enc, tampered); err != nil {
		t.Fatal(err)
	}
	vr, _ = e.handlers.VerifyEncounter(ctx, gen.VerifyEncounterRequestObject{EncounterId: enc, Params: gen.VerifyEncounterParams{Hash: &tampered}})
	if vr.(gen.VerifyEncounter200JSONResponse).Valid {
		t.Fatal("un hash alterado no debe verificar")
	}
	vr, _ = e.handlers.VerifyEncounter(ctx, gen.VerifyEncounterRequestObject{EncounterId: enc, Params: gen.VerifyEncounterParams{Hash: &hash}})
	if !vr.(gen.VerifyEncounter200JSONResponse).Valid {
		t.Fatal("el hash original sigue verificando contra es.events")
	}
	// Desconocido → 404.
	if _, err := e.handlers.VerifyEncounter(ctx, gen.VerifyEncounterRequestObject{EncounterId: uuid.New()}); err == nil {
		t.Fatal("encuentro inexistente debe ser 404")
	}
}

// Custodia (ADR-010): tras revocar el vínculo la clínica conserva lectura de
// lo que firmó, no puede abrir ni editar, y la ficha la ve en modo custody.
func TestClinical_CustodyAfterRevocation(t *testing.T) {
	e := newClinicalEnv(t)
	ctx := context.Background()
	ana, dani, org, luna := e.clinicWithVet(t)
	enc, _ := e.signedEncounter(t, dani, org, luna)
	draft := uuid.Must(uuid.NewV7())
	e.dispatch(t, dani, 0, clinicaldomain.OpenEncounter{EncounterID: draft, PetID: luna, OrganizationID: org,
		Type: "follow_up", Reason: "Control", StartedAt: time.Now(), AttendingVetID: &dani})
	if err := e.asyncSubs().CatchUp(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RevokeCareLink{PetID: luna, OrganizationID: org, ActorID: ana}); err != nil {
		t.Fatal(err)
	}
	if lvl, _ := e.access.CanOrgAccess(ctx, luna, org); lvl != clinicalapp.AccessCustody {
		t.Fatalf("nivel tras revocar: %s", lvl)
	}
	vetCtx := e.principalCtx(t, dani)

	// Lectura de lo firmado: sí (auditada, SEC-005).
	if _, err := e.handlers.GetEncounter(vetCtx, gen.GetEncounterRequestObject{EncounterId: enc}); err != nil {
		t.Fatalf("custodia debe leer lo firmado: %v", err)
	}
	var audits int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM platform.audit_log WHERE action = 'clinical.read' AND actor_id = $1`, dani).Scan(&audits)
	if audits != 1 {
		t.Fatalf("auditoría de lectura clínica: %d", audits)
	}
	// El borrador queda oculto (sin vínculo) y no se puede editar ni abrir.
	if _, err := e.handlers.GetEncounter(vetCtx, gen.GetEncounterRequestObject{EncounterId: draft}); !isNotFound(err) {
		t.Fatalf("borrador sin vínculo: %v", err)
	}
	if _, err := e.handlers.PutEncounterVitals(vetCtx, gen.PutEncounterVitalsRequestObject{EncounterId: draft, Body: &gen.Vitals{}}); !isNotFound(err) {
		t.Fatalf("editar sin vínculo: %v", err)
	}
	_, err := e.handlers.OpenEncounter(vetCtx, gen.OpenEncounterRequestObject{OrgId: org,
		Params: gen.OpenEncounterParams{IdempotencyKey: uuid.New()},
		Body:   &gen.OpenEncounterRequest{PetId: openapi_types.UUID(luna), Type: "consultation", Reason: "x"}})
	if !errors.Is(err, clinicaldomain.ErrPetNotAccessible) {
		t.Fatalf("abrir sin vínculo: %v", err)
	}
	// Ficha en modo custody: solo lo propio.
	sum, err := e.handlers.GetPetSummary(vetCtx, gen.GetPetSummaryRequestObject{PetId: luna})
	if err != nil {
		t.Fatal(err)
	}
	body := sum.(gen.GetPetSummary200JSONResponse).Body
	if *body.Access != gen.PetClinicalSummaryAccessCustody || body.LastEncounter == nil || body.CurrentWeight == nil {
		t.Fatalf("ficha en custodia: %+v", body)
	}
	// Lista de la org: el borrador de la mascota desvinculada no aparece.
	list, err := e.handlers.ListOrgEncounters(vetCtx, gen.ListOrgEncountersRequestObject{OrgId: org})
	if err != nil {
		t.Fatal(err)
	}
	page := list.(gen.ListOrgEncounters200JSONResponse)
	if len(page.Data) != 1 || uuid.UUID(page.Data[0].Id) != enc {
		t.Fatalf("lista de la org: %+v", page.Data)
	}
	// Tutor: ve la firmada, no el borrador.
	anaCtx := e.principalCtx(t, ana)
	if _, err := e.handlers.GetEncounter(anaCtx, gen.GetEncounterRequestObject{EncounterId: draft}); !isNotFound(err) {
		t.Fatalf("tutor no ve borradores: %v", err)
	}
	if _, err := e.handlers.GetEncounter(anaCtx, gen.GetEncounterRequestObject{EncounterId: enc}); err != nil {
		t.Fatal(err)
	}
}

func isNotFound(err error) bool {
	var nf *kernel.NotFoundError
	return errors.As(err, &nf)
}

// Proyectar dos veces deja el mismo estado (ARQ-012): rebuild de las
// asíncronas y re-aplicación de los eventos a la síncrona.
func TestClinical_ProjectionsIdempotent(t *testing.T) {
	e := newClinicalEnv(t)
	ctx := context.Background()
	_, dani, org, luna := e.clinicWithVet(t)
	enc, _ := e.signedEncounter(t, dani, org, luna)
	e.dispatch(t, dani, 0, clinicaldomain.AddAddendum{EncounterID: enc, Text: "Nota", Reason: "other"})

	dump := func() string {
		var out string
		err := e.pool.QueryRow(ctx, `SELECT
			(SELECT coalesce(json_agg(w ORDER BY ref_id), '[]') FROM read_clinical.weight_history w)::text ||
			(SELECT coalesce(json_agg(c ORDER BY encounter_id), '[]') FROM read_clinical.encounter_custody c)::text ||
			(SELECT coalesce(json_agg(json_build_object('k', kind, 'r', ref_id, 't', title, 'v', visibility) ORDER BY occurred_at, ref_id), '[]')
			   FROM read_clinical.medical_timeline)::text ||
			(SELECT row_to_json(x)::text FROM (SELECT id, version, status, signature, addenda, diagnoses, vitals
			   FROM read_clinical.encounters WHERE id = $1) x)`, enc).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	subs := e.asyncSubs()
	if err := subs.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	first := dump()
	if !strings.Contains(first, `"weight_kg":12.5`) && !strings.Contains(first, `"weight_kg":12.50`) {
		t.Fatalf("peso proyectado desde la firma: %s", first)
	}
	head, err := e.store.Head(ctx, e.pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{clinicalapp.ProjectionWeight, clinicalapp.ProjectionCustody, clinicalapp.ProjectionTimeline} {
		if err := subs.Rebuild(ctx, name, head, nil); err != nil {
			t.Fatalf("rebuild %s: %v", name, err)
		}
	}
	// Re-aplicar los eventos del stream al proyector síncrono (guard de versión).
	if err := e.unit.Do(ctx, func(ctx context.Context) error {
		tx, err := db.TxFrom(ctx)
		if err != nil {
			return err
		}
		stored, err := e.store.Load(ctx, tx, enc, 0)
		if err != nil {
			return err
		}
		return clinicalinfra.NewSyncProjector(e.runner).Apply(ctx, stored)
	}); err != nil {
		t.Fatal(err)
	}
	if second := dump(); second != first {
		t.Fatalf("proyectar dos veces cambió el estado:\n%s\n%s", first, second)
	}
}
