#!/usr/bin/env bash
# Despliegue idempotente (PP-F1-INF-006): deploy.sh <env> <version>
#
#   1. pull de la versión (imágenes ghcr.io inmutables, misma imagen promovida)
#   2. migrate up (one-shot; si falla, se aborta sin tocar lo que corre)
#   3. keycloak-config (realm declarativo, idempotente)
#   4. up -d --wait de api y frontend (readyz vía healthcheck)
#   5. smoke test; si falla y hay versión anterior → rollback automático
#   6. anotación en Grafana y aviso al canal del equipo
#
# Rollback manual: deploy.sh <env> <version-anterior> (las migraciones son
# compatibles hacia atrás una versión, DAT-002; nunca se ejecuta `down`).
# Volver a ejecutar con la misma versión no cambia nada (idempotente).
set -euo pipefail

usage() { echo "uso: $0 <env> <version> (p. ej. staging sha-3f2a…)" >&2; exit 2; }
[[ $# -eq 2 ]] || usage
ENV_NAME=$1
VERSION=$2
[[ $ENV_NAME =~ ^(staging|prod)$ ]] || { echo "entorno no soportado: $ENV_NAME" >&2; exit 2; }
[[ $VERSION =~ ^[A-Za-z0-9._-]+$ ]] || { echo "versión inválida: $VERSION" >&2; exit 2; }

REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
ENV_FILE=${PP_ENV_FILE:-/etc/petpassport/$ENV_NAME.env}
STATE_DIR=${PP_STATE_DIR:-/var/lib/petpassport/$ENV_NAME}
COMPOSE_FILE=$REPO_DIR/infra/compose/docker-compose.staging.yml
OBS_FILE=$REPO_DIR/infra/compose/docker-compose.observability.yml

log() { printf '%s [deploy:%s] %s\n' "$(date -u +%FT%TZ)" "$ENV_NAME" "$*"; }
die() { log "ERROR: $*"; exit 1; }

# --- Precondiciones ---------------------------------------------------------
[[ -f $ENV_FILE ]] || die "no existe $ENV_FILE (se genera desde el secret manager, RUNBOOK §2)"
perms=$(stat -c '%a' "$ENV_FILE")
[[ $perms == 600 || $perms == 400 ]] || die "$ENV_FILE debe tener permisos 600 (tiene $perms)"
mkdir -p "$STATE_DIR"
[[ -f $STATE_DIR/certs/origin.pem && -f $STATE_DIR/certs/origin-key.pem ]] ||
  die "falta el certificado de origen en $STATE_DIR/certs (RUNBOOK §1)"
# El certificado de origen no lo ve el blackbox (está tras Cloudflare): se
# comprueba aquí en cada despliegue (alerta CertificateExpiringSoon cubre el del borde).
openssl x509 -checkend $((14 * 86400)) -noout -in "$STATE_DIR/certs/origin.pem" >/dev/null ||
  echo "AVISO: el certificado de origen caduca en < 14 días (RUNBOOK §7.5)" >&2

# Un solo despliegue a la vez por entorno.
exec 9>"$STATE_DIR/deploy.lock"
flock -n 9 || die "otro despliegue de $ENV_NAME está en curso"

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a
: "${PP_DOMAIN:?PP_DOMAIN en $ENV_FILE}"

export PP_ENV=$ENV_NAME PP_VERSION=$VERSION PP_STATE_DIR=$STATE_DIR
export GRAFANA_ANONYMOUS=false
: "${GRAFANA_ADMIN_PASSWORD:?GRAFANA_ADMIN_PASSWORD en $ENV_FILE}"

# Observabilidad: targets de certificados públicos y webhook de alertas (600).
mkdir -p "$STATE_DIR/prom-targets"
printf '[{"targets":["https://app.%s/","https://api.%s/api/v1/healthz","https://auth.%s/realms/petpassport/.well-known/openid-configuration"]}]
'   "$PP_DOMAIN" "$PP_DOMAIN" "$PP_DOMAIN" >"$STATE_DIR/prom-targets/tls.json"
(umask 077 && printf '%s
' "${ALERT_SLACK_WEBHOOK_URL:-https://hooks.invalid/unset}" >"$STATE_DIR/alert-webhook-url")
export PP_PROM_TARGETS_DIR=$STATE_DIR/prom-targets PP_ALERT_WEBHOOK_FILE=$STATE_DIR/alert-webhook-url

# Rangos de Cloudflare: se refrescan si es posible; si no, la lista del repo.
cf_ips() {
  local fresh
  if fresh=$( { curl -fsS --max-time 5 https://www.cloudflare.com/ips-v4 && echo &&
               curl -fsS --max-time 5 https://www.cloudflare.com/ips-v6; } 2>/dev/null ) &&
     [[ $(grep -c '/' <<<"$fresh") -ge 10 ]]; then
    grep '/' <<<"$fresh"
  else
    grep -v '^#' "$REPO_DIR/infra/waf/cloudflare-ips.txt" | grep '/'
  fi
}
CF_TRUSTED_IPS=$(cf_ips | paste -sd, -)
export CF_TRUSTED_IPS

# /config.json del frontend por entorno (INF-005).
cat >"$STATE_DIR/config.json.tmp" <<EOF
{
  "apiBaseUrl": "https://api.$PP_DOMAIN/api/v1",
  "oidc": { "issuer": "https://auth.$PP_DOMAIN/realms/petpassport", "clientId": "petpassport-web" },
  "env": "$ENV_NAME"
}
EOF
mv "$STATE_DIR/config.json.tmp" "$STATE_DIR/config.json"

compose() { docker compose --project-directory "$REPO_DIR/infra/compose" -f "$COMPOSE_FILE" -f "$OBS_FILE" "$@"; }

PREVIOUS=$(cat "$STATE_DIR/current-version" 2>/dev/null || true)
log "versión actual: ${PREVIOUS:-ninguna} → objetivo: $VERSION"

notify() {
  local text=$1
  if [[ -n ${GRAFANA_URL:-} && -n ${GRAFANA_TOKEN:-} ]]; then
    curl -fsS --max-time 5 -X POST "$GRAFANA_URL/api/annotations" \
      -H "Authorization: Bearer $GRAFANA_TOKEN" -H 'Content-Type: application/json' \
      -d "{\"tags\":[\"deploy\",\"$ENV_NAME\"],\"text\":\"$text\"}" >/dev/null ||
      log "aviso: no se pudo anotar en Grafana"
  fi
  if [[ -n ${TEAM_WEBHOOK_URL:-} ]]; then
    curl -fsS --max-time 5 -X POST "$TEAM_WEBHOOK_URL" -H 'Content-Type: application/json' \
      -d "{\"text\":\"[$ENV_NAME] $text\"}" >/dev/null || log "aviso: no se pudo avisar al canal"
  fi
}

# --- 1. Pull ------------------------------------------------------------------
log "1/6 pull $VERSION"
compose --profile oneshot pull --quiet migrate api frontend traefik keycloak keycloak-config

# --- 2. Migraciones (aborta si fallan; lo desplegado sigue corriendo) ---------
log "2/6 migrate up"
compose --profile oneshot run --rm migrate || die "migraciones fallidas: despliegue abortado, $PREVIOUS sigue activo"

# --- 3. Infra de borde + Keycloak y realm ------------------------------------
log "3/6 traefik + keycloak + realm + observabilidad"
compose up -d --wait --wait-timeout 300 traefik keycloak
compose up -d otel-collector tempo prometheus alertmanager blackbox loki alloy grafana
compose --profile oneshot run --rm keycloak-config || die "keycloak-config falló"

# --- 4. Aplicación -----------------------------------------------------------
log "4/6 up api frontend"
if ! compose up -d --wait --wait-timeout 180 api frontend; then
  compose logs --tail 50 api >&2 || true
  ROLLBACK_REASON="readyz no llegó a healthy"
fi

# --- 5. Smoke ----------------------------------------------------------------
if [[ -z ${ROLLBACK_REASON:-} ]]; then
  log "5/6 smoke"
  "$REPO_DIR/infra/deploy/smoke.sh" "$PP_DOMAIN" || ROLLBACK_REASON="smoke test fallido"
fi

if [[ -n ${ROLLBACK_REASON:-} ]]; then
  if [[ -n $PREVIOUS && $PREVIOUS != "$VERSION" && ${PP_NO_AUTO_ROLLBACK:-0} != 1 ]]; then
    log "ROLLBACK a $PREVIOUS: $ROLLBACK_REASON"
    PP_VERSION=$PREVIOUS compose up -d --wait --wait-timeout 180 api frontend ||
      log "ERROR: el rollback no llegó a healthy; intervención manual (RUNBOOK §4)"
    notify "deploy $VERSION FALLIDO ($ROLLBACK_REASON); rollback a $PREVIOUS"
  else
    notify "deploy $VERSION FALLIDO ($ROLLBACK_REASON); sin versión anterior para rollback"
  fi
  die "$ROLLBACK_REASON"
fi

# --- 6. Registro y avisos ----------------------------------------------------
echo "$VERSION" >"$STATE_DIR/current-version"
echo "$(date -u +%FT%TZ) $VERSION ${PREVIOUS:-none}" >>"$STATE_DIR/history"
log "6/6 OK: $VERSION desplegada"
notify "deploy $VERSION OK (anterior: ${PREVIOUS:-ninguna})"
