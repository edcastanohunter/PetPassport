// Package mail es el puerto de email transaccional (DOM-011): en F1 los
// envíos son síncronos y best-effort (un fallo se registra pero NO revierte
// el comando); en F2 migran al process manager del outbox (ADR-012).
// Los enlaces se construyen solo con PP_PUBLIC_BASE_URL, nunca desde
// cabeceras Host (SEC-004).
package mail

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// Mailer es el puerto de envío.
type Mailer interface {
	// Send envía un email ya renderizado. El que llama registra el error y
	// sigue (best-effort).
	Send(ctx context.Context, msg Message) error
}

// Message es un email a enviar.
type Message struct {
	To      string
	Subject string
	Body    string
}

// TemplateIDs de F1 (DOM-011).
const (
	TemplateMemberInvited     = "member_invited"
	TemplateTransferInitiated = "transfer_initiated"
	TemplateTransferCompleted = "transfer_completed"
	TemplateOrgVerified       = "org_verified"
	TemplateOrgRejected       = "org_rejected"
)

// SMTP adapta net/smtp (Mailpit en dev; proveedor gestionado en staging/prod).
type SMTP struct {
	host string
	addr string
	from string
	auth smtp.Auth // nil sin credenciales (Mailpit)
}

// Options configura el adaptador.
type Options struct {
	Host     string
	Port     int
	From     string
	User     string
	Password string
}

// NewSMTP construye el adaptador.
func NewSMTP(o Options) *SMTP {
	s := &SMTP{host: o.Host, addr: fmt.Sprintf("%s:%d", o.Host, o.Port), from: o.From}
	if o.User != "" {
		s.auth = smtp.PlainAuth("", o.User, o.Password, o.Host)
	}
	return s
}

// Send implementa Mailer (best-effort: el error se devuelve; el que llama
// decide registrar y seguir).
func (s *SMTP) Send(_ context.Context, msg Message) error {
	if s.host == "" {
		return fmt.Errorf("mail: SMTP no configurado")
	}
	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") {
		return fmt.Errorf("mail: cabecera inválida") // inyección de cabeceras
	}
	body := strings.Join([]string{
		"From: " + s.from,
		"To: " + msg.To,
		"Subject: " + msg.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		msg.Body,
	}, "\r\n")
	return smtp.SendMail(s.addr, s.auth, s.from, []string{msg.To}, []byte(body))
}

// Render rellena una plantilla por locale. Texto plano; los enlaces siempre
// contra PublicBaseURL (SEC-004).
func Render(template, locale string, data map[string]string, base string) (subject, body string) {
	if locale != "en" {
		locale = "es"
	}
	switch template {
	case TemplateMemberInvited:
		if locale == "en" {
			return "You've been invited to join an organization on PetPassport",
				fmt.Sprintf("Hi!\n\nYou've been invited to join %s as %s.\nAccept your invitation at:\n%s/invitations/%s\n\nIf you weren't expecting this email, ignore it.", data["org_name"], data["role"], base, data["token"])
		}
		return "Te invitaron a una organización en PetPassport",
			fmt.Sprintf("¡Hola!\n\nTe invitaron a unirte a %s como %s.\nAcepta tu invitación en:\n%s/invitations/%s\n\nSi no esperabas este correo, ignóralo.", data["org_name"], data["role"], base, data["token"])
	case TemplateTransferInitiated:
		if locale == "en" {
			return "A pet guardianship transfer is waiting for you",
				fmt.Sprintf("Hi!\n\n%s wants to transfer guardianship of %s to you.\nIf you don't have an account yet, sign up with this email first.\nReview and accept at:\n%s/app/transfers/%s\n\nThe link expires on %s.", data["from_name"], data["pet_name"], base, data["token"], data["expires_at"])
		}
		return "Tienes pendiente una transferencia de tutela",
			fmt.Sprintf("¡Hola!\n\n%s quiere transferirte la tutela de %s.\nSi aún no tienes cuenta, regístrate primero con este email.\nRevisa y acepta en:\n%s/app/transfers/%s\n\nEl enlace caduca el %s.", data["from_name"], data["pet_name"], base, data["token"], data["expires_at"])
	case TemplateTransferCompleted:
		if locale == "en" {
			return "Guardianship transferred",
				fmt.Sprintf("The guardianship of %s was transferred to %s. Care links and history travel with the pet.\n%s/app", data["pet_name"], data["to_name"], base)
		}
		return "Transferencia de tutela completada",
			fmt.Sprintf("La tutela de %s fue transferida a %s. Los vínculos con clínicas y la historia viajan con la mascota.\n%s/app", data["pet_name"], data["to_name"], base)
	case TemplateOrgRejected:
		if locale == "en" {
			return "Your organization could not be verified",
				fmt.Sprintf("We could not verify %s.\nReason: %s\n%s/app", data["org_name"], data["reason"], base)
		}
		return "No pudimos verificar tu organización",
			fmt.Sprintf("No pudimos verificar %s.\nMotivo: %s\n%s/app", data["org_name"], data["reason"], base)
	case TemplateOrgVerified:
		if locale == "en" {
			return "Your organization is verified on PetPassport",
				fmt.Sprintf("%s is now verified. You can receive care links from pet owners.\n%s/app", data["org_name"], base)
		}
		return "Tu organización está verificada en PetPassport",
			fmt.Sprintf("%s está verificada. Ya puedes recibir vínculos de mascotas de sus tutores.\n%s/app", data["org_name"], base)
	}
	return "PetPassport", "Notification from PetPassport."
}
