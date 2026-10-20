# Fase 3 — Search & Performance (Read models avanzados)

> Estado: v1.0 en revisión · Fecha: 2026-10-20 · Requiere: Fase 2 cerrada
> Tracking: [`docs/tracking/phase-3.md`](../tracking/phase-3.md)

## 1. Objetivo

Que PetPassport sea **rápido y encontrable** con decenas de clínicas y miles de mascotas:

- **Búsqueda** en texto libre con relevancia y resaltado sobre historiales, pacientes, carnets y organizaciones;
  **catálogo de medicamentos** con autocompletado (base de F4).
- **Rendimiento con SLOs medidos**: API, búsqueda y retraso de proyecciones bajo objetivos verificados con k6.
- **Redis**: caché compartida, rate limiting distribuido, colas de trabajos paralelos (asynq).
- **Escalado horizontal**: `api` y `worker` con varias réplicas; Helm chart listo.
- Capacidades que F2 dejó para F3: ClamAV en adjuntos, lectura tolerante a huecos si procede, particionado del
  worker, detección de accesos anómalos, notificaciones push web, paciente provisional sin cuenta de tutor.

Al terminar F3, F4 construye la integración B2B sobre un sistema que ya soporta carga y tiene un catálogo de productos.

## 2. Documentos de la fase

| # | Documento | Área |
|---|---|---|
| 01 | [Producto](01-producto.md) | PRD |
| 02 | [Modelo DDD: Search & Reporting, Medication Catalog, cambios](02-modelo-ddd.md) | DOM |
| 03 | [Arquitectura: indexación, consultas, Redis, asynq, escalado](03-arquitectura.md) | ARQ |
| 04 | [API REST v1 (ampliación)](04-api.md) | API |
| 05 | [Datos: índices OpenSearch, PostgreSQL, Redis](05-datos.md) | DAT |
| 06 | [Frontend](06-frontend.md) | FE |
| 07 | [Rendimiento: SLOs, pruebas de carga, optimización](07-rendimiento.md) | PRF |
| 08 | [Seguridad](08-seguridad.md) | SEC |
| 09 | [Infraestructura, observabilidad y calidad](09-infra-observabilidad-calidad.md) | INF / QA |

Nueva área: `PRF` (rendimiento). IDs: `PP-F3-<ÁREA>-nnn`.

## 3. Alcance

### Dentro (IN)

- **Search & Reporting**: índices `pets`, `clinical`, `medications`, `organizations`; pipeline de indexación por suscripción catch-up + asynq; consultas con autorización denormalizada; autocompletado; facetas; resaltado; sinónimos veterinarios; reindex sin downtime; dashboard de plataforma (SuperAdmin) con agregaciones.
- **Medication Catalog**: `ref.medications`, importador, sugerencias de veterinarios, `MedicationItem` v2 (upcaster), *picker* en el editor de consulta.
- **Redis**: caché (membresías, resúmenes, organizaciones, disponibilidad, catálogos), rate limiting por principal/IP/ruta, asynq (PDF, imágenes, email, indexación, antivirus, export), sesiones de enlace, invalidación por eventos.
- **Escalado**: `api` ×N, `worker` ×N con leases por proyección; particionado intra-proyección diseñado (COULD); Helm chart + kind en CI; rolling deploys.
- **Rendimiento**: SLOs, k6 (perfiles tutor/clínica/búsqueda), optimización de consultas e índices PG, paginación por cursor en todos los listados, compresión, CDN para estáticos y fotos (URLs firmadas), `pgbouncer` (COULD), evaluación de particionado de `es.events`, lectura tolerante a huecos (SHOULD condicional).
- **Pendientes de F2**: ClamAV, accesos anómalos automatizados, Web Push, paciente provisional, importación CSV si no entró.

### Fuera (OUT) — y dónde cae

| Fuera de F3 | Fase |
|---|---|
| Recetas, OCR, cotización, integración con farmacias | F4 |
| Migración de producción a Kubernetes, DR multi-zona | F5 |
| SMS / WhatsApp | F5 |
| Interacciones medicamentosas, dosificación por especie | F6 |
| Analítica/BI de negocio avanzada | F5/F6 |
| Extracción de Search como servicio | F6 |

## 4. Hitos

| Hito | Nombre | Resultado observable |
|---|---|---|
| M3.0 | Plataforma | Redis y OpenSearch en dev/staging; `api`×2 y `worker`×2 con leases; membresías en Redis; rate limiting app; asynq operativo; Helm validado en kind. |
| M3.1 | Indexación y búsqueda clínica | Índices `pets` y `clinical` reconstruibles; búsqueda de pacientes e historiales con autorización; pruebas de fuga verdes. |
| M3.2 | Catálogo de medicamentos | `ref.medications` cargado (CO), índice `medications`, picker en consulta, `MedicationItem` v2. |
| M3.3 | Rendimiento | SLOs definidos y cumplidos en k6 sobre staging; optimizaciones aplicadas; CDN; dashboards de SLO con error budget. |
| M3.4 | Pendientes y hardening | ClamAV, Web Push, paciente provisional, accesos anómalos, reindex sin downtime probado. |
| M3.5 | Release | Game day (Redis vacío, worker caído, OpenSearch caído); E2E; `v0.3.0`. |

## 5. Criterios de salida

1. Todos los `MUST` en `VERIFICADO`.
2. SLOs (07-rendimiento §1) cumplidos en k6 sobre staging con datos sintéticos a escala (200 clínicas, 50k mascotas, 500k eventos).
3. Prueba de fuga de autorización en búsqueda: 0 documentos no autorizados en 100% de combinaciones rol × nivel.
4. Reindex completo de `clinical` sin downtime observable (p95 de búsqueda dentro de SLO durante el proceso).
5. Game day documentado: con Redis vacío el sistema sigue correcto; con OpenSearch caído la API degrada a búsqueda simple PG con aviso; con una réplica de worker caída, las proyecciones continúan en < 60 s.
6. `% de ítems de medicación con catalog_id` ≥ 70% en clínicas piloto a 4 semanas de M3.2.

## 6. Riesgos específicos

| Riesgo | Mitigación |
|---|---|
| Fuga de datos por filtros de autorización mal construidos en OpenSearch | Filtro obligatorio generado por una única función; test de fuga exhaustivo; post-filtro por `PetAccess`. |
| Doble verdad PG/OpenSearch | OpenSearch nunca es fuente; rebuild automatizado; comparación periódica de conteos. |
| Catálogo de medicamentos de baja calidad | Importación con revisión, sugerencias, métricas de uso, texto libre siempre permitido. |
| Redis como dependencia dura por accidente | Regla "FLUSHALL no rompe nada" probada en CI y game day. |
| Optimizar sin medir | SLOs y k6 antes de tocar código; cada optimización con antes/después en el PR. |
