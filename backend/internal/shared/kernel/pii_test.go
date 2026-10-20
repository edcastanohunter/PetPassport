package kernel

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestEmail(t *testing.T) {
	e, err := NewEmail("  Ana.Tutora@Example.COM ")
	if err != nil || e != "ana.tutora@example.com" {
		t.Fatalf("got %q, %v", e, err)
	}
	if _, err := NewEmail("no-es-email"); err == nil {
		t.Fatal("expected error")
	}
	if e.Masked() != "a***@example.com" {
		t.Fatalf("masked = %q", e.Masked())
	}
}

func TestPhone(t *testing.T) {
	p, err := NewPhone("+57 (300) 123-4567")
	if err != nil || p != "+573001234567" {
		t.Fatalf("got %q, %v", p, err)
	}
	for _, bad := range []string{"3001234567", "+0123", "+57abc", ""} {
		if _, err := NewPhone(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestCountryAndLocale(t *testing.T) {
	if c, err := NewCountryCode("co"); err != nil || c != "CO" {
		t.Fatalf("country: %q %v", c, err)
	}
	if _, err := NewCountryCode("COL"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NewLocale("fr"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPIINeverInLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	e, _ := NewEmail("ana@example.com")
	p, _ := NewPhone("+573001234567")
	logger.Info("x", "email", e, "phone", p, "address", Address{City: "Medellín", Line1: "Calle 1"}, "contact", Contact{Name: "Juan", Phone: p})
	out := buf.String()
	for _, secret := range []string{"ana@example.com", "+573001234567", "Medellín", "Calle 1", "Juan"} {
		if strings.Contains(out, secret) {
			t.Fatalf("PII leaked in logs: %s in %s", secret, out)
		}
	}
}
