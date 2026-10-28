#!/usr/bin/env bash
# Despliegue idempotente (PP-F1-INF-006, ADR-026): deploy.sh <env> <version>
#
#   1. renderiza las unidades Quadlet de infra/quadlet para <version>, las valida
#      con el generador (-dryrun) y hace podman pull de sus imágenes
#   2. migrate up (unidad oneshot; si falla, se aborta sin tocar lo que corre)
#   3. instala las unidades que cambiaron; reinicia Traefik, Keycloak y
#      observabilidad si cambiaron; aplica el realm (keycloak-config)
#   4. reinicia api y frontend si cambiaron; Notify=healthy espera a /readyz
#   5. smoke test; si falla y hay versión anterior → rollback automático
#   6. anotación en Grafana y aviso al canal del equipo
#
# Rollback manual: deploy.sh <env> <version-anterior> (las migraciones son
# compatibles hacia atrás una versión, DAT-002; nunca se ejecuta `down`).
# Volver a ejecutar con la misma versión no reinicia nada (idempotente).
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
QUADLET_DIR=${PP_QUADLET_DIR:-/etc/containers/systemd}
SYSTEMD_DIR=${PP_SYSTEMD_DIR:-/etc/systemd/system}
NFT_FILE=/etc/petpassport/pp-origin.nft
MIN_PODMAN=5.4.0

# Servicios de larga duración por grupo (unidades pp-<nombre>.service).
EDGE_UNITS=(traefik keycloak)
OBS_UNITS=(otel-collector tempo prometheus alertmanager blackbox loki alloy grafana)
APP_UNITS=(api frontend)

log() { printf '%s [deploy:%s] %s\n' "$(date -u +%FT%TZ)" "$ENV_NAME" "$*"; }
die() { log "ERROR: $*"; exit 1; }

# --- Precondiciones ---------------------------------------------------------
[[ $EUID -eq 0 ]] || die "ejecutar como root (unidades Quadlet de sistema, ADR-026)"
command -v podman >/dev/null || die "falta podman (RUNBOOK §1)"
podman_version=$(podman version --format '{{.Client.Version}}')
[[ $(printf '%s\n%s\n' "$MIN_PODMAN" "$podman_version" | sort -V | head -1) == "$MIN_PODMAN" ]] ||
  die "podman $podman_version < $MIN_PODMAN (Notify=healthy, NetworkAlias; ADR-026)"
QUADLET_BIN=
for q in /usr/lib/systemd/system-generators/podman-system-generator /usr/libexec/podman/quadlet /usr/lib/podman/quadlet; do
  [[ -x $q ]] && { QUADLET_BIN=$q; break; }
done
[[ -n $QUADLET_BIN ]] || die "no se encuentra el generador de Quadlet"

[[ -f $ENV_FILE ]] || die "no existe $ENV_FILE (se genera desde el secret manager, RUNBOOK §2)"
perms=$(stat -c '%a' "$ENV_FILE")
[[ $perms == 600 || $perms == 400 ]] || die "$ENV_FILE debe tener permisos 600 (tiene $perms)"
mkdir -p "$STATE_DIR" "$QUADLET_DIR"
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
: "${PP_DOMAIN:?PP_DOMAIN en $ENV_FILE}" "${GRAFANA_ADMIN_PASSWORD:?GRAFANA_ADMIN_PASSWORD en $ENV_FILE}"
IMAGE_PREFIX=${IMAGE_PREFIX:-ghcr.io/edcastanohunter/petpassport}
[[ $PP_DOMAIN =~ ^[a-z0-9.-]+$ ]] || die "PP_DOMAIN inválido"
[[ $IMAGE_PREFIX =~ ^[a-z0-9./_-]+$ ]] || die "IMAGE_PREFIX inválido"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

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
cf_ips >"$WORK/cf-ips"
grep -Eqv '^[0-9a-fA-F:./]+$' "$WORK/cf-ips" && die "rangos de Cloudflare con formato inesperado"
CF_TRUSTED_IPS=$(paste -sd, - <"$WORK/cf-ips")

