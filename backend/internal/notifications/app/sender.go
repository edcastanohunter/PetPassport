package app

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/notifications/templates"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Email es el mensaje ya renderizado que recibe el canal.
type Email struct {
	To             string
	Subject        string
	HTML           string
	Text           string
	MessageID      string // propio, estable por entrega (NOT-005)
	UnsubscribeURL string // endpoint de un clic para List-Unsubscribe-Post; vacío en security
}

// Channel envía un email (F2: SMTP del proveedor transaccional). Devuelve el
// id del mensaje asignado por el proveedor si lo conoce.
type Channel interface {
	Send(ctx context.Context, m Email) (providerID string, err error)
}

// ErrInvalidAddress es un fallo permanente por dirección inexistente o
// inválida: además de no reintentar, la dirección se suprime.
var ErrInvalidAddress = fmt.Errorf("%w: dirección inválida", ErrPermanent)

// Reader de preferencias y supresiones, y del último envío (limitación).
type (
	// PrefReader lee las preferencias de un usuario (defaults si no hay fila).
	PrefReader interface {
		Preferences(ctx context.Context, user uuid.UUID) (domain.Preferences, error)
	}
	// SuppressionStore consulta y registra direcciones suprimidas (por hash).
	SuppressionStore interface {
		Suppressed(ctx context.Context, addressHash []byte) (bool, error)
		Suppress(ctx context.Context, addressHash []byte, reason string) error
	}
	// ThrottleReader devuelve cuándo se envió por última vez la plantilla.
	ThrottleReader interface {
		LastSent(ctx context.Context, recipient uuid.UUID, template string) (time.Time, bool, error)
	}
	// UserLookup resuelve la cuenta del destinatario.
	UserLookup interface {
		User(ctx context.Context, id uuid.UUID) (User, bool, error)
	}
)

// SenderConfig son los datos fijos del envío.
type SenderConfig struct {
	BaseURL           string // PP_PUBLIC_BASE_URL
	APIBaseURL        string // base pública de la API, para el baja de un clic
	Address           string // dirección física del pie
	FromDomain        string // dominio para el Message-ID
	UnsubscribeSecret []byte
}

// Sender decide el resultado de una entrega: suprimirla (preferencia o
// dirección suprimida), diferirla (limitación) o enviarla por el canal.
type Sender struct {
	users    UserLookup
	prefs    PrefReader
	supp     SuppressionStore
	throttle ThrottleReader
	channel  Channel
	cfg      SenderConfig
	clock    kernel.Clock
}

// NewSender construye el sender.
func NewSender(users UserLookup, prefs PrefReader, supp SuppressionStore, throttle ThrottleReader, ch Channel, cfg SenderConfig, clock kernel.Clock) *Sender {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	return &Sender{users: users, prefs: prefs, supp: supp, throttle: throttle, channel: ch, cfg: cfg, clock: clock}
}

// Outcome es la decisión sobre una entrega ya reclamada (attempts ya incluye
// este intento). Exactamente una de las ramas aplica según Status.
type Outcome struct {
	Status    string        // sent | suppressed | failed | queued (diferida)
	Reason    string        // suppressed: preference|address; failed: causa sin PII
	MessageID string        // proveedor
	Wait      time.Duration // failed/queued: espera hasta el siguiente intento
	Dead      bool          // failed definitivo (sin más intentos)
	Deferred  bool          // queued por limitación: no consume intento
}

// Payload keys reservadas.
const (
	KeyTo     = "_to"     // dirección de un destinatario sin cuenta
	KeySecret = "_secret" // lista (coma) de claves con secretos de un solo uso
)

// Deliver procesa una entrega. El error devuelto es de infraestructura
// (BD) y deja la entrega para reintento; los fallos de envío van en Outcome.
func (s *Sender) Deliver(ctx context.Context, d Delivery) (Outcome, error) {
	to, locale, userID, hasAccount, err := s.recipient(ctx, d)
	if err != nil {
		return Outcome{}, err
	}
	if to == "" {
		return failed(d, "recipient_unavailable", true), nil
	}
	hash := AddressHash(to)
	if sup, err := s.supp.Suppressed(ctx, hash); err != nil {
		return Outcome{}, err
	} else if sup {
		return Outcome{Status: domain.StatusSuppressed, Reason: "address"}, nil
	}
	if hasAccount {
		p, err := s.prefs.Preferences(ctx, userID)
		if err != nil {
			return Outcome{}, err
		}
		if !p.Allows(d.Category) {
			return Outcome{Status: domain.StatusSuppressed, Reason: "preference"}, nil
		}
	}
	now := s.clock.Now()
	if domain.Throttled(d.Template, d.Category) {
		if last, ok, err := s.throttle.LastSent(ctx, d.RecipientID, d.Template); err != nil {
			return Outcome{}, err
		} else if ok && now.Sub(last) < domain.ThrottleWindow {
			return Outcome{Status: domain.StatusQueued, Deferred: true, Wait: last.Add(domain.ThrottleWindow).Sub(now)}, nil
		}
	}

	r, err := s.render(d, locale, userID, hasAccount, now)
	if err != nil {
		// Plantilla o variables rotas: no se arregla reintentando.
		return failed(d, "render", true), nil
	}
	providerID, err := s.channel.Send(ctx, Email{
		To: to, Subject: r.Subject, HTML: r.HTML, Text: r.Text,
		MessageID: "<" + d.ID.String() + "@" + s.cfg.FromDomain + ">", UnsubscribeURL: s.oneClickURL(d, userID, hasAccount, now),
	})
	switch {
	case err == nil:
		return Outcome{Status: domain.StatusSent, MessageID: providerID}, nil
	case errors.Is(err, ErrInvalidAddress):
		_ = s.supp.Suppress(ctx, hash, "invalid_address")
		return failed(d, "invalid_address", true), nil
	case errors.Is(err, ErrPermanent):
		return failed(d, "permanent", true), nil
	}
	return failed(d, "transient", false), nil
}

