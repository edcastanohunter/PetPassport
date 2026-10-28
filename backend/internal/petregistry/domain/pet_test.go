package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

// Identidades fijas para pruebas deterministas.
var (
	petID      = uuid.MustParse("0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f")
	guardianID = uuid.MustParse("7a8f0000-0000-7000-8000-000000000001")
	orgID      = uuid.MustParse("0192f3a1-1111-7000-8000-000000000002")
	now        = time.Date(2026, 10, 20, 14, 3, 11, 0, time.UTC)
)

func newPet() *Pet { return &Pet{} }

func registered() PetRegistered {
	return PetRegistered{
		PetID: petID, GuardianID: guardianID, Name: "Luna", Species: SpeciesDog,
		Breed: "Beagle", Sex: SexFemale, Neutered: TristateYes,
		BirthDate: &BirthDatePayload{Date: "2021-05-10", Approximate: false},
		Alerts:    []PetAlert{},
	}
}

// --- I1: nombre válido ------------------------------------------------------

func TestPet_I1_NameValidation(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{GuardianID: guardianID, Name: "", Species: "dog", Sex: "female", Neutered: "yes", Now: now}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{GuardianID: guardianID, Name: "Luna\tMia", Species: "dog", Sex: "female", Neutered: "yes", Now: now}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{GuardianID: guardianID, Name: "Luna<script>", Species: "dog", Sex: "female", Neutered: "yes", Now: now}).
		ThenError(&kernel.ValidationError{})
	// Trim y colapso de espacios al persistir.
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{PetID: petID, GuardianID: guardianID, Name: "  Luna   Sol ", Species: "dog", Sex: "female", Neutered: "yes", Now: now}).
		Then(PetRegistered{PetID: petID, GuardianID: guardianID, Name: "Luna Sol", Species: SpeciesDog, Sex: SexFemale, Neutered: TristateYes, Alerts: []PetAlert{}})
}

// --- I3: fallecida solo admite revocar/deshacer/foto ------------------------

func TestPet_I3_DeceasedRejectsCommands(t *testing.T) {
	given := []any{registered(), PetMarkedDeceased{PetID: petID}}
	for _, cmd := range []any{
		UpdatePetProfile{PetID: petID, Name: ptr("Lunita"), Now: now},
		AssignMicrochip{PetID: petID, Microchip: "941000012345678"},
		MarkPetDeceased{PetID: petID, Now: now},
		GrantCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID},
		InitiateGuardianshipTransfer{PetID: petID, ToEmail: "nueva@example.com", CurrentGuardianEmail: "ana@example.com", Now: now},
	} {
		testkit.Scenario[*Pet](t, newPet).Given(given...).When(cmd).ThenError(ErrPetDeceased)
	}
	// Excepciones permitidas: cambiar foto y revocar vínculo.
	testkit.Scenario[*Pet](t, newPet).
		Given(append(append([]any{}, given...), CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID})...).
		When(RevokeCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID}).
		Then(CareLinkRevoked{PetID: petID, OrganizationID: orgID, RevokedBy: guardianID})
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(ChangePetPhoto{PetID: petID, Photo: &PhotoRef{
			ObjectKey: "pets/" + petID.String() + "/a.webp", ThumbKey: "pets/" + petID.String() + "/a_t.webp", ContentType: "webp"}}).
		Then(PetPhotoChanged{PetID: petID, Photo: &PhotoRef{
			ObjectKey: "pets/" + petID.String() + "/a.webp", ThumbKey: "pets/" + petID.String() + "/a_t.webp", ContentType: "webp"}})
}

// --- I4: el nuevo tutor es distinto del actual ------------------------------

func TestPet_I4_TransferToSameGuardianRejected(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(InitiateGuardianshipTransfer{
			PetID: petID, ToEmail: "ana@example.com", CurrentGuardianEmail: "ana@example.com", Now: now,
		}).
		ThenError(ErrSameGuardian)
}

// --- I7: una sola corrección de microchip ----------------------------------

