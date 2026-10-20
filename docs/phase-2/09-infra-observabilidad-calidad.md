# F2 · 09 — Infraestructura, observabilidad y calidad

## 1. Infraestructura

**[PP-F2-INF-001] (MUST)** Worker en todos los entornos.
- Compose: servicio `worker` (misma imagen que `api`, `command: worker`), `restart: unless-stopped`, 1 réplica, `healthcheck` sobre `/readyz` interno, recursos (1 vCPU, 1 GB).
- Despliegue (`deploy.sh`): orden migraciones → `worker` (detener antes de migrar si hay cambios en proyecciones; reanudar después) → `api`. Si una migración añade una proyección nueva, el runbook indica `rebuild --projection X` tras el despliegue.
- Imagen incluye **Typst** (o Chromium headless si se opta por HTML) para PDF; tamaño objetivo < 120 MB; `pdfcpu` embebido como librería Go.

**[PP-F2-INF-002] (MUST)** Observabilidad del worker.
- Métricas: `projection_lag_events/seconds{name}`, `projection_batch_duration_seconds`, `projection_status{name}` (gauge por estado), `outbox_pending`, `outbox_dead_letter_total`, `outbox_age_seconds` (del más antiguo pendiente), `scheduled_pending`, `reminder_dispatch_delay_seconds`, `command_jobs_pending`, `command_jobs_failed_total`, `eventstore_append_lock_wait_seconds`, `document_render_duration_seconds{kind}`, `worker_instance_lock{group}`.
- Trazas: cada lote de proyección es un *span* con `projection.name`, `batch.size`, `from/to position`; los PM propagan `correlation_id` del evento origen al comando despachado (`causation_id = event_id`).
- Logs del worker con `projection`, `global_position`, `event_type`, `stream_id`.
- Dashboards nuevos: "Worker & Projections", "Notifications", "Scheduling".

**[PP-F2-INF-003] (MUST)** Alertas (staging y prod):

| Alerta | Condición |
|---|---|
| `ProjectionFailed` | `projection_status{status="failed"} == 1` |
| `ProjectionLagHigh` | `projection_lag_seconds > 60` durante 5 min |
| `OutboxStuck` | `outbox_age_seconds > 300` |
| `OutboxDeadLetter` | incremento > 0 en 15 min |
| `ReminderDelay` | p95 `reminder_dispatch_delay_seconds > 60` |
| `AppendLockContention` | p95 `eventstore_append_lock_wait_seconds > 0.02` durante 10 min |
| `WorkerDown` | `worker_instance_lock == 0` durante 2 min |
| `DocumentRenderFailures` | `document_jobs.status = failed` > 5 en 1 h |

**[PP-F2-INF-004] (SHOULD)** PDF: renderizado en el worker con límite de concurrencia 2 y timeout 30 s; plantillas Typst con fuentes embebidas (licencia libre); caché por `(kind, ref_id, version)` en storage.

**[PP-F2-INF-005] (MUST)** Runbook F2 (`infra/deploy/RUNBOOK.md`, secciones nuevas): rebuild de proyección (parcial/total, tiempos esperados), reanudar proyección fallida, worker atascado (lock huérfano: `pg_terminate_backend` del titular, verificación de `worker_instances.heartbeat_at`), dead-letter de outbox/notificaciones (inspección y reintento), restauración con replay (restaurar `es.*` desde backup + `rebuild --all` para read models), rotación de secretos del proveedor de email, verificación de SPF/DKIM.

## 2. Calidad

**[PP-F2-QA-001] (MUST)** Orden total y concurrencia: test de integración (ARQ-003) como *gate*; además, test de `slot_reservations` (100 intentos concurrentes sobre franjas solapadas → exactamente 1 confirmada por franja) y de `card_entries_dup_uq`.

**[PP-F2-QA-002] (MUST)** Rebuild: test automatizado que genera 5.000 eventos sintéticos (generador de escenarios: clínicas, mascotas, consultas, vacunas, citas), compara *dumps* de todas las proyecciones asíncronas antes y después de `rebuild --all` (igualdad exacta excluyendo `updated_at`). También ejecutado en staging antes de M2.5 (criterio de salida 3).

**[PP-F2-QA-003] (MUST)** Harness de process managers: `testkit.Process(pm).GivenEvents(...).ThenCommands(...)` y `.ThenScheduled(...)`; pruebas de idempotencia (mismo evento 3 veces → mismo resultado); pruebas del scheduler con reloj simulado.

**[PP-F2-QA-004] (MUST)** E2E Playwright (nightly + release):
1. Cita: tutor solicita → clínica confirma → recordatorio T-24h aparece en `scheduled` (comprobación vía API admin de test) → check-in → iniciar consulta.
2. Consulta: editar SOAP con autoguardado → firmar → PDF listo → adenda → tutor ve historial con la consulta y la adenda.
3. Carnet: registrar vacuna con próxima dosis → tutor ve próxima dosis → declara vacuna antigua → clínica verifica → compartir enlace con PIN → página pública.
4. Vínculo: clínica busca por microchip → solicita → tutor aprueba → clínica ve ficha; tutor revoca → clínica solo ve sus consultas (custodia).
5. Emails: Mailpit recibe `appointment_confirmed` y `encounter_signed` en el idioma del tutor.

**[PP-F2-QA-005] (MUST)** Snapshots: plantillas de email (es/en, `.html` y `.txt`) y PDFs (comparación de texto extraído + hash de la estructura, no de bytes) para encuentro y carnet.

**[PP-F2-QA-006] (MUST)** Validación clínica: sesión documentada con ≥ 2 veterinarios del piloto sobre el editor SOAP, el carnet y los protocolos CO; hallazgos convertidos en issues antes de cerrar M2.2; aprobación explícita del catálogo de protocolos por un veterinario (firma en `backend/seeds/ref/APPROVALS.md`).

**[PP-F2-QA-007] (MUST)** Gates F2 (además de F1 QA-006): cobertura `domain/` ≥ 90% en los tres contextos nuevos; suite de autorización ampliada al 100% de acciones; test de orden total y de rebuild obligatorios en CI (`integration`); `oasdiff` sin *breaking*; `specs-status.sh --check`; p95 API < 300 ms y p95 lag de proyección < 2 s en k6 (100 VUs, mezcla 70% lectura / 30% comandos).

## 3. Convenciones nuevas

- Los PM y proyectores llevan prefijo de contexto en el nombre (`clinical.medical_timeline`); el nombre es estable (es la clave del checkpoint): renombrar = rebuild.
- Todo comando programable por el scheduler está en la lista `scheduler.AllowedCommands` con prueba de que ningún otro se puede serializar.
- Las migraciones que crean proyecciones nuevas incluyen en su cabecera `-- rebuild: <nombre>` para que `deploy.sh` lo detecte y lo recuerde en el log de despliegue.
