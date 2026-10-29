package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCanonicalJSON_RFC8785(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"claves ordenadas", `{"b":1,"a":2,"A":3}`, `{"A":3,"a":2,"b":1}`},
		{"sin espacios", ` { "a" : [ 1 , 2 ] } `, `{"a":[1,2]}`},
		{"enteros sin .0", `{"n":1.0,"m":-0.0}`, `{"m":0,"n":1}`},
		{"decimales", `{"w":12.40,"d":0.2}`, `{"d":0.2,"w":12.4}`},
		{"exponente", `{"x":1e21,"y":1e-7}`, `{"x":1e+21,"y":1e-7}`},
		{"unicode sin escapar", `{"t":"á é 汉"}`, `{"t":"á é 汉"}`},
		{"escapes mínimos", `{"t":"a\"b\\c\n\t"}`, `{"t":"a\"b\\c\n\t"}`},
		{"control", `{"t":"\u0001"}`, `{"t":"\u0001"}`},
		{"anidado", `{"z":{"b":true,"a":null},"a":[]}`, `{"a":[],"z":{"a":null,"b":true}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalJSON([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	if _, err := CanonicalJSON([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("contenido extra aceptado")
	}
}

func TestContentHash_DeterministicAndOrdered(t *testing.T) {
	id1 := uuid.MustParse("0192f3a1-0000-7000-8000-000000000001")
	id2 := uuid.MustParse("0192f3a1-0000-7000-8000-000000000002")
	e1 := HashableEvent{EventID: id1, EventType: "EncounterOpened", SchemaVersion: 1, Payload: json.RawMessage(`{"b":2,"a":1}`)}
	e2 := HashableEvent{EventID: id2, EventType: "VitalsRecorded", SchemaVersion: 1, Payload: json.RawMessage(`{"vitals":{"weight_kg":12.4}}`)}

	h1, err := ContentHash([]HashableEvent{e1, e2})
	if err != nil {
		t.Fatal(err)
	}
	// El orden de claves del JSON de entrada no cambia el hash (canónico).
	e1b := e1
	e1b.Payload = json.RawMessage(`{"a":1,"b":2}`)
	h2, err := ContentHash([]HashableEvent{e1b, e2})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash depende del orden de claves: %s vs %s", h1, h2)
	}
	if len(h1) != 64 || strings.Trim(h1, "0123456789abcdef") != "" {
		t.Fatalf("hash no hex-256: %s", h1)
	}
	// Cambiar el orden de los eventos cambia el hash (el orden del stream es
	// parte del contenido firmado).
	h3, err := ContentHash([]HashableEvent{e2, e1})
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Fatal("el orden de eventos no afecta al hash")
	}
	// Cambiar un byte del payload cambia el hash.
	e2b := e2
	e2b.Payload = json.RawMessage(`{"vitals":{"weight_kg":12.5}}`)
	h4, err := ContentHash([]HashableEvent{e1, e2b})
	if err != nil {
		t.Fatal(err)
	}
	if h4 == h1 {
		t.Fatal("el payload no afecta al hash")
	}
}

func TestContentHash_EmptyAndInvalid(t *testing.T) {
	// Un stream vacío tiene hash definido (SHA-256 de la cadena vacía).
	h, err := ContentHash(nil)
	if err != nil {
		t.Fatal(err)
	}
	if h != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("hash vacío: %s", h)
	}
	if _, err := ContentHash([]HashableEvent{{EventID: uuid.Nil, EventType: "X", SchemaVersion: 1, Payload: []byte(`{}`)}}); err == nil {
		t.Fatal("evento sin id aceptado")
	}
	if _, err := ContentHash([]HashableEvent{{EventID: uuid.New(), EventType: "X", SchemaVersion: 0, Payload: []byte(`{}`)}}); err == nil {
		t.Fatal("schema_version 0 aceptada")
	}
}
