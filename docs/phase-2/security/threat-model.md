# Threat model F2 (PP-F2-SEC-007)

- Estado: **borrador para revisión** (M2.0; requiere revisión de dos personas, §6). Se actualiza en cada
  hito de F2 cuando el componente correspondiente entra en código.
- Alcance: lo que F2 añade sobre el threat model F1 (`docs/phase-1/security/threat-model.md`, que sigue
  vigente): worker, outbox y process managers, scheduler de comandos, proyecciones asíncronas, PDF
  firmados, adjuntos clínicos, enlaces compartidos/páginas públicas y webhooks del proveedor de email.
- Método: STRIDE por componente nuevo; controles con su ID de spec y evidencia en el repo. Marco:
  ADR-009, ADR-010, ADR-012, `docs/phase-2/08-seguridad.md`.

## 1. Diagrama y fronteras de confianza (cambios sobre F1)

```
            Internet ──► Cloudflare WAF ──► Traefik ──┬─► SPA
                                                      ├─► api  (HTTP + proyecciones síncronas)
     /share/{token}, /verify/* (públicas, TB-P) ──────┘     │
                                                            │ TB3 (aplicación)
   Proveedor email ──webhook HMAC (TB-W)──► api            │
                                                            ▼
  ┌──────────────────────────── PostgreSQL ──────────────────────────────┐
  │ es.events (append-only, orden total) · es.outbox · es.snapshots      │
  │ read_* (sync + async) · platform.projection_checkpoints              │
  │ scheduler.command_jobs · notifications.* · platform.worker_instances │
  └───────────────▲──────────────────────────────▲───────────────────────┘
                  │ TB-WK (worker: Principal = system)
          ┌───────┴────────┐
          │ worker (1 rép.) │──► SMTP (M2.4) · Object storage (PDF, M2.1)
          └────────────────┘
```

Fronteras nuevas:

- **TB-WK** — el worker actúa como `system`: no pasa por `authz.Can` y escribe read models, outbox,
  jobs y (M2.4) notificaciones. Es un proceso de confianza, por eso su superficie se acota en código.
- **TB-P** — páginas públicas sin autenticación (`/share/{token}`, `/verify/encounters/{id}`,
  `/verify/cards/{petId}`): llegan en M2.1/M2.2.
- **TB-W** — webhooks del proveedor de email (bounces): M2.4.

## 2. STRIDE por componente nuevo

### 2.1 Worker y scheduler de comandos (M2.0)

| STRIDE | Amenaza | Control | Evidencia |
|---|---|---|---|
| E | `system` ejecuta comandos arbitrarios (p. ej. un PM comprometido o un job inyectado en `command_jobs`) | Lista cerrada `scheduler.AllowedCommands`: `Schedule` rechaza y el runner marca `failed` cualquier comando fuera de ella; cada PM declara `Emits()` y `process.Guard` impide programar otro; `ValidateEmits` en el arranque | `internal/platform/scheduler`, `internal/platform/process/process.go`; `TestScheduler_SimulatedClock`, `TestHarness_GuardRejectsUndeclared`, `TestValidateEmits` |
| T | Un job modificado en BD cambia el comando | `command_jobs` solo lo escribe `pp_app_writer` (mismo rol que la app); el comando se deserializa al tipo permitido (campos desconocidos ignorados) y pasa las invariantes del agregado | DAT-006; agregados con invariantes |
| R | No se sabe qué evento causó un comando del sistema | `correlation_id` del evento origen y `causation_id = event_id` viajan en el job y en `bus.SystemOrigin` → metadata del evento resultante (`actor_id = system`, `source = worker`) | INF-002; `process.JobFor` |
| D | Dos workers consumen a la vez (duplicados, carreras) | Lock de instancia `pg_advisory_lock(worker.<group>)` en conexión dedicada; heartbeat; si se pierde la conexión el proceso sale | `internal/platform/worker`; `TestWorker_InstanceLock`; alerta `WorkerDown` |
| D | Un job que falla siempre bloquea la cola | Reintentos con backoff y `failed` tras 5; `FOR UPDATE SKIP LOCKED` | `scheduler/runner.go` |
| I | PII en logs del worker | Logs con `projection`, `global_position`, `event_type`, `stream_id`; nunca payloads. Los errores de proyector pueden incluir texto de error de Postgres, no payloads | INF-002; revisión de `slog` en `platform/projection`, `process`, `scheduler` |

### 2.2 Proyecciones asíncronas (M2.0)

