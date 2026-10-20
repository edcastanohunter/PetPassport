# F2 · 03 — Arquitectura: Event Store v1, worker y proyecciones

Marco: ADR-009 (catch-up + outbox + orden total), ADR-010, ADR-011, ADR-012.

## 1. Vista general F2

```
WAF ─► Traefik ─┬─► frontend
                ├─► api  ──────────────┐
                └─► Keycloak           │  PostgreSQL
                                       ├─► es.events / es.streams / es.outbox / es.snapshots
          worker ◄── catch-up ─────────┤   read_* (sync + async)   platform.projection_checkpoints
          worker ◄── outbox ───────────┤   notifications.*          scheduler.command_jobs
          worker ──► SMTP · Object storage (PDF)
```

Mismo binario, dos modos: `cmd/api` (HTTP + proyecciones síncronas) y `cmd/worker` (suscripciones, outbox,
notificaciones, scheduler, rebuild). Una réplica de `worker` en F2 (lock de instancia); particionado en F3.

## 2. Proceso `worker`

**[PP-F2-ARQ-001] (MUST)**

- Arranque: config → telemetry → db → registry de eventos → **lock de instancia** `pg_advisory_lock(worker.<group>)` (si no lo obtiene, espera y reintenta; evita dos consumidores) → arranca *runners*: `projections`, `outbox`, `notifications-dispatch`, `command-scheduler`.
- Cada runner es un bucle con *polling* adaptativo (10 ms tras lote lleno, 250 ms en vacío, `LISTEN/NOTIFY` en `es.events` como acelerador opcional) y apagado ordenado por `SIGTERM` (termina el lote en curso ≤ 30 s).
- `readyz` del worker: lock obtenido + db + todos los runners vivos; `/metrics` en puerto interno.
- `cmd/worker rebuild --projection <name> [--all] [--to-position N]`: modo one-shot (ARQ-006).

## 3. Suscripciones catch-up y checkpoints

**[PP-F2-ARQ-002] (MUST)**

```go
type AsyncProjector interface {
    Projector                                  // Name, Handles, Project(ctx, tx, event)  (F1)
    // opcional: hooks para rebuild
    Reset(ctx context.Context, tx pgx.Tx) error // trunca sus tablas
}
```

Runner por proyección (`projection.Subscription`):

1. `pos := checkpoint(name)` (fila en `platform.projection_checkpoints`, creada en 0 si no existe).
2. `events := es.ReadAll(from=pos, limit=500, filter=Handles())` — lectura con filtro de tipos en SQL.
3. Por lote, en **una transacción**: `Project` para cada evento (en orden) y `UPDATE projection_checkpoints SET last_position = $last` → atomicidad "proyección + checkpoint" ⇒ *exactly-once* efectivo sobre el read model.
4. Métricas: `projection_lag_events{name}` (= `max(global_position) − last_position`), `projection_lag_seconds{name}` (ahora − `recorded_at` del último proyectado), `projection_batch_duration_seconds`.
5. Varias proyecciones avanzan de forma independiente (cada una su checkpoint). Las de un mismo contexto pueden agruparse en un `ProjectionGroup` para compartir transacción y reducir lecturas.

Proyecciones asíncronas F2:

| Nombre | Tablas | Eventos |
|---|---|---|
| `clinical.medical_timeline` | `read_clinical.medical_timeline` | `EncounterSigned`, `EncounterAddendumAdded`, `Vaccination*`, `Preventive*`, `AppointmentCompleted`, `PetGuardianshipTransferred`, `CareLinkGranted/Revoked` |
| `clinical.weight_history` | `read_clinical.weight_history` | `EncounterSigned` (con vitals), `VaccinationEntryRecorded{weight_kg}` |
| `clinical.vaccination_due` | `read_clinical.vaccination_due` | `Vaccination*`, `Preventive*`, `NextDueAdjusted`, `PetMarkedDeceased` |
| `clinical.encounter_custody` | `read_clinical.encounter_custody` | `EncounterSigned` (org → pet, encounter) |
| `partnernetwork.org_stats` | `read_partnernetwork.org_stats_monthly` | `EncounterSigned`, `Appointment*`, `CareLinkGranted`, `VaccinationEntryRecorded` |
| `petregistry.guardian_inbox` | `read_petregistry.guardian_inbox` | `CareLinkRequested/…`, `AppointmentAlternativeProposed`, `FollowUpRecommended` |

Síncronas (misma tx del comando, como F1): `read_clinical.encounters` (+ `encounter_attachments`), `read_clinical.card_entries`, `read_scheduling.appointments`, `read_scheduling.agendas`, `read_petregistry.*` (incluye `care_link_requests`).

## 4. Orden total en `Append`

**[PP-F2-ARQ-003] (MUST)** `Append` ejecuta `SELECT pg_advisory_xact_lock(hashtext('es.append'))` como primera sentencia tras obtener el *row lock* de `es.streams`. Orden de adquisición fijo (stream → global) para evitar *deadlocks*. Métrica `eventstore_append_lock_wait_seconds` (histograma) con alerta p95 > 20 ms durante 10 min (umbral de revisión ADR-009).

