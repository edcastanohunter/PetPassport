package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	notificationsapp "github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	notificationsinfra "github.com/edcastanohunter/petpassport/backend/internal/notifications/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// notificationsRunners compone los runners de Notifications (M2.4): el
// despachador de programados (`notifications-dispatch`, NOT-002) y el emisor
// de entregas (`notifications-sender`, NOT-003). El worker es el único
// proceso que abre SMTP (ARQ-014). Falla al arrancar si una regla apunta a
// una plantilla que no existe (NOT-005).
func notificationsRunners(a *app, logger *slog.Logger) ([]worker.Runner, error) {
	if err := notificationsapp.Validate(); err != nil {
		return nil, fmt.Errorf("notifications: %w", err)
	}
	cfg := a.cfg
	metrics := notificationsinfra.NewMetrics(a.tel.Registry)
	pool := a.pool.Pool

	var channel notificationsapp.Channel
	if cfg.SMTP.Host == "" {
		logger.Warn("PP_SMTP_HOST vacío: las notificaciones se programan pero no se envían (solo dev)")
		channel = unconfiguredChannel{}
	} else {
		channel = notificationsinfra.NewSMTPChannel(mail.NewSMTP(mail.Options{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, From: cfg.SMTP.From,
			User: cfg.SMTP.User, Password: cfg.SMTP.Password,
		}))
	}

	secret := []byte(cfg.UnsubscribeSecret)
	prefs := notificationsapp.NewPreferences(notificationsinfra.NewPrefStore(pool), secret, nil)
	suppressions := notificationsinfra.NewSuppressions(pool)
	sender := notificationsapp.NewSender(
		notificationsinfra.NewDirectory(pool), prefs, suppressions, suppressions, channel,
		notificationsapp.SenderConfig{
			BaseURL:           cfg.PublicBaseURL,
			APIBaseURL:        cfg.APIPublicURL,
			Address:           cfg.MailAddress,
			FromDomain:        fromDomain(cfg.SMTP.From),
			UnsubscribeSecret: secret,
		}, kernel.RealClock{})

	dispatcher := notificationsinfra.NewDispatcher(pool, metrics, logger, nil)
	deliverer := notificationsinfra.NewDeliverer(pool, sender, metrics, logger, nil)
	return []worker.Runner{
		{Name: "notifications-dispatch", Run: dispatcher.Run},
		{Name: "notifications-sender", Run: deliverer.Run},
	}, nil
}

// fromDomain extrae el dominio de una dirección remitente (para el Message-ID).
func fromDomain(from string) string {
	if i := strings.LastIndex(from, "@"); i >= 0 {
		return strings.TrimRight(from[i+1:], "> ")
	}
	return "petpassport.local"
}

// unconfiguredChannel falla siempre: sin SMTP las entregas se reintentan con
// backoff y acaban en dead-letter visible, en vez de perderse en silencio.
type unconfiguredChannel struct{}

func (unconfiguredChannel) Send(context.Context, notificationsapp.Email) (string, error) {
	return "", errors.New("SMTP no configurado")
}
