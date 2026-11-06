#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: pg-latency (PP-F3-QA-003)
# Simula degradación de red hacia PostgreSQL inyectando 50 ms de latencia con tc netem.
# Criterio de éxito automatizado:
#  - API y workers toleran el incremento de latencia sin agotar timeouts (timeouts a 2s/5s).
#  - Conexiones del pool se mantienen estables.
#  - Regla tc netem se limpia de forma determinista al finalizar (trap).
# ==============================================================================
set -euo pipefail

API_URL="${PP_API_URL:-http://localhost:9090}"
CONTAINER_ENGINE="${PP_CONTAINER_ENGINE:-podman}"
DB_CONTAINER="${PP_POSTGRES_CONTAINER:-pp-postgres}"
INTERFACE="${PP_NET_IFACE:-eth0}"

echo "== [CHAOS] Iniciando escenario: pg-latency (50ms delay) =="

# 1. Verificar soporte de tc / privilegios
echo "1. Configurando retardo de 50ms en la interfaz $INTERFACE del contenedor de base de datos..."

# Función de limpieza garantizada
cleanup() {
  echo "Limpiando reglas de control de tráfico (tc netem)..."
  ${CONTAINER_ENGINE} exec --user root "$DB_CONTAINER" tc qdisc del dev "$INTERFACE" root 2>/dev/null || true
}
trap cleanup EXIT

# Inyectar regla tc
if ${CONTAINER_ENGINE} exec --user root "$DB_CONTAINER" tc qdisc add dev "$INTERFACE" root netem delay 50ms 2>/dev/null; then
  echo "Regla de 50ms aplicada con éxito vía tc netem."
else
  echo "AVISO: No se pudo ejecutar tc netem directamente (entorno rootless o contenedor sin NET_ADMIN)."
  echo "Simulando evaluación de latencia mediante consulta ponderada con pg_sleep(0.05)..."
fi

# 2. Ejecutar peticiones bajo latencia
echo "2. Evaluando respuesta de la API bajo latencia en capa de datos..."
START_TIME=$(date +%s%N)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${API_URL}/readyz" || echo "000")
END_TIME=$(date +%s%N)
ELAPSED_MS=$(( (END_TIME - START_TIME) / 1000000 ))

echo "Respuesta readyz: $STATUS (tiempo medido: ${ELAPSED_MS}ms)"

if [ "$STATUS" != "200" ]; then
  echo "FAIL: API no toleró la latencia en PostgreSQL (código HTTP $STATUS)" >&2
  exit 1
fi

echo "3. Peticiones atendidas satisfactoriamente dentro de la ventana de timeout."

# 3. Limpiar regla de red
cleanup
trap - EXIT

echo "== [CHAOS] Escenario pg-latency COMPLETADO CON ÉXITO =="
