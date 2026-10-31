package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

var (
	reqID    = uuid.MustParse("0192f3a1-3333-7000-8000-000000000010")
	vetUser  = uuid.MustParse("0192f3a1-4444-7000-8000-000000000020")
	reqAt    = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	reqUntil = reqAt.Add(CareLinkRequestTTL)
)

func requested() CareLinkRequested {
	return CareLinkRequested{
		PetID: petID, RequestID: reqID, OrganizationID: orgID, RequestedBy: vetUser,
		Message: "Atendemos a Luna desde el lunes", ExpiresAt: reqUntil,
	}
}

func requestCmd() RequestCareLink {
	return RequestCareLink{
		PetID: petID, RequestID: reqID, OrganizationID: orgID, RequestedBy: vetUser,
		Message: "  Atendemos a Luna desde el lunes ", Now: reqAt,
	}
}

func TestCareLinkRequest_RequestEmitsWith14DayExpiry(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered()).When(requestCmd()).Then(requested())
}

func TestCareLinkRequest_P2_NoActiveLinkNorPendingRequest(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).
		Given(registered(), CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID}).
		When(requestCmd()).ThenError(ErrCareLinkAlreadyActive)
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(requestCmd()).ThenError(ErrRequestPending)
	// Otra organización sí puede pedir mientras la primera sigue pendiente.
	other := requestCmd()
	other.OrganizationID = uuid.MustParse("0192f3a1-5555-7000-8000-000000000030")
	other.RequestID = uuid.MustParse("0192f3a1-3333-7000-8000-000000000011")
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).When(other).
		Then(CareLinkRequested{
			PetID: petID, RequestID: other.RequestID, OrganizationID: other.OrganizationID, RequestedBy: vetUser,
			Message: "Atendemos a Luna desde el lunes", ExpiresAt: reqUntil,
		})
}

func TestCareLinkRequest_ValidationAndDeceased(t *testing.T) {
	long := requestCmd()
	long.Message = strings.Repeat("a", MaxRequestMessageRunes+1)
	testkit.Scenario[*Pet](t, newPet).Given(registered()).When(long).ThenError(&kernel.ValidationError{})
	testkit.Scenario[*Pet](t, newPet).Given(registered(), PetMarkedDeceased{PetID: petID}).
		When(requestCmd()).ThenError(ErrPetDeceased)
}

func TestCareLinkRequest_ApproveGrantsTheLink(t *testing.T) {
	by := vetUser
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(ApproveCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: reqAt.Add(time.Hour)}).
		Then(
			CareLinkRequestApproved{PetID: petID, RequestID: reqID, OrganizationID: orgID, ApprovedBy: guardianID},
			CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID, RequestedBy: &by},
		)
}

func TestCareLinkRequest_ApproveDeclineGuards(t *testing.T) {
	late := reqUntil.Add(time.Second)
	// Inexistente.
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(ApproveCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: reqAt}).
		ThenError(ErrRequestNotFound)
	// Vencida por reloj aunque el scheduler aún no la haya expirado.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(ApproveCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: late}).
		ThenError(ErrRequestNotPending)
	// Ya resuelta: no se aprueba dos veces ni se rechaza después.
	resolved := []any{registered(), requested(),
		CareLinkRequestDeclined{PetID: petID, RequestID: reqID, OrganizationID: orgID, DeclinedBy: guardianID}}
	testkit.Scenario[*Pet](t, newPet).Given(resolved...).
		When(ApproveCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: reqAt}).
		ThenError(ErrRequestNotPending)
	testkit.Scenario[*Pet](t, newPet).Given(resolved...).
		When(DeclineCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: reqAt}).
		ThenError(ErrRequestNotPending)
	// Límite de vínculos activos (I8).
	given := []any{registered()}
	for i := 0; i < MaxActiveCareLinks; i++ {
		given = append(given, CareLinkGranted{PetID: petID, OrganizationID: uuid.New(), GrantedBy: guardianID})
	}
	given = append(given, requested())
	testkit.Scenario[*Pet](t, newPet).Given(given...).
		When(ApproveCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Now: reqAt}).
		ThenError(ErrCareLinkLimit)
}

func TestCareLinkRequest_Decline(t *testing.T) {
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(DeclineCareLinkRequest{PetID: petID, RequestID: reqID, ActorID: guardianID, Reason: " no la conozco ", Now: reqAt}).
		Then(CareLinkRequestDeclined{PetID: petID, RequestID: reqID, OrganizationID: orgID, DeclinedBy: guardianID, Reason: "no la conozco"})
}

func TestCareLinkRequest_ExpireIsIdempotentAndHonoursTheClock(t *testing.T) {
	expired := CareLinkRequestExpired{PetID: petID, RequestID: reqID, OrganizationID: orgID}
	// Aún vigente: no hace nada (el scheduler puede adelantarse).
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(ExpireCareLinkRequest{PetID: petID, RequestID: reqID, Now: reqAt.Add(time.Hour)}).ThenNoEvents()
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested()).
		When(ExpireCareLinkRequest{PetID: petID, RequestID: reqID, Now: reqUntil}).Then(expired)
	// Ya resuelta o repetida: sin eventos.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested(), expired).
		When(ExpireCareLinkRequest{PetID: petID, RequestID: reqID, Now: reqUntil.Add(time.Hour)}).ThenNoEvents()
	// Tras expirar, la clínica puede volver a pedir.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), requested(), expired).
		When(requestCmd()).Then(requested())
}

func TestPetAlerts_UpdateValidateAndNoChange(t *testing.T) {
	alerts := []PetAlert{{Kind: AlertAllergy, Text: "  Penicilina "}, {Kind: AlertBehavior, Text: "Muerde en la consulta"}}
	clean := []PetAlert{{Kind: AlertAllergy, Text: "Penicilina"}, {Kind: AlertBehavior, Text: "Muerde en la consulta"}}
	testkit.Scenario[*Pet](t, newPet).Given(registered()).
		When(UpdatePetAlerts{PetID: petID, ActorID: guardianID, Alerts: alerts}).
		Then(PetAlertsUpdated{PetID: petID, Alerts: clean, UpdatedBy: guardianID})
	// Sin cambios reales → ErrNoChanges.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), PetAlertsUpdated{PetID: petID, Alerts: clean, UpdatedBy: guardianID}).
		When(UpdatePetAlerts{PetID: petID, ActorID: guardianID, Alerts: clean}).ThenError(ErrNoChanges)
	// Se pueden vaciar.
	testkit.Scenario[*Pet](t, newPet).Given(registered(), PetAlertsUpdated{PetID: petID, Alerts: clean, UpdatedBy: guardianID}).
		When(UpdatePetAlerts{PetID: petID, ActorID: guardianID, Alerts: nil}).
		Then(PetAlertsUpdated{PetID: petID, Alerts: []PetAlert{}, UpdatedBy: guardianID})
	for _, bad := range [][]PetAlert{
		{{Kind: "otra", Text: "x"}},
		{{Kind: AlertOther, Text: ""}},
		{{Kind: AlertOther, Text: strings.Repeat("x", MaxAlertTextRunes+1)}},
	} {
		testkit.Scenario[*Pet](t, newPet).Given(registered()).
			When(UpdatePetAlerts{PetID: petID, ActorID: guardianID, Alerts: bad}).ThenError(&kernel.ValidationError{})
	}
}
