# ADR-014: Redis para caché, rate limiting y colas de trabajos no ordenados; PostgreSQL sigue siendo la fuente durable de outbox, recordatorios y jobs

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F3

## Contexto

F1 cachea membresías en memoria por proceso; F2 ejecuta todo el trabajo asíncrono con *polling* sobre
PostgreSQL (`SKIP LOCKED`) en un único worker. Al escalar `api` y `worker` a varias réplicas hacen falta
caché compartida, rate limiting distribuido y un pool de trabajos paralelos. Al mismo tiempo, una cola en
Redis **no preserva el orden por stream** ni ofrece la durabilidad de PostgreSQL.

## Decisión

Usos **permitidos** de Redis:

| Uso | Mecanismo | Durabilidad requerida |
|---|---|---|
| Caché compartida (membresías, resúmenes de mascota, perfil de organización, disponibilidad, catálogos) | cache-aside, TTL corto + invalidación por eventos (PM `cache-invalidator` publica en `PUBLISH cache:invalidate`) | ninguna (pérdida = *miss*) |
| Rate limiting distribuido por principal/IP/ruta | ventana deslizante (script Lua o `redis_rate`), cabeceras `RateLimit-*` | ninguna |
| Trabajos **no ordenados** y paralelizables | `asynq`: renderizado de PDF, procesamiento de imágenes, envío de email, indexación en OpenSearch por lote, escaneo antivirus, export | reintentos y dead-letter de asynq; el origen siempre es una fila en PostgreSQL que puede re-encolar |
| Sesiones efímeras de enlaces compartidos (PIN desbloqueado), tokens de un solo uso de corta vida | `SET … EX` | ninguna |
| Locks cortos (p. ej. evitar renderizar el mismo PDF dos veces) | `SET NX PX` | ninguna |

Usos **prohibidos**: fuente de verdad de cualquier dato; cola para proyecciones (rompe el orden por stream);
almacenamiento de `scheduled`/`command_jobs`/outbox (viven en PostgreSQL); sesiones de usuario (no hay).

Regla de oro: **si Redis se vacía, el sistema sigue siendo correcto** (más lento o con retrasos). Esto se
prueba en CI (test de integración con `FLUSHALL` a mitad de un escenario) y en staging (*game day*).

## Consecuencias

- Las proyecciones ordenadas siguen en el catch-up de PostgreSQL (ADR-009); solo su **salida** hacia
  sistemas externos (OpenSearch bulk) puede pasar por asynq con idempotencia por `(índice, doc_id, versión)`.
- Redis gestionado con persistencia AOF `everysec` (para no perder trabajos de asynq en reinicios), un solo
  nodo + réplica en prod; `maxmemory-policy allkeys-lru` para la base de caché y `noeviction` para la de
  colas → **dos bases lógicas** (`db 0` caché, `db 1` asynq) o dos instancias.
- El pool de workers asynq corre dentro de `cmd/worker` (mismo binario), con colas `critical`, `default`, `low`.

## Alternativas consideradas

- **River / colas sobre PostgreSQL**: válido y más simple; se descarta porque el stack pedido incluye Redis
  y porque el rate limiting y la caché ya lo justifican.
- **Kafka/NATS para el bus de eventos**: innecesario en F3; reconsiderar en F6 al extraer servicios.
