# Prompts de ejecución — Fase 3 (Search & Performance)

Prerrequisito: Fase 2 cerrada. System prompt: `00-agent-operating-instructions.md`.

---

## M3.0 — Plataforma (Redis, OpenSearch, réplicas, leases)

```text
Hito: F3 M3.0 Plataforma. Rama: feat/f3-m3.0-platform desde main. Cimiento de la fase: máxima atención.

Lee: ADR-013 §1–2, ADR-014 (completo), ADR-015 (completo); docs/phase-3/01 (PRD-014), 02 (§1–2), 03 (§6–12, §13 si hay
capacidad), 04 §10, 05 (§3 DAT-003, §3 DAT-004 Redis, DAT-009), 07 (PRF-001, PRF-002), 08 (SEC-002, SEC-003), 09 (INF-001,
INF-002, INF-005, QA-004, QA-006).

IDs: PRD-014, DOM-001, DOM-002, ARQ-005, ARQ-006, ARQ-007, ARQ-008, ARQ-009, ARQ-012, ARQ-013, API-010, DAT-003,
DAT-004, DAT-009, PRF-001, PRF-002, SEC-002, SEC-003, INF-001, INF-002, INF-005, QA-004, QA-006 (prefijo PP-F3-).

Entregables, en orden:
1. Redis (ARQ-005): cliente, dos bases, timeouts, circuit breaker con fail-open medido; esquema de claves (DAT-004).
   Caché compartida con invalidación por eventos y L1 (ARQ-006): membresías y petaccess migran de la LRU en memoria.
   Test de integración "FLUSHALL a mitad de escenario no rompe nada" (QA-006).
2. Rate limiting distribuido (ARQ-007) con cabeceras IETF y mensajes claros (PRD-014, API-010).
3. asynq (ARQ-008): colas, dedup, DLQ; migración de los runners de F2 (notifications-dispatch, documents, media) a
   productores PG + handlers asynq; proyecciones, outbox y scheduler siguen en PG.
4. Leases por runner (ARQ-009, DAT-003) con prueba de failover QA-004 (3 workers, muerte de uno, sin doble holder).
5. api × 2 y worker × 2 en compose con rolling deploy (ARQ-012, INF-005); Helm chart + kind en CI (ARQ-013, SHOULD).
6. OpenSearch y Redis en compose (INF-001) y gestionados en prod con red privada (INF-002); seguridad OpenSearch/Redis
   (SEC-002/003); snapshots y AOF (DAT-009).
7. SLOs definidos (PRF-001) y generador de datos sintéticos a escala vía command bus (PRF-002); gates F3 en CI (QA-006).

DoD: dos réplicas de cada proceso operando; rate limit y caché verificados; failover de leases probado; FLUSHALL test
verde; tracking actualizado.
```

---

## M3.1 — Indexación y búsqueda clínica

```text
Hito: F3 M3.1 Búsqueda. Rama: feat/f3-m3.1-search desde main. Puede ir en paralelo con M3.2.

Lee: ADR-013 (completo); docs/phase-3/01 (E15), 02 (§3.1–3.2, §8), 03 (§2–5), 04 (§1–2, §9, §11), 05 §1, 06 (FE-001, FE-002,
FE-011), 08 (SEC-001, SEC-004), 09 (QA-001, QA-002).

IDs: PRD-001, PRD-002, PRD-003, PRD-010, DOM-003, DOM-004, DOM-008, ARQ-001, ARQ-002, ARQ-004, API-001, API-002, API-009,
API-011, DAT-001, FE-001, FE-002, FE-011, SEC-001, SEC-004, QA-001, QA-002.

Entregables:
1. Mappings versionados (DAT-001) con analizadores español/ICU, sinónimos veterinarios (archivo revisable) y edge_ngram;
   aliases de lectura/escritura; `cmd/worker search init`.
2. Indexers pets y clinical (ARQ-001): catch-up → index_queue → asynq bulk con external_gte; proyección de access
   (DOM-004) con update_by_query por mascota y por organización.
3. Query service (ARQ-002): accessFilter único, post-filtro PetAccess, facetas, highlight como texto plano con marcadores,
   search_after; suggest. Lint: solo internal/search/app importa el cliente OpenSearch.
4. Degradación a PostgreSQL con circuit breaker y cabecera X-PP-Search-Mode (ARQ-004).
5. Endpoints API-001/002/009 (admin índices/reindex/purge) y errores API-011.
6. Test de fuga de autorización (SEC-001) como gate; abuso/scraping (SEC-004); pruebas de indexación y rebuild (QA-001);
   golden queries con MRR ≥ 0,8 (QA-002).
7. Frontend: búsqueda global clínica (FE-001), búsqueda del tutor (FE-002), degradación/frescura (FE-011).

DoD: búsqueda con typo y sinónimo funcional; 0 fugas en el test; degradación demostrada apagando OpenSearch; tracking actualizado.
```

---

## M3.2 — Catálogo de medicamentos

```text
Hito: F3 M3.2 Catálogo. Rama: feat/f3-m3.2-medications desde main. Puede ir en paralelo con M3.1 (coordina el índice).

Lee: ADR-016; docs/phase-3/01 (E16), 02 (§4), 04 §3, 05 §2, 06 (FE-003, FE-004).

IDs: PRD-004, PRD-005, DOM-005, DOM-006, API-003, DAT-002, FE-003, FE-004.

Entregables:
1. ref.medications + sugerencias + lotes de importación (DAT-002) con CRUD auditado, fusión con redirección y
   `cmd/worker catalog-import` (fuente pública CO; documenta la fuente y el formato).
2. MedicationItem v2 + upcaster PlanSet v1→v2 con golden tests; validación de catalog_id activo en SetPlan.
3. Indexer medications (sobre ref.medications_changes) y suggest por especie con boost de usage_count.
4. Endpoints API-003 incluida métrica de uso.
5. Frontend: picker en el editor (FE-003), administración y sugerencias (FE-004).

DoD: autocompletar → guardar catalog_id → % con catalog_id visible en admin; upcaster probado; tracking actualizado.
```

