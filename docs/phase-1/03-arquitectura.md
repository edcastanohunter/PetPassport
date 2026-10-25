# F1 · 03 — Arquitectura

## 1. Vista general

```
Internet ─► WAF gestionado ─► Traefik ─┬─► frontend (estático, Vite build)      app.<dominio>
                                       ├─► api (Go)                             api.<dominio>
                                       └─► Keycloak                             auth.<dominio>
                                             │
          api ──► PostgreSQL (es.*, read_*, identity.*, platform.*)
          api ──► Object storage (MinIO / S3)          api ──► SMTP
          api ──► Keycloak (JWKS; Admin API con service account)
          api ──► OTel Collector ─► Tempo/Jaeger · Prometheus · Loki (dev: compose)
```

Un solo proceso `api` en F1. `worker` (mismo binario) se activa en F2.

## 2. Estructura del backend

**[PP-F1-ARQ-001] (MUST)** Layout y reglas de dependencia (ADR-001), verificadas con `depguard`/`go-arch-lint` en CI.

```
backend/
  cmd/
    api/main.go             # HTTP server
    migrate/main.go         # goose up/down/status
    worker/main.go          # F2 (stub en F1)
  api/
    openapi.yaml            # contrato fuente (ADR-005)
    gen/                    # oapi-codegen (server types + strict handlers)
  internal/
    shared/kernel/          # ids, errores de dominio, VO comunes (Email, Phone, Address), Clock, eventos base
    platform/
      config/               # carga 12-factor, validación
      db/                   # pgxpool, UnitOfWork, helpers tx
      eventstore/           # Event Store v0 (Append/Load/Streams), registry de tipos, serialización
      bus/                  # command bus, in-process event dispatcher a proyectores/reacciones
      projection/           # interfaz Projector, runner síncrono (F1), checkpoints (F2)
      outbox/               # escritura (F1), consumidor (F2)
      auth/                 # OIDC verifier (JWKS), Principal, middleware
      authz/                # políticas, Can(), decisiones auditables
      httpserver/           # chi router, middlewares (recovery, request-id, logging, timeouts, body limit, CORS)
      problem/              # RFC 9457
      idempotency/          # Idempotency-Key store
      storage/              # ObjectStorage port + S3/MinIO adapter + image processing
      mail/                 # Mailer port + SMTP adapter + plantillas
      telemetry/            # slog, OTel, Prometheus
    identity/{domain,app,infra,http}
    petregistry/{domain,app,infra,http}
    partnernetwork/{domain,app,infra,http}
  migrations/               # SQL (goose), por esquema y orden global
  test/                     # helpers de integración (testcontainers), fixtures, JWT de prueba
```

**[PP-F1-ARQ-002] (MUST)** Hexagonal por módulo:

- `domain/`: agregados, VOs, eventos, errores de dominio. Puro. Sin `context.Context`, sin pgx, sin JSON tags (la serialización vive en `infra/`… excepción pragmática: los *payloads* de evento sí llevan tags JSON porque son el contrato persistido).
- `app/`: `commands.go` (structs), `handlers.go` (un handler por comando), `queries.go`, `ports.go` (interfaces), `service.go` (fachada del módulo que registra handlers en el bus).
- `infra/`: repositorios (sobre eventstore), proyectores, adaptadores de puertos a otros módulos, consultas SQL (sqlc).
- `http/`: implementa la interfaz generada por `oapi-codegen` para los paths del módulo; mapea DTO ↔ comandos; mapea errores → `problem`.

## 3. Flujo de un comando

**[PP-F1-ARQ-003] (MUST)** Command bus + Unit of Work.

