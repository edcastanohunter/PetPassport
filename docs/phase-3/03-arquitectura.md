# F3 · 03 — Arquitectura: indexación, consultas, Redis, asynq y escalado

Marco: ADR-013 (OpenSearch), ADR-014 (Redis), ADR-015 (escalado), ADR-016 (catálogo).

## 1. Vista general F3

```
WAF/CDN ─► Traefik ─┬─► frontend (estáticos vía CDN)
                    ├─► api ×N ──┬─► PostgreSQL (fuente de verdad, read models primarios)
                    └─► Keycloak │─► Redis (caché · rate limit · sesiones de enlace)
                                 └─► OpenSearch (búsqueda) ── fallback PG
worker ×N (leases) ──► catch-up PG ──► proyecciones · indexers ──► asynq (Redis) ──► OpenSearch bulk
                  ──► outbox ──► process managers ──► asynq: email · push · PDF · imágenes · clamav · export
```

## 2. Pipeline de indexación

**[PP-F3-ARQ-001] (MUST)**

1. **Indexer** = proyector asíncrono (ADR-009) por índice: `search.pets`, `search.clinical`, `search.medications` (este último también por cambios CRUD vía tabla `ref.medications_changes`), `search.organizations`. Lee `es.events` por checkpoint y construye documentos desde los **read models de PostgreSQL** (no desde el evento aislado) para evitar duplicar lógica de desnormalización: el evento indica *qué* reindexar (`pet_id`, `encounter_id`…), la carga sale de `read_*`.
2. Por lote: `UPSERT` en `search.index_queue(index, doc_id, pet_id, version=global_position)` dentro de la tx del checkpoint; luego encola una tarea asynq `search:bulk{index, doc_ids}` (cola `default`). Idempotencia: el documento lleva `version = global_position` y la indexación usa `external_gte` → versiones antiguas se descartan.
3. Tarea `search:bulk`: carga documentos desde PG, `_bulk` a OpenSearch (≤ 500 docs / 5 MB), reintentos asynq (backoff, 10 intentos), dead-letter → alerta. Elimina filas de `index_queue` con `version ≤ indexada`.
4. Cambios de `access`: tarea `search:access{pet_id}` hace `update_by_query` sobre `clinical` y reindexa `pets`; para `OrganizationSuspended` se encola por página de mascotas de la org.
5. Documentos eliminados (mascota fusionada, medicamento `merged`): `delete` por id.

Garantías: *at-least-once* + versión monótona ⇒ convergencia. Métricas `search_index_lag_seconds{index}`, `search_bulk_errors_total`.

## 3. Servicio de consulta

**[PP-F3-ARQ-002] (MUST)** `internal/search/app/query.go`:

- Entrada: `SearchRequest{Index, Q, Filters, Facets, Page{cursor,size}, Highlight}` + `Principal`.
- **`accessFilter(principal, scope)`** es la única función que construye el filtro de autorización: tutor → `term access.guardian_id`; miembro de org → `bool should [terms access.org_ids_linked, terms access.org_ids_custody]` (custodia solo para `clinical`/`pets` con marca), org suspendida → consulta rechazada; `super_admin` → requiere motivo y se audita. Se añade como `filter` (no afecta a la puntuación).
- Consulta: `multi_match` (`best_fields`, `fuzziness: AUTO`, `operator: and` para ≥ 3 términos), campos con pesos (`diagnoses.text^3`, `pet_name^2`, `text`), `highlight` con fragmentos de 150 caracteres, facetas por `species`, `encounter_type`, `organization_id`, `year`.
- **Post-filtro**: los `pet_id` devueltos se validan contra `PetAccess` (PG, cacheado en Redis 60 s); cualquier discrepancia se oculta y se cuenta en `search_postfilter_dropped_total` (alerta si > 0 sostenido: el índice va atrasado o hay un bug).
- Paginación por `search_after` con cursor opaco; máximo 10.000 resultados navegables.
- Timeout 800 ms; `track_total_hits: 1000`.
- Autocompletado (`/suggest`): `completion` suggester sobre `name_suggest` con contexto `access`? No: el `completion` no admite filtros complejos → se usa `match_phrase_prefix` sobre `edge_ngram` con el mismo `accessFilter`, `size 8`, timeout 200 ms.

