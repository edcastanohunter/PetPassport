package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Antelaciones por defecto cuando la organización aún no tiene agendas activas.
const (
	defaultLeadMin = domain.DefaultLeadMinH * time.Hour
	defaultLeadMax = domain.DefaultLeadMaxDays * 24 * time.Hour
)

// leadBounds devuelve la ventana más amplia que ofrecen las agendas activas
// de la organización: el tutor aún no eligió profesional (RN-11).
func (s *Service) leadBounds(ctx context.Context, orgID uuid.UUID) (time.Duration, time.Duration, error) {
	agendas, err := s.agendaReader.Agendas(ctx, orgID)
	if err != nil {
		return 0, 0, err
	}
	var minD, maxD time.Duration
	found := false
	for _, a := range agendas {
		if !a.Active() {
			continue
		}
		c := a.Config()
		lo, hi := time.Duration(c.LeadMinHours)*time.Hour, time.Duration(c.LeadMaxDays)*24*time.Hour
		if !found || lo < minD {
			minD = lo
		}
		if !found || hi > maxD {
			maxD = hi
		}
		found = true
	}
	if !found {
		return defaultLeadMin, defaultLeadMax, nil
	}
	return minD, maxD, nil
}

func (s *Service) handleRequest(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(RequestAppointment)
	me := actor(ctx)
	pet, err := s.pets.Pet(ctx, cmd.PetID)
	if err != nil {
		return bus.Result{}, err
	}
	if pet.GuardianID != me {
		return bus.Result{}, &kernel.NotFoundError{Resource: "pet"}
	}
	linked, err := s.pets.Linked(ctx, cmd.PetID, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	pending, err := s.queries.PendingRequests(ctx, cmd.PetID, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	lo, hi, err := s.leadBounds(ctx, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	return s.exec(ctx, cmd.AppointmentID, execOpts{create: true},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.RequestAppointment{
				AppointmentID: cmd.AppointmentID, PetID: cmd.PetID, GuardianID: pet.GuardianID,
				OrganizationID: cmd.OrganizationID, Type: cmd.Type, Reason: cmd.Reason,
				Preferences: cmd.Preferences, Linked: linked, Pending: pending,
				LeadMin: lo, LeadMax: hi, RequestedBy: me, Now: s.clock.Now(),
			}, nil
		}, nil)
}

func (s *Service) handleCreate(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(CreateAppointment)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	pet, err := s.pets.Pet(ctx, cmd.PetID)
	if err != nil {
		return bus.Result{}, err
	}
	linked, err := s.pets.Linked(ctx, cmd.PetID, cmd.OrganizationID)
	if err != nil {
		return bus.Result{}, err
	}
	ag, profOK, err := s.professional(ctx, cmd.OrganizationID, cmd.ProfessionalID)
	if err != nil {
		return bus.Result{}, err
	}
	tz, policy := agendaFacts(ag)
	return s.exec(ctx, cmd.AppointmentID, execOpts{create: true},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.CreateAppointment{
				AppointmentID: cmd.AppointmentID, PetID: cmd.PetID, GuardianID: pet.GuardianID,
				OrganizationID: cmd.OrganizationID, ProfessionalID: cmd.ProfessionalID, Type: cmd.Type,
				Reason: cmd.Reason, Start: cmd.Start, End: cmd.End, TZ: tz, PolicyHours: policy,
				Linked: linked, ProfessionalOK: profOK, CreatedBy: actor(ctx), Now: s.clock.Now(),
			}, nil
		},
		func(ctx context.Context, a *domain.Appointment, _ []any) error {
			return s.reserve(ctx, ag, a.ID(), cmd.ProfessionalID, *a.Slot(), nil)
		})
}

// agendaFacts devuelve la zona horaria y la política de cancelación de la agenda.
func agendaFacts(ag *domain.ProfessionalAgenda) (string, int) {
	if ag == nil {
		return "UTC", domain.DefaultCancelHours
	}
	c := ag.Config()
	return c.TimeZone, c.CancelPolicyHrs
}

func (s *Service) handleConfirm(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(ConfirmAppointment)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	ag, profOK, err := s.professional(ctx, cmd.OrganizationID, cmd.ProfessionalID)
	if err != nil {
		return bus.Result{}, err
	}
	tz, policy := agendaFacts(ag)
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.ConfirmAppointment{
				ProfessionalID: cmd.ProfessionalID, Start: cmd.Start, End: cmd.End, TZ: tz, PolicyHours: policy,
				ProfessionalOK: profOK, By: actor(ctx), Now: s.clock.Now(),
			}, nil
		},
		func(ctx context.Context, a *domain.Appointment, _ []any) error {
			return s.reserve(ctx, ag, a.ID(), cmd.ProfessionalID, *a.Slot(), nil)
		})
}

