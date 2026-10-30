package authz

import (
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

// Acciones de Scheduling F2 (PP-F2-SEC-001, docs/phase-2/08 §2, ADR-011).
const (
	AppointmentRequest Action = "appointment:request"
	AppointmentRead    Action = "appointment:read"
	AppointmentCancel  Action = "appointment:cancel"
	AppointmentManage  Action = "appointment:manage"
	AgendaManage       Action = "agenda:manage"
	AgendaRead         Action = "agenda:read"
	AvailabilityRead   Action = "availability:read"
)

// AppointmentResource es la vista de autorización de una cita o de la agenda de
// una organización. GuardianID es el tutor de la mascota; LinkedGuardian
// indica que el principal es tutor de una mascota con vínculo activo a la
// organización (lo resuelve el handler con PetAccess).
type AppointmentResource struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	GuardianID     uuid.UUID
	// LinkedGuardian: el tutor tiene una mascota con vínculo activo a la org.
	LinkedGuardian bool
}

func appointmentResource(res Resource) (*AppointmentResource, bool) {
	r, ok := res.(AppointmentResource)
	if !ok || (r.OrganizationID == uuid.Nil && r.GuardianID == uuid.Nil) {
		return nil, false
	}
	return &r, true
}

func apptStaff(p auth.Principal, r *AppointmentResource) bool {
	_, ok := activeClinicMember(p, r.OrganizationID)
	return ok
}

// appointmentRequest: solo el tutor de la mascota solicita (E11).
func appointmentRequest(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := appointmentResource(res)
	if !ok {
		return deny("appointment resource required")
	}
	if p.UserID != r.GuardianID {
		return deny("guardian only")
	}
	return allow
}

// appointmentParticipant: el tutor de la cita o un miembro clínico activo de la org.
func appointmentParticipant(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := appointmentResource(res)
	if !ok {
		return deny("appointment resource required")
	}
	if p.UserID == r.GuardianID || apptStaff(p, r) {
		return allow
	}
	return deny("guardian or clinic member only")
}

// appointmentManage: miembro clínico activo de la organización de la cita.
func appointmentManage(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := appointmentResource(res)
	if !ok {
		return deny("appointment resource required")
	}
	if apptStaff(p, r) {
		return allow
	}
	return deny("clinic member only")
}

// agendaManage: administrador de la organización (07 §agendas).
func agendaManage(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := appointmentResource(res)
	if !ok {
		return deny("agenda resource required")
	}
	if m, ok := activeClinicMember(p, r.OrganizationID); ok && adminRoles[m.Role] {
		return allow
	}
	return deny("clinic admin only")
}

// availabilityRead: miembro clínico activo o tutor vinculado a la org.
func availabilityRead(p auth.Principal, res Resource) Decision {
	if d := authenticatedOnly(p, nil); !d.Allow {
		return d
	}
	r, ok := appointmentResource(res)
	if !ok {
		return deny("availability resource required")
	}
	if apptStaff(p, r) || r.LinkedGuardian {
		return allow
	}
	return deny("clinic member or linked guardian only")
}

func init() {
	policies[AppointmentRequest] = appointmentRequest
	policies[AppointmentRead] = appointmentParticipant
	policies[AppointmentCancel] = appointmentParticipant
	policies[AppointmentManage] = appointmentManage
	policies[AgendaManage] = agendaManage
	policies[AgendaRead] = appointmentManage
	policies[AvailabilityRead] = availabilityRead
}