# --- Renderizado -------------------------------------------------------------
# Las unidades (644, legibles por todos) solo llevan versión, rutas y rangos;
# los secretos van en un EnvironmentFile por servicio (600).
subst() {
  sed -e "s|@PP_VERSION@|$1|g" -e "s|@PP_ENV@|$ENV_NAME|g" -e "s|@IMAGE_PREFIX@|$IMAGE_PREFIX|g" \
      -e "s|@STATE_DIR@|$STATE_DIR|g" -e "s|@REPO_DIR@|$REPO_DIR|g" -e "s|@CF_TRUSTED_IPS@|$CF_TRUSTED_IPS|g"
}

render_units() {
  local version=$1 out=$2 f
  mkdir -p "$out/quadlet" "$out/systemd"
  for f in "$REPO_DIR"/infra/quadlet/pp-*; do subst "$version" <"$f" >"$out/quadlet/${f##*/}"; done
  for f in "$REPO_DIR"/infra/deploy/systemd/pp-*; do subst "$version" <"$f" >"$out/systemd/${f##*/}"; done
  ! grep -rEl '@[A-Z0-9_]+@' "$out/quadlet" "$out/systemd" || die "marcadores sin sustituir en las unidades"
  QUADLET_UNIT_DIRS=$out/quadlet "$QUADLET_BIN" -dryrun >/dev/null 2>"$out/quadlet.log" ||
    { cat "$out/quadlet.log" >&2; die "el generador de Quadlet rechaza las unidades de $version"; }
}

