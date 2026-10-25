package domain

import (
	"encoding/json"
	"testing"
)

// TestPetRegistered_DocExample (ARQ-007): el ejemplo serializado de
// docs/phase-1/02 §3.3 se deserializa sin pérdida.
func TestPetRegistered_DocExample(t *testing.T) {
	doc := `{
  "pet_id": "0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f",
  "guardian_id": "7a8f0000-0000-7000-8000-000000000001",
  "name": "Luna", "species": "dog",
  "breed": "Beagle", "sex": "female", "neutered": "yes",
  "birth_date": { "date": "2021-05-10", "approximate": false }
}`
	var e PetRegistered
	if err := json.Unmarshal([]byte(doc), &e); err != nil {
		t.Fatal(err)
	}
	if e.Name != "Luna" || e.Species != SpeciesDog || e.Breed != "Beagle" || e.Sex != SexFemale ||
		e.Neutered != TristateYes || e.BirthDate == nil || e.BirthDate.Date != "2021-05-10" || e.BirthDate.Approximate {
		t.Fatalf("pérdida al deserializar: %+v", e)
	}
	// El nombre y la versión son estables.
	if e.EventName() != "PetRegistered" || e.SchemaVersion() != 1 {
		t.Fatalf("sobre: %s@%d", e.EventName(), e.SchemaVersion())
	}
}
