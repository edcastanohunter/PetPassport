// Package notify es el puerto por el que un comando de otro módulo encola una
// notificación cuyo contenido no puede viajar en un evento (p. ej. el token en
// claro de una invitación o de una transferencia: en el event store solo vive
// su hash). El adaptador (notifications/infra) escribe una fila en
// notifications.deliveries; el SMTP solo lo abre el worker (PP-F2-ARQ-014).
package notify

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Notification es una entrega inmediata a encolar.
type Notification struct {
	Rule     string // regla del catálogo (07 §1), p. ej. "member_invited"
	Template string // plantilla, p. ej. "org_invitation"
	Category string // appointments|preventive_care|clinical|account|security
	// RecipientID es el usuario destinatario; uuid.Nil si aún no tiene cuenta
	// (invitados): entonces To es obligatorio.
	RecipientID uuid.UUID
	To          string
	Locale      string // "es" por defecto cuando se desconoce
	SubjectID   uuid.UUID
	Occurrence  string // distingue envíos del mismo sujeto (id de invitación + rotación)
	// Payload son variables de la plantilla. Los valores de Secret se purgan
	// de la fila en cuanto la entrega sale (o falla definitivamente).
	Payload map[string]string
	Secret  []string // claves de Payload que contienen secretos de un solo uso
}

// Enqueuer encola una notificación de forma durable. Si el contexto lleva una
// transacción de unidad de trabajo, se escribe en ella; si no, en una propia.
type Enqueuer interface {
	Enqueue(ctx context.Context, n Notification) error
}

// ghostNamespace deriva un id estable para destinatarios sin cuenta.
var ghostNamespace = uuid.MustParse("6f1b8f0e-5b7c-4a62-9a52-0d7c6c0f1a11")

// GhostRecipientID es el RecipientID estable de una dirección sin cuenta.
func GhostRecipientID(address string) uuid.UUID {
	return uuid.NewSHA1(ghostNamespace, []byte(strings.ToLower(strings.TrimSpace(address))))
}
