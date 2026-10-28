package eventstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Registry mapea `event_type` → factory de su versión vigente (ARQ-007) y
// encadena los upcasters de las versiones anteriores (PP-F2-ARQ-005). Solo se
// registra la versión vigente de cada tipo: las anteriores se leen siempre a
// través de su upcaster. Registrar un evento sin versión (schema_version < 1)
// es error de arranque. El registry se congela con Validate(), llamado en la
// secuencia de arranque (03 §8); falla si existe vN sin cadena completa desde v1.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]func() Event // event_type → factory de la versión vigente
	current   map[string]int          // event_type → versión vigente
	upcasters map[string]Upcaster     // key: event_type@fromVersion
	frozen    bool
}

// NewRegistry construye un registry vacío.
func NewRegistry() *Registry {
	return &Registry{
		factories: map[string]func() Event{},
		current:   map[string]int{},
		upcasters: map[string]Upcaster{},
	}
}

func key(eventType string, version int) string { return fmt.Sprintf("%s@%d", eventType, version) }

// Register añade el tipo con la versión declarada por el propio evento.
// Pánico si la versión es < 1 (registrar sin versión es error de arranque) o
// si el tipo ya estaba registrado (una sola versión vigente por tipo).
func (r *Registry) Register(factory func() Event) {
	probe := factory()
	if probe == nil {
		panic("eventstore: factory devuelve nil")
	}
	name, ver := probe.EventName(), probe.SchemaVersion()
	if ver < 1 {
		panic(fmt.Sprintf("eventstore: %s registrado sin schema_version válido (%d)", name, ver))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		panic("eventstore: registry congelado; registra antes de Validate: " + name)
	}
	if _, dup := r.factories[name]; dup {
		panic("eventstore: evento ya registrado: " + name)
	}
	r.factories[name] = factory
	r.current[name] = ver
}

// RegisterUpcaster registra un upcaster. Pánico si ya existe uno para el
// mismo tipo y versión origen.
func (r *Registry) RegisterUpcaster(u Upcaster) {
	name, from := u.From()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		panic("eventstore: registry congelado; registra antes de Validate: " + key(name, from))
	}
	k := key(name, from)
	if _, dup := r.upcasters[k]; dup {
		panic("eventstore: upcaster ya registrado: " + k)
	}
	r.upcasters[k] = u
}

// Encode serializa el payload y devuelve tipo y versión del evento. Solo se
// escribe la versión vigente: escribir una versión antigua es un error.
func (r *Registry) Encode(payload Event) (eventType string, schemaVersion int, raw json.RawMessage, err error) {
	eventType, schemaVersion = payload.EventName(), payload.SchemaVersion()
	if schemaVersion < 1 {
		return "", 0, nil, fmt.Errorf("eventstore: %s sin schema_version", eventType)
	}
	r.mu.RLock()
	cur, known := r.current[eventType]
	r.mu.RUnlock()
	if known && cur != schemaVersion {
		return "", 0, nil, fmt.Errorf("eventstore: %s@%d no es la versión vigente (%d)", eventType, schemaVersion, cur)
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		return "", 0, nil, fmt.Errorf("eventstore: serializar %s: %w", eventType, err)
	}
	return eventType, schemaVersion, raw, nil
}

// Upcast lleva el payload de e a la versión vigente de su tipo aplicando la
// cadena de upcasters (v1→v2→…). Un tipo no registrado se devuelve sin
// cambios (los consumidores filtran por tipo; Decode sí falla). Un hueco en la
// cadena o una versión posterior a la vigente es error.
func (r *Registry) Upcast(e StoredEvent) (StoredEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cur, known := r.current[e.Type]
	if !known {
		return e, nil
	}
	if e.SchemaVersion > cur {
		return e, fmt.Errorf("eventstore: %s@%d es posterior a la versión vigente %d (¿código anterior al evento?)", e.Type, e.SchemaVersion, cur)
	}
	for e.SchemaVersion < cur {
		u, ok := r.upcasters[key(e.Type, e.SchemaVersion)]
		if !ok {
			return e, fmt.Errorf("eventstore: sin upcaster para %s@%d", e.Type, e.SchemaVersion)
		}
		payload, next, err := u.Upcast(e.Payload)
		if err != nil {
			return e, fmt.Errorf("eventstore: upcast %s@%d: %w", e.Type, e.SchemaVersion, err)
		}
		if next != e.SchemaVersion+1 {
			return e, fmt.Errorf("eventstore: upcast %s@%d produce v%d (se espera v%d)", e.Type, e.SchemaVersion, next, e.SchemaVersion+1)
		}
		e.Payload, e.SchemaVersion = payload, next
	}
	return e, nil
}

