package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	now     = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	current = ConsentVersions{Terms: "2026-10-draft", Privacy: "2026-10-draft"}
)

func snap() TokenSnapshot {
	return TokenSnapshot{Email: "ana@example.com", EmailVerified: true, DisplayName: "Ana", Roles: []string{"default-roles-petpassport", "pet_owner", "offline_access", "uma_authorization"}}
}

func TestProvision(t *testing.T) {
	a := Provision(uuid.Must(uuid.NewV7()), snap(), now)
	if a.Status != StatusActive || a.Locale != kernel.LocaleES || a.IsOnboarded() || a.ConsentCurrent(current) {
		t.Fatalf("estado inicial: %+v", a)
	}
	if len(a.GlobalRoles) != 1 || a.GlobalRoles[0] != "pet_owner" {
		t.Fatalf("roles técnicos no filtrados: %v", a.GlobalRoles)
	}
	if a.LastLoginAt == nil || !a.LastLoginAt.Equal(now) {
		t.Fatal("last_login_at")
	}
}

func TestSyncFromToken(t *testing.T) {
	a := Provision(uuid.Must(uuid.NewV7()), snap(), now)
	if a.SyncFromToken(snap(), now.Add(time.Minute)) {
		t.Fatal("sin cambios debe devolver false")
	}
	s := snap()
	s.EmailVerified = false
	s.Roles = []string{"pet_owner", "super_admin"}
	s.DisplayName = "Otro"
	if !a.SyncFromToken(s, now.Add(time.Minute)) || a.EmailVerified || len(a.GlobalRoles) != 2 {
		t.Fatalf("sync: %+v", a)
	}
	if a.DisplayName != "Ana" {
		t.Fatal("el nombre existente no se sobrescribe desde el token")
	}
	a.DisplayName = ""
	if !a.SyncFromToken(s, now) || a.DisplayName != "Otro" {
		t.Fatal("el nombre vacío se rellena desde el token")
	}
}

func TestCompleteOnboarding(t *testing.T) {
	a := Provision(uuid.Must(uuid.NewV7()), snap(), now)
	phone := kernel.Phone("+573001234567")
	data := OnboardingData{DisplayName: "Ana T.", Country: "CO", Locale: kernel.LocaleEN, Phone: &phone, TermsVersion: "2026-10-draft", PrivacyVersion: "2026-10-draft"}
	if err := a.CompleteOnboarding(data, current, now); err != nil {
		t.Fatal(err)
	}
	if !a.IsOnboarded() || !a.ConsentCurrent(current) || *a.Country != "CO" || a.Locale != kernel.LocaleEN || *a.Phone != phone {
		t.Fatalf("onboarding: %+v", a)
	}
	first := *a.OnboardingCompletedAt
	// Idempotente: repetir no cambia la fecha de onboarding.
	if err := a.CompleteOnboarding(data, current, now.Add(time.Hour)); err != nil || !a.OnboardingCompletedAt.Equal(first) {
		t.Fatalf("repetición: %v %v", err, a.OnboardingCompletedAt)
	}
	// Versión no vigente.
	bad := data
	bad.TermsVersion = "2025-01"
	if err := a.CompleteOnboarding(bad, current, now); !errors.Is(err, ErrConsentOutdated) {
		t.Fatalf("esperaba ErrConsentOutdated, got %v", err)
	}
	// Suspendida.
	a.Status = StatusSuspended
	if err := a.CompleteOnboarding(data, current, now); !errors.Is(err, ErrAccountSuspended) {
		t.Fatalf("esperaba ErrAccountSuspended, got %v", err)
	}
}

func TestAcceptConsent(t *testing.T) {
	a := Provision(uuid.Must(uuid.NewV7()), snap(), now)
	if err := a.AcceptConsent("2026-10-draft", "old", current, now); !errors.Is(err, ErrConsentOutdated) {
		t.Fatal(err)
	}
	if err := a.AcceptConsent("2026-10-draft", "2026-10-draft", current, now); err != nil || !a.ConsentCurrent(current) {
		t.Fatal(err)
	}
	newer := ConsentVersions{Terms: "2027-01", Privacy: "2026-10-draft"}
	if a.ConsentCurrent(newer) {
		t.Fatal("consentimiento debe quedar caducado al cambiar la versión vigente")
	}
}

func TestUpdateProfile(t *testing.T) {
	a := Provision(uuid.Must(uuid.NewV7()), snap(), now)
	name := "Ana María"
	co := kernel.CountryCode("CO")
	ph := kernel.Phone("+573001234567")
	addr := kernel.Address{Country: "CO", City: "Bogotá", Line1: "Calle 1"}
	contact := kernel.Contact{Name: "Luis", Phone: "+573009876543"}
	fields, err := a.UpdateProfile(ProfileChanges{DisplayName: &name, Country: &co, Phone: &ph, Address: &addr, EmergencyContact: &contact}, now)
	if err != nil || len(fields) != 5 {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	// Sin cambios reales → sin campos.
	fields, _ = a.UpdateProfile(ProfileChanges{DisplayName: &name, Country: &co}, now)
	if len(fields) != 0 {
		t.Fatalf("sin cambios: %v", fields)
	}
	// Clear.
	fields, _ = a.UpdateProfile(ProfileChanges{ClearPhone: true, ClearAddress: true, ClearEmergencyContact: true, ClearCountry: true}, now)
	if len(fields) != 4 || a.Phone != nil || a.Address != nil || a.EmergencyContact != nil || a.Country != nil {
		t.Fatalf("clear: %v %+v", fields, a)
	}
	a.Status = StatusSuspended
	if _, err := a.UpdateProfile(ProfileChanges{DisplayName: &name}, now); !errors.Is(err, ErrAccountSuspended) {
		t.Fatal(err)
	}
}

func TestTruncateAndRoles(t *testing.T) {
	long := make([]rune, MaxDisplayName+10)
	for i := range long {
		long[i] = 'á'
	}
	a := Provision(uuid.Must(uuid.NewV7()), TokenSnapshot{DisplayName: string(long)}, now)
	if n := len([]rune(a.DisplayName)); n != MaxDisplayName {
		t.Fatalf("truncado a %d", n)
	}
	if got := normalizeRoles([]string{"b", "a", "a", " "}); len(got) != 2 || got[0] != "a" {
		t.Fatalf("roles: %v", got)
	}
}
