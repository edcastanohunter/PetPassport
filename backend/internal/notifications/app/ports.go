// Package app contiene la lógica de Notifications sin SQL ni SMTP (ADR-012): el
// planificador traduce eventos del outbox en acciones (enviar ya, programar,
// cancelar), el sender decide qué hacer con una entrega y el servicio de
// preferencias atiende la API. Los adaptadores (notifications/infra) los
// conectan con PostgreSQL y el proveedor de email.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
)

// User es lo que se necesita de una cuenta para notificarla.
type User struct {
	ID          uuid.UUID
	Email       string
	Locale      string // es|en
	DisplayName string
	Country     string
}

// Pet es lo mínimo de la mascota (nunca datos clínicos).
type Pet struct {
	ID         uuid.UUID
	Name       string
	GuardianID uuid.UUID
	Deceased   bool
}

// CardEntry es la entrada del carnet que respalda un recordatorio.
type CardEntry struct {
	ID      uuid.UUID
	PetID   uuid.UUID
	Status  string // active|voided
	NextDue string // YYYY-MM-DD o vacío
}

// Org es lo mínimo de la organización.
type Org struct {
	ID           uuid.UUID
	Name         string
	Country      string
	ContactEmail string
	Status       string
}

// Appointment es la vista de la cita que las reglas necesitan.
type Appointment struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	PetID      uuid.UUID
	GuardianID uuid.UUID
	Status     string
	Start      *time.Time
	TZ         string
}

// Encounter es la vista de la consulta para los avisos de control.
type Encounter struct {
	ID       uuid.UUID
	PetID    uuid.UUID
	OrgID    uuid.UUID
	Status   string
	FollowUp *FollowUp
}

// FollowUp es el control recomendado de una consulta firmada.
type FollowUp struct {
	DueDate string // YYYY-MM-DD
}

// Directory resuelve destinatarios y datos de presentación desde los read
// models. En producción lee con la misma transacción del outbox, de modo que
// ve las proyecciones síncronas del evento que está procesando.
type Directory interface {
	User(ctx context.Context, id uuid.UUID) (User, bool, error)
	Pet(ctx context.Context, id uuid.UUID) (Pet, bool, error)
	Org(ctx context.Context, id uuid.UUID) (Org, bool, error)
	Appointment(ctx context.Context, id uuid.UUID) (Appointment, bool, error)
	Encounter(ctx context.Context, id uuid.UUID) (Encounter, bool, error)
	CardEntry(ctx context.Context, id uuid.UUID) (CardEntry, bool, error)
	// OrgMembers devuelve los miembros activos con alguno de los roles.
	OrgMembers(ctx context.Context, orgID uuid.UUID, roles ...string) ([]uuid.UUID, error)
	// FutureConfirmedGuardians: tutores con citas confirmadas futuras en la org.
	FutureConfirmedGuardians(ctx context.Context, orgID uuid.UUID, now time.Time) ([]uuid.UUID, error)
}

// Subject types de notifications.scheduled.
const (
	SubjectAppointment = "appointment"
	SubjectCardEntry   = "card_entry"
	SubjectEncounter   = "encounter"
)

// Action es lo que el planificador decide para un evento.
type Action interface{ action() }

// SendNow encola una entrega inmediata (idempotente por Dedup).
type SendNow struct {
	Rule        string
	Template    string
	Category    domain.Category
	Recipient   User
	SubjectType string
	SubjectID   uuid.UUID
	Occurrence  string // event_id
	Vars        map[string]string
}

// Schedule crea o reprograma un recordatorio (upsert por Dedup).
type Schedule struct {
	Rule        string
	Template    string
	Category    domain.Category
	RecipientID uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Offset      string
	DueAt       time.Time
	Vars        map[string]string
}

// CancelSubject cancela los recordatorios pendientes de un sujeto. Si Rules
// no está vacío, solo los de esas reglas.
type CancelSubject struct {
	SubjectType string
	SubjectID   uuid.UUID
	Rules       []string
}

// CancelPet cancela todos los recordatorios pendientes de una mascota
// (fallecimiento): las filas llevan pet_id en su payload.
type CancelPet struct{ PetID uuid.UUID }

func (SendNow) action()       {}
func (Schedule) action()      {}
func (CancelSubject) action() {}
func (CancelPet) action()     {}

// DedupKey de una entrega inmediata.
func (a SendNow) DedupKey() string {
	return domain.DedupKey(a.Rule, a.SubjectID.String(), a.Recipient.ID.String(), a.Occurrence)
}

// DedupKey del recordatorio programado (una fila por rule|sujeto|destinatario|offset).
func (a Schedule) DedupKey() string {
	return domain.DedupKey(a.Rule, a.SubjectID.String(), a.RecipientID.String(), a.Offset)
}

// Delivery es una entrega pendiente de envío (fila de notifications.deliveries).
type Delivery struct {
	ID          uuid.UUID
	DedupKey    string
	Category    domain.Category
	Template    string
	Locale      string
	RecipientID uuid.UUID
	Payload     map[string]string
	Attempts    int
	CreatedAt   time.Time
}

// ErrPermanent marca un fallo de entrega que no se reintenta (dirección
// inválida): la entrega pasa a failed y se suprime la dirección.
var ErrPermanent = errors.New("notifications: fallo permanente de entrega")
