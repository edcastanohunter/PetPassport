package infra

import (
	"encoding/json"
	"fmt"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// RegisterUpcasters registra los upcasters de Pet Registry (PP-F2-ARQ-005).
// Se llama junto a RegisterEvents, antes de Registry.Validate.
func RegisterUpcasters(reg *eventstore.Registry) {
	reg.RegisterUpcaster(PetRegisteredV1toV2{})
}

// PetRegisteredV1toV2 añade `alerts: []` (perfil ampliado, DOM-013). Primer
// upcaster real: ejercita el mecanismo con un cambio inocuo. Opera sobre el
// JSON genérico para conservar intactos los demás campos.
type PetRegisteredV1toV2 struct{}

// From implementa eventstore.Upcaster.
func (PetRegisteredV1toV2) From() (string, int) { return "PetRegistered", 1 }

// Upcast implementa eventstore.Upcaster.
func (PetRegisteredV1toV2) Upcast(payload json.RawMessage) (json.RawMessage, int, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, 0, fmt.Errorf("PetRegistered v1: %w", err)
	}
	if _, ok := fields["alerts"]; !ok {
		fields["alerts"] = json.RawMessage(`[]`)
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, 0, err
	}
	return out, 2, nil
}
