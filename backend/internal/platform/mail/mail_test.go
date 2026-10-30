package mail

import (
	"io"
	"mime"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"
)

func newTestSMTP() *SMTP {
	return &SMTP{host: "h", from: "PetPassport <no-reply@notify.example>", now: func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }}
}

func TestBuildMultipartWithUnsubscribeHeaders(t *testing.T) {
	raw, err := newTestSMTP().build(Message{
		To: "tutor@example.com", Subject: "Cita confirmada: Luna", Text: "hola ñandú", HTML: "<p>hola ñandú</p>",
		MessageID: "<abc@notify.example>", UnsubscribeURL: "https://app.example/api/v1/unsubscribe/tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := stdmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Header.Get("Message-ID"); got != "<abc@notify.example>" {
		t.Fatalf("message-id: %q", got)
	}
	if got := m.Header.Get("List-Unsubscribe"); got != "<https://app.example/api/v1/unsubscribe/tok>" {
		t.Fatalf("list-unsubscribe: %q", got)
	}
	if got := m.Header.Get("List-Unsubscribe-Post"); got != "List-Unsubscribe=One-Click" {
		t.Fatalf("list-unsubscribe-post: %q", got)
	}
	if !strings.HasPrefix(m.Header.Get("Content-Type"), "multipart/alternative") {
		t.Fatalf("content-type: %q", m.Header.Get("Content-Type"))
	}
	dec := new(mime.WordDecoder)
	if subj, _ := dec.DecodeHeader(m.Header.Get("Subject")); subj != "Cita confirmada: Luna" {
		t.Fatalf("subject: %q", subj)
	}
	body, _ := io.ReadAll(m.Body)
	if !strings.Contains(string(body), "text/plain") || !strings.Contains(string(body), "text/html") {
		t.Fatalf("faltan partes: %s", body)
	}
}

func TestBuildPlainTextAndGeneratedID(t *testing.T) {
	raw, err := newTestSMTP().build(Message{To: "a@b.co", Subject: "x", Text: "hola"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := stdmail.ReadMessage(strings.NewReader(string(raw)))
	if !strings.HasPrefix(m.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("content-type: %q", m.Header.Get("Content-Type"))
	}
	if id := m.Header.Get("Message-ID"); !strings.HasSuffix(id, "@notify.example>") {
		t.Fatalf("message-id generado: %q", id)
	}
	if m.Header.Get("List-Unsubscribe") != "" {
		t.Fatal("sin URL no hay List-Unsubscribe")
	}
}

func TestBuildRejectsHeaderInjection(t *testing.T) {
	s := newTestSMTP()
	for _, m := range []Message{
		{To: "a@b.co\r\nBcc: x@y.z", Subject: "x", Text: "t"},
		{To: "a@b.co", Subject: "x\nBcc: x@y.z", Text: "t"},
		{To: "a@b.co", Subject: "x", Text: "t", MessageID: "<a>\r\nX: y"},
		{To: "a@b.co", Subject: "x", Text: "t", UnsubscribeURL: "https://x\r\nBcc: z"},
	} {
		if _, err := s.build(m); err == nil {
			t.Errorf("debía rechazar %+v", m)
		}
	}
}

func TestSendWithoutHostFails(t *testing.T) {
	if err := NewSMTP(Options{}).Send(t.Context(), Message{To: "a@b.co", Text: "t"}); err == nil {
		t.Fatal("sin SMTP configurado debe fallar")
	}
}

func TestEnvelopeAndDomain(t *testing.T) {
	if got := envelopeAddress("PetPassport <no-reply@notify.example>"); got != "no-reply@notify.example" {
		t.Fatalf("envelope: %q", got)
	}
	if got := domainOf("no-reply@notify.example"); got != "notify.example" {
		t.Fatalf("domain: %q", got)
	}
	if got := domainOf("sin-arroba"); got != "localhost" {
		t.Fatalf("domain: %q", got)
	}
}
