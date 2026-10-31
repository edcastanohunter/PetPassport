package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
)

func TestParsePreferencesInvalidJSON(t *testing.T) {
	if _, err := domain.ParsePreferences([]byte(`{`), nil); err == nil {
		t.Fatal("email inválido debe fallar")
	}
	if _, err := domain.ParsePreferences(nil, []byte(`not-json`)); err == nil {
		t.Fatal("digest inválido debe fallar")
	}
}

func TestTimeZoneFor(t *testing.T) {
	cases := map[string]string{
		"CO": "America/Bogota", "": "America/Bogota", "MX": "America/Mexico_City",
		"AR": "America/Argentina/Buenos_Aires", "CL": "America/Santiago", "PE": "America/Lima",
		"EC": "America/Guayaquil", "ES": "Europe/Madrid", "XX": "UTC",
	}
	for country, want := range cases {
		if got := domain.TimeZoneFor(country); got != want {
			t.Errorf("TimeZoneFor(%q) = %q, want %q", country, got, want)
		}
	}
}

func TestVerifyUnsubscribeRejects(t *testing.T) {
	secret := []byte("secreto")
	user := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	valid := domain.SignUnsubscribe(secret, user, domain.Appointments, now.Add(time.Hour))

	if u, c, err := domain.VerifyUnsubscribe(secret, valid, now); err != nil || u != user || c != domain.Appointments {
		t.Fatalf("token válido: %v %v %v", u, c, err)
	}
	bad := map[string]string{
		"sin separador":      "abc",
		"firma no base64":    "abc.%%%",
		"firma errónea":      strings.Split(valid, ".")[0] + ".AAAA",
		"caducado":           domain.SignUnsubscribe(secret, user, domain.Appointments, now.Add(-time.Hour)),
		"categoría security": domain.SignUnsubscribe(secret, user, domain.Security, now.Add(time.Hour)),
		"otro secreto":       domain.SignUnsubscribe([]byte("otro"), user, domain.Appointments, now.Add(time.Hour)),
	}
	for name, tok := range bad {
		if _, _, err := domain.VerifyUnsubscribe(secret, tok, now); !errors.Is(err, domain.ErrTokenInvalid) {
			t.Errorf("%s: error = %v, want ErrTokenInvalid", name, err)
		}
	}
}
