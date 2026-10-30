package infra

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
)

// Enqueuer implementa notify.Enqueuer: escribe la entrega en la tx del
// contexto (UnitOfWork del comando) si la hay, o en su propia sentencia con el
// pool. El SMTP lo abre luego el worker (ARQ-014).
type Enqueuer struct{ pool Querier }

var _ notify.Enqueuer = (*Enqueuer)(nil)

// NewEnqueuer construye el adaptador.
func NewEnqueuer(pool Querier) *Enqueuer { return &Enqueuer{pool: pool} }

// Enqueue valida y encola la notificación.
func (e *Enqueuer) Enqueue(ctx context.Context, n notify.Notification) error {
	cat := domain.Category(n.Category)
	if !cat.Valid() {
		return fmt.Errorf("notifications: categoría %q inválida", n.Category)
	}
	recipient := app.User{ID: n.RecipientID, Locale: n.Locale}
	if n.To != "" {
		recipient.Email = strings.TrimSpace(n.To)
		if recipient.ID == uuid.Nil {
			recipient.ID = notify.GhostRecipientID(recipient.Email)
		}
	}
	if recipient.ID == uuid.Nil {
		return errors.New("notifications: destinatario sin id ni dirección")
	}
	if recipient.Email == "" {
		// Con cuenta: la dirección se resuelve al enviar; el hash lo calcula el store.
		u, ok, err := NewDirectory(e.querier(ctx)).User(ctx, n.RecipientID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("notifications: destinatario %s sin cuenta", n.RecipientID)
		}
		recipient = u
		if n.Locale != "" {
			recipient.Locale = n.Locale
		}
	}
	key := domain.DedupKey(n.Rule, n.SubjectID.String(), recipient.ID.String(), n.Occurrence)
	return NewStore(e.querier(ctx)).insertDelivery(ctx, newDelivery{
		DedupKey: key, Category: cat, Template: n.Template, Recipient: recipient,
		Vars: n.Payload, Secret: n.Secret,
	})
}

func (e *Enqueuer) querier(ctx context.Context) Querier {
	if tx, err := db.TxFrom(ctx); err == nil {
		return tx
	}
	return e.pool
}
