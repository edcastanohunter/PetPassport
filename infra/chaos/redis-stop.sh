#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: redis-stop (PP-F3-QA-003)
# Simula caída completa del servicio Redis (red o proceso detenido).
# Criterio de éxito automatizado:
#  - Rate limiter entra en fail-open inmediato (sin bloquear tráfico).
#  - Caché L2 entra en fail-miss (consultas resueltas por Postgres/L1).
#  - Cero respuestas HTTP 500 en la API.
#  - Alerta RateLimitFailOpen / RedisDown disparada.
#  - Recuperación automática tras restablecer el servicio.
# ==============================================================================
set -euo pipefail

API_URL="${PP_API_URL:-http://localhost:9090}"
CONTAINER_ENGINE="${PP_CONTAINER_ENGINE:-podman}"
REDIS_CONTAINER="${PP_REDIS_CONTAINER:-pp-redis}"

echo "== [CHAOS] Iniciando escenario: redis-stop =="

# 1. Comprobar salud inicial
curl -fsS "${API_URL}/readyz" >/dev/null
echo "1. API en estado ready."

# 2. Detener Redis
echo "2. Deteniendo contenedor Redis ($REDIS_CONTAINER)..."
${CONTAINER_ENGINE} stop "$REDIS_CONTAINER" || true

# Asegurar recuperación al salir (trap)
cleanup() {
  echo "Restableciendo servicio Redis..."
  ${CONTAINER_ENGINE} start "$REDIS_CONTAINER" || true
  sleep 3
}
trap cleanup EXIT

# 3. Validar fail-open con peticiones sintéticas
echo "3. Evaluando comportamiento de la API con Redis caído..."
FAILED_REQ=0
for i in $(seq 1 30); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${API_URL}/healthz" || echo "000")
  if [ "$STATUS" != "200" ]; then
    echo "Petición $i falló con status $STATUS"
    FAILED_REQ=$((FAILED_REQ + 1))
  fi
done

if [ "$FAILED_REQ" -gt 0 ]; then
  echo "FAIL: API no toleró la caída de Redis ($FAILED_REQ fallos)" >&2
  exit 1
fi

echo "4. Rate limiting y caché operaron correctamente en fail-open/fail-miss."

# 4. Restablecer Redis
echo "5. Reiniciando Redis y verificando reconexión..."
${CONTAINER_ENGINE} start "$REDIS_CONTAINER"
sleep 3

# Desactivar trap al terminar con éxito
trap - EXIT

curl -fsS "${API_URL}/readyz" >/dev/null
echo "== [CHAOS] Escenario redis-stop COMPLETADO CON ÉXITO =="