Test de *gate* (QA-001): 50 goroutines × 100 appends en streams distintos; un lector concurrente por `global_position` registra la secuencia vista; se verifica estrictamente creciente sin huecos, y que `recorded_at` es monótono no decreciente.

## 5. Snapshots

**[PP-F2-ARQ-004] (MUST)**

- Tabla `es.snapshots(stream_id, stream_version, schema_version, state jsonb, created_at)` — una fila por stream (se reemplaza).
- `Snapshotter` opcional por agregado: `SnapshotState() (any, int)` / `RestoreSnapshot(state json.RawMessage, schema int)`. El repositorio genérico: `Load` = snapshot (si existe y `schema_version` compatible) + eventos posteriores; `Save` = tras `Append`, si `version − snapshotVersion ≥ threshold` escribe snapshot **fuera** de la tx del comando (best-effort, en el worker o asíncrono en api).
- Activado en F2 para `ProfessionalAgenda` (threshold 100) y `VaccinationCard` (threshold 200). Snapshots inválidos (cambio de `schema_version` del estado) se ignoran y se regeneran.
- Los snapshots **nunca** son fuente de verdad: `cmd/worker snapshots --drop` los borra sin pérdida.

## 6. Upcasters

**[PP-F2-ARQ-005] (MUST)**

```go
type Upcaster interface {
    From() (eventType string, schemaVersion int)
    Upcast(payload json.RawMessage) (newPayload json.RawMessage, newSchemaVersion int, err error)
}
```

- El `Registry` encadena upcasters al deserializar (`v1→v2→v3`) tanto en `Load` como en `ReadAll`; los eventos almacenados no se reescriben.
- Primer uso real en F2: `PetRegistered v1 → v2` añadiendo `alerts: []` (perfil ampliado, DOM-013) — ejercita el mecanismo con un cambio inocuo.
- Prueba: cada upcaster tiene *golden* de entrada/salida; el `Registry` falla al arrancar si existe `vN` sin cadena completa desde `v1`.

## 7. Rebuild

**[PP-F2-ARQ-006] (MUST)**

- `cmd/worker rebuild --projection clinical.medical_timeline`:
  1. Pausa la suscripción (flag en `projection_checkpoints.paused`).
  2. `Reset` (truncado de tablas propias) y `last_position = 0` en una tx.
  3. Replay por lotes hasta `max(global_position)` (o `--to-position`), con progreso en logs/métrica `projection_rebuild_progress`.
  4. Reanuda. Durante el rebuild la API sirve la tabla vacía/parcial: las vistas afectadas muestran "Actualizando" (FE-014).
- `--all` reconstruye todas las asíncronas en orden de dependencia (ninguna en F2 depende de otra).
- Rebuild **síncronas** (`read_*` primarias): mismo mecanismo con `--sync` pero con la API en modo mantenimiento (solo para desastre; runbook INF-005). COULD en F2: *blue/green* por tabla sombra + `RENAME`.
- Prueba de gate (QA-002): rebuild completo en staging con comparación de *dumps* ordenados (excluyendo `updated_at`).

## 8. Fallos, reintentos y fail-stop

**[PP-F2-ARQ-007] (MUST)**

- Error en `Project` → rollback del lote → reintento del lote con *backoff* 1 s, 2 s, 4 s, 8 s, 16 s. Si persiste: `projection_checkpoints.status = 'failed'`, `last_error`, la suscripción se detiene, alerta `ProjectionFailed`. Las demás continúan.
- Reanudación manual (`cmd/worker resume --projection X`) tras corregir el código (nuevo despliegue) o los datos.
- Panic en un proyector se recupera y se trata como error.
- Lotes parcialmente proyectados nunca confirman checkpoint (atomicidad §3).

## 9. Consumidor de outbox y process managers

**[PP-F2-ARQ-008] (MUST)** Runner `outbox`:

1. `SELECT … FROM es.outbox WHERE published_at IS NULL ORDER BY global_position LIMIT 200 FOR UPDATE SKIP LOCKED`.
2. Para cada evento: entrega a los *process managers* suscritos (`Handle(ctx, event) error`); todos idempotentes.
3. Éxito → `published_at = now()`; fallo → `attempts++`, `last_error`, reintento con backoff (hasta 10); > 10 → `dead_letter = true` + alerta; no bloquea los siguientes (los PM no dependen del orden entre streams distintos; dentro del mismo stream el orden se respeta porque el outbox se procesa por `global_position` y un fallo no marca `published_at`, y el runner reintenta el **mismo** evento antes de avanzar en ese stream — implementado con `stream_id` en `es.outbox` y salto de eventos de streams con uno fallido pendiente).

**[PP-F2-ARQ-009] (MUST)** Framework de process managers (`platform/process`):

```go
type ProcessManager interface {
    Name() string; Handles() []string
    Handle(ctx context.Context, e StoredEvent, tx pgx.Tx) error   // puede: despachar comandos, escribir en notifications.*, programar jobs
}
```

