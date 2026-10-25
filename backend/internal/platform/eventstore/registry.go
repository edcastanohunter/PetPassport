package eventstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Registry mapea `event_type@schema_version` → factory (ARQ-007). Registrar
// un evento sin versión (schema_version < 1) es error de arranque. El registry
// se congela con Validate(), llamado en la secuencia de arranque (03 §8).
type Registry struct {
	mu        sync.RWMutex
	factories map[string]func() Event
	versions  map[string][]int // event_type → versiones registradas
	upcasters map[string]Upcaster // key: event_type@fromVersion
	frozen    bool
}

// NewRegistry construye un registry vacío.
func NewRegistry() *Registry {
	return &Registry{
		factories: map[string]func() Event{},
		versions:  map[string][]int{},
		upcasters: map[string]Upcaster{},
	}
}

func key(eventType string, version int) string { return fmt.Sprintf("%s@%d", eventType, version) }

// Register añade el tipo con la versión declarada por el propio evento.
// Pánico si la versión es < 1 (registrar sin versión es error de arranque) o
// si ya estaba registrado.
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
	k := key(name, ver)
	if _, dup := r.factories[k]; dup {
		panic("eventstore: evento ya registrado: " + k)
	}
	r.factories[k] = factory
	r.versions[name] = append(r.versions[name], ver)
}

// RegisterUpcaster registra un upcaster (sin implementaciones en F1; primer
// uso en F2). Pánico si ya existe uno para el mismo tipo y versión origen.
func (r *Registry) RegisterUpcaster(u Upcaster) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(u.EventType(), u.FromVersion())
	if _, dup := r.upcasters[k]; dup {
		panic("eventstore: upcaster ya registrado: " + k)
	}
	r.upcasters[k] = u
}

// Encode serializa el payload y devuelve tipo y versión del evento.
func (r *Registry) Encode(payload Event) (eventType string, schemaVersion int, raw json.RawMessage, err error) {
	eventType, schemaVersion = payload.EventName(), payload.SchemaVersion()
	if schemaVersion < 1 {
		return "", 0, nil, fmt.Errorf("eventstore: %s sin schema_version", eventType)
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		return "", 0, nil, fmt.Errorf("eventstore: serializar %s: %w", eventType, err)
	}
	return eventType, schemaVersion, raw, nil
}

// Decode reconstruye el evento tipado desde el store. Los factories devuelven
// VALORES (no punteros): Decode asigna la memoria, deserializa sobre un
// puntero nuevo y devuelve el valor para que los type switches de
// ApplyEvent/Project reciban el tipo por valor. Si la versión persistida no
// tiene factory pero hay upcaster registrado, lo aplica primero (F2+).
// Tipo desconocido → error: el arranque debe registrar todos los tipos.
func (r *Registry) Decode(e StoredEvent) (Event, error) {
	r.mu.RLock()
	factory, ok := r.factories[key(e.Type, e.SchemaVersion)]
	upcaster := r.upcasters[key(e.Type, e.SchemaVersion)]
	r.mu.RUnlock()

	if !ok {
		if upcaster != nil {
			upcasted, err := upcaster.Upcast(e)
			if err != nil {
				return nil, fmt.Errorf("eventstore: upcast %s@%d: %w", e.Type, e.SchemaVersion, err)
			}
			ev, isEvent := upcasted.(Event)
			if !isEvent {
				return nil, fmt.Errorf("eventstore: upcast %s@%d no produce un Event", e.Type, e.SchemaVersion)
			}
			return ev, nil
		}
		return nil, fmt.Errorf("eventstore: evento no registrado: %s@%d", e.Type, e.SchemaVersion)
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

// Validate congela el registry y verifica invariantes de arranque: cada tipo
// con varias versiones debe tener upcaster para todas menos la vigente.
func (r *Registry) Validate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return nil
	}
	for name, vers := range r.versions {
		if len(vers) < 2 {
			continue
		}
		sort.Ints(vers)
		for _, v := range vers[:len(vers)-1] {
			if _, ok := r.upcasters[key(name, v)]; !ok {
				return fmt.Errorf("eventstore: %s@%d no tiene upcaster y existe una versión superior", name, v)
			}
		}
	}
	r.frozen = true
	return nil
}

// Len devuelve cuántas combinaciones tipo@versión hay registradas (métricas
// de arranque y pruebas).
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.factories)
}