```
HTTP handler
  └─ valida DTO (generado) → construye Command + CommandContext{Principal, CorrelationID, IdempotencyKey}
     └─ authz.Can(principal, action, resource)            # deny by default (SEC-005)
        └─ bus.Dispatch(ctx, cmd)
           └─ idempotency.Begin(key)                        # si repetida: devuelve respuesta previa
              └─ uow.Do(ctx, func(tx) error {
                    agg, _ := repo.Load(tx, id)             # eventstore.Load + replay
                    events, err := agg.Handle(cmd)          # reglas de dominio
                    repo.Save(tx, agg, events)              # eventstore.Append(expectedVersion)
                    projections.Apply(tx, events)           # síncrono F1 (ARQ-005)
                    outbox.Enqueue(tx, events)              # ARQ-006
                    reactions.Apply(tx, events)             # DOM-011 (in-process)
                 })
                 └─ idempotency.Commit(key, response)
```

Reglas:

- Un comando toca **un** agregado. Operaciones que afectan a dos (p. ej. aceptar transferencia) se modelan como comando sobre uno + reacción/consulta sobre el otro, nunca dos `Append` en la misma transacción de distintos contextos.
- Los handlers devuelven `CommandResult{AggregateID, Version}`; HTTP responde `201/200` con `{id, version}` o el recurso proyectado (lectura en la misma tx para evitar *read-your-writes* fallidos).
- Reintento automático una vez ante `ErrConcurrency` si el comando es idempotente por naturaleza (p. ej. `GrantCareLink`); si no, 409.

## 4. Event Store v0

**[PP-F1-ARQ-004] (MUST)** API Go y semántica.

```go
package eventstore

type NewEvent struct {
    ID            uuid.UUID        // v7
    Type          string           // "PetRegistered"
    SchemaVersion int
    Payload       any              // serializado por el registry
    Metadata      Metadata
    OccurredAt    time.Time
}
type StoredEvent struct {
    GlobalPosition int64
    StreamID       uuid.UUID
    StreamType     string
    StreamVersion  int64
    Type           string
    SchemaVersion  int
    Payload        json.RawMessage
    Metadata       Metadata
    OccurredAt     time.Time
}

const ExpectedNewStream int64 = 0

type Store interface {
    // Append inserta events al stream. expectedVersion es la versión actual conocida (0 = stream nuevo).
    // Devuelve ErrConcurrency si la versión no coincide. Debe ejecutarse dentro de la tx del UoW.
    Append(ctx context.Context, tx pgx.Tx, streamID uuid.UUID, streamType string, expectedVersion int64, events []NewEvent) ([]StoredEvent, error)
    // Load devuelve los eventos del stream desde fromVersion (exclusive) en orden.
    Load(ctx context.Context, tx pgx.Tx, streamID uuid.UUID, fromVersion int64) ([]StoredEvent, error)
    // ReadAll lee por posición global (para rebuild en F2 y para pruebas).
    ReadAll(ctx context.Context, fromPosition int64, limit int) ([]StoredEvent, error)
}
```

Semántica de `Append` (una sola transacción):

1. `INSERT INTO es.streams(stream_id, stream_type, version) VALUES ($1,$2,0) ON CONFLICT DO NOTHING` si `expectedVersion == 0`.
2. `UPDATE es.streams SET version = version + $n, updated_at = now() WHERE stream_id = $1 AND version = $expected RETURNING version` → si 0 filas, `ErrConcurrency`. El `UPDATE` toma el *row lock*, serializando escritores del mismo stream.
3. `INSERT INTO es.events (...)` con `stream_version = expected + i`. La `UNIQUE(stream_id, stream_version)` es la red de seguridad.
4. `INSERT INTO es.outbox (event_id, global_position)` por evento.

Agregado base y repositorio:

```go
type AggregateRoot interface {
    ID() uuid.UUID; Version() int64; StreamType() string
    Apply(e StoredEvent) error           // mutación pura al rehidratar
    UncommittedEvents() []NewEvent; MarkCommitted(newVersion int64)
}
// repo genérico: Load = ReadStream + Apply en bucle; Save = Append(expected=agg.Version())
```