- **Scheduler de comandos** (`scheduler.command_jobs`): `Schedule(runAt, command, dedupKey)` desde un PM; el runner `command-scheduler` ejecuta vencidos (`FOR UPDATE SKIP LOCKED`) despachando por el command bus con `Principal = system`. Usos F2: expirar solicitudes de vínculo (14 d), expirar propuestas (48 h), no-show automático (24 h), marcar borradores abandonados (7 d).
- PMs F2: `scheduling.complete-on-sign`, `clinical.sync-species`, `notifications.rules` (07), `petregistry.expire-care-link-requests`, `scheduling.expire-proposals`, `scheduling.auto-no-show`, `clinical.abandoned-drafts`.

## 10. Documentos PDF e integridad

**[PP-F2-ARQ-010] (MUST)**

- Generación con **Typst** (binario en la imagen del worker) desde plantillas `.typ` versionadas; alternativa `chromedp`+HTML si Typst no cubre (decisión en M2.1 spike). Nunca se generan en el proceso `api` (CPU); la API encola `document_jobs` y responde `202` con URL de estado, o sirve el PDF cacheado si existe.
- Encuentro firmado: PDF = contenido firmado + adendas + pie con `content_hash` (hex) y `stream_version`; se guarda en object storage `clinical/<pet>/<encounter>/v<k>.pdf` (nueva versión por adenda). Endpoint de verificación `GET /verify/encounters/{id}?hash=` recomputa el hash desde `es.events` (API-013).
- Carnet: PDF con QR a `/share/{token}` (si el tutor creó un enlace) o al verificador público mínimo `GET /verify/cards/{petId}?h=` (muestra solo "entradas verificadas: n, última: fecha").
- `ContentHash` = SHA-256 de la concatenación `event_id|event_type|schema_version|payload canónico (RFC 8785)` de los eventos del stream hasta `stream_version`, calculado en el repositorio al firmar y recomputable.

## 11. Adjuntos

**[PP-F2-ARQ-011] (SHOULD)** Reutiliza `platform/storage` (F1) ampliando: PDF permitido (validación de cabecera `%PDF-`, ≤ 10 MB, sin JavaScript embebido — rechazo si `/JS` o `/JavaScript` aparecen en el *trailer/objects* escaneados; análisis con `pdfcpu validate`), imágenes re-encodeadas como en F1 (sin *strip* de EXIF para radiografías/laboratorio: se conservan metadatos salvo GPS). Claves `clinical/<pet>/<encounter>/<attachment>`; URLs prefirmadas 15 min; descarga auditada.

## 12. Read models derivados (detalle)

**[PP-F2-ARQ-012] (MUST)**

- `medical_timeline`: una fila por ítem `{pet_id, occurred_at, kind encounter|addendum|vaccine|preventive|appointment|guardianship|care_link, ref_id, organization_id?, title, summary (≤ 300), visibility guardian|org|both, position}`; orden `(pet_id, occurred_at DESC, position DESC)`.
- `vaccination_due`: `{pet_id, entry_id, kind, product_name, due_date, basis, status upcoming|overdue|satisfied|cancelled}`; `satisfied` cuando una entrada posterior del mismo producto se registra; recalculado por eventos.
- `weight_history`: `{pet_id, measured_at, weight_kg, source encounter|vaccination, ref_id, organization_id}`.
- `encounter_custody`: `{organization_id, pet_id, encounter_id, signed_at}` — base de la regla de custodia (`PetAccess`).

## 13. Enlaces compartidos

**[PP-F2-ARQ-013] (SHOULD)** Token 32 bytes (base64url) → `SHA-256` en `read_clinical.record_shares`; PIN con Argon2id; endpoint público `GET /share/{token}` servido por la API con *rate limit* por token e IP (WAF) y cabecera `X-Robots-Tag: noindex`; la respuesta es un DTO reducido (sin contacto del tutor); cada acceso incrementa contador y registra `audit_log` con `ip_hash`.

## 14. Migración de reacciones síncronas de F1

**[PP-F2-ARQ-014] (MUST)** Los emails síncronos de F1 (invitaciones, transferencias, verificación de organización) se mueven a reglas del process manager de Notifications; se elimina el `Mailer` de las reacciones de F1. Criterio: ninguna transacción de comando abre conexiones SMTP.

## 15. Secuencia — firmar una consulta

```
SPA ─► POST /encounters/{id}/sign (If-Match)
api: authz(encounter:sign, EncounterResource{org, attending_vet}) → ok
api: uow: load enc-<id> → SignEncounter → [EncounterSigned{hash}] → Append (advisory lock) → proyección sync encounters → outbox
api: 200 {status: signed, signature{…}, pdf: {status: "pending"}}
worker/outbox: EncounterSigned → PM complete-on-sign → Appointment.Complete ; PM notifications → email tutor "Nueva consulta" (según prefs)
worker/projections: medical_timeline, weight_history, encounter_custody, org_stats
worker/documents: render PDF → storage → read_clinical.encounters.pdf_object_key
SPA (poll /encounters/{id}) ─► pdf.url disponible
```
