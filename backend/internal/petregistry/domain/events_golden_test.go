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

var update = flag.Bool("update", false, "regenera los golden files")

// TestEvents_Golden (ARQ-007): el JSON persistido de cada evento v1 es un
// contrato estable; cualquier cambio obliga a crear schema_version+1 y un
// upcaster, nunca a editar el golden. Regenerar: `go test ./internal/petregistry/domain -update`.
func TestEvents_Golden(t *testing.T) {
	events := []any{
		registered(),
		PetProfileUpdated{PetID: petID, Changes: Changes{Name: ptr("Lunita"), Breed: ptr("")}},
		PetMicrochipAssigned{PetID: petID, Microchip: "941000012345679", Previous: "941000012345678"},
		PetPhotoChanged{PetID: petID, Photo: &PhotoRef{
			ObjectKey:   "pets/" + petID.String() + "/f1c2.webp",
			ThumbKey:    "pets/" + petID.String() + "/f1c2_t.webp",
			ContentType: "webp",
		}},
		PetMarkedDeceased{PetID: petID, DeceasedAt: "2026-10-01"},
		PetDeceasedUndone{PetID: petID},
		CareLinkGranted{PetID: petID, OrganizationID: orgID, GrantedBy: guardianID},
		CareLinkRevoked{PetID: petID, OrganizationID: orgID, RevokedBy: guardianID, Reason: "mudanza"},
		GuardianshipTransferInitiated{
			PetID: petID, FromGuardianID: guardianID,
			ExpiresAt: time.Date(2026, 10, 27, 14, 3, 11, 0, time.UTC),
			PII:       &TransferPII{ToEmail: "nueva@example.com"},
		},
		CareLinkRequested{PetID: petID, RequestID: uuid.MustParse("0192f3a1-3333-7000-8000-000000000010"), OrganizationID: orgID,
			RequestedBy: uuid.MustParse("0192f3a1-4444-7000-8000-000000000020"), Message: "Atendemos a Luna",
			ExpiresAt: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)},
		CareLinkRequestApproved{PetID: petID, RequestID: uuid.MustParse("0192f3a1-3333-7000-8000-000000000010"), OrganizationID: orgID, ApprovedBy: guardianID},
		CareLinkRequestDeclined{PetID: petID, RequestID: uuid.MustParse("0192f3a1-3333-7000-8000-000000000010"), OrganizationID: orgID, DeclinedBy: guardianID, Reason: "no la conozco"},
		CareLinkRequestExpired{PetID: petID, RequestID: uuid.MustParse("0192f3a1-3333-7000-8000-000000000010"), OrganizationID: orgID},
		PetAlertsUpdated{PetID: petID, Alerts: []PetAlert{{Kind: "allergy", Text: "Penicilina"}}, UpdatedBy: guardianID},
		GuardianshipTransferCancelled{PetID: petID},
		PetGuardianshipTransferred{PetID: petID, FromGuardianID: guardianID, ToGuardianID: uuid.MustParse("0192f3a1-2222-7000-8000-000000000003")},
	}
	for _, e := range events {
		name, ok := e.(interface{ EventName() string })
		if !ok {
			t.Fatalf("%T no implementa EventName", e)
		}
		t.Run(name.EventName(), func(t *testing.T) {
			raw, err := json.MarshalIndent(e, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", name.EventName()+".json")
			if *update {
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
				t.Fatalf("lee golden (¿ejecuta con -update la primera vez?): %v", err)
			}
			if string(raw) != string(want) {
				t.Errorf("golden %s cambió:\ngot:\n%s\nwant:\n%s\nSi el cambio es intencional, crea schema_version+1 y upcaster (ARQ-007).", golden, raw, want)
			}
		})
	}
}

// TestEvents_SchemaVersion: versión vigente declarada por evento. PetRegistered
// pasó a v2 en F2 (ARQ-005, upcaster en infra con golden v1→v2).
func TestEvents_SchemaVersion(t *testing.T) {
	for _, tc := range []struct {
		e interface {
			EventName() string
			SchemaVersion() int
		}
		want int
	}{{registered(), 2}, {PetMarkedDeceased{}, 1}} {
		if tc.e.SchemaVersion() != tc.want {
			t.Errorf("%s: schema_version %d, want %d", tc.e.EventName(), tc.e.SchemaVersion(), tc.want)
		}
	}
}