// Decode reconstruye el evento tipado desde el store, upcasteando antes a la
// versión vigente. Los factories devuelven VALORES (no punteros): Decode
// asigna la memoria, deserializa sobre un puntero nuevo y devuelve el valor
// para que los type switches de ApplyEvent/Project reciban el tipo por valor.
// Tipo desconocido → error: el arranque debe registrar todos los tipos.
func (r *Registry) Decode(e StoredEvent) (Event, error) {
	r.mu.RLock()
	factory, ok := r.factories[e.Type]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("eventstore: evento no registrado: %s@%d", e.Type, e.SchemaVersion)
	}
	e, err := r.Upcast(e)
	if err != nil {
		return nil, err
	}
	probe := factory()
	target := reflect.New(reflect.TypeOf(probe))
	if err := json.Unmarshal(e.Payload, target.Interface()); err != nil {
		return nil, fmt.Errorf("eventstore: deserializar %s@%d: %w", e.Type, e.SchemaVersion, err)
	}
	ev, isEvent := target.Elem().Interface().(Event)
	if !isEvent {
		return nil, fmt.Errorf("eventstore: %s no implementa eventstore.Event por valor", e.Type)
	}
	return ev, nil
}

// PIISubjects devuelve los sujetos cuya PII viaja encapsulada en `pii{}` del
// evento (diseño crypto-shredding, SEC-009; el cifrado se activa en F5).
func PIISubjects(payload Event) []uuid.UUID {
	if carrier, ok := payload.(PIICarrier); ok {
		return carrier.PIISubjects()
	}
	return nil
}

// Validate congela el registry y verifica las invariantes de arranque
// (ARQ-005): todo tipo con versión vigente N > 1 tiene upcaster para cada
// v ∈ [1, N), y no hay upcasters huérfanos (tipo no registrado o versión
// origen ≥ vigente).
func (r *Registry) Validate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return nil
	}
	for name, cur := range r.current {
		for v := 1; v < cur; v++ {
			if _, ok := r.upcasters[key(name, v)]; !ok {
				return fmt.Errorf("eventstore: %s@%d no tiene cadena completa desde v1: falta upcaster %s@%d", name, cur, name, v)
			}
		}
	}
	for k, u := range r.upcasters {
		name, from := u.From()
		cur, ok := r.current[name]
		if !ok {
			return fmt.Errorf("eventstore: upcaster %s sobre un tipo no registrado", k)
		}
		if from < 1 || from >= cur {
			return fmt.Errorf("eventstore: upcaster %s fuera de rango (vigente v%d)", k, cur)
		}
	}
	r.frozen = true
	return nil
}

// StoredVersion es un par tipo@versión presente en es.events.
type StoredVersion struct {
	Type          string
	SchemaVersion int
}

// VerifyStored comprueba que cada tipo@versión persistido es legible por este
// binario: tipo registrado, versión ≤ vigente y cadena de upcasters completa
// hasta la vigente (verificación de arranque del worker, ARQ-005). Recibe los
// pares para no acoplar el registry a SQL; Store.StoredVersions los obtiene.
func (r *Registry) VerifyStored(stored []StoredVersion) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sv := range stored {
		cur, ok := r.current[sv.Type]
		if !ok {
			return fmt.Errorf("eventstore: es.events contiene %s@%d y el tipo no está registrado", sv.Type, sv.SchemaVersion)
		}
		if sv.SchemaVersion > cur {
			return fmt.Errorf("eventstore: es.events contiene %s@%d, posterior a la vigente v%d", sv.Type, sv.SchemaVersion, cur)
		}
		for v := sv.SchemaVersion; v < cur; v++ {
			if _, ok := r.upcasters[key(sv.Type, v)]; !ok {
				return fmt.Errorf("eventstore: es.events contiene %s@%d sin upcaster %s@%d", sv.Type, sv.SchemaVersion, sv.Type, v)
			}
		}
	}
	return nil
}

// CurrentVersion devuelve la versión vigente del tipo (0 si no está registrado).
func (r *Registry) CurrentVersion(eventType string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current[eventType]
}

// Len devuelve cuántos tipos hay registrados (métricas de arranque y pruebas).
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.factories)
}

// Events devuelve un valor cero de cada tipo registrado (versión vigente),
// ordenado por nombre. Lo usa el inventario de PII (SEC-009) para exigir que
// todo evento con `pii{}` declare sus sujetos.
func (r *Registry) Events() []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.factories))
	for k := range r.factories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Event, 0, len(keys))
	for _, k := range keys {
		out = append(out, r.factories[k]())
	}
	return out
}
