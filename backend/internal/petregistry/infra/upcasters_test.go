package infra

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

var update = flag.Bool("update", false, "regenera los golden de salida de upcasters")

// TestUpcaster_PetRegisteredV1toV2_Golden (ARQ-005): entrada = golden v1 tal
// como se persistió en F1 (nunca se edita); salida = golden v2.
func TestUpcaster_PetRegisteredV1toV2_Golden(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "upcasters", "PetRegistered@1.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, ver, err := PetRegisteredV1toV2{}.Upcast(in)
	if err != nil || ver != 2 {
		t.Fatalf("upcast: v%d %v", ver, err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "upcasters", "PetRegistered@2.json")
	if *update {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // ruta fija en testdata
	if err != nil {
		t.Fatalf("lee golden (¿-update la primera vez?): %v", err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Fatalf("salida del upcaster cambió:\ngot:\n%s\nwant:\n%s", pretty.Bytes(), want)
	}
	// La salida deserializa al evento v2 vigente.
	var ev domain.PetRegistered
	if err := json.Unmarshal(out, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Alerts == nil || len(ev.Alerts) != 0 || ev.Name != "Luna" {
		t.Fatalf("evento v2: %+v", ev)
	}
}

// TestUpcaster_PreservesExistingAlerts: idempotente sobre un payload que ya
// trae alerts (no debería ocurrir en v1, pero no se pisa).
func TestUpcaster_PreservesExistingAlerts(t *testing.T) {
	out, _, err := PetRegisteredV1toV2{}.Upcast(json.RawMessage(`{"pet_id":"x","alerts":[{"kind":"allergy","text":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if len(m["alerts"].([]any)) != 1 {
		t.Fatalf("alerts pisadas: %s", out)
	}
}

// TestRegistry_DecodesV1AsV2: el registry encadena el upcaster al decodificar
// un evento v1 persistido y Validate exige la cadena completa.
func TestRegistry_DecodesV1AsV2(t *testing.T) {
	reg := eventstore.NewRegistry()
	RegisterEvents(reg)
	RegisterUpcasters(reg)
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	in, err := os.ReadFile(filepath.Join("testdata", "upcasters", "PetRegistered@1.json"))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := reg.Decode(eventstore.StoredEvent{Type: "PetRegistered", SchemaVersion: 1, Payload: in})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ev.(domain.PetRegistered)
	if !ok || !reflect.DeepEqual(got.Alerts, []domain.PetAlert{}) {
		t.Fatalf("decode v1 → %#v", ev)
	}
	if err := reg.VerifyStored([]eventstore.StoredVersion{{Type: "PetRegistered", SchemaVersion: 1}, {Type: "PetRegistered", SchemaVersion: 2}}); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Sin el upcaster, el arranque falla (vN sin cadena desde v1).
	bare := eventstore.NewRegistry()
	RegisterEvents(bare)
	if err := bare.Validate(); err == nil {
		t.Fatal("Validate debe fallar: PetRegistered@2 sin upcaster desde v1")
	}
}
