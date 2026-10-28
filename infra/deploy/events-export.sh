#!/usr/bin/env bash
# Exportación semanal de es.events al bucket de respaldos (PP-F1-DAT-009).
# La lanza pp-events-export.timer (infra/deploy/systemd); también a mano:
#   sudo infra/deploy/events-export.sh [staging|prod]
# Usa la versión desplegada (current-version de deploy.sh) y avisa al canal del
# equipo si falla (TEAM_WEBHOOK_URL). Detalle y restauración: RUNBOOK §9.
set -euo pipefail
ENV_NAME=${1:-staging}
ENV_FILE=${PP_ENV_FILE:-/etc/petpassport/$ENV_NAME.env}
STATE_DIR=${PP_STATE_DIR:-/var/lib/petpassport/$ENV_NAME}
REPO=$(cd "$(dirname "$0")/../.." && pwd)

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a
PP_VERSION=$(cat "$STATE_DIR/current-version")
export PP_ENV=$ENV_NAME PP_VERSION PP_STATE_DIR=$STATE_DIR

notify_failure() {
  local msg="PetPassport $ENV_NAME: falló la exportación semanal de es.events (journalctl -u pp-events-export)"
  echo "$msg" >&2
  if [[ -n ${TEAM_WEBHOOK_URL:-} ]]; then
    curl -fsS --max-time 10 -H 'Content-Type: application/json' \
      -d "{\"text\":\"$msg\"}" "$TEAM_WEBHOOK_URL" >/dev/null || true
  fi
}
trap notify_failure ERR

docker compose --project-directory "$REPO/infra/compose" \
  -f "$REPO/infra/compose/docker-compose.staging.yml" --profile oneshot run --rm events-export
