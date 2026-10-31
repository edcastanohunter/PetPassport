package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// defaultTimeZone propone la zona horaria de una agenda nueva según el país
// de la organización; el admin puede indicar otra al crearla.
func defaultTimeZone(country string) string {
	switch country {
	case "CO":
		return "America/Bogota"
	case "MX":
		return "America/Mexico_City"
	case "AR":
		return "America/Argentina/Buenos_Aires"
	case "CL":
		return "America/Santiago"
	case "PE":
		return "America/Lima"
	case "EC":
		return "America/Guayaquil"
	case "ES":
		return "Europe/Madrid"
	}
	return "UTC"
}

func (s *Service) agendaMeta(ctx context.Context, orgID uuid.UUID) eventstore.Metadata {
	return s.metadata(ctx, orgID)
}

// runAgenda carga la agenda, ejecuta los comandos de dominio en orden y
// persiste + proyecta lo emitido.
func (s *Service) runAgenda(ctx context.Context, orgID, profID uuid.UUID,
	build func(ctx context.Context, a *domain.ProfessionalAgenda) ([]any, error)) (bus.Result, error) {
	var res bus.Result
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		a, err := s.agendas.Load(ctx, orgID, profID)
		if err != nil {
			return err
		}
		cc := CommandContextFrom(ctx)
		if cc.ExpectedVersion > 0 && a.Version() != cc.ExpectedVersion {
			return &kernel.ConcurrencyError{Expected: cc.ExpectedVersion, Actual: a.Version()}
		}
		cmds, err := build(ctx, a)
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			if _, err := a.HandleCommand(cmd); err != nil {
				return err
			}
		}
		if len(a.UncommittedEvents()) == 0 {
			res = bus.Result{AggregateID: profID, Version: a.Version()}
			return nil
		}
		stored, err := s.agendas.Save(ctx, a, s.agendaMeta(ctx, orgID))
		if err != nil {
			return err
		}
		res = bus.Result{AggregateID: profID, Version: versionOf(stored)}
		return s.projects.Apply(ctx, stored)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return res, nil
}

func (s *Service) handleSaveAgenda(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(SaveAgenda)
	role, active, err := s.membership.RoleOf(ctx, cmd.ProfessionalID, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	profOK := active && role == "veterinarian"
	org, err := s.membership.Org(ctx, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	now, me := s.clock.Now(), actor(ctx)
	return s.runAgenda(ctx, cmd.OrganizationID, cmd.ProfessionalID,
		func(_ context.Context, a *domain.ProfessionalAgenda) ([]any, error) {
			if !a.Defined() {
				cfg := cmd.Config
				if cfg.TimeZone == "" {
					cfg.TimeZone = defaultTimeZone(org.Country)
				}
				out := []any{domain.DefineAgenda{
					OrganizationID: cmd.OrganizationID, ProfessionalID: cmd.ProfessionalID,
					Config: cfg, ProfessionalOK: profOK, By: me, Now: now,
				}}
				if !cmd.Active {
					out = append(out, domain.DeactivateAgenda{By: me, Now: now})
				}
				return out, nil
			}
			out := []any{
				domain.UpdateWeeklyTemplate{WeeklyTemplate: cmd.Config.WeeklyTemplate, By: me, Now: now},
				domain.SetSlotDurations{SlotMinutes: cmd.Config.SlotMinutes, By: me, Now: now},
				domain.SetPolicies{
					LeadMinHours: cmd.Config.LeadMinHours, LeadMaxDays: cmd.Config.LeadMaxDays,
					CancelHours: cmd.Config.CancelPolicyHrs, By: me, Now: now,
				},
			}
			if cmd.Active {
				out = append(out, domain.ActivateAgenda{By: me, Now: now})
			} else {
				out = append(out, domain.DeactivateAgenda{By: me, Now: now})
			}
			return out, nil
		})
}

func (s *Service) handleAddException(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(AddAgendaException)
	return s.runAgenda(ctx, cmd.OrganizationID, cmd.ProfessionalID,
		func(_ context.Context, _ *domain.ProfessionalAgenda) ([]any, error) {
			return []any{domain.AddException{Date: cmd.Date, Exception: cmd.Exception, By: actor(ctx), Now: s.clock.Now()}}, nil
		})
}

func (s *Service) handleRemoveException(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(RemoveAgendaException)
	return s.runAgenda(ctx, cmd.OrganizationID, cmd.ProfessionalID,
		func(_ context.Context, _ *domain.ProfessionalAgenda) ([]any, error) {
			return []any{domain.RemoveException{Date: cmd.Date, By: actor(ctx), Now: s.clock.Now()}}, nil
		})
}

// ---------------------------------------------------------- disponibilidad

// ProfessionalSlot es una franja libre de un profesional concreto.
type ProfessionalSlot struct {
	ProfessionalID uuid.UUID
	Start, End     time.Time
}

// MaxAvailabilityWindow acota el rango consultable (la consulta es en Go).
const MaxAvailabilityWindow = 62 * 24 * time.Hour

// Availability calcula la disponibilidad publicada de la organización
// (API-008): plantilla − excepciones − reservas vigentes. Es informativa; la
// autoridad es la restricción de exclusión al confirmar (ADR-011).
func (s *Service) Availability(ctx context.Context, orgID uuid.UUID, professionalID *uuid.UUID,
	typ domain.AppointmentType, from, to time.Time, guardianView bool) ([]ProfessionalSlot, error) {
	if !to.After(from) || to.Sub(from) > MaxAvailabilityWindow {
		ve := &kernel.ValidationError{}
		ve.Add("to", "OUT_OF_RANGE")
		return nil, ve
	}
	var agendas []*domain.ProfessionalAgenda
	if professionalID != nil {
		a, err := s.agendaReader.Agenda(ctx, orgID, *professionalID)
		var nf *kernel.NotFoundError
		if err != nil && !errors.As(err, &nf) {
			return nil, err
		}
		if a != nil {
			agendas = append(agendas, a)
		}
	} else {
		var err error
		if agendas, err = s.agendaReader.Agendas(ctx, orgID); err != nil {
			return nil, err
		}
	}
	now := s.clock.Now()
	var out []ProfessionalSlot
	for _, a := range agendas {
		if !a.Active() {
			continue
		}
		reserved, err := s.reservations.Reserved(ctx, a.Professional(), from, to)
		if err != nil {
			return nil, err
		}
		for _, sl := range a.Availability(domain.AvailabilityQuery{
			Type: typ, From: from, To: to, Now: now, Reserved: reserved, Guardian: guardianView,
		}) {
			out = append(out, ProfessionalSlot{ProfessionalID: a.Professional(), Start: sl.Start, End: sl.End})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ProfessionalID.String() < out[j].ProfessionalID.String()
	})
	return out, nil
}

// Agendas devuelve las agendas de la organización (lectura de cualquier miembro).
func (s *Service) Agendas(ctx context.Context, orgID uuid.UUID) ([]*domain.ProfessionalAgenda, error) {
	return s.agendaReader.Agendas(ctx, orgID)
}

// Get devuelve la cita (la autorización la decide el handler http).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (AppointmentView, error) {
	return s.queries.Get(ctx, id)
}

// List devuelve citas filtradas.
func (s *Service) List(ctx context.Context, f ListFilter) ([]AppointmentView, string, error) {
	return s.queries.List(ctx, f)
}