**[PP-F1-ARQ-005] (MUST)** Proyecciones síncronas y registro de proyectores.

```go
type Projector interface {
    Name() string                         // "petregistry.pets"
    Handles() []string                    // tipos de evento
    Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error
}
```

- El `projection.Runner` recibe los eventos recién añadidos y llama a cada proyector suscrito **en la misma tx**. Un fallo de proyección revierte el comando (consistencia fuerte en F1, explícitamente aceptada).
- Los proyectores deben ser **idempotentes** (`INSERT … ON CONFLICT DO UPDATE`, comparaciones por `stream_version`) para que en F2 puedan ejecutarse asíncronamente y en *rebuild* sin cambios.
- Cada proyector tiene prueba de *rebuild*: dado un conjunto de eventos, proyectar dos veces produce el mismo estado.

**[PP-F1-ARQ-006] (MUST)** Outbox escrito desde F1. Tabla `es.outbox` (DAT-003). En F1 nadie consume; se expone métrica `outbox_pending` para comprobar que crece y el *stub* de `cmd/worker` documenta el contrato del consumidor (F2): lectura por `global_position` con `FOR UPDATE SKIP LOCKED`, publicación, marca `published_at`.

**[PP-F1-ARQ-007] (MUST)** Serialización y versionado de eventos.

- `eventstore.Registry`: mapa `event_type@schema_version → factory`. Registrar un evento sin versión es error de arranque.
- Los payloads son structs Go con tags JSON; **nunca** se renombra un campo ya persistido: se crea `schema_version+1` y un *upcaster* `(v1) → (v2)` (interfaz definida en F1, primer uso en F2).
- Campos PII en sub-objeto `pii` (ADR-002).
- Prueba *golden* por evento: el JSON de ejemplo de `02-modelo-ddd.md` se deserializa sin pérdida.

## 5. Idempotencia

**[PP-F1-ARQ-008] (MUST)**

- Cabecera `Idempotency-Key` (UUID) obligatoria en `POST` de creación (`/pets`, `/organizations`, invitaciones, transferencias) y opcional en el resto de comandos.
- Clave compuesta `(actor_id, key)`. Se guarda `request_hash` (SHA-256 del cuerpo canónico), `status`, `response_body`, `expires_at = now()+24h`.
- Misma clave + mismo hash → se devuelve la respuesta almacenada con cabecera `Idempotent-Replayed: true`. Misma clave + distinto hash → `422 IDEMPOTENCY_KEY_REUSED`.
- Mientras un request con la clave está en vuelo, un segundo recibe `409 REQUEST_IN_PROGRESS` (fila con `status = in_progress` + lock).

## 6. Lado de consulta

**[PP-F1-ARQ-009] (MUST)**

- Queries en `app/queries.go` con structs de entrada/salida propios; implementación en `infra/queries_pg.go` con `sqlc` sobre `read_*`.
- Nunca leen `es.events`. Nunca cruzan esquemas de otro contexto (la lista de pacientes de la clínica se sirve desde `read_petregistry`, que ya desnormaliza nombre de tutor y organización a partir de eventos/puertos).
- Paginación por cursor opaco (base64 de `(sort_key, id)`), tamaño por defecto 20, máximo 100.
- Autorización en la consulta: filtros obligatorios por `Principal` (p. ej. `guardian_id = $actor` o `org_id IN ($memberships)`); una consulta sin filtro de principal no compila sin anotación explícita `// authz: public`.

## 7. Errores

**[PP-F1-ARQ-010] (MUST)** Tres familias:

| Familia | Ejemplos | HTTP |
|---|---|---|
| `domain.Error{Code, Message, Details}` | `PET_DECEASED`, `MICROCHIP_ALREADY_ASSIGNED`, `LAST_ADMIN_CANNOT_BE_REMOVED` | 409 / 422 según catálogo (API-011) |
| `app.Error` | `NOT_FOUND`, `FORBIDDEN`, `CONFLICT_VERSION`, `IDEMPOTENCY_KEY_REUSED` | 404 / 403 / 409 / 422 |
| infraestructura | timeouts, pg errors | 500/503 con `trace_id`, sin detalles internos |

