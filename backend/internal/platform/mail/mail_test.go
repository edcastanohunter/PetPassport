package mail

import (
	"context"
	"strings"
	"testing"
)

func TestRender_LinksUsePublicBaseURL(t *testing.T) {
	cases := []struct {
		template string
		data     map[string]string
		want     string
	}{
		{TemplateMemberInvited, map[string]string{"org_name": "Clínica Sur", "role": "veterinarian", "token": "abc"}, "https://app.example/invitations/abc"},
		{TemplateTransferInitiated, map[string]string{"pet_name": "Luna", "from_name": "Ana", "token": "xyz", "expires_at": "2026-11-01"}, "https://app.example/app/transfers/xyz"},
		{TemplateTransferCompleted, map[string]string{"pet_name": "Luna", "to_name": "Beto"}, "https://app.example/app"},
		{TemplateOrgVerified, map[string]string{"org_name": "Clínica Sur"}, "Clínica Sur"},
		{TemplateOrgRejected, map[string]string{"org_name": "Clínica Sur", "reason": "sin RUT"}, "sin RUT"},
	}
	for _, c := range cases {
		for _, locale := range []string{"es", "en", "fr"} {
			subject, body := Render(c.template, locale, c.data, "https://app.example")
			if subject == "" || !strings.Contains(body, c.want) {
				t.Errorf("%s/%s: subject=%q body=%q", c.template, locale, subject, body)
			}
		}
	}
	if s, _ := Render("desconocida", "es", nil, ""); s != "PetPassport" {
		t.Errorf("plantilla desconocida: %q", s)
	}
}

func TestSMTP_RejectsHeaderInjectionAndMissingHost(t *testing.T) {
	if err := NewSMTP(Options{}).Send(context.Background(), Message{To: "a@b.co"}); err == nil {
		t.Fatal("sin host debe fallar")
	}
	s := NewSMTP(Options{Host: "127.0.0.1", Port: 1, From: "no-reply@x"})
	if err := s.Send(context.Background(), Message{To: "a@b.co\r\nBcc: x@y.z", Subject: "s"}); err == nil || !strings.Contains(err.Error(), "cabecera") {
		t.Fatalf("inyección no rechazada: %v", err)
	}
}