## 4. Reindex sin downtime

**[PP-F3-ARQ-003] (MUST)**

- Índices físicos `clinical-v<N>`; alias `clinical` (lectura) y `clinical-write` (escritura). `cmd/worker search reindex --index clinical`:
  1. Crea `clinical-v<N+1>` con el mapping del repo (`backend/internal/search/mappings/clinical.json`, versionado).
  2. Apunta `clinical-write` a **ambos** (escritura doble) y lanza un rebuild del indexer hacia el nuevo (checkpoint temporal).
  3. Al alcanzar el final, cambia `clinical` → `v<N+1>` atómicamente (`_aliases` con `remove`+`add`), retira el viejo de `clinical-write`, borra el viejo tras 24 h.
- Validación previa: `search mapping-check` compara el mapping del repo con el del índice activo y falla el despliegue si difieren sin reindex planificado.
- Pruebas de relevancia (QA-002) se ejecutan contra el nuevo índice antes del cambio de alias.

## 5. Degradación

**[PP-F3-ARQ-004] (MUST)** `SearchService` envuelve OpenSearch con *circuit breaker* (5 fallos/30 s → abierto 60 s). Abierto o timeout → `FallbackPG`: pacientes por `pg_trgm` sobre `read_petregistry.pets` (nombre, microchip) y `identity.user_accounts.display_name` vía puerto; historial por `ILIKE` limitado a diagnósticos (`read_clinical.encounters.diagnoses` con índice GIN) y últimos 90 días; sin facetas ni resaltado. Respuesta con cabecera `X-PP-Search-Mode: degraded` y `freshness`. Métrica `search_degraded_requests_total`.

## 6. Redis: cliente y disciplina

**[PP-F3-ARQ-005] (MUST)**

- Cliente `go-redis/v9` con `DialTimeout 200 ms`, `ReadTimeout 100 ms` (caché) / `1 s` (colas), pool por réplica (≤ 50), TLS en prod, `ClientName` por proceso.
- Dos bases lógicas: `cache` (db 0, `allkeys-lru`) y `jobs` (db 1, `noeviction`, AOF `everysec`) — o dos instancias gestionadas.
- *Circuit breaker* en caché: fallo de Redis ⇒ *miss* (nunca error al usuario); en rate limit ⇒ **permitir** y contar `ratelimit_bypass_total` (fail-open documentado y alertado); en asynq ⇒ el productor reintenta y, si no puede, la fila origen en PG permanece pendiente (los runners PG re-encolan).
- Prohibido usar `KEYS`; invalidaciones por patrón con `SCAN` solo en jobs administrativos.

## 7. Caché compartida e invalidación

**[PP-F3-ARQ-006] (MUST)**

| Clave | Contenido | TTL | Invalidación |
|---|---|---|---|
| `pp:v1:membership:{user_id}` | `[]Membership` | 300 s | `MemberRemoved/RoleChanged/Joined`, `OrganizationSuspended/Reactivated/Verified` |
| `pp:v1:petaccess:{pet_id}` | `{guardian_id, linked[], custody[]}` | 60 s | `CareLink*`, `EncounterSigned`, `PetGuardianshipTransferred`, `PetClaimed` |
| `pp:v1:pet:summary:{pet_id}` | DTO de `/summary` | 120 s | eventos de `Pet`, `Encounter` firmado, carnet |
| `pp:v1:org:profile:{org_id}` | `OrgSummary` | 600 s | `Organization*` |
| `pp:v1:availability:{org}:{prof}:{date}:{type}` | franjas | 30 s | `Appointment*`, `Agenda*` |
| `pp:v1:catalog:vaccines:{species}` / `protocols:{country}:{species}` | listas | 3600 s | CRUD de catálogos |
| `pp:v1:legal:current` | versiones | 3600 s | CRUD |

