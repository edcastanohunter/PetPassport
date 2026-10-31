// Package mail es el adaptador SMTP del email transaccional. Desde F2 (ADR-012,
// PP-F2-ARQ-014) solo lo usa el worker de Notifications: ningún comando ni
// reacción abre SMTP. Los correos llegan ya renderizados (plantillas en
// notifications/templates); los enlaces se construyen solo con
// PP_PUBLIC_BASE_URL, nunca desde cabeceras Host (SEC-004).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// Mailer es el puerto de envío.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Message es un email ya renderizado.
type Message struct {
	To      string
	Subject string
	Text    string // text/plain (obligatorio)
	HTML    string // text/html opcional: con él el mensaje es multipart/alternative

	// MessageID propio (con <>), estable por entrega: el proveedor puede
	// deduplicar reenvíos. Vacío = se genera uno.
	MessageID string
	// UnsubscribeURL añade List-Unsubscribe y List-Unsubscribe-Post (un clic).
	UnsubscribeURL string
}

// SMTP adapta net/smtp (Mailpit en dev; proveedor gestionado en staging/prod).
type SMTP struct {
	host string
	addr string
	from string
	auth smtp.Auth // nil sin credenciales (Mailpit)
	now  func() time.Time
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
	s := &SMTP{host: o.Host, addr: fmt.Sprintf("%s:%d", o.Host, o.Port), from: o.From, now: time.Now}
	if o.User != "" {
		s.auth = smtp.PlainAuth("", o.User, o.Password, o.Host)
	}
	return s
}

// Send implementa Mailer. El error de net/smtp se devuelve tal cual
// (*textproto.Error con el código SMTP) para que el llamador lo clasifique.
func (s *SMTP) Send(_ context.Context, msg Message) error {
	if s.host == "" {
		return fmt.Errorf("mail: SMTP no configurado")
	}
	raw, err := s.build(msg)
	if err != nil {
		return err
	}
	return smtp.SendMail(s.addr, s.auth, envelopeAddress(s.from), []string{msg.To}, raw)
}

// build arma el mensaje RFC 5322. Rechaza saltos de línea en las cabeceras
// (inyección de cabeceras).
func (s *SMTP) build(msg Message) ([]byte, error) {
	for _, h := range []string{msg.To, msg.Subject, msg.MessageID, msg.UnsubscribeURL} {
		if strings.ContainsAny(h, "\r\n") {
			return nil, fmt.Errorf("mail: cabecera inválida")
		}
	}
	id := msg.MessageID
	if id == "" {
		var b [12]byte
		_, _ = rand.Read(b[:])
		id = "<" + hex.EncodeToString(b[:]) + "@" + domainOf(s.from) + ">"
	}
	var buf bytes.Buffer
	h := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	h("From", s.from)
	h("To", msg.To)
	h("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	h("Date", s.now().UTC().Format(time.RFC1123Z))
	h("Message-ID", id)
	h("MIME-Version", "1.0")
	if msg.UnsubscribeURL != "" {
		h("List-Unsubscribe", "<"+msg.UnsubscribeURL+">")
		h("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	if msg.HTML == "" {
		h("Content-Type", "text/plain; charset=utf-8")
		h("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		if err := writeQP(&buf, msg.Text); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	mw := multipart.NewWriter(&buf)
	h("Content-Type", "multipart/alternative; boundary="+mw.Boundary())
	buf.WriteString("\r\n")
	for _, part := range []struct{ ct, body string }{{"text/plain", msg.Text}, {"text/html", msg.HTML}} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ct + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeQP(pw, part.body); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return err
	}
	return qp.Close()
}

// envelopeAddress extrae la dirección de un "Nombre <a@b>".
func envelopeAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

func domainOf(from string) string {
	a := envelopeAddress(from)
	if i := strings.LastIndex(a, "@"); i >= 0 {
		return a[i+1:]
	}
	return "localhost"
}