Mapeo centralizado en `platform/problem` (`problem.From(err)`). Los `404` sobre recursos de otro usuario se devuelven como `404`, no `403`, para no revelar existencia (regla explícita en authz: *not found if not visible*).

## 8. Configuración y arranque

**[PP-F1-ARQ-011] (MUST)** 12-factor; variables con prefijo `PP_`. Validación al arranque (falla rápido).

| Variable | Ejemplo | Nota |
|---|---|---|
| `PP_ENV` | `dev\|staging\|prod` | |
| `PP_HTTP_ADDR` / `PP_INTERNAL_ADDR` | `:8080` / `:9090` | interno: `/metrics`, `/healthz`, `/readyz`, pprof (solo dev) |
| `PP_DB_URL` | `postgres://app_writer:…@…/petpassport?sslmode=require` | |
| `PP_DB_MAX_CONNS` | `20` | |
| `PP_OIDC_ISSUER` | `https://auth.<dominio>/realms/petpassport` | |
| `PP_OIDC_AUDIENCE` | `petpassport-api` | |
| `PP_KC_ADMIN_CLIENT_ID` / `_SECRET` | `petpassport-backend` | desde secret manager |
| `PP_STORAGE_*` | endpoint, bucket, credenciales | |
| `PP_SMTP_*` | | |
| `PP_CORS_ORIGINS` | `https://app.<dominio>` | |
| `PP_OTEL_EXPORTER_OTLP_ENDPOINT` | | |
| `PP_LOG_LEVEL` | `info` | |
| `PP_PUBLIC_BASE_URL` | para enlaces en emails | |

Secuencia de arranque: config → telemetry → db pool + `SELECT 1` → eventstore registry (valida tipos) → OIDC verifier (descarga JWKS; si falla en prod, no arranca) → módulos (`identity`, `partnernetwork`, `petregistry`) → router → `readyz = ok`.

Apagado: `SIGTERM` → `readyz = fail` → espera 5 s (drenado del LB) → `server.Shutdown(30 s)` → cierra pool.

## 9. Almacenamiento de objetos e imágenes

**[PP-F1-ARQ-012] (SHOULD)**

- Puerto `storage.ObjectStorage{Put, Get, Delete, PresignGet}`; adaptador S3 (MinIO en dev).
- Pipeline de foto (`platform/storage/image`): límite 5 MB → detección por *magic bytes* → decodificación con límite de píxeles (≤ 25 MP) → *strip* de metadatos EXIF (la re-codificación no los conserva) → redimensionar a 1024 px y 256 px → re-encodear. **Decisión M1.2**: salida JPEG calidad 82 en vez de WebP — no existe encoder WebP pure-Go compatible con `CGO_ENABLED=0`; revisar si cambia la imagen base → `Put` con `Content-Disposition: inline` y `Cache-Control: private, max-age=86400`.
- Servir fotos por URL prefirmada de 15 min emitida por la API (autorizada), nunca bucket público.

## 10. Diagrama de secuencia — `POST /pets`

```
SPA ─► Traefik ─► api: POST /api/v1/pets (Bearer, Idempotency-Key, body)
api: auth.Verify(JWT) → Principal · identity.EnsureUserAccount (JIT, throttled)
api: authz.Can(principal, "pet:register", nil) → ok (pet_owner)
api: idempotency.Begin
api: uow.Do:
      pet := petregistry.NewPet(RegisterPet{...})     → [PetRegistered]
      eventstore.Append(stream pet-<id>, expected 0)
      projector pets.Project(PetRegistered)           → INSERT read_petregistry.pets
      outbox.Enqueue
api: idempotency.Commit · 201 {id, version:1, ...}
```
