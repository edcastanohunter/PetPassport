# Tracking — Fase 2: Clínica y CQRS-ES (Medical Records)

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F2-PRD-001 | PRD | Abrir consulta (desde cita o directa) | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-002 | PRD | Constantes vitales con rangos por especie | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-003 | PRD | Documentación SOAP con medicación estructurada y autoguardado | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-004 | PRD | Adjuntos en consulta | SHOULD | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-005 | PRD | Firma de consulta: inmutabilidad, hash, PDF | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-006 | PRD | Adendas post-firma | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-007 | PRD | Control recomendado con recordatorio | SHOULD | M2.4 | PENDIENTE | | | |
| PP-F2-PRD-008 | PRD | Tutor: historial cronológico y detalle de consulta | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-009 | PRD | Clínica: ficha completa del paciente con custodia | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-010 | PRD | Registrar vacuna con lote y próxima dosis | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-011 | PRD | Registrar tratamiento preventivo | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-012 | PRD | Anular entrada de carnet con motivo y ventana | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-013 | PRD | Vacunas declaradas por el tutor y verificación | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-014 | PRD | Catálogo de vacunas y protocolos; cálculo de próximas dosis | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-015 | PRD | Carnet PDF/QR y enlace compartido con PIN | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-PRD-016 | PRD | Configuración de agenda por profesional | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-PRD-017 | PRD | Tutor solicita y cancela citas | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-PRD-018 | PRD | Clínica gestiona citas: confirmar, proponer, reprogramar, cancelar, vista de agenda | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-PRD-019 | PRD | Check-in, no-show y completar cita | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-PRD-020 | PRD | Solicitud de vínculo por la clínica con aprobación del tutor | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-PRD-021 | PRD | Co-tutor con permisos limitados | COULD | M2.5 | PENDIENTE | | | |
| PP-F2-PRD-022 | PRD | Importación CSV para clínicas piloto | COULD | M2.5 | PENDIENTE | | | |
| PP-F2-PRD-023 | PRD | Preferencias de notificación por categoría | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-PRD-024 | PRD | Recordatorios de citas, vacunas y controles | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-PRD-025 | PRD | Historial de peso con gráfico y alerta | SHOULD | M2.1 | EN_PROGRESO | | | |
| PP-F2-PRD-026 | PRD | Estadísticas básicas de clínica | SHOULD | M2.5 | PENDIENTE | | | |
| PP-F2-DOM-001 | DOM | Glosario F2 aplicado en código | MUST | M2.0 | IMPLEMENTADO | | | Nombres de código de M2.0 según el glosario (`TimelineProjector`, `PetAccess`, `AccessLevel`, `Membership`…); los términos de cada agregado se aplican en su hito (Encounter M2.1, Vaccination M2.2, Appointment M2.3). |
| PP-F2-DOM-002 | DOM | Context map F2: Clinical, Scheduling, Notifications | MUST | M2.0 | IMPLEMENTADO | | | Paquetes `internal/clinical`, `scheduling`, `notifications` + reglas depguard por contexto en `.golangci.yml`. |
| PP-F2-DOM-003 | DOM | Agregado Encounter: comandos e invariantes E1–E9 | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-DOM-004 | DOM | Value Objects clínicos | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-DOM-005 | DOM | Eventos Encounter v1 | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-DOM-006 | DOM | Agregado VaccinationCard: comandos e invariantes V1–V7 | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-DOM-007 | DOM | Value Objects del carnet | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-DOM-008 | DOM | Eventos VaccinationCard v1 | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-DOM-009 | DOM | Catálogos y servicio NextDueCalculator | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-DOM-010 | DOM | Agregado Appointment: comandos e invariantes A1–A8 | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-DOM-011 | DOM | Agregado ProfessionalAgenda y cálculo de disponibilidad | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-DOM-012 | DOM | Eventos Scheduling v1 | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-DOM-013 | DOM | Pet: solicitudes de vínculo, alertas, búsqueda | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-DOM-014 | DOM | Puertos F2 (PetAccess, Membership, Catalog, SlotReservations, DocumentRenderer) | MUST | M2.0 | IMPLEMENTADO | | | `PetAccess` (none/linked/custody) y `Membership` en `clinical/app/ports.go`; implementación de PetAccess con custodia en M2.1; Catalog (M2.2), SlotReservations (M2.3, DAT-004) y DocumentRenderer (M2.1, ARQ-010) se definen con su consumidor. |
| PP-F2-DOM-015 | DOM | Políticas y process managers F2 | MUST | M2.0 | EN_PROGRESO | | | Framework listo en ARQ-009 (Emits + Guard + ValidateEmits, harness QA-003); ninguna política de la tabla existe aún: llegan con Encounter (M2.1), complete-on-sign (M2.3) y Notifications (M2.4). |
| PP-F2-DOM-016 | DOM | RecordShare (enlace compartido) | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-ARQ-001 | ARQ | Proceso worker: runners, lock de instancia, apagado | MUST | M2.0 | IMPLEMENTADO | | | `cmd/worker` (run/rebuild/pause/resume/status/snapshots/healthcheck), lock de instancia, readyz, métricas, apagado ordenado. |
| PP-F2-ARQ-002 | ARQ | Suscripciones catch-up con checkpoints atómicos | MUST | M2.0 | IMPLEMENTADO | | | `platform/projection/async.go`: checkpoint `FOR UPDATE` + lote en la misma tx. |
| PP-F2-ARQ-003 | ARQ | Orden total en Append (advisory lock) | MUST | M2.0 | IMPLEMENTADO | | | `pg_advisory_xact_lock` tras el lock de fila del stream; `recorded_at = clock_timestamp()`; métrica de espera. Gate QA-001. |
| PP-F2-ARQ-004 | ARQ | Snapshots | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-ARQ-005 | ARQ | Upcasters (primer uso PetRegistered v2) | MUST | M2.0 | IMPLEMENTADO | | | Cadena de upcasters en `Registry`; `PetRegistered` v1→v2 (`alerts: []`) con golden; `VerifyStored` al arrancar el worker. |
| PP-F2-ARQ-006 | ARQ | Rebuild de proyecciones | MUST | M2.0 | IMPLEMENTADO | | | rebuild/pause/resume de asíncronas con `--to-position` y `--all` (QA-002); `rebuild --sync` de primarias F1 en una tx con lock de orden total, preservando datos operativos, con la API en mantenimiento (`platform.maintenance`, 503); `TestProjection_SyncRebuildPrimaries`; RUNBOOK §15.1. |
| PP-F2-ARQ-007 | ARQ | Reintentos, fail-stop y alertas de proyección | MUST | M2.0 | IMPLEMENTADO | | | Backoff 1–16 s, `failed` + `last_error`, alerta `ProjectionFailed`; `TestProjection_FailStopAndResume`. |
| PP-F2-ARQ-008 | ARQ | Consumidor de outbox con orden por stream y dead-letter | MUST | M2.0 | IMPLEMENTADO | | | `process.Consumer`: orden por stream, savepoint por evento, `next_attempt_at`, dead-letter tras 10. |
| PP-F2-ARQ-009 | ARQ | Framework de process managers y scheduler de comandos | MUST | M2.0 | IMPLEMENTADO | | | `platform/process` + `platform/scheduler` (lista cerrada `AllowedCommands`, dedup, runner con reloj inyectable). |
| PP-F2-ARQ-010 | ARQ | Generación de PDF e integridad por hash | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-ARQ-011 | ARQ | Pipeline de adjuntos (PDF e imágenes clínicas) | SHOULD | M2.1 | EN_PROGRESO | | | |
| PP-F2-ARQ-012 | ARQ | Read models derivados: timeline, due, peso, custodia | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-ARQ-013 | ARQ | Infraestructura de enlaces compartidos | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-ARQ-014 | ARQ | Migración de reacciones síncronas de F1 al process manager | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-API-001 | API | Endpoints de encuentros | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-API-002 | API | Endpoints de adjuntos | SHOULD | M2.1 | EN_PROGRESO | | | |
| PP-F2-API-003 | API | Endpoints de historial, resumen, peso, alertas | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-API-004 | API | Endpoints de carnet | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-API-005 | API | Endpoints de catálogos y administración | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-API-006 | API | Endpoints de enlaces compartidos y verificación pública | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-API-007 | API | Endpoints de citas (tutor y clínica) | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-API-008 | API | Endpoints de agenda y disponibilidad | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-API-009 | API | Endpoints de solicitud de vínculo, búsqueda y bandeja | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-API-010 | API | Endpoints de preferencias de notificación y baja | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-API-011 | API | Endpoint de estadísticas de clínica | SHOULD | M2.5 | PENDIENTE | | | |
| PP-F2-API-012 | API | Catálogo de errores F2 | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-API-013 | API | Jobs de documentos, verificación y cabecera de frescura | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-DAT-001 | DAT | Event Store v1: snapshots, outbox por stream, checkpoints ampliados | MUST | M2.0 | IMPLEMENTADO | | | Migración `20261028000008_event_store_v1.sql` (snapshots, outbox stream_id/dead_letter/next_attempt_at + trigger de compatibilidad, checkpoints ampliados). |
| PP-F2-DAT-002 | DAT | DDL read_clinical encuentros, custodia, timeline, peso, documentos | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-DAT-003 | DAT | DDL carnet, vaccination_due, catálogos, enlaces | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-DAT-004 | DAT | DDL scheduling con restricción de exclusión | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-DAT-005 | DAT | DDL notifications | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-DAT-006 | DAT | DDL scheduler de comandos y worker_instances | MUST | M2.0 | IMPLEMENTADO | | | `scheduler.command_jobs` y `platform.worker_instances` en la misma migración. |
| PP-F2-DAT-007 | DAT | DDL Pet Registry ampliado, bandeja, org_stats | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-DAT-008 | DAT | Retención, volumen y particionado de audit_log | SHOULD | M2.5 | PENDIENTE | | | |
| PP-F2-DAT-009 | DAT | Semillas de catálogos CO con aprobación veterinaria | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-FE-001 | FE | Agenda clínica día/semana | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-FE-002 | FE | Gestión de citas en clínica | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-FE-003 | FE | Editor de consulta SOAP con autoguardado | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-FE-004 | FE | Firma, adendas, PDF y borradores abandonados | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-FE-005 | FE | Ficha completa del paciente | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-FE-006 | FE | Carnet en clínica: registro, anulación, verificación | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-FE-007 | FE | Configuración de agenda del profesional | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-FE-008 | FE | Solicitud de vínculo desde clínica | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-FE-009 | FE | Tutor: historial y detalle de consulta | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-FE-010 | FE | Tutor: carnet, declarar, compartir, PDF | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-FE-011 | FE | Tutor: citas | MUST | M2.3 | PENDIENTE | | | |
| PP-F2-FE-012 | FE | Tutor: bandeja y preferencias | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-FE-013 | FE | Páginas públicas de enlace compartido y verificación | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-FE-014 | FE | Indicador de frescura en vistas derivadas | MUST | M2.0 | IMPLEMENTADO | | | `FreshnessIndicator.vue`, `useDerivedView`, cabecera `X-PP-Freshness` en el cliente API (+CORS expose). |
| PP-F2-NOT-001 | NOT | Process manager y catálogo de reglas reactivas | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-002 | NOT | Recordatorios programados y despacho | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-003 | NOT | Plantillas es/en versionadas sin contenido clínico | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-004 | NOT | Preferencias, categorías y baja por enlace | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-005 | NOT | Entrega, reintentos, dead-letter y métricas | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-006 | NOT | Idempotencia de envíos | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-NOT-007 | NOT | Proveedor de email, bounces, SPF/DKIM/DMARC | SHOULD | M2.4 | PENDIENTE | | | |
| PP-F2-SEC-001 | SEC | Autorización clínica ampliada y custodia | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-SEC-002 | SEC | Enlaces compartidos y páginas públicas | SHOULD | M2.2 | PENDIENTE | | | |
| PP-F2-SEC-003 | SEC | Adjuntos seguros | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-SEC-004 | SEC | Integridad de documentos firmados | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-SEC-005 | SEC | Auditoría de accesos clínicos | MUST | M2.1 | EN_PROGRESO | | | |
| PP-F2-SEC-006 | SEC | Privacidad clínica, exportación, consentimiento actualizado | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-SEC-007 | SEC | Threat model F2 | MUST | M2.0 | IMPLEMENTADO | | | `docs/phase-2/security/threat-model.md`; pendiente la revisión de dos personas (§6). |
| PP-F2-INF-001 | INF | Worker en compose y despliegue; imagen con Typst | MUST | M2.0 | IMPLEMENTADO | | | Worker en Quadlet dev/deploy (1 vCPU, 1 GB, healthcheck, Notify=healthy) y `deploy.sh` (marcadores `-- rebuild:`, worker antes que api); ventana de la migración medida (RUNBOOK §15.2). Typst o Chromium en la imagen con el spike de M2.1 (ARQ-010). |
| PP-F2-INF-002 | INF | Observabilidad del worker | MUST | M2.0 | IMPLEMENTADO | | | Métricas de proyección/outbox/scheduler/lock/instancia, span por lote, logs con projection/global_position/event_type/stream_id, dashboard Worker & Projections. `scheduled_pending`/`reminder_dispatch_delay_seconds` (M2.4) y `document_render_duration_seconds` (M2.1) se registran con su componente. |
| PP-F2-INF-003 | INF | Alertas F2 | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-INF-004 | INF | Renderizado PDF: concurrencia, timeout, caché | SHOULD | M2.1 | EN_PROGRESO | | | |
| PP-F2-INF-005 | INF | Runbook F2 | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-QA-001 | QA | Tests de orden total y reservas concurrentes | MUST | M2.0 | IMPLEMENTADO | | | Orden total 50×100 sin huecos como gate CI (`event-store-gates`). Pruebas de `slot_reservations` (M2.3, "QA-001 parte de reservas") y `card_entries_dup_uq` (M2.2) con sus tablas. |
| PP-F2-QA-002 | QA | Test de rebuild con comparación de dumps | MUST | M2.0 | IMPLEMENTADO | | | 5.000 eventos sintéticos, rebuild con el worker en marcha y `--to-position`; el generador se amplía con cada agregado. Staging antes de M2.5. |
| PP-F2-QA-003 | QA | Harness de process managers y scheduler | MUST | M2.0 | IMPLEMENTADO | | | `process/testkit` (reloj simulado, entrega ×3) + tests de outbox/scheduler con `SimClock`. |
| PP-F2-QA-004 | QA | E2E Playwright F2 | MUST | M2.5 | PENDIENTE | | | |
| PP-F2-QA-005 | QA | Snapshots de plantillas y PDFs | MUST | M2.4 | PENDIENTE | | | |
| PP-F2-QA-006 | QA | Validación clínica con veterinarios del piloto | MUST | M2.2 | PENDIENTE | | | |
| PP-F2-QA-007 | QA | Gates F2 | MUST | M2.0 | IMPLEMENTADO | | | CI: job `event-store-gates`, cobertura domain/ ≥ 90 % (`scripts/domain-coverage.sh`), promtool; suite de authz exige filas para toda acción/operación (`TestSuite_EveryActionHasRows`, `TestRoutes_AuthzMatrix`); oasdiff y specs-status existentes; k6 `tests/load/k6/f2-mix.js` (100 VUs 70/30, p95 API < 300 ms, lag < 2 s). Primera ejecución de k6 en staging pendiente (como QA-006 F1, requiere INF-006 F1). |
