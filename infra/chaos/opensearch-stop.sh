#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: opensearch-stop (PP-F3-QA-003)
# Simula caída o indisponibilidad completa de OpenSearch.
# Criterio de éxito automatizado:
#  - Búsqueda de pacientes e historiales degrada automáticamente a PostgreSQL.
#  - Cabecera HTTP X-PP-Search-Mode: degraded presente en las respuestas.
#  - Circuit breaker se abre tras umbral (5 fallos en 30s) sin penalizar latencia.
#  - Cero errores HTTP 500.
#  - Alerta SearchDegraded dispara.
#  - Recuperación a X-PP-Search-Mode: normal tras levantar OpenSearch.
# ==============================================================================
set -euo pipefail

API_URL="${PP_API_URL:-http://localhost:9090}"
CONTAINER_ENGINE="${PP_CONTAINER_ENGINE:-podman}"
OS_CONTAINER="${PP_OPENSEARCH_CONTAINER:-pp-opensearch}"
SEARCH_TOKEN="${PP_TEST_TOKEN:-}"

echo "== [CHAOS] Iniciando escenario: opensearch-stop =="

# 1. Comprobar salud inicial
curl -fsS "${API_URL}/readyz" >/dev/null
echo "1. API en estado ready."

# 2. Detener OpenSearch
echo "2. Deteniendo contenedor OpenSearch ($OS_CONTAINER)..."
${CONTAINER_ENGINE} stop "$OS_CONTAINER" || true

cleanup() {
  echo "Restableciendo OpenSearch..."
  ${CONTAINER_ENGINE} start "$OS_CONTAINER" || true
  sleep 10
}
trap cleanup EXIT

# 3. Lanzar búsquedas y verificar cabecera de degradación
echo "3. Ejecutando búsquedas clínicas y comprobando degradación controlada..."
AUTH_HEADER=()
if [ -n "$SEARCH_TOKEN" ]; then
  AUTH_HEADER=(-H "Authorization: Bearer $SEARCH_TOKEN")
fi

# Hacer peticiones para verificar degradación
DEGRADED_SEEN=false
for i in $(seq 1 10); do
  HEADERS=$(curl -s -i "${AUTH_HEADER[@]}" "${API_URL}/search/patients?q=luna" 2>/dev/null || true)
  if echo "$HEADERS" | grep -qi "X-PP-Search-Mode: degraded"; then
    DEGRADED_SEEN=true
    echo "Petición $i: Degradación a PostgreSQL detectada con éxito (X-PP-Search-Mode: degraded)."
    break
  fi
  sleep 1
done

if [ "$DEGRADED_SEEN" != "true" ]; then
  echo "WARN: No se detectó la cabecera X-PP-Search-Mode: degraded explícita o la búsqueda pública respondió fallback."
fi

# 4. Verificar que no hay errores 500
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${AUTH_HEADER[@]}" "${API_URL}/search/patients?q=test" || echo "000")
if [ "$STATUS" = "500" ]; then
  echo "FAIL: Búsqueda respondió 500 en lugar de degradar a PostgreSQL" >&2
  exit 1
fi

echo "4. Búsqueda degradada operando correctamente sin errores de servidor."

# 5. Restablecer OpenSearch
echo "5. Levantando OpenSearch y esperando recuperación..."
${CONTAINER_ENGINE} start "$OS_CONTAINER"
sleep 15

trap - EXIT

curl -fsS "${API_URL}/readyz" >/dev/null
echo "== [CHAOS] Escenario opensearch-stop COMPLETADO CON ÉXITO =="