func TestPet_I7_MicrochipCorrectionLimit(t *testing.T) {
	given := []any{
		registered(),
		PetMicrochipAssigned{PetID: petID, Microchip: "941000012345678"},
	}
	// Primera corrección: permitida.
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(AssignMicrochip{PetID: petID, Microchip: "941000012345679"}).
		Then(PetMicrochipAssigned{PetID: petID, Microchip: "941000012345679", Previous: "941000012345678"})
	// Segunda corrección: rechazada.
	testkit.Scenario[*Pet](t, newPet).
		Given(append(append([]any{}, given...), PetMicrochipAssigned{PetID: petID, Microchip: "941000012345679", Previous: "941000012345678"})...).
		When(AssignMicrochip{PetID: petID, Microchip: "941000012345680"}).
		ThenError(ErrMicrochipCorrectionLimit)
	// Reasignar el mismo valor es idempotente (no gasta la corrección).
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(AssignMicrochip{PetID: petID, Microchip: "941000012345678"}).
		ThenNoEvents()
}

// --- I8: máximo 20 vínculos activos -----------------------------------------

func TestPet_I8_CareLinkLimit(t *testing.T) {
	given := []any{registered()}
	for i := 0; i < MaxActiveCareLinks; i++ {
		given = append(given, CareLinkGranted{PetID: petID, OrganizationID: uuid.Must(uuid.NewV7()), GrantedBy: guardianID})
	}
	oneMore := uuid.Must(uuid.NewV7())
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(GrantCareLink{PetID: petID, OrganizationID: oneMore, ActorID: guardianID}).
		ThenError(ErrCareLinkLimit)
	// Revocar uno libera cupo: 19 activos → conceder uno nuevo emite evento.
	toRevoke := given[1].(CareLinkGranted).OrganizationID
	testkit.Scenario[*Pet](t, newPet).
		Given(append(append([]any{}, given...), CareLinkRevoked{PetID: petID, OrganizationID: toRevoke, RevokedBy: guardianID})...).
		When(GrantCareLink{PetID: petID, OrganizationID: oneMore, ActorID: guardianID}).
		Then(CareLinkGranted{PetID: petID, OrganizationID: oneMore, GrantedBy: guardianID})
}

// Comportamientos adicionales del agregado (DOM-003).

func TestPet_RegisterPet_EmitsEventWithDefaults(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{PetID: petID, GuardianID: guardianID, Name: "Rex", Species: "other", SpeciesOther: "Erizo", Sex: "unknown", Neutered: "unknown", Now: now}).
		Then(PetRegistered{PetID: petID, GuardianID: guardianID, Name: "Rex", Species: SpeciesOther, SpeciesOther: "Erizo", Sex: SexUnknown, Neutered: TristateUnknown, Alerts: []PetAlert{}})
}

func TestPet_RegisterPet_SpeciesOtherRequired(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{GuardianID: guardianID, Name: "Rex", Species: "other", Sex: "unknown", Neutered: "unknown", Now: now}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Pet](t, newPet).
		When(RegisterPet{GuardianID: guardianID, Name: "Rex", Species: "dog", SpeciesOther: "no aplica", Sex: "unknown", Neutered: "unknown", Now: now}).
		ThenError(&kernel.ValidationError{})
}

func TestPet_UpdatePetProfile_OnlyRealChanges(t *testing.T) {
	// Sin cambios reales: no emite evento (DOM-003).
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(UpdatePetProfile{PetID: petID, Name: ptr("Luna"), Now: now}).
		ThenNoEvents()
	// Cambio mixto con vaciado de opcional.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(UpdatePetProfile{PetID: petID, Name: ptr("Lunita"), Breed: ptr(""), Markings: ptr("blanca"), Now: now}).
		Then(PetProfileUpdated{PetID: petID, Changes: Changes{Name: ptr("Lunita"), Breed: ptr(""), Markings: ptr("blanca")}})
}

func TestPet_MarkPetDeceased_ValidatesDate(t *testing.T) {
	future := "2027-01-01"
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(MarkPetDeceased{PetID: petID, DeceasedAt: &future, Now: now}).
		ThenError(&kernel.ValidationError{})
	beforeBirth := "2021-01-01"
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(MarkPetDeceased{PetID: petID, DeceasedAt: &beforeBirth, Now: now}).
		ThenError(&kernel.ValidationError{})
	ok := "2026-10-01"
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(MarkPetDeceased{PetID: petID, DeceasedAt: &ok, Now: now}).
		Then(PetMarkedDeceased{PetID: petID, DeceasedAt: "2026-10-01"})
}