kv() { printf '%s=%s\n' "$1" "$2"; }
write_envs() {
  local out=$1
  mkdir -p "$out"
  {
    kv PP_DOMAIN "$PP_DOMAIN"
    kv PP_ORIGIN_AUTH_SECRET "${PP_ORIGIN_AUTH_SECRET:?PP_ORIGIN_AUTH_SECRET}"
    kv TEAM_IP_RANGES "${TEAM_IP_RANGES:?TEAM_IP_RANGES}"
  } >"$out/traefik.env"
  {
    kv KC_DB postgres
    kv KC_DB_URL "${KC_DB_URL:?KC_DB_URL}"
    kv KC_DB_USERNAME "${KC_DB_USERNAME:-keycloak}"
    kv KC_DB_PASSWORD "${KC_DB_PASSWORD:?KC_DB_PASSWORD}"
    kv KC_HOSTNAME "https://auth.$PP_DOMAIN"
    kv KC_HOSTNAME_BACKCHANNEL_DYNAMIC true
    kv KC_PROXY_HEADERS xforwarded
    kv KC_HTTP_ENABLED true
    kv KC_BOOTSTRAP_ADMIN_USERNAME "${KC_ADMIN_USER:?KC_ADMIN_USER}"
    kv KC_BOOTSTRAP_ADMIN_PASSWORD "${KC_ADMIN_PASSWORD:?KC_ADMIN_PASSWORD}"
  } >"$out/keycloak.env"
  {
    kv KEYCLOAK_URL http://keycloak:8080
    kv KEYCLOAK_USER "$KC_ADMIN_USER"
    kv KEYCLOAK_PASSWORD "$KC_ADMIN_PASSWORD"
    kv KEYCLOAK_AVAILABILITYCHECK_ENABLED true
    kv KEYCLOAK_AVAILABILITYCHECK_TIMEOUT 180s
    kv IMPORT_FILES_LOCATIONS /config/realm.yaml
    kv IMPORT_VARSUBSTITUTION_ENABLED true
    kv IMPORT_MANAGED_CLIENT full
    kv IMPORT_MANAGED_ROLE full
    kv PP_WEB_ORIGIN "https://app.$PP_DOMAIN"
    # Sin origen de desarrollo fuera de dev: se repite el de la app.
    kv PP_WEB_ORIGIN_DEV "https://app.$PP_DOMAIN"
    kv PP_BACKEND_CLIENT_SECRET "${PP_BACKEND_CLIENT_SECRET:?PP_BACKEND_CLIENT_SECRET}"
    kv PP_SMTP_HOST "${PP_SMTP_HOST:?PP_SMTP_HOST}"
    kv PP_SMTP_PORT "${PP_SMTP_PORT:-587}"
    kv PP_SMTP_FROM "${PP_SMTP_FROM:-no-reply@$PP_DOMAIN}"
    kv PP_SMTP_AUTH true
    kv PP_SMTP_USER "${PP_SMTP_USER:?PP_SMTP_USER}"
    kv PP_SMTP_PASSWORD "${PP_SMTP_PASSWORD:?PP_SMTP_PASSWORD}"
    kv PP_SMTP_STARTTLS true
  } >"$out/keycloak-config.env"
  kv PP_DB_URL "${PP_MIGRATOR_DB_URL:?PP_MIGRATOR_DB_URL}" >"$out/migrate.env"
  {
    kv PP_DB_URL "${PP_BACKUP_DB_URL:?PP_BACKUP_DB_URL}"
    kv PP_BACKUP_STORAGE_ENDPOINT "${PP_BACKUP_STORAGE_ENDPOINT:?PP_BACKUP_STORAGE_ENDPOINT}"
    kv PP_BACKUP_STORAGE_BUCKET "${PP_BACKUP_STORAGE_BUCKET:?PP_BACKUP_STORAGE_BUCKET}"
    kv PP_BACKUP_STORAGE_ACCESS_KEY "${PP_BACKUP_STORAGE_ACCESS_KEY:?PP_BACKUP_STORAGE_ACCESS_KEY}"
    kv PP_BACKUP_STORAGE_SECRET_KEY "${PP_BACKUP_STORAGE_SECRET_KEY:?PP_BACKUP_STORAGE_SECRET_KEY}"
    kv PP_BACKUP_STORAGE_USE_SSL "${PP_BACKUP_STORAGE_USE_SSL:-true}"
  } >"$out/events-export.env"
  {
    kv PP_ENV "$ENV_NAME"
    kv PP_HTTP_ADDR :8080
    kv PP_INTERNAL_ADDR :9090
    kv PP_DB_URL "${PP_DB_URL:?PP_DB_URL}"
    kv PP_DB_MAX_CONNS "${PP_DB_MAX_CONNS:-20}"
    kv PP_OIDC_ISSUER "https://auth.$PP_DOMAIN/realms/petpassport"
    kv PP_OIDC_DISCOVERY_URL http://keycloak:8080/realms/petpassport
    kv PP_OIDC_AUDIENCE petpassport-api
    kv PP_OIDC_ALLOWED_AZP petpassport-web
    kv PP_KC_ADMIN_URL http://keycloak:8080
    kv PP_KC_ADMIN_CLIENT_ID petpassport-backend
    kv PP_KC_ADMIN_CLIENT_SECRET "$PP_BACKEND_CLIENT_SECRET"
    kv PP_IP_HASH_SALT "${PP_IP_HASH_SALT:?PP_IP_HASH_SALT}"
    kv PP_STORAGE_ENDPOINT "${PP_STORAGE_ENDPOINT:?PP_STORAGE_ENDPOINT}"
    kv PP_STORAGE_BUCKET "${PP_STORAGE_BUCKET:-petpassport-media}"
    kv PP_STORAGE_ACCESS_KEY "${PP_STORAGE_ACCESS_KEY:?PP_STORAGE_ACCESS_KEY}"
    kv PP_STORAGE_SECRET_KEY "${PP_STORAGE_SECRET_KEY:?PP_STORAGE_SECRET_KEY}"
    kv PP_STORAGE_USE_SSL "${PP_STORAGE_USE_SSL:-true}"
    kv PP_SMTP_HOST "$PP_SMTP_HOST"
    kv PP_SMTP_PORT "${PP_SMTP_PORT:-587}"
    kv PP_SMTP_USER "$PP_SMTP_USER"
    kv PP_SMTP_PASSWORD "$PP_SMTP_PASSWORD"
    kv PP_SMTP_FROM "${PP_SMTP_FROM:-no-reply@$PP_DOMAIN}"
    kv PP_CORS_ORIGINS "https://app.$PP_DOMAIN"
    kv PP_PUBLIC_BASE_URL "https://app.$PP_DOMAIN"
    kv PP_LOG_LEVEL info
    kv PP_OTEL_EXPORTER_OTLP_ENDPOINT http://otel-collector:4318
  } >"$out/api.env"
  {
    kv GF_SECURITY_ADMIN_PASSWORD "$GRAFANA_ADMIN_PASSWORD"
    kv GF_AUTH_ANONYMOUS_ENABLED false
    kv GF_USERS_ALLOW_SIGN_UP false
    kv GF_ANALYTICS_REPORTING_ENABLED false
  } >"$out/grafana.env"
}

