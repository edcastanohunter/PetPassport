package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/domain"
)

func TestPreferencesSecurityAlwaysOn(t *testing.T) {
	p := domain.Preferences{Email: map[domain.Category]bool{domain.Security: false, domain.Appointments: false, "bogus": false}}.Normalize()
	if !p.Allows(domain.Security) || !p.Email[domain.Security] {
		t.Fatal("security debe seguir activa")
	}
	if p.Allows(domain.Appointments) {
		t.Fatal("appointments se desactivó")
	}
	if !p.Allows(domain.Clinical) {
		t.Fatal("las categorías ausentes quedan activas")
	}
	if _, ok := p.Email["bogus"]; ok {
		t.Fatal("las categorías desconocidas se descartan")
	}
}

func TestParsePreferencesDefaults(t *testing.T) {
	p, err := domain.ParsePreferences(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range domain.All() {
		if !p.Allows(c) {
			t.Fatalf("%s debería estar activa por defecto", c)
		}
	}
	if p.Digest.ClinicDailyAgenda {
		t.Fatal("el resumen diario es opt-in")
	}
	p, err = domain.ParsePreferences([]byte(`{"clinical":false}`), []byte(`{"clinic_daily_agenda":true}`))
	if err != nil || p.Allows(domain.Clinical) || !p.Digest.ClinicDailyAgenda {
		t.Fatalf("parse: %+v %v", p, err)
	}
	if _, err := domain.ParsePreferences([]byte(`{`), nil); err == nil {
		t.Fatal("JSON inválido debe fallar")
	}
}

func TestUnsubscribeToken(t *testing.T) {
	secret := []byte("secreto-de-prueba-0123456789abcdef")
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	user := uuid.New()
	tok := domain.SignUnsubscribe(secret, user, domain.Appointments, now.Add(time.Hour))

	got, cat, err := domain.VerifyUnsubscribe(secret, tok, now)
	if err != nil || got != user || cat != domain.Appointments {
		t.Fatalf("verify: %v %v %v", got, cat, err)
	}
	cases := map[string]string{
		"firma de otro secreto": domain.SignUnsubscribe([]byte("otro-secreto-otro-secreto-otro-12"), user, domain.Appointments, now.Add(time.Hour)),
		"caducado":              domain.SignUnsubscribe(secret, user, domain.Appointments, now.Add(-time.Second)),
		"security no se puede":  domain.SignUnsubscribe(secret, user, domain.Security, now.Add(time.Hour)),
		"categoría desconocida": domain.SignUnsubscribe(secret, user, "bogus", now.Add(time.Hour)),
		"sin punto":             "abc",
		"vacío":                 "",
		"basura":                "abc.def",
	}
	for name, bad := range cases {
		if _, _, err := domain.VerifyUnsubscribe(secret, bad, now); !errors.Is(err, domain.ErrTokenInvalid) {
			t.Errorf("%s: esperaba ErrTokenInvalid, obtuvo %v", name, err)
		}
	}
	// Manipular el cuerpo (otro usuario) invalida la firma.
	other := domain.SignUnsubscribe(secret, uuid.New(), domain.Appointments, now.Add(time.Hour))
	body, _, _ := splitToken(other)
	_, sig, _ := splitToken(tok)
	if _, _, err := domain.VerifyUnsubscribe(secret, body+"."+sig, now); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("cuerpo manipulado debe fallar: %v", err)
	}
}

func splitToken(t string) (string, string, bool) {
	for i := 0; i < len(t); i++ {
		if t[i] == '.' {
			return t[:i], t[i+1:], true
		}
	}
	return t, "", false
}

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		wait, dead := domain.NextAttempt(i + 1)
		if wait != w || dead {
			t.Fatalf("intento %d: %v %v", i+1, wait, dead)
		}
	}
	if wait, dead := domain.NextAttempt(domain.MaxAttempts - 0); !dead || wait != 0 {
		t.Fatalf("el intento %d es el último: %v %v", domain.MaxAttempts, wait, dead)
	}
	if wait, _ := domain.NextAttempt(0); wait != 30*time.Second {
		t.Fatalf("intento 0 se trata como el primero: %v", wait)
	}
}

func TestThrottled(t *testing.T) {
	if domain.Throttled("appointment_reminder", domain.Appointments) {
		t.Fatal("los recordatorios de cita no se limitan")
	}
	if domain.Throttled("anything", domain.Security) {
		t.Fatal("security no se limita")
	}
	if !domain.Throttled("encounter_signed", domain.Clinical) {
		t.Fatal("encounter_signed sí se limita")
	}
}

func TestDedupKeyIsStable(t *testing.T) {
	a := domain.DedupKey("r", "s", "u", "o")
	if a != "r|s|u|o" || a != domain.DedupKey("r", "s", "u", "o") {
		t.Fatalf("clave: %q", a)
	}
}

func TestAppointmentDueSkipsPast(t *testing.T) {
	now := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	// En 30 h: ambos avisos futuros.
	due := domain.AppointmentDue(now.Add(30*time.Hour), now)
	if len(due) != 2 || due[0].Offset != "T-24h" || due[1].Offset != "T-2h" {
		t.Fatalf("due: %+v", due)
	}
	// En 3 h: el T-24h ya pasó, queda T-2h.
	due = domain.AppointmentDue(now.Add(3*time.Hour), now)
	if len(due) != 1 || due[0].Offset != "T-2h" || !due[0].At.Equal(now.Add(time.Hour)) {
		t.Fatalf("due: %+v", due)
	}
	// En 1 h: ninguno.
	if due = domain.AppointmentDue(now.Add(time.Hour), now); len(due) != 0 {
		t.Fatalf("due: %+v", due)
	}
}

func TestDateDueLocalNine(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	due, err := domain.DateDue("2026-11-20", "America/Bogota", domain.VaccineOffsets, now)
	if err != nil || len(due) != 2 {
		t.Fatalf("due: %+v %v", due, err)
	}
	// T-14d = 6 nov 09:00 hora de Bogotá (UTC-5) = 14:00 UTC; T-0 = 20 nov.
	if want := time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC); !due[0].At.Equal(want) {
		t.Fatalf("T-14d: %v", due[0].At)
	}
	if want := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC); !due[1].At.Equal(want) {
		t.Fatalf("T-0: %v", due[1].At)
	}
	// Con now después de T-14d solo queda T-0.
	due, _ = domain.DateDue("2026-11-20", "America/Bogota", domain.VaccineOffsets, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	if len(due) != 1 || due[0].Offset != "T-0" {
		t.Fatalf("due: %+v", due)
	}
	if _, err := domain.DateDue("2026-11-20", "No/Existe", domain.VaccineOffsets, now); err == nil {
		t.Fatal("zona inválida debe fallar")
	}
	if _, err := domain.DateDue("no-es-fecha", "UTC", domain.VaccineOffsets, now); err == nil {
		t.Fatal("fecha inválida debe fallar")
	}
}