| STRIDE | Amenaza | Control | Evidencia |
|---|---|---|---|
| T / I | Un evento saltado deja un read model incorrecto (p. ej. una revocación no proyectada mantiene acceso en una vista derivada) | Orden total en `Append` (advisory lock, ADR-009 §3): un lector nunca ve N+1 antes que N. Checkpoint y proyección en la misma tx; fail-stop en vez de saltar | QA-001 `TestEventStore_QA001_TotalOrder` (gate CI); `TestProjection_FailStopAndResume` |
| E | Decidir autorización con un read model asíncrono retrasado | Regla: la autorización usa solo read models **síncronos** (`read_petregistry.pet_care_links`, membresías) o `encounter_custody`, cuya escritura es asíncrona pero solo **amplía** a lo que la propia organización firmó; las vistas derivadas nunca deciden acceso | ADR-009 §1; PetAccess (DOM-014, `internal/clinical/app/ports.go`) |
| I | El timeline expone a una clínica datos de otra | `visibility` por ítem; los ítems de vínculo/transferencia son `guardian`; el texto libre del motivo de revocación no se copia al read model | `internal/clinical/infra/timeline_projector.go`; `TestProjection_CatchUpTimeline` |
| D | Un proyector roto consume CPU en bucle o detiene todo | Backoff 1–16 s y fail-stop por proyección; las demás siguen; alerta `ProjectionFailed` | ARQ-007; `infra/observability/alerts.yml` |
| T | Rebuild accidental o concurrente corrompe datos | Rebuild bajo `FOR UPDATE` del checkpoint y status `rebuilding`; solo borra las tablas propias de la proyección; nunca toca `es.events` | ARQ-006; `TestProjection_QA002_RebuildDumpsEqual` |
| T / I | `rebuild --sync` de primarias pierde datos que no están en eventos (tokens de invitación/transferencia) o reabre accesos | Una sola tx con el lock de orden total (ningún comando confirma a medias); cada proyector guarda y restaura sus datos operativos; replay de todas las primarias juntas en orden global; un fallo revierte todo | ARQ-006; `TestProjection_SyncRebuildPrimaries` (dumps iguales y token reenviado válido tras el rebuild) |
| D | Activar el modo mantenimiento deja la API en 503 | Solo con escritura en `platform.maintenance` (rol de la app, CLI del worker dentro del host); healthz excluido; `worker maintenance status/off` en el runbook; un error de BD no cambia el estado cacheado | `internal/platform/maintenance`; `TestMaintenance_GateBlocksAPI`; RUNBOOK §15.1 |

### 2.3 Outbox y process managers (M2.0; PM concretos M2.1–M2.5)

| STRIDE | Amenaza | Control | Evidencia |
|---|---|---|---|
| T | Reentregas duplican efectos (dos emails, dos comandos) | PM idempotentes; despachar = programar con clave de deduplicación (`ON CONFLICT (dedup_key)`); harness QA-003 entrega cada evento 3 veces y exige el mismo resultado | `process/testkit`; `TestScheduler_SimulatedClock` |
| T | Efectos parciales de un PM que falla | Savepoint por evento: un fallo revierte solo sus escrituras | `TestOutbox_StreamOrderRetriesAndDeadLetter` |
| D | Un evento envenenado bloquea el outbox | Reintentos con backoff persistido (`next_attempt_at`), dead-letter tras 10 sin bloquear el resto; orden por stream | ARQ-008; alerta `OutboxPendingGrowing` (M2.4: `OutboxStuck`, `OutboxDeadLetter`) |
| I | Emails con datos clínicos | Plantillas sin contenido clínico | NOT-003 (M2.4) |

### 2.4 Event Store v1: upcasters y snapshots (M2.0)

| STRIDE | Amenaza | Control | Evidencia |
|---|---|---|---|
| T | Reescritura de eventos históricos para "migrarlos" | Upcasters en lectura; `es.events` sigue append-only (trigger + privilegios) | ARQ-005; `TestEventStore_UpcastOnRead` comprueba que la fila no cambia |
| T | Upcaster erróneo altera hechos clínicos | Golden de entrada/salida por upcaster; `Registry.Validate` exige cadena completa; el worker verifica al arrancar los tipos persistidos (`VerifyStored`) | `internal/petregistry/infra/upcasters_test.go`; `registry_test.go` |
| T | Snapshot manipulado | Snapshots nunca son fuente de verdad; `worker snapshots --drop` los borra sin pérdida; el hash de firma (ARQ-010) se calcula desde `es.events`, no desde snapshots | ARQ-004 (M2.3) |

### 2.5 Componentes de hitos posteriores (controles exigidos, aún sin código)