func (s *Service) handlePropose(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(ProposeAlternative)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	ag, profOK, err := s.professional(ctx, cmd.OrganizationID, cmd.ProfessionalID)
	if err != nil {
		return bus.Result{}, err
	}
	tz, policy := agendaFacts(ag)
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.ProposeAlternative{
				ProfessionalID: cmd.ProfessionalID, Start: cmd.Start, End: cmd.End, TZ: tz, PolicyHours: policy,
				ProfessionalOK: profOK, By: actor(ctx), Now: s.clock.Now(),
			}, nil
		},
		func(ctx context.Context, a *domain.Appointment, _ []any) error {
			p, _ := a.ProposedSlot()
			exp := p.ExpiresAt
			return s.reserve(ctx, ag, a.ID(), cmd.ProfessionalID, p.Slot, &exp)
		})
}

func (s *Service) handleAccept(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(AcceptProposal)
	me := actor(ctx)
	return s.exec(ctx, cmd.AppointmentID, execOpts{},
		func(ctx context.Context, a *domain.Appointment) (any, error) {
			if a.GuardianID() != me {
				return nil, &kernel.NotFoundError{Resource: "appointment"}
			}
			return domain.AcceptProposal{By: me, Now: s.clock.Now()}, nil
		},
		func(ctx context.Context, a *domain.Appointment, _ []any) error {
			// La reserva provisional pasa a confirmada. La agenda ya cubrió la
			// franja al proponerla; si el rango cambió en 48 h la restricción
			// de exclusión sigue siendo la autoridad.
			return s.reservations.Reserve(ctx, Reservation{
				AppointmentID: a.ID(), ProfessionalID: *a.ProfessionalID(), Slot: *a.Slot(),
			})
		})
}

func (s *Service) handleReject(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(RejectRequest)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.RejectRequest{Reason: cmd.Reason, By: actor(ctx), Now: s.clock.Now()}, nil
		}, s.release)
}

func (s *Service) handleReschedule(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(RescheduleAppointment)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	var (
		ag     *domain.ProfessionalAgenda
		profOK bool
		prof   uuid.UUID
	)
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, a *domain.Appointment) (any, error) {
			if a.ProfessionalID() == nil {
				return nil, domain.ErrInvalidTransition
			}
			prof = *a.ProfessionalID()
			if cmd.ProfessionalID != nil {
				prof = *cmd.ProfessionalID
			}
			var err error
			if ag, profOK, err = s.professional(ctx, cmd.OrganizationID, prof); err != nil {
				return nil, err
			}
			tz, _ := agendaFacts(ag)
			return domain.Reschedule{
				ProfessionalID: cmd.ProfessionalID, Start: cmd.Start, End: cmd.End, TZ: tz,
				ProfessionalOK: profOK, By: actor(ctx), Now: s.clock.Now(),
			}, nil
		},
		func(ctx context.Context, a *domain.Appointment, _ []any) error {
			return s.reserve(ctx, ag, a.ID(), prof, *a.Slot(), nil)
		})
}

func (s *Service) handleCancel(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(CancelAppointment)
	me := actor(ctx)
	opts := execOpts{}
	by := domain.ByGuardian
	if cmd.OrganizationID != nil {
		if err := s.requireStaff(ctx, *cmd.OrganizationID); err != nil {
			return bus.Result{}, err
		}
		opts.orgID, by = cmd.OrganizationID, domain.ByClinic
	}
	return s.exec(ctx, cmd.AppointmentID, opts,
		func(ctx context.Context, a *domain.Appointment) (any, error) {
			if by == domain.ByGuardian && a.GuardianID() != me {
				return nil, &kernel.NotFoundError{Resource: "appointment"}
			}
			return domain.Cancel{Reason: cmd.Reason, By: by, ByUser: me, Now: s.clock.Now()}, nil
		}, s.release)
}

func (s *Service) handleCheckIn(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(CheckInAppointment)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.CheckIn{By: actor(ctx), Now: s.clock.Now()}, nil
		}, nil)
}

func (s *Service) handleNoShow(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(MarkAppointmentNoShow)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.MarkNoShow{Now: s.clock.Now()}, nil
		}, s.release)
}

func (s *Service) handleComplete(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(CompleteAppointment)
	if err := s.requireStaff(ctx, cmd.OrganizationID); err != nil {
		return bus.Result{}, err
	}
	return s.exec(ctx, cmd.AppointmentID, execOpts{orgID: &cmd.OrganizationID},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.Complete{Now: s.clock.Now()}, nil
		}, s.release)
}

// --- comandos del sistema (sin Principal: scheduler de comandos / PM) ---

func (s *Service) handleExpire(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(ExpireProposal)
	return s.exec(ctx, cmd.AppointmentID, execOpts{},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.ExpireProposal{Now: s.clock.Now()}, nil
		},
		s.release)
}

func (s *Service) handleAutoNoShow(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(AutoNoShow)
	return s.exec(ctx, cmd.AppointmentID, execOpts{},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.MarkNoShow{Automatic: true, Now: s.clock.Now()}, nil
		}, s.release)
}

func (s *Service) handleCompleteOnSign(ctx context.Context, c bus.Command) (bus.Result, error) {
	cmd := c.(CompleteOnSign)
	enc := cmd.EncounterID
	return s.exec(ctx, cmd.AppointmentID, execOpts{},
		func(ctx context.Context, _ *domain.Appointment) (any, error) {
			return domain.Complete{EncounterID: &enc, Now: s.clock.Now()}, nil
		}, s.release)
}