- Cache-aside con *single-flight* por clave (`singleflight`) para evitar estampidas; valores serializados con `msgpack` + prefijo de versión de esquema; **nunca** PII de contacto en caché salvo `membership` (ids y roles).
- `cache-invalidator` (process manager sobre outbox): `DEL` de claves derivadas del evento + `PUBLISH pp:cache:invalidate <key>` para que cada réplica `api` limpie su caché L1 en memoria (pequeña, 5 s TTL, `ristretto`). L1 solo para `membership` y `petaccess`.
- Métricas `cache_requests_total{key_family,result}`, ratio objetivo ≥ 80% en `membership`/`petaccess`.

## 8. Rate limiting distribuido

**[PP-F3-ARQ-007] (MUST)** Middleware `platform/ratelimit` (ventana deslizante en Redis, script Lua atómico, clave `pp:rl:{scope}:{id}:{route_group}`):

| Ámbito | Grupo de rutas | Límite |
|---|---|---|
| por usuario | `search` (búsqueda y suggest) | 120/min |
| por usuario | `commands` (POST/PUT/PATCH/DELETE) | 60/min |
| por usuario | `lookup` (`pet-lookup`, `organizations/lookup`) | 20/h |
| por usuario | `export` (cohortes, export) | 5/h |
| por organización | `search` | 1.200/min (suma de miembros) |
| por IP (no autenticado) | `public` (`/share/*`, `/verify/*`, `/unsubscribe/*`, `/directory`) | 60/min |
| por token de enlace | `/share/{token}` | 30/min |

Respuesta `429` con `code RATE_LIMITED`, `Retry-After`, cabeceras `RateLimit-Limit/Remaining/Reset` (IETF *draft-ietf-httpapi-ratelimit-headers*). WAF mantiene límites más laxos por IP como primera capa. Límites configurables por entorno; *allowlist* para pruebas de carga en staging.

## 9. asynq: colas y trabajos

**[PP-F3-ARQ-008] (MUST)**

| Cola | Prioridad | Trabajos |
|---|---|---|
| `critical` | 6 | `notify:email`, `notify:push` |
| `default` | 3 | `search:bulk`, `search:access`, `doc:render` (PDF), `media:process` (imágenes), `media:scan` (ClamAV) |
| `low` | 1 | `export:user`, `export:cohort`, `catalog:import`, `stats:refresh` |

- Cada tipo tiene `TaskID` determinista (p. ej. `doc:render:{kind}:{ref}:{version}`) → `asynq` deduplica (`Unique`); handlers idempotentes.
- Reintentos: 10 con backoff exponencial (máx 1 h); tras agotar → `archived` (DLQ de asynq) + alerta + fila origen marcada `failed` en PG (`deliveries`, `document_jobs`, `index_queue`).
- Migración de F2: los runners PG de `notifications-dispatch`, `documents` y `media` pasan a **productores** (leen PG, encolan en asynq con dedup) y los handlers se ejecutan en el pool asynq (concurrencia 10 por réplica, configurable). Los runners de **proyecciones**, **outbox** y **command-scheduler** siguen en PG (orden).
- `asynqmon` en dev/staging (sin exposición pública).

## 10. Leases de worker

**[PP-F3-ARQ-009] (MUST)** `platform.worker_leases(name PK, holder, acquired_at, expires_at)`:

