package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// TestCommandNames: todo comando tiene nombre estable de despacho.
func TestCommandNames(t *testing.T) {
	want := map[string]bool{
		"petregistry.RegisterPet": true, "petregistry.UpdatePetProfile": true,
		"petregistry.AssignMicrochip": true, "petregistry.ChangePetPhoto": true,
		"petregistry.MarkPetDeceased": true, "petregistry.UndoDeceased": true,
		"petregistry.GrantCareLink": true, "petregistry.RevokeCareLink": true,
		"petregistry.InitiateGuardianshipTransfer": true, "petregistry.CancelGuardianshipTransfer": true,
		"petregistry.AcceptGuardianshipTransfer": true,
	}
	cmds := []any{
		RegisterPet{}, UpdatePetProfile{}, AssignMicrochip{}, ChangePetPhoto{}, MarkPetDeceased{},
		UndoDeceased{}, GrantCareLink{}, RevokeCareLink{}, InitiateGuardianshipTransfer{},
		CancelGuardianshipTransfer{}, AcceptGuardianshipTransfer{},
	}
	for _, c := range cmds {
		n := c.(interface{ CommandName() string }).CommandName()
		if !want[n] {
			t.Errorf("nombre de comando inesperado: %s", n)
		}
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("faltan comandos: %v", want)
	}
}

// TestEventNamesAndVersions: los 11 eventos declaran nombre y versión (v1
// salvo PetRegistered, v2 desde F2 por el upcaster de alerts).
func TestEventNamesAndVersions(t *testing.T) {
	events := []interface {
		EventName() string
		SchemaVersion() int
	}{
		PetRegistered{}, PetProfileUpdated{}, PetMicrochipAssigned{}, PetPhotoChanged{},
		PetMarkedDeceased{}, PetDeceasedUndone{}, CareLinkGranted{}, CareLinkRevoked{},
		GuardianshipTransferInitiated{}, GuardianshipTransferCancelled{}, PetGuardianshipTransferred{},
	}
	names := map[string]bool{}
	for _, e := range events {
		want := 1
		if e.EventName() == "PetRegistered" {
			want = 2
		}
		if e.SchemaVersion() != want {
			t.Errorf("%s: schema_version %d", e.EventName(), e.SchemaVersion())
		}
		names[e.EventName()] = true
	}
	if len(names) != 11 {
		t.Errorf("se esperaban 11 eventos, hay %d", len(names))
	}
}

// TestPet_ApplyChanges_AllFields cubre todas las ramas de mutación del replay.
func TestPet_ApplyChanges_AllFields(t *testing.T) {
	p := newPet()
	if err := p.ApplyEvent(registered()); err != nil {
		t.Fatal(err)
	}
	if err := p.ApplyEvent(PetProfileUpdated{PetID: petID, Changes: Changes{
		Name: ptr("Lunita"), Species: ptr("cat"), SpeciesOther: ptr(""), Breed: ptr("Mestizo"),
		Sex: ptr("male"), Neutered: ptr("no"),
		BirthDate: &BirthDatePayload{Date: "2022-01-02", Approximate: true},
		Markings:  ptr("blanca"),
	}}); err != nil {
		t.Fatal(err)
	}
	if p.name != "Lunita" || p.species != SpeciesCat || p.speciesOther != "" ||
		p.breed == nil || *p.breed != "Mestizo" || p.sex != SexMale || p.neutered != TristateNo ||
		p.birthDate == nil || p.birthDate.Date != kernel.CivilDate("2022-01-02") || !p.birthDate.Approximate ||
		p.markings == nil || *p.markings != "blanca" {
		t.Fatalf("applyChanges: %+v", p)
	}
	// Vaciar opcionales.
	if err := p.ApplyEvent(PetProfileUpdated{PetID: petID, Changes: Changes{
		Breed: ptr(""), BirthDate: &BirthDatePayload{}, Markings: ptr(""),
	}}); err != nil {
		t.Fatal(err)
	}
	if p.breed != nil || p.birthDate != nil || p.markings != nil {
		t.Fatalf("vaciar opcionales: %+v", p)
	}
}

// TestPet_ActiveCareLinkOrgIDs_OrderIndependent.
func TestPet_ActiveCareLinkOrgIDs_OrderIndependent(t *testing.T) {
	p := newPet()
	_ = p.ApplyEvent(registered())
	orgs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	for _, o := range orgs {
		_ = p.ApplyEvent(CareLinkGranted{PetID: petID, OrganizationID: o, GrantedBy: guardianID})
	}
	got := p.ActiveCareLinkOrgIDs()
	if len(got) != 2 {
		t.Fatalf("esperaba 2 activos, got %v", got)
	}
}

// TestPet_CivilDateHelpers cubre el límite de 40 años y la rama DeceasedAt nil.
func TestPet_CivilDateHelpers(t *testing.T) {
	today := kernel.CivilDate("2026-10-20")
	if min := CivilMinBirthDate(today); min.String() != "1986-10-20" {
		t.Fatalf("min: %s", min)
	}
	p := newPet()
	_ = p.ApplyEvent(registered())
	if _, err := p.HandleCommand(MarkPetDeceased{PetID: p.id, Now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if p.Status() != PetDeceased || p.deceasedAt != nil {
		t.Fatalf("deceased sin fecha: %+v", p)
	}
}
