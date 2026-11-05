package infra

import (
	"encoding/json"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// PlanSetUpcasterV1ToV2 implementa eventstore.Upcaster para PlanSet v1 → v2 (PP-F3-DOM-006, ADR-016).
// Rellena catalog_id = null, free_text = true, catalog_name_snapshot = null en cada MedicationItem.
type PlanSetUpcasterV1ToV2 struct{}

var _ eventstore.Upcaster = PlanSetUpcasterV1ToV2{}

// From devuelve el tipo y versión de origen.
func (PlanSetUpcasterV1ToV2) From() (string, int) {
	return "PlanSet", 1
}

// Upcast transforma el payload v1 al payload v2.
func (PlanSetUpcasterV1ToV2) Upcast(payload json.RawMessage) (json.RawMessage, int, error) {
	var raw struct {
		EncounterID any `json:"encounter_id"`
		Plan        struct {
			Medications     []map[string]any `json:"medications"`
			Procedures      any              `json:"procedures,omitempty"`
			Recommendations any              `json:"recommendations,omitempty"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, 0, err
	}
	for i := range raw.Plan.Medications {
		raw.Plan.Medications[i]["catalog_id"] = nil
		raw.Plan.Medications[i]["free_text"] = true
		raw.Plan.Medications[i]["catalog_name_snapshot"] = nil
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, 0, err
	}
	return out, 2, nil
}
