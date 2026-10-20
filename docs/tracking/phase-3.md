# Tracking — Fase 3: Search & Performance

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F3-PRD-001 | PRD | Búsqueda de pacientes con autocompletado tolerante a errores | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-PRD-002 | PRD | Búsqueda en historiales con filtros, facetas, resaltado y sinónimos | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-PRD-003 | PRD | Búsqueda del tutor en sus mascotas | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-PRD-004 | PRD | Autocompletado de medicamentos en el plan con texto libre y sugerencia | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-PRD-005 | PRD | Mantenimiento del catálogo: CRUD, fusión, importación, sugerencias, métrica | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-PRD-006 | PRD | Directorio público opt-in de clínicas | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-007 | PRD | Cohortes de pacientes y campañas de recordatorio | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-008 | PRD | Panel de plataforma para SuperAdmin | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-009 | PRD | Objetivos de rendimiento percibido en móvil | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRD-010 | PRD | Indicadores de frescura y degradación en búsqueda | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-PRD-011 | PRD | Notificaciones push web opt-in | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-012 | PRD | Paciente provisional y reclamación | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-013 | PRD | Importación CSV con pacientes provisionales | COULD | M3.4 | PENDIENTE | | | |
| PP-F3-PRD-014 | PRD | Mensajes claros de límites de uso | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-DOM-001 | DOM | Glosario F3 | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-DOM-002 | DOM | Context map F3 | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-DOM-003 | DOM | Modelo de documentos de búsqueda | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-DOM-004 | DOM | Proyección de access por mascota | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-DOM-005 | DOM | Modelo del catálogo de medicamentos y sugerencias | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-DOM-006 | DOM | MedicationItem v2 y upcaster PlanSet | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-DOM-007 | DOM | Paciente provisional: registro, reclamación, fusión | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DOM-008 | DOM | Políticas F3: indexación, access, invalidación, anomalías | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-DOM-009 | DOM | Cohortes como consultas parametrizadas | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DOM-010 | DOM | DirectoryListing en Organization | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DOM-011 | DOM | PushSubscription y canal push | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-ARQ-001 | ARQ | Pipeline de indexación idempotente (catch-up → asynq → bulk) | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-ARQ-002 | ARQ | Servicio de consulta con accessFilter y post-filtro | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-ARQ-003 | ARQ | Reindex sin downtime por alias | MUST | M3.4 | PENDIENTE | | | |
| PP-F3-ARQ-004 | ARQ | Degradación a PostgreSQL con circuit breaker | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-ARQ-005 | ARQ | Cliente Redis, bases, timeouts, fail-open | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-006 | ARQ | Caché compartida e invalidación por eventos | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-007 | ARQ | Rate limiting distribuido | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-008 | ARQ | asynq: colas, dedup, DLQ, migración de jobs F2 | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-009 | ARQ | Leases de worker por runner | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-010 | ARQ | Particionado intra-proyección | COULD | M3.3 | PENDIENTE | | | |
| PP-F3-ARQ-011 | ARQ | Lectura tolerante a huecos (condicional a PRF-009) | SHOULD | M3.3 | PENDIENTE | | | |
| PP-F3-ARQ-012 | ARQ | api con N réplicas y rolling deploy | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-013 | ARQ | Helm chart y valores compartidos con compose | SHOULD | M3.0 | PENDIENTE | | | |
| PP-F3-ARQ-014 | ARQ | ClamAV en adjuntos | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-ARQ-015 | ARQ | Canal Web Push | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-ARQ-016 | ARQ | CDN para estáticos y media | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-API-001 | API | Endpoints de búsqueda | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-API-002 | API | Endpoints de autocompletado | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-API-003 | API | Endpoints del catálogo de medicamentos | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-API-004 | API | Endpoints de cohortes | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-API-005 | API | Endpoints del directorio | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-API-006 | API | Endpoints del panel de plataforma | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-API-007 | API | Endpoints de pacientes provisionales | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-API-008 | API | Endpoints de push | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-API-009 | API | Administración de búsqueda, caché y workers | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-API-010 | API | Límites y degradación en respuestas | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-API-011 | API | Catálogo de errores F3 | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-DAT-001 | DAT | Índices, mappings, analizadores y sinónimos OpenSearch | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-DAT-002 | DAT | DDL del catálogo de medicamentos | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-DAT-003 | DAT | DDL leases, index_queue, reindex_jobs | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-DAT-004 | DAT | Esquema de claves Redis | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-DAT-005 | DAT | DDL pacientes provisionales, claims, aliases | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DAT-006 | DAT | DDL push subscriptions y preferencias por canal | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DAT-007 | DAT | Revisión de es.events, particionado e índices | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-DAT-008 | DAT | Vistas materializadas de plataforma | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-DAT-009 | DAT | Snapshots OpenSearch y persistencia Redis | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-FE-001 | FE | Búsqueda global clínica | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-FE-002 | FE | Búsqueda del tutor | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-FE-003 | FE | Medication picker en el editor | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-FE-004 | FE | Administración del catálogo y sugerencias | MUST | M3.2 | PENDIENTE | | | |
| PP-F3-FE-005 | FE | Cohortes y campañas | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-FE-006 | FE | Panel de plataforma | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-FE-007 | FE | Directorio público y ajustes de listado | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-FE-008 | FE | PWA y push | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-FE-009 | FE | Pacientes provisionales y reclamación | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-FE-010 | FE | Rendimiento del cliente: presupuesto, splitting, RUM | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-FE-011 | FE | Degradación y frescura en búsqueda | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-PRF-001 | PRF | SLOs y error budgets | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-PRF-002 | PRF | Generador de datos sintéticos a escala | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-PRF-003 | PRF | Escenarios k6 y ejecución nightly | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-004 | PRF | Optimización PostgreSQL | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-005 | PRF | pgbouncer | COULD | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-006 | PRF | HTTP: compresión, ETag, timeouts | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-007 | PRF | Presupuesto front y Lighthouse CI | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-008 | PRF | Plan de capacidad y reglas de escalado | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-PRF-009 | PRF | Medición de contención del Event Store y decisión | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-SEC-001 | SEC | Test de fuga de autorización en búsqueda | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-SEC-002 | SEC | Seguridad OpenSearch | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-SEC-003 | SEC | Seguridad Redis | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-SEC-004 | SEC | Abuso, scraping y límites | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-SEC-005 | SEC | Antivirus y cuarentena | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-SEC-006 | SEC | Detección de accesos anómalos | MUST | M3.4 | PENDIENTE | | | |
| PP-F3-SEC-007 | SEC | Seguridad de push | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-SEC-008 | SEC | Directorio público: datos mínimos | SHOULD | M3.4 | PENDIENTE | | | |
| PP-F3-SEC-009 | SEC | Threat model F3 | MUST | M3.4 | PENDIENTE | | | |
| PP-F3-INF-001 | INF | Compose con Redis, OpenSearch, ClamAV, asynqmon | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-INF-002 | INF | Redis y OpenSearch gestionados; red privada | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-INF-003 | INF | Dashboards Search, Redis, Jobs, Workers, SLOs, RUM | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-INF-004 | INF | Alertas F3 | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-INF-005 | INF | Rolling deploy multi-réplica; Helm en CI | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-INF-006 | INF | Runbook F3 | MUST | M3.5 | PENDIENTE | | | |
| PP-F3-INF-007 | INF | Configuración de CDN | MUST | M3.3 | PENDIENTE | | | |
| PP-F3-QA-001 | QA | Pruebas de indexación y access | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-QA-002 | QA | Pruebas de relevancia (golden queries) | MUST | M3.1 | PENDIENTE | | | |
| PP-F3-QA-003 | QA | Game day y scripts de caos | MUST | M3.5 | PENDIENTE | | | |
| PP-F3-QA-004 | QA | Pruebas de leases y failover | MUST | M3.0 | PENDIENTE | | | |
| PP-F3-QA-005 | QA | E2E F3 | MUST | M3.5 | PENDIENTE | | | |
| PP-F3-QA-006 | QA | Gates F3 | MUST | M3.0 | PENDIENTE | | | |
