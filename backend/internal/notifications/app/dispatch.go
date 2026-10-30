package app

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Scheduled es un recordatorio vencido a punto de despacharse.
type Scheduled struct {
	ID          uuid.UUID
	DedupKey    string
	Rule        string
	Category    string
	Template    string
	RecipientID uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	DueAt       time.Time
	Payload     map[string]string
}

// StillValid re-valida el sujeto al despachar (NOT-002): la cita sigue
// confirmada, la dosis sigue pendiente con la misma fecha, el control no se
// retiró, la mascota sigue viva y el destinatario sigue siendo su tutor.
// Un recordatorio que ya no aplica se cancela sin enviar nada.
func StillValid(ctx context.Context, dir Directory, s Scheduled) (bool, error) {
	switch s.SubjectType {
	case SubjectAppointment:
		a, ok, err := dir.Appointment(ctx, s.SubjectID)
		if err != nil || !ok {
			return false, err
		}
		return a.Status == "confirmed" && a.GuardianID == s.RecipientID, nil
	case SubjectCardEntry:
		c, ok, err := dir.CardEntry(ctx, s.SubjectID)
		if err != nil || !ok {
			return false, err
		}
		if c.Status != "active" || c.NextDue == "" || c.NextDue != s.Payload["due_date"] {
			return false, nil
		}
		return guardianOf(ctx, dir, c.PetID, s.RecipientID)
	case SubjectEncounter:
		e, ok, err := dir.Encounter(ctx, s.SubjectID)
		if err != nil || !ok {
			return false, err
		}
		if e.FollowUp == nil || e.FollowUp.DueDate != s.Payload["due_date"] {
			return false, nil
		}
		return guardianOf(ctx, dir, e.PetID, s.RecipientID)
	}
	return false, nil
}

func guardianOf(ctx context.Context, dir Directory, petID, user uuid.UUID) (bool, error) {
	p, ok, err := dir.Pet(ctx, petID)
	if err != nil || !ok {
		return false, err
	}
	return !p.Deceased && p.GuardianID == user, nil
}
