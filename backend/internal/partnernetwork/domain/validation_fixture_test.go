package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTaxID_SharedFixture_FE011 ejecuta los casos de taxId del fixture
// compartido con la SPA (docs/phase-1/fixtures/validation-cases.json,
// generado por tools/gen-validation-fixtures) contra NewTaxID.
func TestTaxID_SharedFixture_FE011(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "phase-1", "fixtures", "validation-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Fields map[string]struct {
			Valid   []string `json:"valid"`
			Invalid []string `json:"invalid"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	for field, country := range map[string]string{"taxIdCO": "CO", "taxIdGeneric": "MX"} {
		cases, ok := fx.Fields[field]
		if !ok || len(cases.Valid) == 0 || len(cases.Invalid) == 0 {
			t.Fatalf("fixture sin casos para %s", field)
		}
		for _, v := range cases.Valid {
			if _, err := NewTaxID(country, v); err != nil {
				t.Errorf("%s %q debe ser válido: %v", field, v, err)
			}
		}
		for _, v := range cases.Invalid {
			if _, err := NewTaxID(country, v); err == nil {
				t.Errorf("%s %q debe ser inválido", field, v)
			}
		}
	}
}
