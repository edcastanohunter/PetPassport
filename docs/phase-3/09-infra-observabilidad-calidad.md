# F3 · 09 — Infraestructura, observabilidad y calidad

## 1. Infraestructura

**[PP-F3-INF-001] (MUST)** Compose de desarrollo: `redis:7` (dos instancias o dos `db`), `opensearchproject/opensearch:2.x` (single-node, `DISABLE_SECURITY_PLUGIN=false` con usuario de desarrollo), `opensearch-dashboards` (perfil `search`), `clamav/clamav` (perfil `security`), `asynqmon` (perfil `jobs`); `make dev-up` con perfiles por defecto `search`; semillas de sinónimos y mappings aplicadas por `cmd/worker search init`.

**[PP-F3-INF-002] (MUST)** Producción/staging: Redis gestionado (1 GB, réplica, AOF) y OpenSearch gestionado (2 nodos pequeños, 1 réplica por índice) en prod; en staging contenedores en la VM (o gestionados si el presupuesto lo permite). Dimensionado inicial documentado en `capacity.md` (PRF-008). Red privada entre VM y servicios gestionados; sin IPs públicas.

**[PP-F3-INF-003] (MUST)** Observabilidad: dashboards "Search" (lag por índice, latencia de consultas, tasa degradada, post-filtro), "Redis" (memoria, hit ratio por familia, latencia, evictions), "Jobs" (asynq: colas, procesados, fallidos, latencia por tipo), "Workers" (leases por holder, takeovers), "SLOs" (burn rate por SLO con ventanas 1 h/6 h/3 d), "RUM" (Web Vitals p75 por ruta). Exportadores: `redis_exporter`, `opensearch` Prometheus plugin o exporter, métricas asynq nativas.

**[PP-F3-INF-004] (MUST)** Alertas F3: `SearchIndexLagHigh` (> 30 s 5 min), `SearchDegraded` (> 5% requests 5 min), `SearchPostfilterDrops` (> 0 10 min), `RedisMemoryHigh` (> 80%), `RedisDown` (fallback activo > 2 min), `RateLimitFailOpen` (> 0), `AsynqQueueBacklog` (`default` > 5.000 o `critical` > 500 por 5 min), `AsynqArchived` (> 10/h), `LeaseTakeoverStorm` (> 5 en 5 min), `SLOBurnRateHigh` (burn rate 14× en 1 h o 6× en 6 h), `ClamAVDefinitionsStale` (> 48 h), `MappingDrift`.

**[PP-F3-INF-005] (MUST)** Despliegue multi-réplica: `deploy.sh` *rolling* por réplica con `readyz` y drenado; `compose render` desde `infra/values/<env>.yaml`; Helm chart con `helm lint`, `kubeconform` y despliegue en `kind` + smoke en CI (`helm.yml`); imágenes idénticas en ambos.

**[PP-F3-INF-006] (MUST)** Runbook F3: reindex sin downtime (paso a paso, verificación de alias, rollback de alias), OpenSearch caído (confirmar degradación, restaurar snapshot vs rebuild), Redis caído/vaciado (comprobar fail-open y backlog de productores), leases huérfanos (`cmd/worker leases --release`), DLQ de asynq (inspección en asynqmon, reintento, descarte con motivo), cuarentena antivirus, rotación de claves VAPID, purga de CDN.

**[PP-F3-INF-007] (MUST)** CDN: reglas de caché por ruta (estáticos `immutable`; `index.html` `no-cache`; `/api/*` *bypass*; media por URL firmada con `private`), purga en despliegue del front (solo `index.html`), cabeceras de seguridad preservadas (SEC-003 de F1), *origin shield* si el proveedor lo ofrece.

## 2. Calidad

**[PP-F3-QA-001] (MUST)** Pruebas de indexación: idempotencia (`external_gte`: reindexar versión antigua no sobrescribe), rebuild de cada índice desde eventos con comparación de conteos y muestra de documentos contra read models, recálculo de `access` tras `CareLinkRevoked` y `OrganizationSuspended` (documentos de todas las mascotas afectadas), borrado por fusión.

**[PP-F3-QA-002] (MUST)** Relevancia: `backend/test/search/golden/*.yaml` con ≥ 60 consultas (es/en, typos, sinónimos, acentos, códigos) y los resultados esperados en el top-3 sobre el dataset sintético; métricas MRR y recall@10 con umbral (MRR ≥ 0,8); se ejecutan antes de cada cambio de alias y en CI (dataset reducido).

**[PP-F3-QA-003] (MUST)** Game day (scripts en `infra/chaos/`): `redis-flush`, `redis-stop`, `opensearch-stop`, `worker-kill-one`, `api-kill-one`, `pg-latency` (tc netem 50 ms); cada uno con criterio de éxito automatizado (k6 ligero en paralelo + comprobación de alertas disparadas y recuperación). Ejecutado en staging antes de M3.5 y trimestralmente.

**[PP-F3-QA-004] (MUST)** Leases y failover: test de integración con 3 workers simulados: reparto equilibrado, muerte de uno → retoma en < 30 s sin duplicar lotes (checkpoint atómico), sin dos holders simultáneos (aserción sobre `worker_leases`).

**[PP-F3-QA-005] (MUST)** E2E F3: búsqueda de paciente con typo → encuentra; búsqueda clínica con sinónimo → resalta; picker de medicamentos → `catalog_id` guardado y visible; sugerencia de catálogo → aprobación admin → disponible; cohorte "vacunas vencidas" → campaña → email en Mailpit; directorio → vincular; paciente provisional → reclamación con fusión; push opt-in (Playwright con permisos) → notificación recibida (service worker en modo test).

**[PP-F3-QA-006] (MUST)** Gates F3: test de fuga (SEC-001) obligatorio; relevancia MRR ≥ 0,8; k6 nightly dentro de SLO 5 días consecutivos antes de cerrar M3.3; Lighthouse CI ≥ 80 móvil; `helm lint`/kind verde; `FLUSHALL` a mitad de escenario de integración sin fallos; cobertura `internal/search` ≥ 85%.

## 3. Costes orientativos F3 (adicionales)

| Partida | Rango mensual |
|---|---|
| Redis gestionado (1 GB + réplica) | 15–40 USD |
| OpenSearch gestionado (2 × t3.small.search o equivalente) | 60–120 USD |
| CDN/WAF (tráfico media) | 10–30 USD |
| VM staging ampliada (8 GB → 16 GB por OpenSearch local) | +20–40 USD |