- Cada runner con orden (proyección, outbox, scheduler, productores) es una **unidad de lease**. Al arrancar, cada réplica intenta `INSERT … ON CONFLICT DO UPDATE WHERE expires_at < now()` para cada unidad; renueva cada 10 s (`TTL 30 s`); si pierde el lease (renovación falla), detiene el runner inmediatamente.
- Reparto: la réplica con menos leases intenta primero (jitter) → equilibrio aproximado; `cmd/worker leases` muestra el mapa; `--release <name>` fuerza rebalanceo.
- Métricas `worker_leases_held{holder}`, `worker_lease_takeovers_total`.
- El lock global de instancia de F2 desaparece.

**[PP-F3-ARQ-010] (COULD)** Particionado intra-proyección: `name = clinical.medical_timeline#p{k}` con `k ∈ [0, P)`, cada partición filtra `hashtext(stream_id::text) % P = k` en la lectura de `es.events` y tiene su checkpoint; se activa por configuración (`PP_PROJECTION_PARTITIONS["clinical.medical_timeline"]=4`) y exige rebuild al cambiar `P`. Orden por stream se conserva (un stream siempre cae en la misma partición).

**[PP-F3-ARQ-011] (SHOULD)** Lectura tolerante a huecos (ADR-009 §3): se implementa **solo si** `eventstore_append_lock_wait_seconds` p95 > 20 ms durante 10 min en staging bajo k6 (PRF-009). Diseño: el lector no avanza el checkpoint más allá de `min(global_position de transacciones en curso) − 1` obtenido de `pg_snapshot_xmin`/`txid` registrado en `es.events.xid` (columna `xid8` nueva), y el advisory lock se retira. Si no se dispara el umbral, queda documentado y `DESCARTADO` con nota.

## 11. `api` con N réplicas

**[PP-F3-ARQ-012] (MUST)** Estado por proceso permitido: caché L1 5 s, pool de conexiones, JWKS. Todo lo demás en Redis/PG. Rolling: `deploy.sh` actualiza réplica a réplica esperando `readyz` y 5 s de drenado; Traefik `healthcheck.interval 5s`. Idempotency-Key y advisory locks ya son de BD. Límite de conexiones PG = réplicas × `PP_DB_MAX_CONNS` ≤ 80% de `max_connections`; `pgbouncer` (PRF-005) si se supera.

## 12. Helm y valores compartidos

**[PP-F3-ARQ-013] (SHOULD)** `infra/helm/petpassport` (Deployments `api`, `worker`; Services; Ingress Traefik; ConfigMaps desde `infra/values/<env>.yaml`; Secrets externos vía `ExternalSecret`; HPA para `api` por CPU; PDB). `infra/compose/` se genera desde los mismos valores con una plantilla (`make compose-render ENV=staging`) para una única fuente de configuración. CI: `helm lint` + despliegue en kind + smoke.

## 13. Antivirus

**[PP-F3-ARQ-014] (SHOULD)** `media:scan` con ClamAV (`clamd` como servicio; `clamdscan` por socket): todo adjunto PDF/imagen se marca `scan: pending` al subir y no es descargable hasta `clean`; `infected` → cuarentena (objeto movido a `quarantine/`, adjunto marcado, aviso a la clínica, alerta). Definiciones actualizadas diariamente (`freshclam`).

## 14. Web Push

**[PP-F3-ARQ-015] (SHOULD)** Canal `push` en Notifications: claves VAPID por entorno (secret manager), `notify:push` con `webpush-go`; payload cifrado mínimo (`{title, body, url, category}` ≤ 2 KB); `410 Gone`/`404` → suscripción `expired`; service worker en la SPA (FE-008); throttle: push y email no se duplican si el usuario tiene ambos (preferencia por canal).

## 15. CDN

**[PP-F3-ARQ-016] (MUST)** Estáticos del front servidos por la CDN del WAF (`Cache-Control: public, max-age=31536000, immutable` para *hashed assets*). Fotos y miniaturas: URLs firmadas del object storage detrás de la CDN (firmas 15 min; `Cache-Control: private`), nunca bucket público; adjuntos clínicos **sin** CDN (prefirmadas directas).
