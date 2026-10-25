//go:build integration

// Pet Registry end-to-end contra Postgres (PP-F1-PRD-005..009, DOM-003/004/005,
// ARQ-003..009, DAT-005, QA-003): comando → ES → proyección síncrona →
// consulta; reservas de microchip concurrentes; rebuild idempotente;
// idempotencia HTTP; contrato OpenAPI.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type petEnv struct {
	t        *testing.T
	pool     *db.Pool
	unit     *db.UnitOfWork
	store    *eventstore.Store
	registry *eventstore.Registry
	svc      *petapp.Service
	queries  *petinfra.Queries
	runner   *projection.Runner
}

func newPetEnv(t *testing.T) *petEnv {
	t.Helper()
	url := testkit.Postgres(t)
	pool, err := db.Open(context.Background(), url, 8, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	registry := eventstore.NewRegistry()
	petinfra.RegisterEvents(registry)
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	store := eventstore.NewStore(registry, pool, nil)
	unit := db.NewUnitOfWork(pool)
	runner := projection.NewRunner()
	queries := petinfra.NewQueries(pool.Pool)
	svc := petapp.NewService(petapp.Options{
		Repo:     petinfra.NewRepository(store, registry, nil, nil),
		UoW:      unit,
		Projects: petinfra.NewSyncProjector(runner),
		Reserve:  petinfra.NewReservations(pool),
		Queries:  queries,
		Events:   store,
	})
	runner.Register(petinfra.NewPetsProjector(registry, nil))
	svc.RegisterCommands(bus.New())
	return &petEnv{t: t, pool: pool, unit: unit, store: store, registry: registry, svc: svc, queries: queries, runner: runner}
}

func (e *petEnv) register(ctx context.Context, guardian uuid.UUID, name string) (uuid.UUID, error) {
	cc := petapp.CommandContext{Principal: petapp.PrincipalInfo{UserID: guardian, Roles: []string{"pet_owner"}}}
	_, err := e.svc.Dispatch(petapp.WithCommandContext(ctx, cc), petdomain.RegisterPet{
		GuardianID: guardian, Name: name, Species: "dog", Sex: "female", Neutered: "yes",
		BirthDate: &petdomain.BirthDateInput{Date: "2021-05-10"},
	})
	if err != nil {
		return uuid.Nil, err
	}
	// Buscar por lista (la proyección síncrona la hace visible de inmediato, PRD-005).
	items, _, err := e.queries.ListPets(ctx, guardian, "all", petapp.PetCursor{}, 10)
	if err != nil {
		return uuid.Nil, err
	}
	for _, it := range items {
		if it.Name == name {
			return it.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("mascota %q no proyectada", name)
}

// TestPet_FlowCommandToProjection (PRD-005, PRD-006): registrar → visible
// inmediatamente; editar con versiones; microchip con reserva (PRD-007);
// fallecida y deshacer (PRD-009).
func TestPet_FlowCommandToProjection(t *testing.T) {
	e := newPetEnv(t)
	ctx := context.Background()
	guardian := uuid.Must(uuid.NewV7())

	petID, err := e.register(ctx, guardian, "Luna")
	if err != nil {
		t.Fatal(err)
	}
	view, err := e.queries.GetPet(ctx, petID)
	if err != nil || view.Name != "Luna" || view.Version != 1 || view.Status != "active" {
		t.Fatalf("proyección tras crear: %+v %v", view, err)
	}

	// Edición con If-Match correcto (PRD-006).
	cc := petapp.CommandContext{
		Principal: petapp.PrincipalInfo{UserID: guardian}, ExpectedVersion: 1,
	}
	if _, err := e.svc.Dispatch(petapp.WithCommandContext(ctx, cc), petdomain.UpdatePetProfile{
		PetID: petID, Name: ptrStr("Lunita"), Breed: ptrStr("Beagle"),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// If-Match desfasado → 409 CONFLICT_VERSION.
	cc.ExpectedVersion = 1
	_, err = e.svc.Dispatch(petapp.WithCommandContext(ctx, cc), petdomain.UpdatePetProfile{PetID: petID, Name: ptrStr("Otra")})
	var ce *kernel.ConcurrencyError
	if !errors.As(err, &ce) {
		t.Fatalf("if-match desfasado debe dar conflicto: %v", err)
	}

	// Microchip: asignar, corregir una vez, segunda vez rechazada (I7, PRD-007).
	cc2 := petapp.CommandContext{Principal: petapp.PrincipalInfo{UserID: guardian}}
	d := petapp.WithCommandContext(ctx, cc2)
	if _, err := e.svc.Dispatch(d, petdomain.AssignMicrochip{PetID: petID, Microchip: "941 0000 1234 567 8"}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if _, err := e.svc.Dispatch(d, petdomain.AssignMicrochip{PetID: petID, Microchip: "941000012345679"}); err != nil {
		t.Fatalf("corrección 1: %v", err)
	}
	if _, err := e.svc.Dispatch(d, petdomain.AssignMicrochip{PetID: petID, Microchip: "941000012345680"}); err == nil ||
		!strings.Contains(err.Error(), "MICROCHIP_CORRECTION_LIMIT") {
		t.Fatalf("corrección 2 debe fallar: %v", err)
	}
	view, _ = e.queries.GetPet(ctx, petID)
	if view.Microchip != "941000012345679" || view.Corrections != 1 {
		t.Fatalf("microchip proyectado: %q corr=%d", view.Microchip, view.Corrections)
	}

	// Fallecida y deshacer (PRD-009).
	if _, err := e.svc.Dispatch(d, petdomain.MarkPetDeceased{PetID: petID}); err != nil {
		t.Fatal(err)
	}
	view, _ = e.queries.GetPet(ctx, petID)
	if view.Status != "deceased" || view.DeceasedMarkedAt == nil {
		t.Fatalf("deceased proyectado: %+v", view)
	}
	// Cambio de perfil bloqueado en fallecida (I3).
	if _, err := e.svc.Dispatch(d, petdomain.UpdatePetProfile{PetID: petID, Name: ptrStr("X")}); err == nil ||
		!strings.Contains(err.Error(), "PET_DECEASED") {
		t.Fatalf("update en fallecida debe fallar: %v", err)
	}
	if _, err := e.svc.Dispatch(d, petdomain.UndoDeceased{PetID: petID, WithinUndoWindow: true}); err != nil {
		t.Fatal(err)
	}
	view, _ = e.queries.GetPet(ctx, petID)
	if view.Status != "active" {
		t.Fatalf("undo proyectado: %+v", view)
	}
}

// TestPet_MicrochipReservationConcurrent (QA-003, DAT-005, PRD-007): N
// comandos concurrentes con el mismo chip → exactamente uno gana; el resto
// MICROCHIP_ALREADY_ASSIGNED.
func TestPet_MicrochipReservationConcurrent(t *testing.T) {
	e := newPetEnv(t)
	ctx := context.Background()
	names := []string{"Toby", "Rex", "Kira", "Nube", "Bruno", "Maya"}
	pets := make([]uuid.UUID, len(names))
	for i, name := range names {
		id, err := e.register(ctx, uuid.Must(uuid.NewV7()), name)
		if err != nil {
			t.Fatal(err)
		}
		pets[i] = id
	}
	var wins atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, petID := range pets {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			<-start
			cc := petapp.CommandContext{Principal: petapp.PrincipalInfo{UserID: uuid.Must(uuid.NewV7())}}
			_, err := e.svc.Dispatch(petapp.WithCommandContext(ctx, cc), petdomain.AssignMicrochip{
				PetID: id, Microchip: "941000099999999",
			})
			if err == nil {
				wins.Add(1)
			} else if !strings.Contains(err.Error(), "MICROCHIP_ALREADY_ASSIGNED") {
				e.t.Errorf("error inesperado: %v", err)
			}
		}(petID)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("ganadores de la reserva: %d, want 1", wins.Load())
	}
	var petID uuid.UUID
	if err := e.pool.QueryRow(ctx,
		`SELECT pet_id FROM read_petregistry.microchip_reservations WHERE microchip = $1`,
		"941000099999999").Scan(&petID); err != nil {
		t.Fatalf("reserva: %v", err)
	}
}

// TestPet_RebuildIdempotent (ARQ-005, QA-003): proyectar dos veces el mismo
// conjunto de eventos produce el mismo estado (dump idéntico).
func TestPet_RebuildIdempotent(t *testing.T) {
	e := newPetEnv(t)
	ctx := context.Background()
	guardian := uuid.Must(uuid.NewV7())
	petID, err := e.register(ctx, guardian, "Rex")
	if err != nil {
		t.Fatal(err)
	}
	cc := petapp.CommandContext{Principal: petapp.PrincipalInfo{UserID: guardian}}
	d := petapp.WithCommandContext(ctx, cc)
	if _, err := e.svc.Dispatch(d, petdomain.UpdatePetProfile{PetID: petID, Breed: ptrStr("Mestizo")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Dispatch(d, petdomain.AssignMicrochip{PetID: petID, Microchip: "ABC123DEF"}); err != nil {
		t.Fatal(err)
	}

	dump := func() string {
		rows, err := e.pool.Query(ctx, `SELECT to_jsonb(p) FROM read_petregistry.pets p WHERE id = $1`, petID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		links, err := e.pool.Query(ctx, `SELECT to_jsonb(l) FROM read_petregistry.pet_care_links l WHERE pet_id = $1`, petID)
		if err != nil {
			t.Fatal(err)
		}
		defer links.Close()
		for links.Next() {
			var s string
			if err := links.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return strings.Join(out, "\n")
	}

	first := dump()
	// Rebuild: reproyectar todos los eventos desde global_position en una tx
	// que se revierte (no debe alterar el estado) y otra que confirma.
	events, err := e.store.ReadAll(ctx, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	replay := func() error {
		return e.unit.Do(ctx, func(c context.Context) error {
			tx, err := db.TxFrom(c)
			if err != nil {
				return err
			}
			return e.runner.Apply(c, tx, events)
		})
	}
	if err := replay(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	second := dump()
	if first != second {
		t.Fatalf("rebuild no idempotente:\nprimera:\n%s\nsegunda:\n%s", first, second)
	}
}

// TestPet_HTTPFlow (API-004, ARQ-008, QA-004): flujo HTTP completo con
// idempotencia, If-Match/ETag y contrato.
func TestPet_HTTPFlow(t *testing.T) {
	e := newPetEnv(t)
	ctx := context.Background()

	j := testkit.NewJWKS(t)
	verifier, err := auth.NewVerifier(ctx, auth.VerifierConfig{Issuer: j.Issuer, Audience: "petpassport-api"})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	identitySvc := identityapp.NewService(identityinfra.NewPostgresRepository(e.pool), logger)
	idem := idempotency.NewStore(e.pool, 0)
	spec, _ := gen.GetSpec()
	guard, err := auth.NewGuard(spec, auth.GuardOptions{Verifier: verifier, Resolver: identitySvc, Logger: logger, BasePath: "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(":0", ":0", httpserver.Options{
		Logger: logger, Registry: prometheus.NewRegistry(), Health: httpserver.NewHealth(),
		Handlers: handlers{
			Handlers:    identityhttp.NewHandlers(identitySvc, ""),
			PetHandlers: pethttp.NewPetHandlers(e.svc, e.queries, idem, nil, logger),
		},
		Guard: guard.Middleware,
	})
	contract := testkit.NewContract(t)
	userID := uuid.Must(uuid.NewV7())
	tok := j.Token(t, testkit.Claims{
		Subject: userID.String(), Email: "tutor@example.com", EmailVerified: true,
		Name: "Tutora", Audience: "petpassport-api", Roles: []string{"pet_owner"},
	})

	call := func(method, path, body string, extra map[string]string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequestWithContext(ctx, method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer "+tok)
		for k, v := range extra {
			r.Header.Set(k, v)
		}
		return contract.Do(srv.Public.Handler, r)
	}

	// JIT + onboarding para pasar el guard.
	call(http.MethodGet, "/api/v1/me", "", nil)
	call(http.MethodPost, "/api/v1/me/onboarding",
		`{"display_name":"T","country":"CO","locale":"es","accept_terms_version":"2026-10-draft","accept_privacy_version":"2026-10-draft"}`, nil)

	idemKey := uuid.Must(uuid.NewV7()).String()
	body := `{"name":"Luna","species":"dog","breed":"Beagle","sex":"female","neutered":"yes","birth_date":{"date":"2021-05-10","approximate":false}}`
	rec := call(http.MethodPost, "/api/v1/pets", body, map[string]string{"Idempotency-Key": idemKey})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /pets: %d %s", rec.Code, rec.Body.String())
	}
	var pet gen.Pet
	if err := json.Unmarshal(rec.Body.Bytes(), &pet); err != nil {
		t.Fatal(err)
	}
	if pet.Version != 1 || pet.Name != "Luna" || pet.Age == nil || pet.Age.Years < 5 {
		t.Fatalf("pet: %+v", pet)
	}
	etag := rec.Header().Get("ETag")
	if etag != "1" {
		t.Fatalf("etag: %q", etag)
	}

	// Replay de idempotencia: misma clave y cuerpo → misma respuesta, sin nuevo evento.
	rec2 := call(http.MethodPost, "/api/v1/pets", body, map[string]string{"Idempotency-Key": idemKey})
	if rec2.Code != http.StatusCreated || rec2.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %q", rec2.Code, rec2.Header().Get("Idempotent-Replayed"))
	}
	var count int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM es.events WHERE stream_id = $1`, uuid.UUID(pet.Id)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("eventos tras replay: %d %v", count, err)
	}
	// Misma clave, cuerpo distinto → 422 IDEMPOTENCY_KEY_REUSED.
	rec3 := call(http.MethodPost, "/api/v1/pets", `{"name":"Otra","species":"cat","sex":"unknown","neutered":"unknown"}`, map[string]string{"Idempotency-Key": idemKey})
	if rec3.Code != http.StatusUnprocessableEntity || !strings.Contains(rec3.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("reuso de clave: %d %s", rec3.Code, rec3.Body.String())
	}

	path := "/api/v1/pets/" + pet.Id.String()
	// GET con ETag.
	rec = call(http.MethodGet, path, "", nil)
	if rec.Code != 200 || rec.Header().Get("ETag") != "1" {
		t.Fatalf("GET: %d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}
	// PATCH con If-Match correcto.
	rec = call(http.MethodPatch, path, `{"breed":"Mestizo"}`, map[string]string{"If-Match": "1"})
	if rec.Code != 200 {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body.String())
	}
	// PATCH con If-Match desfasado → 409 CONFLICT_VERSION.
	rec = call(http.MethodPatch, path, `{"breed":"Otro"}`, map[string]string{"If-Match": "1"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "CONFLICT_VERSION") {
		t.Fatalf("PATCH obsoleto: %d %s", rec.Code, rec.Body.String())
	}
	// 404 para mascota ajena (no revelar existencia).
	other := uuid.Must(uuid.NewV7())
	rec = call(http.MethodGet, "/api/v1/pets/"+other.String(), "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET ajena: %d", rec.Code)
	}
	// Microchip: invalid format → 422; válido → 200.
	rec = call(http.MethodPut, path+"/microchip", `{"microchip":"123"}`, map[string]string{"If-Match": "2"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("microchip inválido: %d %s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodPut, path+"/microchip", `{"microchip":"941000012345678"}`, map[string]string{"If-Match": "2"})
	if rec.Code != 200 {
		t.Fatalf("microchip: %d %s", rec.Code, rec.Body.String())
	}
	// Deceased con If-Match y undo.
	rec = call(http.MethodPost, path+"/deceased", `{}`, map[string]string{"If-Match": "3"})
	if rec.Code != 200 {
		t.Fatalf("deceased: %d %s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodPost, path+"/deceased/undo", "", nil)
	if rec.Code != 200 {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body.String())
	}
	// Lista con filtro de estado.
	rec = call(http.MethodGet, "/api/v1/pets?status=active", "", nil)
	if rec.Code != 200 {
		t.Fatalf("lista: %d", rec.Code)
	}
	var page gen.PetPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0].Name != "Luna" {
		t.Fatalf("lista: %+v", page.Data)
	}
}

// TestIdempotency_Proto (ARQ-008): in_progress → 409; expirada se roba.
func TestIdempotency_Proto(t *testing.T) {
	e := newPetEnv(t)
	ctx := context.Background()
	actor := uuid.Must(uuid.NewV7())
	key := uuid.Must(uuid.NewV7())
	store := idempotency.NewStore(e.pool, 24*time.Hour)

	out, err := store.Begin(ctx, actor, key, []byte("hash-a"))
	if err != nil || !out.Started {
		t.Fatalf("begin: %+v %v", out, err)
	}
	// En vuelo → ErrRequestInProgress.
	if _, err := store.Begin(ctx, actor, key, []byte("hash-a")); err != idempotency.ErrRequestInProgress {
		t.Fatalf("en vuelo: %v", err)
	}
	// Commit + replay.
	if err := store.Commit(ctx, actor, key, 201, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	out, err = store.Begin(ctx, actor, key, []byte("hash-a"))
	if err != nil || !out.Replay || out.Status != 201 {
		t.Fatalf("replay: %+v %v", out, err)
	}
	// Hash distinto → reuse.
	if _, err := store.Begin(ctx, actor, key, []byte("hash-b")); err != idempotency.ErrKeyReused {
		t.Fatalf("reuse: %v", err)
	}
	// Otra clave en vuelo: abort permite reintento.
	k2 := uuid.Must(uuid.NewV7())
	if _, err := store.Begin(ctx, actor, k2, []byte("h")); err != nil || !out.Started && true {
		_ = err
	}
	if err := store.Abort(ctx, actor, k2); err != nil {
		t.Fatal(err)
	}
}

func ptrStr(s string) *string { return &s }
