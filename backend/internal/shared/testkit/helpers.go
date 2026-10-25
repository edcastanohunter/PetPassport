package testkit

import (
	"errors"
	"fmt"
	"reflect"
)

// sameEvent compara dos eventos de dominio por tipo y valor. Ignora lo que
// no existe en los eventos de dominio (event_id/occurred_at viven en el
// sobre del event store, no en el payload).
func sameEvent(got, want any) bool {
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		return false
	}
	return reflect.DeepEqual(got, want)
}

func errorIs(got, want error) bool {
	if errors.Is(got, want) {
		return true
	}
	return want != nil && reflect.TypeOf(got) == reflect.TypeOf(want)
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }
