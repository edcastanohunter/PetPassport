package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

func TestPet_RegisterTwiceRejected(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(RegisterPet{GuardianID: guardianID, Name: "Otra", Species: "cat", Sex: "unknown", Neutered: "unknown", Now: now}).
		ThenError(kernel.NewDomainError("PET_ALREADY_REGISTERED", ""))
}

func TestPet_UnknownCommandRejected(t *testing.T) {
	p := newPet()
	if _, err := p.HandleCommand(struct{ X int }{1}); err == nil {
		t.Fatal("comando desconocido debe fallar")
	}
	if err := p.ApplyEvent(struct{ X int }{1}); err == nil {
		t.Fatal("evento desconocido debe fallar")
	}
}

func TestPet_RegisterValidationBranches(t *testing.T) {
	bad := []RegisterPet{
		{Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes", Now: now},                            // guardian nil
		{GuardianID: guardianID, Name: "Luna", Species: "perro", Sex: "female", Neutered: "yes", Now: now},  // especie
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "noaplica", Neutered: "yes", Now: now},  // sexo
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "female", Neutered: "talvez", Now: now}, // esterilizado
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes", Breed: ptr(repeat("b", 81)), Now: now},
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes", Markings: ptr(repeat("m", 201)), Now: now},
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes", BirthDate: &BirthDateInput{Date: "2030-01-01"}, Now: now},
		{GuardianID: guardianID, Name: "Luna", Species: "dog", Sex: "female", Neutered: "yes", BirthDate: &BirthDateInput{Date: "no-fecha"}, Now: now},
	}
	for i, cmd := range bad {
		testkit.Scenario[*Pet](t, newPet).When(cmd).ThenError(&kernel.ValidationError{})
		_ = i
	}
}

func TestPet_UpdateProfileValidationBranches(t *testing.T) {
	bad := []UpdatePetProfile{
		{PetID: petID, Name: ptr(""), Now: now},
		{PetID: petID, Species: ptr("dragón"), Now: now},
		{PetID: petID, Sex: ptr("noaplica"), Now: now},
		{PetID: petID, Neutered: ptr("talvez"), Now: now},
		{PetID: petID, Breed: ptr(repeat("b", 81)), Now: now},
		{PetID: petID, Markings: ptr(repeat("m", 201)), Now: now},
		{PetID: petID, BirthDate: &BirthDateInput{Date: "2030-01-01"}, Now: now},
		{PetID: petID, Species: ptr("other"), Now: now}, // other sin texto
	}
	for _, cmd := range bad {
		testkit.Scenario[*Pet](t, newPet).Given(registered()).When(cmd).ThenError(&kernel.ValidationError{})
	}
	// Cambiar a otra especie exige texto; a especie cerrada lo limpia.
	testkit.Scenario[*Pet](t, newPet).
		Given(PetRegistered{PetID: petID, GuardianID: guardianID, Name: "Luna", Species: SpeciesOther, SpeciesOther: "Erizo", Sex: SexUnknown, Neutered: TristateUnknown}).
		When(UpdatePetProfile{PetID: petID, Species: ptr("dog"), Now: now}).
		Then(PetProfileUpdated{PetID: petID, Changes: Changes{Species: ptr("dog"), SpeciesOther: ptr("")}})
}

func TestPet_PhotoNoopsAndValidation(t *testing.T) {
	ref := &PhotoRef{
		ObjectKey: "pets/" + petID.String() + "/a.webp", ThumbKey: "pets/" + petID.String() + "/a_t.webp", ContentType: "webp",
	}
	// Quitar foto inexistente: sin evento.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(ChangePetPhoto{PetID: petID}).ThenNoEvents()
	// Misma foto: sin evento.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), PetPhotoChanged{PetID: petID, Photo: ref}).
		When(ChangePetPhoto{PetID: petID, Photo: ref}).ThenNoEvents()
	// Prefijo incorrecto: inválida.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(ChangePetPhoto{PetID: petID, Photo: &PhotoRef{ObjectKey: "other/x.webp", ThumbKey: "other/x_t.webp", ContentType: "webp"}}).
		ThenError(&kernel.ValidationError{})
	// Quitar foto existente emite PetPhotoChanged con photo nil y previous.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), PetPhotoChanged{PetID: petID, Photo: ref}).
		When(ChangePetPhoto{PetID: petID}).
		Then(PetPhotoChanged{PetID: petID, Previous: ref})
}

func TestPet_AssignMicrochipInvalidFormat(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(AssignMicrochip{PetID: petID, Microchip: "123"}).
		ThenError(&kernel.ValidationError{})
}

func TestPet_Accessors(t *testing.T) {
	p := newPet()
	second := uuid.Must(uuid.NewV7())
	for _, e := range []any{
		registered(),
		CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID},
		CareLinkGranted{PetID: petID, OrganizationID: second, GrantedBy: guardianID},
		CareLinkRevoked{PetID: petID, OrganizationID: second, RevokedBy: guardianID},
	} {
		if err := p.ApplyEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	if p.ID() != petID || p.Version() != 4 || p.GuardianID() != guardianID || p.Status() != PetActive || p.IsDeceased() {
		t.Fatalf("accesores: %+v", p)
	}
	if len(p.UncommittedEvents()) != 0 {
		t.Fatal("sin eventos no confirmados tras replay")
	}
	// record marca no confirmados; MarkCommitted los limpia sin tocar la versión.
	_, _ = p.HandleCommand(RevokeCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID})
	if len(p.UncommittedEvents()) != 1 || p.Version() != 5 {
		t.Fatalf("uncommitted: %d v%d", len(p.UncommittedEvents()), p.Version())
	}
	p.MarkCommitted()
	if len(p.UncommittedEvents()) != 0 || p.Version() != 5 {
		t.Fatalf("commit: %d v%d", len(p.UncommittedEvents()), p.Version())
	}
	active := p.ActiveCareLinkOrgIDs()
	if len(active) != 0 {
		t.Fatalf("vínculos activos tras revocar: %v", active)
	}
}

func TestPet_TransferValidationBranches(t *testing.T) {
	// Email destino inválido.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(InitiateGuardianshipTransfer{PetID: petID, ToEmail: "no-email", CurrentGuardianEmail: "ana@example.com", Now: now}).
		ThenError(&kernel.ValidationError{})
	// Email del tutor actual inválido (dato interno corrupto): validación.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(InitiateGuardianshipTransfer{PetID: petID, ToEmail: "nueva@example.com", CurrentGuardianEmail: "roto", Now: now}).
		ThenError(&kernel.ValidationError{})
	// Caducidad en el pasado.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(InitiateGuardianshipTransfer{PetID: petID, ToEmail: "nueva@example.com", CurrentGuardianEmail: "ana@example.com", ExpiresAt: now.Add(-time.Hour), Now: now}).
		ThenError(&kernel.ValidationError{})
	// Aceptar sin pendiente.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(AcceptGuardianshipTransfer{PetID: petID, ActorEmail: "nueva@example.com", ActorUserID: uuid.Must(uuid.NewV7()), Now: now}).
		ThenError(ErrTransferNotPending)
}

func TestPet_TransferEventWithoutPIIRejected(t *testing.T) {
	p := newPet()
	if err := p.ApplyEvent(GuardianshipTransferInitiated{PetID: petID, FromGuardianID: guardianID, ExpiresAt: now}); err == nil {
		t.Fatal("evento sin destinatario debe fallar")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
