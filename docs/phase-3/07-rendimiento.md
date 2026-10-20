# F3 · 07 — Rendimiento: SLOs, pruebas de carga y optimización

Área `PRF`. Principio: **medir antes, medir después**; ninguna optimización se mergea sin antes/después en el PR.

## 1. SLOs

**[PP-F3-PRF-001] (MUST)** Definidos sobre staging con datos a escala (PRF-002) y, desde el piloto, sobre producción. Ventana de error budget: 30 días.

| SLI | Objetivo | Medición |
|---|---|---|
| Disponibilidad API (`/api/v1/*`, respuestas no-5xx) | 99,9% | Traefik + métricas app |
| Latencia API lectura (`GET` no búsqueda) p95 / p99 | < 250 ms / < 600 ms | `http_server_request_duration_seconds` |
| Latencia API comandos (POST/PUT/PATCH/DELETE) p95 | < 400 ms | ídem |
| Latencia búsqueda (`/search/*`) p95 | < 300 ms | ídem, por ruta |
| Latencia autocompletado (`/suggest/*`) p95 | < 150 ms | ídem |
| Retraso de proyecciones asíncronas p95 | < 2 s | `projection_lag_seconds` |
| Retraso de indexación p95 | < 5 s | `search_index_lag_seconds` |
| Retraso de recordatorios p95 | < 60 s | `reminder_dispatch_delay_seconds` |
| Entrega de notificaciones (no `failed`) | 99% | `notifications_deliveries_total` |
| Lock de `Append` p95 | < 20 ms | `eventstore_append_lock_wait_seconds` |
| Front: LCP / INP (p75 móvil, RUM) | < 2,5 s / < 200 ms | Web Vitals |

Error budget y política: si el presupuesto mensual de un SLO se consume > 50% antes de día 15, la siguiente iteración prioriza fiabilidad sobre funcionalidad (regla de equipo, registrada en tracking como nota).

## 2. Datos sintéticos a escala

**[PP-F3-PRF-002] (MUST)** `cmd/worker synth --profile pilot|scale` genera datos **a través del command bus** (eventos reales): `scale` = 200 clínicas, 1.500 profesionales, 50.000 tutores, 80.000 mascotas, 400.000 encuentros firmados, 300.000 entradas de carnet, 150.000 citas, ≈ 2 M eventos; distribución realista (Zipf en actividad por clínica, estacionalidad de vacunas). Determinista por semilla; tiempo objetivo < 2 h en staging; textos clínicos generados desde plantillas con vocabulario veterinario (para relevancia).

## 3. Pruebas de carga (k6)

**[PP-F3-PRF-003] (MUST)** `backend/test/load/` con escenarios:

| Escenario | Perfil | Carga |
|---|---|---|
| `owner_browse` | tutor: `/me`, lista mascotas, ficha, timeline, carnet | 60% del tráfico |
| `clinic_day` | clínica: agenda, búsqueda de pacientes, abrir/editar/firmar encuentro, registrar vacuna | 30% |
| `search_heavy` | búsquedas clínicas y suggest con términos reales | 10% |
| `spike` | ráfaga ×5 durante 2 min | prueba de resiliencia |
| `soak` | 2 h a carga nominal | fugas/degradación |

- Carga nominal objetivo F3: 100 req/s sostenidos (≈ 300 usuarios concurrentes), 20 comandos/s, con `api×2`, `worker×2` en la VM de staging.
- Umbrales k6 = SLOs; ejecución **nightly** en staging (allowlist de rate limit) y manual en PR con etiqueta `perf`; resultados publicados en Grafana (k6 → Prometheus remote write) y resumidos en el PR.

## 4. PostgreSQL

**[PP-F3-PRF-004] (MUST)**

- `pg_stat_statements` + `auto_explain` (`log_min_duration 200ms`, `analyze`) en staging; informe quincenal de top-20 consultas; cada consulta > 100 ms p95 tiene issue.
- Revisión de índices: cubrir consultas de listados por cursor (`(guardian_id, status, created_at DESC, id)`), `encounters_org_status_idx`, `care_links_org_active_idx`; eliminar no usados; `fillfactor` 90 en tablas con muchas actualizaciones (`read_scheduling.appointments`, `projection_checkpoints`).
- N+1: lint de consultas en handlers (`sqlc` + revisión) y trazas con `db.statement_count` por request (alerta > 20).
- Pool: `PP_DB_MAX_CONNS` = 20 por réplica `api`, 10 por `worker`; `max_connections` dimensionado; `idle_in_transaction_session_timeout 10s`; `statement_timeout` 5 s (API) / 60 s (worker lotes).
- Vacuum: `autovacuum_vacuum_scale_factor 0.05` en `es.events` (append-only: casi no hay) y en read models calientes; monitor de bloat.
- `work_mem` 16 MB, `shared_buffers` 25% RAM, `effective_cache_size` 75%, `random_page_cost 1.1` (SSD) en la instancia gestionada.

**[PP-F3-PRF-005] (COULD)** `pgbouncer` en modo transacción si `réplicas × conns` supera el 80% de `max_connections`; verificar compatibilidad: advisory locks `xact` (ok), `LISTEN/NOTIFY` (no: solo desde el worker con conexión directa), `SET` de sesión (prohibido, usar `SET LOCAL`).

## 5. HTTP y red

**[PP-F3-PRF-006] (MUST)** Compresión `br`/`gzip` en Traefik para JSON > 1 KB; `ETag`/`304` ya existentes aplicados a todos los `GET` de recurso; HTTP/2 en el borde; `keep-alive` entre Traefik y `api`; timeouts revisados (lectura 30 s, búsqueda 2 s); respuestas de listado con campos mínimos (`PetSummary` vs `Pet`); `JSON` con `encoding/json` v2 o `sonic` evaluado (sólo si aparece en perfiles).

## 6. Frontend

**[PP-F3-PRF-007] (MUST)** Presupuesto y RUM según FE-010; Lighthouse CI en staging bloqueante si Performance < 80 en móvil.

## 7. Capacidad y límites

**[PP-F3-PRF-008] (MUST)** Documento `docs/phase-3/capacity.md` (generado en M3.3) con: consumo CPU/RAM por réplica a carga nominal, conexiones PG, memoria Redis, tamaño de índices OpenSearch por 10k mascotas, crecimiento mensual estimado de `es.events`, y el punto de saturación medido en `spike` (req/s donde se rompe el SLO). Reglas de escalado: añadir réplica `api` si CPU > 70% 10 min; `worker` si `projection_lag` p95 > 1 s 10 min.

## 8. Contención del Event Store

**[PP-F3-PRF-009] (MUST)** Medir `eventstore_append_lock_wait_seconds` durante `clinic_day` y `spike`. Si p95 > 20 ms sostenido → activar ARQ-011 (lectura tolerante a huecos) y retirar el advisory lock; si no → registrar la medida y marcar ARQ-011 `DESCARTADO` con la cifra en la nota. Además: perfiles (`pprof`) de `Append` y de proyecciones síncronas; objetivo de latencia de comando con 3 proyectores síncronos < 15 ms p95 en BD.

## 9. Perfilado continuo

`pprof` expuesto en el puerto interno; perfiles de CPU/heap capturados automáticamente durante k6 (`soak`) y adjuntados al informe; *flame graphs* en el PR de cada optimización. Objetivo de asignaciones por request en rutas calientes documentado (`allocs/op` en benchmarks Go de `authz`, `eventstore.Load`, serialización).
