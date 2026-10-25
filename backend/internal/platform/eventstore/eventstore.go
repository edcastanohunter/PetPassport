// Package eventstore implementa el Event Store v0 en PostgreSQL (ADR-002,
// PP-F1-ARQ-004, docs/phase-1/03 §4): Append con concurrencia optimista por
// versión de stream, Load por stream y ReadAll por posición global para
// rebuild y pruebas. La semántica exacta de Append (incluida la escritura del
// outbox en la misma transacción) vive en store.go.
package eventstore

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ExpectedNewStream es la versión esperada de un stream que aún no existe.
const ExpectedNewStream int64 = 0

// NewEvent es un evento a persistir: el payload es un struct con tags JSON
// registrado en el Registry; el ID es UUIDv7 (ADR-007).
type NewEvent struct {
	ID            uuid.UUID // v7
	Type          string    // "PetRegistered"
	SchemaVersion int
	Payload       any       // serializado por el registry
	Metadata      Metadata
	OccurredAt    time.Time
}

// StoredEvent es un evento persistido, tal como sale del store.
type StoredEvent struct {
	GlobalPosition int64
	EventID        uuid.UUID
	StreamID       uuid.UUID
	StreamType     string
	StreamVersion  int64
	Type           string
	SchemaVersion  int
	Payload        json.RawMessage
	Metadata       Metadata
	OccurredAt     time.Time
}

// Metadata acompaña a todo evento (PP-F1-DOM-012, docs/phase-1/02 §8).
// Los hashes se guardan en hex; nunca PII en claro.
type Metadata struct {
	ActorID       string   `json:"actor_id,omitempty"` // uuid o "system"
	ActorRoles    []string `json:"actor_roles,omitempty"`
	ActorOrgID    string   `json:"actor_org_id,omitempty"`
	CorrelationID string   `json:"correlation_id,omitempty"`
	CausationID   string   `json:"causation_id,omitempty"`
	Source        string   `json:"source,omitempty"` // api | worker | migration | admin-cli
	IPHash        string   `json:"ip_hash,omitempty"`
	UserAgentHash string   `json:"user_agent_hash,omitempty"`
	IdempotencyKey string  `json:"idempotency_key,omitempty"`
}

// Orígenes admitidos en metadata.source.
const (
	SourceAPI      = "api"
	SourceWorker   = "worker"
	SourceMigration = "migration"
	SourceAdminCLI = "admin-cli"
	SourceSystem   = "system"
)

// Event es el contrato que cumple todo evento de dominio persistible:
// nombre estable y schema_version ≥ 1 (ARQ-007). Los payloads llevan tags
// JSON porque son el contrato persistido (excepción de ARQ-002).
type Event interface {
	EventName() string
	SchemaVersion() int
}

// PIICarrier lo implementan los eventos que encapsulan PII en `pii{}` para
// que el registro sepa de qué sujetos es (diseño crypto-shredding, SEC-009;
// la implementación de cifrado llega en F5). Registry.PIISubjects lo consulta.
type PIICarrier interface {
	PIISubjects() []uuid.UUID
}

// Upcaster transforma un evento persistido en schema_version N a su forma
// N+1 (ARQ-007). Interfaz definida en F1; primer uso en F2. Nunca se renombra
// un campo ya persistido: se crea una versión nueva y un upcaster.
type Upcaster interface {
	EventType() string
	FromVersion() int
	Upcast(e StoredEvent) (any, error)
}
