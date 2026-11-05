package infra_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// TestPlanSet_Upcaster_V1ToV2 (PP-F3-DOM-006, ADR-016):
// Verifica que un evento PlanSet v1 persistido se decodifica y upcastea a v2
// con catalog_id = null, free_text = true y catalog_name_snapshot = null.
func TestPlanSet_Upcaster_V1ToV2(t *testing.T) {
	goldenPath := filepath.Clean(filepath.Join("..", "domain", "testdata", "golden", "PlanSet.json"))
	rawV1, err := os.ReadFile(goldenPath) //nolint:gosec // ruta de test relativa fija
	if err != nil {
		t.Fatalf("leer golden PlanSet.json v1: %v", err)
	}

	reg := eventstore.NewRegistry()
	infra.RegisterEvents(reg)
	if err := reg.Validate(); err != nil {
		t.Fatalf("validar registry: %v", err)
	}

	stored := eventstore.StoredEvent{
		Type:          "PlanSet",
		SchemaVersion: 1,
		Payload:       json.RawMessage(rawV1),
	}

	ev, err := reg.Decode(stored)
	if err != nil {
		t.Fatalf("Decode PlanSet v1: %v", err)
	}

	planSet, ok := ev.(domain.PlanSet)
	if !ok {
		t.Fatalf("tipo decodificado incorrecto: %T", ev)
	}

	if planSet.SchemaVersion() != 2 {
		t.Errorf("SchemaVersion = %d, want 2", planSet.SchemaVersion())
	}

	if len(planSet.Plan.Medications) == 0 {
		t.Fatal("plan.medications está vacío")
	}

	for i, med := range planSet.Plan.Medications {
		if med.CatalogID != nil {
			t.Errorf("medication[%d].CatalogID = %v, want nil", i, med.CatalogID)
		}
		if !med.FreeText {
			t.Errorf("medication[%d].FreeText = false, want true", i)
		}
		if med.CatalogNameSnapshot != nil {
			t.Errorf("medication[%d].CatalogNameSnapshot = %v, want nil", i, med.CatalogNameSnapshot)
		}
		if med.MedicationName != "Meloxicam" {
			t.Errorf("medication[%d].MedicationName = %s, want Meloxicam", i, med.MedicationName)
		}
	}
}
