#!/usr/bin/env bash
# ==============================================================================
# PetPassport — Caos: Suite completa de Game Day (PP-F3-QA-003)
# Ejecuta de forma secuencial los 6 escenarios de inyección de fallos y resiliencia.
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "========================================================================"
echo "          PETPASSPORT — GAME DAY & CHAOS SUITE (PP-F3-QA-003)          "
echo "========================================================================"
echo "Fecha de inicio: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo ""

declare -A RESULTS
SCENARIOS=(
  "redis-flush"
  "redis-stop"
  "opensearch-stop"
  "worker-kill-one"
  "api-kill-one"
  "pg-latency"
)

TOTAL_PASS=0
TOTAL_FAIL=0

for scenario in "${SCENARIOS[@]}"; do
  echo ">>> [EJECUTANDO] $scenario.sh ..."
  if bash "${SCRIPT_DIR}/${scenario}.sh"; then
    RESULTS["$scenario"]="PASS"
    TOTAL_PASS=$((TOTAL_PASS + 1))
    echo ">>> [OK] $scenario.sh superado exitosamente."
  else
    RESULTS["$scenario"]="FAIL"
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    echo ">>> [ERROR] $scenario.sh falló." >&2
  fi
  echo "------------------------------------------------------------------------"
  sleep 3
done

echo ""
echo "========================================================================"
echo "                       RESUMEN DEL GAME DAY                             "
echo "========================================================================"
printf "%-25s | %-10s\n" "Escenario" "Resultado"
echo "------------------------------------------------------------------------"
for scenario in "${SCENARIOS[@]}"; do
  printf "%-25s | %-10s\n" "$scenario" "${RESULTS[$scenario]}"
done
echo "========================================================================"
echo "Total superados: $TOTAL_PASS / ${#SCENARIOS[@]}"
echo "Total fallidos:  $TOTAL_FAIL / ${#SCENARIOS[@]}"
echo "Fecha de fin:    $(date -u +"%Y-%m-%dT%H:%M:%SZ")"

if [ "$TOTAL_FAIL" -gt 0 ]; then
  echo "Game Day concluido con fallos." >&2
  exit 1
fi

echo "¡Game Day concluido con éxito! Todos los criterios de resiliencia validados."
