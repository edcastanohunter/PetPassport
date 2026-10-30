//go:build integration

// Solicitud de vínculo por la clínica de extremo a extremo contra Postgres
// (PP-F2-PRD-020, DAT-007, QA-004): búsqueda con resultados mínimos y límite
// → solicitud → bandeja del tutor → aprobación → acceso → revocación;
// rechazo y expiración; reasignación de la bandeja al cambiar de tutor y
// rebuild idempotente de las proyecciones.
package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	orgdomain "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	petdomain "github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/google/uuid"
)

func TestCareLinkRequests_RequestApproveRevokeAndExpire(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	ana := e.user(t, "Ana Tutora", "ana@example.com")
	beto := e.user(t, "Beto Vet", "beto@example.com")
	admin := uuid.Must(uuid.NewV7())

	res, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RegisterPet{
		GuardianID: ana, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	luna := res.AggregateID
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.AssignMicrochip{PetID: luna, Microchip: "982000123456789"}); err != nil {
		t.Fatal(err)
	}

	res, err = e.orgs.Dispatch(orgCtx(beto, "beto@example.com"), orgdomain.RegisterOrganization{
		Type: "clinic", LegalName: "Clínica Veterinaria Sur S.A.S.", DisplayName: "Clínica Sur",
		TaxCountry: "CO", TaxValue: "900.123.456-8",
		Address:      orgdomain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 20-30"},
		ContactEmail: "contacto@clinicasur.co", ContactPhone: "+573001234567", FounderID: beto,
	})
	if err != nil {
		t.Fatal(err)
	}
	org := res.AggregateID
	if _, err := e.orgs.Dispatch(orgCtx(admin, "", "super_admin"), orgdomain.VerifyOrganization{OrganizationID: org}); err != nil {
		t.Fatal(err)
	}

	// Búsqueda: resultado mínimo con el tutor enmascarado.
	found, err := e.pets.LookupPetForRequest(ctx, beto, "982000123456789", "")
	if err != nil || len(found) != 1 || found[0].PetID != luna || found[0].GuardianMasked != "A*** T***" {
		t.Fatalf("búsqueda por microchip: %+v %v", found, err)
	}
	if byMail, err := e.pets.LookupPetForRequest(ctx, beto, "", "ana@example.com"); err != nil || len(byMail) != 1 {
		t.Fatalf("búsqueda por email: %+v %v", byMail, err)
	}

	// Solicitud → pendiente en la bandeja de Ana; segunda del mismo org → REQUEST_PENDING.
	req, err := e.pets.Dispatch(petCtx(beto, "beto@example.com"), petdomain.RequestCareLink{
		PetID: luna, OrganizationID: org, RequestedBy: beto, Message: "Atendemos a Luna desde el lunes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pets.Dispatch(petCtx(beto, "beto@example.com"), petdomain.RequestCareLink{
		PetID: luna, OrganizationID: org, RequestedBy: beto,
	}); !errors.Is(err, petdomain.ErrRequestPending) {
		t.Fatalf("segunda solicitud: %v", err)
	}
	inbox, err := e.pets.ListInbox(ctx, ana)
	if err != nil || len(inbox) != 1 || inbox[0].Kind != "care_link_request" || inbox[0].RefID != req.AggregateID {
		t.Fatalf("bandeja de Ana: %+v %v", inbox, err)
	}
	if stranger, _ := e.pets.ListInbox(ctx, beto); len(stranger) != 0 {
		t.Fatalf("la bandeja es del tutor: %+v", stranger)
	}

	// Aprobar → vínculo activo, bandeja vacía, la clínica ve al paciente.
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.ApproveCareLinkRequest{
		PetID: luna, RequestID: req.AggregateID, ActorID: ana,
	}); err != nil {
		t.Fatal(err)
	}
	view, err := e.pets.GetOrgRequest(ctx, org, req.AggregateID)
	if err != nil || view.Status != "approved" {
		t.Fatalf("estado de la solicitud: %+v %v", view, err)
	}
	if inbox, _ := e.pets.ListInbox(ctx, ana); len(inbox) != 0 {
		t.Fatalf("bandeja tras aprobar: %+v", inbox)
	}
	pet, _ := e.petQ.GetPet(ctx, luna)
	if len(pet.ActiveLinks) != 1 || pet.ActiveLinks[0].OrganizationID != org {
		t.Fatalf("vínculo tras aprobar: %+v", pet.ActiveLinks)
	}
	if _, err := e.pets.Dispatch(petCtx(beto, "beto@example.com"), petdomain.RequestCareLink{
		PetID: luna, OrganizationID: org, RequestedBy: beto,
	}); !errors.Is(err, petdomain.ErrCareLinkAlreadyActive) {
		t.Fatalf("con vínculo activo: %v", err)
	}

	// Revocar y pedir de nuevo; esta vez Ana rechaza.
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.RevokeCareLink{PetID: luna, OrganizationID: org, ActorID: ana}); err != nil {
		t.Fatal(err)
	}
	second, err := e.pets.Dispatch(petCtx(beto, "beto@example.com"), petdomain.RequestCareLink{PetID: luna, OrganizationID: org, RequestedBy: beto})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pets.Dispatch(petCtx(ana, "ana@example.com"), petdomain.DeclineCareLinkRequest{
		PetID: luna, RequestID: second.AggregateID, ActorID: ana, Reason: "no la conozco",
	}); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.pets.GetOrgRequest(ctx, org, second.AggregateID); v.Status != "declined" {
		t.Fatalf("rechazada: %+v", v)
	}

	// Expiración por el scheduler: tercera solicitud, expira al vencer los 14 días.
	third, err := e.pets.Dispatch(petCtx(beto, "beto@example.com"), petdomain.RequestCareLink{PetID: luna, OrganizationID: org, RequestedBy: beto})
	if err != nil {
		t.Fatal(err)
	}
	early := petdomain.ExpireCareLinkRequest{PetID: luna, RequestID: third.AggregateID, Now: time.Now().Add(time.Hour)}
	if _, err := e.pets.Dispatch(petCtx(uuid.Nil, ""), early); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.pets.GetOrgRequest(ctx, org, third.AggregateID); v.Status != "pending" {
		t.Fatalf("expirar antes de tiempo no debe cerrar la solicitud: %+v", v)
	}
	late := petdomain.ExpireCareLinkRequest{PetID: luna, RequestID: third.AggregateID, Now: time.Now().Add(petdomain.CareLinkRequestTTL + time.Minute)}
	for i := 0; i < 2; i++ { // idempotente
		if _, err := e.pets.Dispatch(petCtx(uuid.Nil, ""), late); err != nil {
			t.Fatal(err)
		}
	}
	if v, _ := e.pets.GetOrgRequest(ctx, org, third.AggregateID); v.Status != "expired" {
		t.Fatalf("expirada: %+v", v)
	}

	// Listado de la clínica, más recientes primero, con filtro por estado.
	all, err := e.pets.ListOrgRequests(ctx, org, "")
	if err != nil || len(all) != 3 || all[0].ID != third.AggregateID {
		t.Fatalf("listado: %+v %v", all, err)
	}
	if declined, _ := e.pets.ListOrgRequests(ctx, org, "declined"); len(declined) != 1 {
		t.Fatalf("filtro declined: %+v", declined)
	}
}

func TestCareLinkRequests_LookupRateLimit(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	beto := e.user(t, "Beto Vet", "beto@example.com")
	for i := 0; i < 20; i++ {
		if _, err := e.pets.LookupPetForRequest(ctx, beto, "", "nadie@example.com"); err != nil {
			t.Fatalf("búsqueda %d: %v", i+1, err)
		}
	}
	if _, err := e.pets.LookupPetForRequest(ctx, beto, "", "nadie@example.com"); err == nil {
		t.Fatal("la búsqueda 21 en una hora debe rechazarse (RATE_LIMITED)")
	}
}