// failed: terminal si permanent; si no, backoff o dead-letter según intentos.
func failed(d Delivery, reason string, permanent bool) Outcome {
	if permanent {
		return Outcome{Status: domain.StatusFailed, Reason: reason, Dead: true}
	}
	wait, dead := domain.NextAttempt(d.Attempts)
	return Outcome{Status: domain.StatusFailed, Reason: reason, Wait: wait, Dead: dead}
}

// recipient resuelve la dirección y el idioma. Un destinatario sin cuenta
// lleva su dirección en el payload (KeyTo).
func (s *Sender) recipient(ctx context.Context, d Delivery) (addr, locale string, id uuid.UUID, hasAccount bool, err error) {
	if to := d.Payload[KeyTo]; to != "" {
		return to, d.Locale, d.RecipientID, false, nil
	}
	u, ok, err := s.users.User(ctx, d.RecipientID)
	if err != nil || !ok {
		return "", "", uuid.Nil, false, err
	}
	loc := u.Locale
	if loc == "" {
		loc = d.Locale
	}
	return u.Email, loc, u.ID, true, nil
}

func (s *Sender) render(d Delivery, locale string, userID uuid.UUID, hasAccount bool, now time.Time) (templates.Rendered, error) {
	vars := make(map[string]string, len(d.Payload)+4)
	for k, v := range d.Payload {
		vars[k] = v
	}
	if p := vars["link_path"]; p != "" {
		vars["link"] = strings.TrimRight(s.cfg.BaseURL, "/") + p
	}
	localizeTimes(vars, locale)
	c := templates.Common{
		BaseURL: s.cfg.BaseURL, PreferencesURL: strings.TrimRight(s.cfg.BaseURL, "/") + "/app/settings/notifications",
		Address: s.cfg.Address, Category: string(d.Category),
	}
	if hasAccount && d.Category.Optional() {
		tok := domain.SignUnsubscribe(s.cfg.UnsubscribeSecret, userID, d.Category, now.Add(domain.UnsubscribeTTL))
		c.UnsubscribeURL = strings.TrimRight(s.cfg.BaseURL, "/") + "/unsubscribe/" + tok
	}
	return templates.Render(d.Template, locale, vars, c)
}

// oneClickURL es el endpoint POST de la API para List-Unsubscribe-Post.
func (s *Sender) oneClickURL(d Delivery, userID uuid.UUID, hasAccount bool, now time.Time) string {
	if !hasAccount || !d.Category.Optional() || s.cfg.APIBaseURL == "" {
		return ""
	}
	tok := domain.SignUnsubscribe(s.cfg.UnsubscribeSecret, userID, d.Category, now.Add(domain.UnsubscribeTTL))
	return strings.TrimRight(s.cfg.APIBaseURL, "/") + "/unsubscribe/" + tok
}

// AddressHash es el hash estable de una dirección (minúsculas, sin espacios).
func AddressHash(address string) []byte {
	return kernel.HashToken(NormalizeAddress(address))
}

// NormalizeAddress deja la dirección en su forma de comparación.
func NormalizeAddress(address string) string {
	a := strings.TrimSpace(address)
	if p, err := mail.ParseAddress(a); err == nil {
		a = p.Address
	}
	return strings.ToLower(a)
}

// localizeTimes convierte cada `clave@time` (RFC 3339) en `clave` formateada
// en la zona `tz` y el idioma del destinatario.
func localizeTimes(vars map[string]string, locale string) {
	tz := vars["tz"]
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	for k, v := range vars {
		name, ok := strings.CutSuffix(k, "@time")
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			vars[name] = v
			continue
		}
		vars[name] = formatTime(t.In(loc), locale)
		delete(vars, k)
	}
}

var (
	esDays   = []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	esMonths = []string{"", "ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

func formatTime(t time.Time, locale string) string {
	if locale == "en" {
		return t.Format("Mon, Jan 2 · 3:04 PM")
	}
	return fmt.Sprintf("%s %d %s · %02d:%02d", esDays[t.Weekday()], t.Day(), esMonths[t.Month()], t.Hour(), t.Minute())
}