# Instala los ficheros de <src> en <dst> que difieran y añade a CHANGED el
# servicio afectado (pp-api.container o env/api.env → api).
CHANGED=()
install_changed() {
  local src=$1 dst=$2 mode=$3 f name
  mkdir -p "$dst"
  for f in "$src"/*; do
    name=${f##*/}
    cmp -s "$f" "$dst/$name" && continue
    install -m "$mode" "$f" "$dst/$name"
    name=${name#pp-}
    CHANGED+=("${name%.*}")
  done
}
changed() { local s; for s in "${CHANGED[@]}"; do [[ $s == "$1" ]] && return 0; done; return 1; }

# Reinicia los servicios que cambiaron y arranca los que no corren. Con
# Notify=healthy, systemctl no vuelve hasta que el contenedor está sano.
apply_units() {
  local restart=() start=() s
  for s in "$@"; do
    if changed "$s"; then restart+=("pp-$s.service"); else start+=("pp-$s.service"); fi
  done
  if ((${#restart[@]})); then log "  reinicio: ${restart[*]}"; systemctl restart "${restart[@]}" || return 1; fi
  if ((${#start[@]})); then systemctl start "${start[@]}" || return 1; fi
}

# Unidades pp-* instaladas que ya no existen en el repo: se detienen y se borran.
prune_units() {
  local f name
  for f in "$QUADLET_DIR"/pp-*; do
    [[ -e $f ]] || continue
    name=${f##*/}
    [[ -e $WORK/new/quadlet/$name ]] && continue
    log "  retirada: $name"
    systemctl stop "${name%.*}.service" 2>/dev/null || true
    rm -f "$f"
  done
}

PREVIOUS=$(cat "$STATE_DIR/current-version" 2>/dev/null || true)
log "versión actual: ${PREVIOUS:-ninguna} → objetivo: $VERSION"

notify() {
  local text=$1
  if [[ -n ${GRAFANA_URL:-} && -n ${GRAFANA_TOKEN:-} ]]; then
    jq -cn --arg env "$ENV_NAME" --arg text "$text" '{tags: ["deploy", $env], text: $text}' |
      curl -fsS --max-time 5 -X POST "$GRAFANA_URL/api/annotations" \
        -H "Authorization: Bearer $GRAFANA_TOKEN" -H 'Content-Type: application/json' -d @- >/dev/null ||
      log "aviso: no se pudo anotar en Grafana"
  fi
  if [[ -n ${TEAM_WEBHOOK_URL:-} ]]; then
    jq -cn --arg text "[$ENV_NAME] $text" '{text: $text}' |
      curl -fsS --max-time 5 -X POST "$TEAM_WEBHOOK_URL" -H 'Content-Type: application/json' -d @- >/dev/null ||
      log "aviso: no se pudo avisar al canal"
  fi
}

# --- 1. Render, validación y pull -------------------------------------------
log "1/6 render + validación Quadlet + pull $VERSION"
render_units "$VERSION" "$WORK/new"
(umask 077 && write_envs "$WORK/new/env")
grep -h '^Image=' "$WORK"/new/quadlet/*.container | cut -d= -f2- | sort -u | while read -r image; do
  podman pull -q "$image" >/dev/null || die "pull fallido: $image"
done

# Ficheros de estado que montan los contenedores.
cat >"$STATE_DIR/config.json.tmp" <<EOF
{
  "apiBaseUrl": "https://api.$PP_DOMAIN/api/v1",
  "oidc": { "issuer": "https://auth.$PP_DOMAIN/realms/petpassport", "clientId": "petpassport-web" },
  "env": "$ENV_NAME"
}
EOF
mv "$STATE_DIR/config.json.tmp" "$STATE_DIR/config.json"
mkdir -p "$STATE_DIR/prom-targets"
jq -cn --arg d "$PP_DOMAIN" \
  '[{targets: ["https://app.\($d)/", "https://api.\($d)/api/v1/healthz", "https://auth.\($d)/realms/petpassport/.well-known/openid-configuration"]}]' \
  >"$STATE_DIR/prom-targets/tls.json"
(umask 077 && printf '%s\n' "${ALERT_SLACK_WEBHOOK_URL:-https://hooks.invalid/unset}" >"$STATE_DIR/alert-webhook-url")

# --- 2. Migraciones (aborta si fallan; lo desplegado sigue corriendo) ---------
# Solo se instalan las redes, la unidad de migrate y su entorno: si fallan,
# las unidades de api/frontend siguen apuntando a la versión anterior.
log "2/6 migrate up"
mkdir -p "$WORK/stage1"
cp "$WORK"/new/quadlet/pp-*.network "$WORK/new/quadlet/pp-migrate.container" "$WORK/stage1/"
install_changed "$WORK/stage1" "$QUADLET_DIR" 644
(umask 077 && mkdir -p "$STATE_DIR/env" && install -m 600 "$WORK/new/env/migrate.env" "$STATE_DIR/env/migrate.env")
systemctl daemon-reload
systemctl start pp-migrate.service ||
  die "migraciones fallidas (journalctl -u pp-migrate): despliegue abortado, ${PREVIOUS:-nada} sigue activo"

# --- 3. Unidades, borde, Keycloak, realm y observabilidad --------------------
log "3/6 unidades + traefik + keycloak + realm + observabilidad"
install_changed "$WORK/new/quadlet" "$QUADLET_DIR" 644
install_changed "$WORK/new/systemd" "$SYSTEMD_DIR" 644
install_changed "$WORK/new/env" "$STATE_DIR/env" 600
prune_units
systemctl daemon-reload

# Filtro de origen con los rangos de Cloudflare vigentes (SEC-001).
grep -v ':' "$WORK/cf-ips" | paste -sd, - | sed 's/,/, /g' >"$WORK/cf4"
grep ':' "$WORK/cf-ips" | paste -sd, - | sed 's/,/, /g' >"$WORK/cf6"
sed -e "s|@CF_IPV4@|$(cat "$WORK/cf4")|" -e "s|@CF_IPV6@|$(cat "$WORK/cf6")|" \
  "$REPO_DIR/infra/deploy/nftables/pp-origin.nft" >"$WORK/pp-origin.nft"
nft -c -f "$WORK/pp-origin.nft" || die "tabla nftables pp-origin inválida"
install -m 644 "$WORK/pp-origin.nft" "$NFT_FILE"
systemctl enable pp-origin-firewall.service pp-events-export.timer >/dev/null 2>&1
systemctl reload-or-restart pp-origin-firewall.service
systemctl start pp-events-export.timer

apply_units "${EDGE_UNITS[@]}" || die "traefik/keycloak no llegaron a healthy (journalctl -u pp-keycloak)"
apply_units "${OBS_UNITS[@]}" || log "aviso: algún servicio de observabilidad no arrancó"
systemctl start pp-keycloak-config.service || die "keycloak-config falló (journalctl -u pp-keycloak-config)"

# --- 4. Aplicación -----------------------------------------------------------
log "4/6 api frontend"
if ! apply_units "${APP_UNITS[@]}"; then
  journalctl -u pp-api.service -n 50 --no-pager >&2 || true
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
    render_units "$PREVIOUS" "$WORK/rollback"
    CHANGED=()
    install_changed "$WORK/rollback/quadlet" "$QUADLET_DIR" 644
    install_changed "$WORK/rollback/systemd" "$SYSTEMD_DIR" 644
    systemctl daemon-reload
    systemctl restart pp-api.service pp-frontend.service ||
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
log "6/6 OK: $VERSION desplegada (servicios reiniciados: ${CHANGED[*]:-ninguno})"
notify "deploy $VERSION OK (anterior: ${PREVIOUS:-ninguna})"
