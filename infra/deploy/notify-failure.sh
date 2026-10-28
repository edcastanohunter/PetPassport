#!/usr/bin/env bash
# Aviso de fallo de una unidad systemd al canal del equipo (TEAM_WEBHOOK_URL).
# Lo invoca pp-notify-failure@.service: notify-failure.sh <env> <unidad>.
# Solo envía el nombre de la unidad: el detalle se consulta con journalctl.
set -euo pipefail
ENV_NAME=${1:?entorno}
UNIT=${2:?unidad}
msg="PetPassport $ENV_NAME: falló $UNIT (journalctl -u $UNIT -n 100)"
echo "$msg" >&2
if [[ -n ${TEAM_WEBHOOK_URL:-} ]]; then
  jq -cn --arg text "$msg" '{text: $text}' |
    curl -fsS --max-time 10 -H 'Content-Type: application/json' -d @- "$TEAM_WEBHOOK_URL" >/dev/null
fi