func TestPet_UndoDeceased_Window(t *testing.T) {
	given := []any{registered(), PetMarkedDeceased{PetID: petID}}
	// Fuera de ventana y sin super_admin: rechazado.
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(UndoDeceased{PetID: petID}).
		ThenError(ErrUndoWindowExpired)
	// En ventana o super_admin: permitido.
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(UndoDeceased{PetID: petID, WithinUndoWindow: true}).
		Then(PetDeceasedUndone{PetID: petID})
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(UndoDeceased{PetID: petID, ActorIsSuperAdmin: true}).
		Then(PetDeceasedUndone{PetID: petID})
	// No fallecida: error específico.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(UndoDeceased{PetID: petID, WithinUndoWindow: true}).
		ThenError(ErrPetNotDeceased)
}

func TestPet_GrantCareLink_Idempotent(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).
		Given(registered(), CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID}).
		When(GrantCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID}).
		ThenNoEvents()
}

func TestPet_RevokeCareLink_RequiresActive(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(RevokeCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID}).
		ThenError(ErrCareLinkNotActive)
	testkit.Scenario[*Pet](t, newPet).
		Given(registered(), CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID},
			CareLinkRevoked{PetID: petID, OrganizationID: orgID, RevokedBy: guardianID}).
		When(RevokeCareLink{PetID: petID, OrganizationID: orgID, ActorID: guardianID}).
		ThenError(ErrCareLinkNotActive)
}

func TestPet_TransferLifecycle(t *testing.T) {
	expires := now.Add(7 * 24 * time.Hour)
	newGuardian := uuid.Must(uuid.NewV7())
	initiated := GuardianshipTransferInitiated{
		PetID: petID, FromGuardianID: guardianID, ExpiresAt: expires, PII: &TransferPII{ToEmail: "nueva@example.com"},
	}
	// Pendiente única.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), initiated).
		When(InitiateGuardianshipTransfer{PetID: petID, ToEmail: "otra@example.com", CurrentGuardianEmail: "ana@example.com", Now: now}).
		ThenError(ErrTransferAlreadyPending)
	// Aceptar fuera de plazo.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), initiated).
		When(AcceptGuardianshipTransfer{PetID: petID, ActorEmail: "nueva@example.com", ActorUserID: newGuardian, Now: expires.Add(time.Minute)}).
		ThenError(ErrTransferTokenInvalid)
	// Aceptar con email equivocado.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), initiated).
		When(AcceptGuardianshipTransfer{PetID: petID, ActorEmail: "otra@example.com", ActorUserID: newGuardian, Now: now}).
		ThenError(ErrTransferEmailMismatch)
	// Aceptar válido: cambia el tutor, misma PetID.
	s := testkit.Scenario[*Pet](t, newPet).Given(registered(), initiated).
		When(AcceptGuardianshipTransfer{PetID: petID, ActorEmail: "nueva@example.com", ActorUserID: newGuardian, Now: now}).
		Then(PetGuardianshipTransferred{PetID: petID, FromGuardianID: guardianID, ToGuardianID: newGuardian})
	if s.Agg().GuardianID() != newGuardian {
		t.Fatalf("tutor tras aceptar: %v", s.Agg().GuardianID())
	}
	// Cancelar.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), initiated).
		When(CancelGuardianshipTransfer{PetID: petID, Now: now}).
		Then(GuardianshipTransferCancelled{PetID: petID})
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(CancelGuardianshipTransfer{PetID: petID, Now: now}).
		ThenError(ErrTransferNotPending)
}

func TestPet_ReplayRebuildsState(t *testing.T) {
	p := newPet()
	events := []any{
		registered(),
		PetProfileUpdated{PetID: petID, Changes: Changes{Name: ptr("Lunita")}},
		PetMicrochipAssigned{PetID: petID, Microchip: "941000012345678"},
		CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID},
		PetMarkedDeceased{PetID: petID},
	}
	for _, e := range events {
		if err := p.ApplyEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	if p.ID() != petID || p.Version() != int64(len(events)) || p.Status() != PetDeceased ||
		p.name != "Lunita" || p.microchip == nil || p.activeLinks != 1 {
		t.Fatalf("estado tras replay: %+v", p)
	}
}

func ptr[T any](v T) *T { return &v }
