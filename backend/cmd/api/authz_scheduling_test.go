package main

// Fixture de Scheduling para la suite por rutas (PP-F2-SEC-001, M2.3): una
// cita solicitada por el tutor en orgActive. Los comandos fallan tras authz
// (fakes): lo que se prueba es la decisión y su traducción a 403/404.

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	schedulingapp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	schedulingdomain "github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	schedulinghttp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/http"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var apptID = uuid.MustParse("01930000-0000-7000-8000-00000000f001")

type fakeSchedulingService struct{}

func (fakeSchedulingService) Dispatch(context.Context, bus.Command) (bus.Result, error) {
	return bus.Result{}, errSuite
}

func (fakeSchedulingService) Get(_ context.Context, id uuid.UUID) (schedulingapp.AppointmentView, error) {
	if id != apptID {
		return schedulingapp.AppointmentView{}, &kernel.NotFoundError{Resource: "appointment"}
	}
	now := time.Now()
	return schedulingapp.AppointmentView{
		ID: apptID, Version: 1, PetID: petID, GuardianID: tutorID, OrganizationID: orgActive,
		Type: "consultation", Origin: "guardian", Status: "requested", CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (fakeSchedulingService) List(context.Context, schedulingapp.ListFilter) ([]schedulingapp.AppointmentView, string, error) {
	return nil, "", nil
}

func (fakeSchedulingService) Agendas(context.Context, uuid.UUID) ([]*schedulingdomain.ProfessionalAgenda, error) {
	return nil, nil
}

func (fakeSchedulingService) Availability(context.Context, uuid.UUID, *uuid.UUID, schedulingdomain.AppointmentType,
	time.Time, time.Time, bool) ([]schedulingapp.ProfessionalSlot, error) {
	return nil, nil
}

type fakeAgendaQueries struct{}

func (fakeAgendaQueries) Agenda(context.Context, uuid.UUID, uuid.UUID) (*schedulingdomain.ProfessionalAgenda, error) {
	return nil, &kernel.NotFoundError{Resource: "agenda"}
}

func (fakeAgendaQueries) ProfessionalNames(context.Context, []uuid.UUID) (map[uuid.UUID]string, error) {
	return nil, nil
}

// fakeSchedulingPets: la mascota es del tutor y solo orgActive tiene vínculo.
type fakeSchedulingPets struct{}

func (fakeSchedulingPets) Pet(context.Context, uuid.UUID) (schedulingapp.PetInfo, error) {
	return schedulingapp.PetInfo{ID: petID, GuardianID: tutorID, Name: "Luna", Species: "dog"}, nil
}

func (fakeSchedulingPets) Linked(_ context.Context, _, org uuid.UUID) (bool, error) {
	return org == orgActive, nil
}

func (fakeSchedulingPets) GuardianLinked(_ context.Context, guardian, org uuid.UUID) (bool, error) {
	return guardian == tutorID && org == orgActive, nil
}

type fakeSchedulingMembership struct{}

func (fakeSchedulingMembership) RoleOf(context.Context, uuid.UUID, uuid.UUID) (string, bool, error) {
	return "", false, nil
}

func (fakeSchedulingMembership) Org(context.Context, uuid.UUID) (schedulingapp.OrgInfo, error) {
	return schedulingapp.OrgInfo{ID: orgActive, Country: "CO", Type: "clinic", Status: "active"}, nil
}

func (fakeSchedulingMembership) Veterinarians(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func newSuiteSchedulingHandlers(idem *idempotency.Store, logger *slog.Logger) *schedulinghttp.SchedulingHandlers {
	return schedulinghttp.NewSchedulingHandlers(schedulinghttp.Deps{
		Service: fakeSchedulingService{}, Agendas: fakeAgendaQueries{}, Pets: fakeSchedulingPets{},
		Membership: fakeSchedulingMembership{}, Idem: idem, Logger: logger,
	})
}

// Filas de Scheduling (SEC-001, docs/phase-2/08 §2) sobre la cita del
// fixture. Columnas: tutor vetL admL recL vetU vetS phAd str sup. Una cita
// ajena es 404 (no se revela); las rutas de organización sin pertenencia
// responden 403.
var schedulingMatrix = map[string][9]int{
	// Tutor.
	"requestAppointment":        {ok, 404, 404, 404, 404, 404, 404, 404, 404},
	"listMyAppointments":        {ok, ok, ok, ok, ok, ok, ok, ok, ok},
	"getAppointment":            {ok, ok, ok, ok, 404, 404, 404, 404, 404},
	"acceptAppointmentProposal": {ok, 403, 403, 403, 404, 404, 404, 404, 404},
	"cancelAppointment":         {ok, ok, ok, ok, 404, 404, 404, 404, 404},
	// Clínica: appointment:manage — miembro clínico activo.
	"createOrgAppointment":          {403, ok, ok, ok, 403, 403, 403, 403, 403},
	"listOrgAppointments":           {403, ok, ok, ok, 403, 403, 403, 403, 403},
	"confirmAppointment":            {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"proposeAppointmentAlternative": {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"rejectAppointment":             {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"rescheduleAppointment":         {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"checkInAppointment":            {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"markAppointmentNoShow":         {403, ok, ok, ok, 404, 404, 404, 404, 404},
	"completeAppointment":           {403, ok, ok, ok, 404, 404, 404, 404, 404},
	// appointment:cancel admite también al tutor; el servicio exige membresía
	// cuando se cancela por la ruta de la organización.
	"cancelOrgAppointment": {ok, ok, ok, ok, 404, 404, 404, 404, 404},
	// Agendas: lectura a miembros clínicos, escritura solo a clinic_admin.
	"listOrgAgendas":        {403, ok, ok, ok, 403, 403, 403, 403, 403},
	"putOrgAgenda":          {403, 403, ok, 403, 403, 403, 403, 403, 403},
	"addAgendaException":    {403, 403, ok, 403, 403, 403, 403, 403, 403},
	"removeAgendaException": {403, 403, ok, 403, 403, 403, 403, 403, 403},
	// availability:read — miembros y tutores vinculados; el resto no ve la agenda.
	"getOrgAvailability": {ok, ok, ok, ok, 404, 404, 404, 404, 404},
}

func init() {
	for k, v := range schedulingMatrix {
		matrix[k] = v
	}
}