| Componente | Amenaza prioritaria (08 §1) | Control | Hito |
|---|---|---|---|
| Acceso clínico | IDOR por `pet_id`; ver consultas de otras clínicas tras revocación | `PetAccess` única puerta; custodia solo sobre encuentros propios; suite de authz ampliada | M2.1 (SEC-001) |
| Consultas firmadas y PDF | Alteración de consultas firmadas | Append-only + `ContentHash` RFC 8785 recomputable + PDF con hash; render solo en el worker | M2.1 (SEC-004, ARQ-010) |
| Adjuntos | PDF con JS, polyglots | Validación estructural (`pdfcpu`), re-encode, prefirmadas 15 min | M2.1 (SEC-003) |
| Enlaces compartidos | Fuga/enumeración | Token 256 bits, caducidad, PIN Argon2id, revocación, sin PII, `noindex`, rate limit | M2.2 (SEC-002) |
| Búsqueda de mascotas | Enumeración de tutores | Resultados mínimos, 20/h, tiempo constante, auditoría | M2.5 (DOM-013) |
| Webhooks de email | Falsificación | HMAC del proveedor, idempotencia por `message_id` | M2.4 (NOT-007) |

## 3. Amenazas prioritarias y estado

| Amenaza (08 §1) | Estado tras M2.0 |
|---|---|
| Worker con `Principal = system` ejecutando comandos | **Mitigada en código**: lista cerrada + `Emits` + Guard, con pruebas |
| Clínica accede a historial no vinculado (IDOR) | Puerto `PetAccess` definido; implementación y suite en M2.1 |
| Clínica ve consultas de otras tras revocación | Diseño (custodia) en M2.1 |
| Alteración de consultas firmadas | Base lista (append-only, orden total, upcasters sin reescritura); hash en M2.1 |
| Enumeración vía `pet-lookup` | M2.5 |
| Fuga por enlaces compartidos | M2.2 |
| Adjuntos maliciosos | M2.1 |
| Webhook de email falsificado | M2.4 |
| Emails con datos clínicos | M2.4 |

## 4. Hallazgos durante la elaboración (M2.0)

1. **`event_id` vacío en el outbox (F1).** `Append` no copiaba `event_id` a los eventos devueltos y el
   outbox guardaba el UUID cero. Corregido en M2.0 (`TestEventStore_OutboxCarriesStreamAndEventID`); las
   filas antiguas del outbox conservan el cero, sin impacto (los PM leen `es.events`).
2. **`recorded_at` no monótono.** Con `DEFAULT now()` (inicio de la transacción) dos commits podían
   registrar instantes invertidos respecto a `global_position`; el `lag_seconds` sería engañoso. Ahora
   se toma `clock_timestamp()` bajo el lock de orden total (gate QA-001 lo verifica).
3. **Compatibilidad durante el despliegue.** El api anterior inserta en el outbox sin `stream_id`: un
   trigger lo completa desde `es.events` para no rechazar comandos durante la ventana de despliegue.
   La migración bloquea (en espera, sin error) los appends del api anterior: 5,1 s medidos con
   500 000 eventos (`TestMigration_EventStoreV1Backfill`, RUNBOOK §15.2).
4. **El replay de F1 no es seguro para `pet_transfers`.** Reproyectar un `GuardianshipTransferCancelled`
   antiguo ejecuta `UPDATE … SET status = 'cancelled' WHERE pet_id = $1 AND status = 'pending'` y
   cancelaría la transferencia pendiente actual (y el hash del token no está en los eventos). El
   rebuild de primarias guarda la tabla, la vacía durante el replay y la restaura tal cual. Las
   invitaciones reciben un hash aleatorio en replay: se restaura el hash y los contadores de reenvío.

## 5. Riesgos residuales

| Riesgo | Dueño | Fase |
|---|---|---|
| Serialización global de appends (lock) limita el throughput | Backend | F3: revisión si p95 de espera > 20 ms (alerta `AppendLockContention`) |
| Un único worker es punto único de fallo de las vistas derivadas (no de los comandos) | Infra | F3 (particionado); `WorkerDown` + reinicio de systemd |
| `system` comparte rol de BD con el api (`pp_app_writer`) | Backend | F3: rol propio del worker con privilegios mínimos |

## 6. Checklist de revisión (dos personas)

- [ ] El diagrama de §1 refleja los procesos y conexiones desplegados (`infra/quadlet/deploy`).
- [ ] Cada fila de §2.1–§2.4 tiene evidencia que existe y pasa en CI.
- [ ] El ensayo de `rebuild --sync` en staging (RUNBOOK §15.1) devuelve la API al servicio y conserva invitaciones y transferencias pendientes.
- [ ] §2.5 coincide con 08 §1 y con el plan de hitos.
- [ ] Riesgos residuales aceptados por el responsable de producto.
- Revisor 1: ________ · fecha ____
- Revisor 2: ________ · fecha ____
