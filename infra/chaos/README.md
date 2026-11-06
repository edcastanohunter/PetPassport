# Scripts de Caos y Game Day — Fase 3 (PP-F3-QA-003)

Esta carpeta contiene la suite de inyección de caos y verificación de resiliencia automatizada para PetPassport Fase 3 (*Search & Performance*).

## Escenarios incluidos

| Script | Escenario de Caos | Criterio de Éxito Automatizado |
|---|---|---|
| `redis-flush.sh` | Vaciado total de Redis en caliente (`FLUSHALL ASYNC`) | Cero errores 500; caché L2 degrada a fail-miss y consulta Postgres/L1; rate limit continúa permitiendo tráfico. |
| `redis-stop.sh` | Detención completa del servicio Redis | Rate limit entra en fail-open inmediato; caché entra en fail-miss; recuperación automática tras reinicio. |
| `opensearch-stop.sh` | Detención del cluster OpenSearch | Búsqueda de pacientes e historiales degrada a PostgreSQL (`X-PP-Search-Mode: degraded`); circuit breaker abre y cierra automáticamente. |
| `worker-kill-one.sh` | Muerte violenta (`SIGKILL`) de una réplica worker | Retoma de leases por réplica superviviente en < 30 s; cero doble holder (sin split-brain); proyecciones sin eventos duplicados. |
| `api-kill-one.sh` | Muerte violenta de una réplica de API | Traefik/Gateway enruta el 100% del tráfico a la réplica sana; cero errores 502/503 percibidos por el cliente. |
| `pg-latency.sh` | Inyección de 50 ms de latencia en PostgreSQL (`tc netem`) | Consultas y comandos se completan dentro de los timeouts configurados (2s/5s) sin saturación de conexiones. |
| `run-all.sh` | Orquestador de la suite completa | Ejecuta secuencialmente los 6 escenarios y genera informe consolidado de ejecución. |

## Variables de entorno

| Variable | Descripción | Valor por defecto |
|---|---|---|
| `PP_API_URL` | URL base de la API para sondas de salud | `http://localhost:9090` |
| `PP_GATEWAY_URL` | URL pública del API Gateway / Traefik | `https://app.pp.localhost` |
| `PP_CONTAINER_ENGINE` | Motor de contenedores (`podman` o `docker`) | `podman` |
| `PP_REDIS_HOST` | Host de Redis | `127.0.0.1` |
| `PP_REDIS_PORT` | Puerto de Redis | `6379` |
| `PP_REDIS_CONTAINER` | Nombre del contenedor Redis | `pp-redis` |
| `PP_OPENSEARCH_CONTAINER` | Nombre del contenedor OpenSearch | `pp-opensearch` |
| `PP_WORKER_TARGET` | Nombre del contenedor worker a terminar | `pp-worker-1` |
| `PP_API_TARGET` | Nombre del contenedor API a terminar | `pp-api-1` |
| `PP_POSTGRES_CONTAINER` | Nombre del contenedor PostgreSQL | `pp-postgres` |

## Modo de ejecución

```bash
# Ejecutar un escenario individual
bash infra/chaos/redis-flush.sh

# Ejecutar la suite completa de Game Day
bash infra/chaos/run-all.sh
```
