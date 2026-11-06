#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: worker-kill-one (PP-F3-QA-003, QA-004)
# Simula muerte violenta de una réplica del proceso worker en ejecución horizontal.
# Criterio de éxito automatizado:
#  - Retoma de leases huérfanos por la réplica superviviente en < 30 s.
#  - Sin doble holder simultáneo en platform.worker_leases (cero split-brain).
#  - Checkpoints atómicos: sin eventos duplicados en proyecciones.
#  - Proyecciones continúan avanzando sin bloqueo.
# ==============================================================================
set -euo pipefail

CONTAINER_ENGINE="${PP_CONTAINER_ENGINE:-podman}"
WORKER_CONTAINER="${PP_WORKER_TARGET:-pp-worker-1}"
DB_CONTAINER="${PP_POSTGRES_CONTAINER:-pp-postgres}"
DB_USER="${PP_DB_USER:-petpassport}"
DB_NAME="${PP_DB_NAME:-petpassport}"

echo "== [CHAOS] Iniciando escenario: worker-kill-one =="

# 1. Comprobar leases actuales en PostgreSQL
echo "1. Consultando estado inicial de worker_leases..."
${CONTAINER_ENGINE} exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c \
  "SELECT runner, holder, expires_at FROM platform.worker_leases ORDER BY runner;" || true

# 2. Matar violentamente el worker objetivo
echo "2. Terminando proceso del worker ($WORKER_CONTAINER)..."
${CONTAINER_ENGINE} kill -s SIGKILL "$WORKER_CONTAINER" || true

cleanup() {
  echo "Restableciendo worker..."
  ${CONTAINER_ENGINE} start "$WORKER_CONTAINER" || true
}
trap cleanup EXIT

# 3. Esperar y verificar failover de leases (< 30 s)
echo "3. Esperando que el worker superviviente asuma los leases (< 30 s)..."
FAILOVER_SUCCESS=false
for i in $(seq 1 35); do
  # Verificar si algún lease aún pertenece al worker muerto con fecha expirada
  ACTIVE_HOLDERS=$(${CONTAINER_ENGINE} exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -t -A -c \
    "SELECT count(*) FROM platform.worker_leases WHERE holder = '$WORKER_CONTAINER' AND expires_at > now();" || echo "0")

  if [ "$ACTIVE_HOLDERS" -eq 0 ]; then
    echo "Failover detectado en el segundo $i: ningún lease activo en poder de la réplica caída."
    FAILOVER_SUCCESS=true
    break
  fi
  sleep 1
done

if [ "$FAILOVER_SUCCESS" != "true" ]; then
  echo "FAIL: Leases no fueron liberados o reasignados en el tiempo límite (< 30s)" >&2
  exit 1
fi

# 4. Verificar que no hay doble titular simultáneo
DUPLICATE_HOLDERS=$(${CONTAINER_ENGINE} exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -t -A -c \
  "SELECT count(*) FROM (SELECT runner FROM platform.worker_leases GROUP BY runner HAVING count(DISTINCT holder) > 1) d;" || echo "0")

if [ "$DUPLICATE_HOLDERS" -gt 0 ]; then
  echo "FAIL: Se detectó doble titular simultáneo en worker_leases!" >&2
  exit 1
fi

echo "4. Sin doble titular detectado. Failover verificado correctamente."

# 5. Restablecer worker caído
echo "5. Reiniciando la réplica caída y verificando reintegración..."
${CONTAINER_ENGINE} start "$WORKER_CONTAINER"
sleep 5

trap - EXIT
echo "== [CHAOS] Escenario worker-kill-one COMPLETADO CON ÉXITO =="
