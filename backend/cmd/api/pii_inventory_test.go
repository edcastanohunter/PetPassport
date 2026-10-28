package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// TestRegistry_PIIInventory (SEC-009): todo evento con sub-objeto `pii` debe
// implementar PIICarrier (decisión explícita de sujetos para el
// crypto-shredding de F5) y figurar en el inventario documentado en
// docs/phase-1/security/privacy-runbook.md §4. Un evento nuevo con PII
// rompe esta prueba hasta que se actualicen ambos.
func TestRegistry_PIIInventory(t *testing.T) {
	reg := eventstore.NewRegistry()
	petinfra.RegisterEvents(reg)
	petinfra.RegisterUpcasters(reg)
	orginfra.RegisterEvents(reg)

	var withPII []string
	for _, ev := range reg.Events() {
		typ := reflect.TypeOf(ev)
		if !hasPIIField(typ) {
			continue
		}
		name := ev.EventName()
		withPII = append(withPII, name)
		if _, ok := ev.(eventstore.PIICarrier); !ok {
			t.Errorf("%s lleva pii{} pero no implementa PIISubjects()", name)
		}
	}
	sort.Strings(withPII)
	want := []string{"GuardianshipTransferInitiated", "MemberInvited"}
	if strings.Join(withPII, ",") != strings.Join(want, ",") {
		t.Fatalf("inventario PII: %v; documentado: %v (actualiza privacy-runbook.md §4)", withPII, want)
	}
}

func hasPIIField(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		if tag == "pii" || strings.HasPrefix(tag, "pii,") {
			return true
		}
	}
	return false
}