---

## M3.3 — Rendimiento

```text
Hito: F3 M3.3 Rendimiento. Rama: feat/f3-m3.3-performance desde main. Regla: cada optimización con antes/después en el PR.

Lee: docs/phase-3/01 (PRD-009), 03 (§10 ARQ-010/011, §15 ARQ-016), 05 (DAT-007), 06 (FE-010), 07 (completo), 09 (INF-003,
INF-004, INF-007).

IDs: PRD-009, ARQ-010, ARQ-011, ARQ-016, DAT-007, FE-010, PRF-003, PRF-004, PRF-005, PRF-006, PRF-007, PRF-008, PRF-009,
INF-003, INF-004, INF-007.

Entregables:
1. k6: escenarios owner_browse, clinic_day, search_heavy, spike, soak con umbrales = SLOs; nightly en staging con allowlist
   de rate limit; resultados a Grafana (PRF-003).
2. Medición de contención del advisory lock (PRF-009): decide ARQ-011 (implementar lectura tolerante a huecos) o
   DESCARTADO con la cifra en tracking. Particionado intra-proyección (ARQ-010) solo si una proyección no sigue el ritmo.
3. PostgreSQL (PRF-004): pg_stat_statements, auto_explain, índices, pool, timeouts, vacuum; informe en docs/phase-3/
   perf/; pgbouncer solo si se supera el 80 % de conexiones (PRF-005). Revisión de es.events y particionado (DAT-007).
4. HTTP (PRF-006) y CDN (ARQ-016, INF-007).
5. Frontend (FE-010, PRF-007): presupuesto de bundle en CI, splitting, imágenes, TanStack Query, RUM, Lighthouse CI ≥ 80.
6. capacity.md (PRF-008); dashboards SLO con burn rate y RUM (INF-003); alertas F3 (INF-004).

DoD: SLOs cumplidos en k6 sobre datos a escala 5 noches seguidas; capacity.md publicado; decisión sobre ARQ-011 registrada;
tracking actualizado.
```

---

## M3.4 — Pendientes y hardening

```text
Hito: F3 M3.4. Rama: feat/f3-m3.4-features-hardening desde main. Varias líneas independientes: puedes repartirlas
entre agentes (A: directorio+cohortes+panel; B: provisional+push; C: ClamAV+anomalías+threat model).

Lee: docs/phase-3/01 (E17, E19), 02 (§3.3–3.4, §5–7), 03 (§4 reindex ARQ-003, §13–14 ARQ-014/015), 04 (§4–8), 05 (DAT-005,
DAT-006, DAT-008), 06 (FE-005..FE-009), 08 (SEC-005..SEC-009).

IDs: PRD-006, PRD-007, PRD-008, PRD-011, PRD-012, PRD-013, DOM-007, DOM-009, DOM-010, DOM-011, ARQ-003, ARQ-014, ARQ-015,
API-004, API-005, API-006, API-007, API-008, DAT-005, DAT-006, DAT-008, FE-005, FE-006, FE-007, FE-008, FE-009, SEC-005,
SEC-006, SEC-007, SEC-008, SEC-009.

Entregables (por línea):
A. Cohortes y campañas (DOM-009, API-004, FE-005; exportación auditada ≤ 1.000 filas); directorio opt-in con geo redondeada
   (DOM-010, API-005, FE-007, SEC-008); panel de plataforma con vistas materializadas (DAT-008, API-006, FE-006).
B. Paciente provisional y reclamación/fusión con pet_aliases y PM clinical.merge-pet (DOM-007, DAT-005, API-007, FE-009);
   Web Push con VAPID y PWA (DOM-011, ARQ-015, DAT-006, API-008, FE-008, SEC-007); importación CSV (PRD-013) solo si hay capacidad.
C. Reindex sin downtime por alias con mapping-check en despliegue (ARQ-003); ClamAV con cuarentena y backfill (ARQ-014,
   SEC-005); anomaly-detector horario con alertas (SEC-006); threat model F3 (SEC-009).

DoD: cada línea demostrable y probada; reindex de clinical sin degradar p95; tracking actualizado por línea.
```

---

## M3.5 — Release

```text
Hito: F3 M3.5 Release. Rama: feat/f3-m3.5-release desde main.

Lee: docs/phase-3/00 §5 (criterios de salida), 09 (INF-006, QA-003, QA-005).

IDs: INF-006, QA-003, QA-005.

Entregables:
1. Runbook F3 (INF-006). E2E F3 completo (QA-005).
2. Game day en staging con los scripts de infra/chaos/ (QA-003): redis-flush, redis-stop, opensearch-stop, worker-kill-one,
   api-kill-one, pg-latency; criterios automatizados; informe en docs/phase-3/gameday/.
3. Ejecuta los seis criterios de salida de 00 §5 y adjunta evidencia; tag v0.3.0 tras aprobación humana.

DoD: game day documentado con todas las alertas esperadas disparadas y recuperación verificada; tracking actualizado.
```
