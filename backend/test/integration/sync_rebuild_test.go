//go:build integration

// Rebuild de las proyecciones primarias de F1 (PP-F2-ARQ-006, `worker rebuild
// --sync`) y modo mantenimiento de la API.
package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/maintenance"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// primaryTables son todos los read models primarios de F1 (DAT-005, DAT-006).
var primaryTables = []string{
	"read_partnernetwork.organizations", "read_partnernetwork.org_members",
	"read_partnernetwork.invitations", "read_partnernetwork.public_code_reservations",
	"read_petregistry.pets", "read_petregistry.pet_care_links",
	"read_petregistry.microchip_reservations", "read_petregistry.pet_transfers",
}

// dumpPrimaries vuelca las tablas primarias en orden determinista, sin
// updated_at (03 §7: igualdad exacta excluyendo updated_at).
func dumpPrimaries(t *testing.T, e *orgEnv) string {
	t.Helper()
	var b strings.Builder
	for _, table := range primaryTables {
		rows, err := e.pool.Query(context.Background(), fmt.Sprintf(
			`SELECT (to_jsonb(x) - 'updated_at')::text AS j FROM %s x ORDER BY 1`, table))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var j string
			if err := rows.Scan(&j); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%s %s\n", table, j)
			n++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatalf("escenario sin filas en %s: la prueba no cubriría esa tabla", table)
		}
	}
	return b.String()
}

func TestProjection_SyncRebuildPrimaries(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	ana := e.user(t, "Ana Tutora", "ana@example.com")
	beto := e.user(t, "Beto Vet", "beto@example.com")
	carla := e.user(t, "Carla Recepción", "carla@example.com")
	admin := uuid.Must(uuid.NewV7())
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	res, err := e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900.123.456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567", FounderID: beto,
	})
	must(res, err)
	org := res.AggregateID
	must(e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.VerifyOrganization{OrganizationID: org}))

	// Mascotas, vínculos (uno revocado) y microchip.
	pet := func(name string) uuid.UUID {
		res, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RegisterPet{
			GuardianID: ana, Name: name, Species: "dog", Sex: "female", Neutered: "yes",
		})
		must(res, err)
		return res.AggregateID
	}
	luna, rex := pet("Luna"), pet("Rex")
	anaCtx := petCtx(ana, "ana@example.com")
	must(e.pets.Dispatch(anaCtx, petdomain.GrantCareLink{PetID: luna, OrganizationID: org, ActorID: ana}))
	must(e.pets.Dispatch(anaCtx, petdomain.GrantCareLink{PetID: rex, OrganizationID: org, ActorID: ana}))
	must(e.pets.Dispatch(anaCtx, petdomain.RevokeCareLink{PetID: rex, OrganizationID: org, ActorID: ana}))
	must(e.pets.Dispatch(anaCtx, petdomain.AssignMicrochip{PetID: rex, Microchip: "985112345678901"}))

	// Reacciones entre contextos: perfil, suspensión y reactivación de la clínica.
	must(e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.UpdateOrganizationProfile{
		OrganizationID: org, DisplayName: ptrStr("Clínica del Sur")}))
	must(e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.SuspendOrganization{OrganizationID: org, Reason: "documentación"}))
	must(e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.ReactivateOrganization{OrganizationID: org}))

	// Invitación reenviada: el token y el contador cambian fuera de los eventos.
	must(e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.InviteMember{
		OrganizationID: org, Email: "carla@example.com", Role: "receptionist"}))
	invs, err := e.orgQ.ListInvitations(ctx, org, "pending", time.Now())
	if err != nil || len(invs) != 1 {
		t.Fatalf("invitaciones: %v %v", invs, err)
	}
	if err := e.orgs.ResendInvitation(ctx, org, invs[0].ID); err != nil {
		t.Fatal(err)
	}
	carlaToken := e.mail.lastToken(t, "carla@example.com")

	// Transferencia cancelada y otra pendiente: el replay de la cancelación no
	// debe cancelar la pendiente (pet_transfers no deriva de eventos).
	must(e.pets.Dispatch(anaCtx, petdomain.InitiateGuardianshipTransfer{PetID: luna, ToEmail: "eva@example.com"}))
	must(e.pets.Dispatch(anaCtx, petdomain.CancelGuardianshipTransfer{PetID: luna}))
	must(e.pets.Dispatch(anaCtx, petdomain.InitiateGuardianshipTransfer{PetID: luna, ToEmail: "eva@example.com"}))

	before := dumpPrimaries(t, e)
	report, err := e.runner.RebuildSync(ctx, e.pool, e.store, projection.SyncRebuildOptions{BatchSize: 3})
	if err != nil {
		t.Fatalf("rebuild --sync: %v", err)
	}
	if report.Events == 0 || len(report.Projectors) != 3 {
		t.Fatalf("informe: %+v", report)
	}
	if after := dumpPrimaries(t, e); after != before {
		t.Fatalf("los read models primarios cambian tras rebuild --sync:\nantes:\n%s\ndespués:\n%s", before, after)
	}

	// La invitación reenviada sigue aceptándose con el último token.
	if _, err := e.orgs.AcceptInvitationToken(orgCtx(carla, "carla@example.com"), carlaToken); err != nil {
		t.Fatalf("aceptar invitación tras rebuild: %v", err)
	}
	// Y el sistema sigue proyectando comandos nuevos con normalidad.
	must(e.pets.Dispatch(anaCtx, petdomain.GrantCareLink{PetID: rex, OrganizationID: org, ActorID: ana}))
}

// TestMaintenance_GateBlocksAPI: con la fila de platform.maintenance, /api/v1
// responde 503 con Retry-After (salvo healthz); al quitarla, vuelve a servir.
func TestMaintenance_GateBlocksAPI(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	gate := maintenance.NewGate(e.pool.Pool, time.Millisecond)
	h := gate.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		return rec
	}

	if rec := get("/api/v1/pets"); rec.Code != http.StatusOK {
		t.Fatalf("sin mantenimiento: %d", rec.Code)
	}
	if err := maintenance.Enable(ctx, e.pool, "prueba"); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Enable(ctx, e.pool, "prueba 2"); err != nil { // idempotente
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	rec := get("/api/v1/pets")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" ||
		!strings.Contains(rec.Body.String(), `"UNAVAILABLE"`) {
		t.Fatalf("en mantenimiento: %d %q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if rec := get("/api/v1/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz en mantenimiento: %d", rec.Code)
	}
	st, err := maintenance.Get(ctx, e.pool)
	if err != nil || !st.Active || st.Reason != "prueba 2" {
		t.Fatalf("estado: %+v %v", st, err)
	}
	if err := maintenance.Disable(ctx, e.pool); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if rec := get("/api/v1/pets"); rec.Code != http.StatusOK {
		t.Fatalf("tras desactivar: %d", rec.Code)
	}
}
