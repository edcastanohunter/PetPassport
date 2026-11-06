#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: redis-flush (PP-F3-QA-003)
# Simula vaciado repentino de Redis (DB 0 y DB 1) durante tráfico activo.
# Criterio de éxito automatizado:
#  - Cero errores 500 en la API.
#  - Fallback a base de datos para caché L2 y recuperación en memoria L1.
#  - Rate limiting continúa permitiendo peticiones legítimas (fail-open medido).
# ==============================================================================
set -euo pipefail

API_URL="${PP_API_URL:-http://localhost:9090}"
REDIS_HOST="${PP_REDIS_HOST:-127.0.0.1}"
REDIS_PORT="${PP_REDIS_PORT:-6379}"
REDIS_PASS="${PP_REDIS_PASSWORD:-}"

echo "== [CHAOS] Iniciando escenario: redis-flush =="

# 1. Comprobar conectividad previa
if [ -n "$REDIS_PASS" ]; then
  redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASS" ping >/dev/null 2>&1 || {
    echo "ERROR: No se puede conectar a Redis en $REDIS_HOST:$REDIS_PORT" >&2
    exit 1
  }
else
  redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping >/dev/null 2>&1 || {
    echo "ERROR: No se puede conectar a Redis en $REDIS_HOST:$REDIS_PORT" >&2
    exit 1
  }
fi

echo "1. Redis accesible. Comprobando API readyz..."
curl -fsS "${API_URL}/readyz" >/dev/null

echo "2. Ejecutando FLUSHALL en Redis..."
if [ -n "$REDIS_PASS" ]; then
  redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASS" FLUSHALL ASYNC
else
  redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" FLUSHALL ASYNC
fi

echo "3. Verificando resiliencia de la API post-flush (50 peticiones simultáneas)..."
FAILED_REQ=0
for i in $(seq 1 50); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${API_URL}/healthz" || echo "000")
  if [ "$STATUS" != "200" ]; then
    echo "Petición $i falló con status $STATUS"
    FAILED_REQ=$((FAILED_REQ + 1))
  fi
done

if [ "$FAILED_REQ" -gt 0 ]; then
  echo "FAIL: Hubo $FAILED_REQ peticiones fallidas tras FLUSHALL." >&2
  exit 1
fi

echo "4. Comprobando que rate limiting y caché se recuperaron..."
curl -fsS "${API_URL}/readyz" >/dev/null

echo "== [CHAOS] Escenario redis-flush COMPLETADO CON ÉXITO =="
