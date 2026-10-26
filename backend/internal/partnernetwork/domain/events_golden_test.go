package domain

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

var updateGolden = flag.Bool("update", false, "regenera los golden files")

// TestEvents_Golden (ARQ-007): el JSON persistido de cada evento v1 es un
// contrato estable. Regenerar: go test ./internal/partnernetwork/domain -update.
func TestEvents_Golden(t *testing.T) {
	invID := uuid.MustParse("01930000-0000-7000-8000-0000000000e1")
	vetUUID := uuid.MustParse("01930000-0000-7000-8000-0000000000bb")
	events := []any{
		clinicRegistered(),
		OrganizationProfileUpdated{OrganizationID: orgID, Changes: ProfileChanges{
			DisplayName: ptrStr("Clínica Norte"),
			TaxID:       &TaxIDPayload{Country: "CO", Value: "900123456-8"},
		}},
		OrganizationVerificationReset{OrganizationID: orgID, Cause: "tax_id_change"},
		OrganizationVerified{OrganizationID: orgID},
		OrganizationRejected{OrganizationID: orgID, Reason: "no pudimos verificar la existencia legal"},
		OrganizationSuspended{OrganizationID: orgID, Reason: "motivo suficientemente largo"},
		OrganizationReactivated{OrganizationID: orgID},
		MemberInvited{OrganizationID: orgID, InvitationID: invID, Role: RoleVeterinarian,
			ExpiresAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), PII: &InvitationPII{Email: "vet@sur.co"}},
		InvitationRevoked{OrganizationID: orgID, InvitationID: invID},
		MemberJoined{OrganizationID: orgID, UserID: vetUUID, Role: RoleVeterinarian, InvitationID: invID},
		MemberRoleChanged{OrganizationID: orgID, UserID: vetUUID, From: RoleVeterinarian, To: RoleReceptionist},
		MemberRemoved{OrganizationID: orgID, UserID: vetUUID, Reason: "renuncia"},
	}
	for _, e := range events {
		name := e.(interface{ EventName() string }).EventName()
		t.Run(name, func(t *testing.T) {
			raw, err := json.MarshalIndent(e, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", name+".json")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil { //nolint:gosec // testdata local
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, raw, 0o600); err != nil { //nolint:gosec // golden de prueba
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden) //nolint:gosec // ruta fija en testdata
			if err != nil {
				t.Fatalf("lee golden (¿-update?): %v", err)
			}
			if string(raw) != string(want) {
				t.Errorf("golden %s cambió:\ngot:\n%s\nwant:\n%s\nSi es intencional: schema_version+1 y upcaster (ARQ-007).", golden, raw, want)
			}
		})
	}
}
