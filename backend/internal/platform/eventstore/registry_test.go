package eventstore

import (
	"encoding/json"
	"strings"
	"testing"
)

type evV3 struct {
	A string `json:"a"`
	B string `json:"b"`
	C string `json:"c"`
}

func (evV3) EventName() string  { return "Thing" }
func (evV3) SchemaVersion() int { return 3 }

// addField es un upcaster de prueba que añade field=value.
type addField struct {
	from         int
	field, value string
}

func (u addField) From() (string, int) { return "Thing", u.from }
func (u addField) Upcast(p json.RawMessage) (json.RawMessage, int, error) {
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		return nil, 0, err
	}
	m[u.field] = u.value
	out, err := json.Marshal(m)
	return out, u.from + 1, err
}

func newThingRegistry(upcasters ...Upcaster) *Registry {
	r := NewRegistry()
	r.Register(func() Event { return evV3{} })
	for _, u := range upcasters {
		r.RegisterUpcaster(u)
	}
	return r
}

// TestRegistry_ChainsUpcasters (ARQ-005): v1 → v2 → v3 al decodificar.
func TestRegistry_ChainsUpcasters(t *testing.T) {
	r := newThingRegistry(addField{1, "b", "from-v1"}, addField{2, "c", "from-v2"})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	ev, err := r.Decode(StoredEvent{Type: "Thing", SchemaVersion: 1, Payload: json.RawMessage(`{"a":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	got := ev.(evV3)
	if got != (evV3{A: "x", B: "from-v1", C: "from-v2"}) {
		t.Fatalf("decode: %+v", got)
	}
	// v2 solo aplica el último eslabón.
	up, err := r.Upcast(StoredEvent{Type: "Thing", SchemaVersion: 2, Payload: json.RawMessage(`{"a":"x","b":"keep"}`)})
	if err != nil || up.SchemaVersion != 3 || !strings.Contains(string(up.Payload), `"b":"keep"`) {
		t.Fatalf("upcast v2: %+v %v", up, err)
	}
	// Tipos desconocidos pasan sin cambios (consumidores filtran por tipo).
	other := StoredEvent{Type: "Other", SchemaVersion: 7, Payload: json.RawMessage(`{}`)}
	if up, err := r.Upcast(other); err != nil || up.SchemaVersion != 7 {
		t.Fatalf("tipo desconocido: %+v %v", up, err)
	}
}

// TestRegistry_ValidateRequiresFullChain: falla al arrancar si existe vN sin
// cadena completa desde v1, o con upcasters huérfanos.
func TestRegistry_ValidateRequiresFullChain(t *testing.T) {
	if err := newThingRegistry(addField{2, "c", "x"}).Validate(); err == nil || !strings.Contains(err.Error(), "Thing@1") {
		t.Fatalf("falta v1→v2: %v", err)
	}
	if err := newThingRegistry(addField{1, "b", "x"}, addField{2, "c", "x"}, addField{3, "d", "x"}).Validate(); err == nil {
		t.Fatal("upcaster desde la versión vigente debe fallar")
	}
	r := NewRegistry()
	r.RegisterUpcaster(addField{1, "b", "x"})
	if err := r.Validate(); err == nil {
		t.Fatal("upcaster de tipo no registrado debe fallar")
	}
}

// TestRegistry_UpcastErrors: versión futura y upcaster que no avanza una versión.
func TestRegistry_UpcastErrors(t *testing.T) {
	r := newThingRegistry(addField{1, "b", "x"}, addField{2, "c", "x"})
	if _, err := r.Upcast(StoredEvent{Type: "Thing", SchemaVersion: 4, Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("v4 > vigente debe fallar")
	}
	bad := newThingRegistry(addField{1, "b", "x"}, skipping{})
	if _, err := bad.Upcast(StoredEvent{Type: "Thing", SchemaVersion: 2, Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("upcaster que salta versiones debe fallar")
	}
}

type skipping struct{}

func (skipping) From() (string, int) { return "Thing", 2 }
func (skipping) Upcast(p json.RawMessage) (json.RawMessage, int, error) {
	return p, 4, nil
}

// TestRegistry_EncodeOnlyCurrent: nunca se escribe una versión antigua.
func TestRegistry_EncodeOnlyCurrent(t *testing.T) {
	r := NewRegistry()
	r.Register(func() Event { return evV3{} })
	if _, v, _, err := r.Encode(evV3{A: "x"}); err != nil || v != 3 {
		t.Fatalf("encode: v%d %v", v, err)
	}
	if _, _, _, err := r.Encode(evV1{}); err == nil {
		t.Fatal("encode de v1 con v3 vigente debe fallar")
	}
}

type evV1 struct{}

func (evV1) EventName() string  { return "Thing" }
func (evV1) SchemaVersion() int { return 1 }

// TestRegistry_VerifyStored: verificación de arranque contra es.events.
func TestRegistry_VerifyStored(t *testing.T) {
	r := newThingRegistry(addField{1, "b", "x"}, addField{2, "c", "x"})
	if err := r.VerifyStored([]StoredVersion{{"Thing", 1}, {"Thing", 3}}); err != nil {
		t.Fatal(err)
	}
	for _, sv := range []StoredVersion{{"Unknown", 1}, {"Thing", 4}} {
		if err := r.VerifyStored([]StoredVersion{sv}); err == nil {
			t.Fatalf("%v debe fallar", sv)
		}
	}
}

// TestRegistry_RegisterTwicePanics: una sola versión vigente por tipo.
func TestRegistry_RegisterTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("se esperaba panic")
		}
	}()
	r := NewRegistry()
	r.Register(func() Event { return evV3{} })
	r.Register(func() Event { return evV1{} })
}
