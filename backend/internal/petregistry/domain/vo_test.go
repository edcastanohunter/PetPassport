package domain

import (
	"strings"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Casos de validación de VOs. Este catálogo es el espejo del fixture
// docs/phase-1/fixtures/validation-cases.json que consumen las pruebas zod
// del frontend (FE-011); tools/gen-validation-fixtures lo serializa.

func TestNewPetName(t *testing.T) {
	valid := []string{"Luna", "  Rex  ", "O'Brien", "María José", "Rex Jr", "a"}
	for _, s := range valid {
		if _, err := NewPetName(s); err != nil {
			t.Errorf("NewPetName(%q) error inesperado: %v", s, err)
		}
	}
	invalid := []string{"", "   ", strings.Repeat("x", 61), "Luna<script>", "Luna\t"}
	for _, s := range invalid {
		if _, err := NewPetName(s); err == nil {
			t.Errorf("NewPetName(%q) debe fallar", s)
		}
	}
	if n, _ := NewPetName("  Rex   Jr "); n != PetName("Rex Jr") {
		t.Errorf("normalización de espacios: %q", n)
	}
}

func TestNewSpecies(t *testing.T) {
	for _, s := range []string{"dog", "Dog", " cat ", "other"} {
		if _, err := NewSpecies(s); err != nil {
			t.Errorf("NewSpecies(%q) error inesperado: %v", s, err)
		}
	}
	if _, err := NewSpecies("dragon"); err == nil {
		t.Error("NewSpecies(dragon) debe fallar")
	}
}

func TestNewMicrochip(t *testing.T) {
	valid := []string{
		"941000012345678",     // ISO 15 dígitos
		"941 0000 1234 567 8", // con espacios
		"941-00001234567-8",   // con guiones
		"ABC123DEF",           // legado 9
		"abc123defg",          // legado 10 → upper
	}
	for _, s := range valid {
		if _, err := NewMicrochip(s); err != nil {
			t.Errorf("NewMicrochip(%q) error inesperado: %v", s, err)
		}
	}
	invalid := []string{"", "94100001234567", "12345678", "9410000123456789X"}
	for _, s := range invalid {
		if _, err := NewMicrochip(s); err == nil {
			t.Errorf("NewMicrochip(%q) debe fallar", s)
		}
	}
	if m, _ := NewMicrochip("abc 123-def"); m != Microchip("ABC123DEF") {
		t.Errorf("normalización: %q", m)
	}
}

func TestNewBirthDate(t *testing.T) {
	today := kernel.CivilDate("2026-10-20")
	cases := []struct {
		date  string
		valid bool
	}{
		{"2021-05-10", true},
		{"2026-10-20", true},
		{"2026-10-21", false}, // futuro
		{"2021-5-10", false},  // formato
		{"1985-01-01", false}, // > 40 años
		{"2026-10-21T00:00:00Z", false},
	}
	for _, tc := range cases {
		_, err := NewBirthDate(tc.date, false, today)
		if tc.valid && err != nil {
			t.Errorf("NewBirthDate(%q) error inesperado: %v", tc.date, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("NewBirthDate(%q) debe fallar", tc.date)
		}
	}
}

func TestNewBreedAndMarkings(t *testing.T) {
	if _, err := NewBreed(strings.Repeat("b", 81)); err == nil {
		t.Error("raza de 81 debe fallar")
	}
	if b, _ := NewBreed("  Beagle  "); b != Breed("Beagle") {
		t.Errorf("raza no recortada: %q", b)
	}
	if _, err := NewMarkings(strings.Repeat("m", 201)); err == nil {
		t.Error("señas de 201 deben fallar")
	}
}

func TestNewSexTristate(t *testing.T) {
	for _, s := range []string{"male", "female", "unknown"} {
		if _, err := NewSex(s); err != nil {
			t.Errorf("NewSex(%q): %v", s, err)
		}
	}
	if _, err := NewSex("n/a"); err == nil {
		t.Error("NewSex(n/a) debe fallar")
	}
	for _, s := range []string{"yes", "no", "unknown"} {
		if _, err := NewTristate(s); err != nil {
			t.Errorf("NewTristate(%q): %v", s, err)
		}
	}
	if _, err := NewTristate("maybe"); err == nil {
		t.Error("NewTristate(maybe) debe fallar")
	}
}
