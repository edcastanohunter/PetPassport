package infra

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
)

// SMTPChannel es el app.Channel de F2: email por el SMTP del proveedor
// transaccional (IAM-012). Clasifica los fallos por código SMTP: 5xx son
// permanentes (550/551/553, dirección inválida; el resto, rechazo del
// mensaje) y 4xx o errores de red se reintentan con backoff.
type SMTPChannel struct{ mailer mail.Mailer }

var _ app.Channel = (*SMTPChannel)(nil)

// NewSMTPChannel construye el canal.
func NewSMTPChannel(m mail.Mailer) *SMTPChannel { return &SMTPChannel{mailer: m} }

// Send implementa app.Channel. El id del proveedor es el Message-ID propio.
func (c *SMTPChannel) Send(ctx context.Context, e app.Email) (string, error) {
	err := c.mailer.Send(ctx, mail.Message{
		To: e.To, Subject: e.Subject, Text: e.Text, HTML: e.HTML,
		MessageID: e.MessageID, UnsubscribeURL: e.UnsubscribeURL,
	})
	if err != nil {
		return "", classify(err)
	}
	return e.MessageID, nil
}

// classify traduce el error de net/smtp. El texto del error del servidor
// puede contener la dirección: solo se conserva el código.
func classify(err error) error {
	var te *textproto.Error
	if !errors.As(err, &te) {
		return fmt.Errorf("smtp: error de transporte") // reintentable, sin detalle
	}
	switch {
	case te.Code == 550 || te.Code == 551 || te.Code == 553:
		return fmt.Errorf("%w: SMTP %d", app.ErrInvalidAddress, te.Code)
	case te.Code >= 500:
		return fmt.Errorf("%w: SMTP %d", app.ErrPermanent, te.Code)
	}
	return fmt.Errorf("smtp: respuesta temporal %d", te.Code)
}
