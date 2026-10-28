#!/usr/bin/env bash
# Preparación del host como root (RUNBOOK §1, ADR-026): host-setup.sh <env>
#
# Es lo único del despliegue que necesita root; deploy.sh corre como el usuario
# petpassport y gestiona sus unidades con `systemctl --user`. Idempotente: se
# vuelve a ejecutar si cambian los rangos de Cloudflare (deploy.sh avisa).
#
#   1. usuario de servicio petpassport (sin contraseña), subuid/subgid y linger
#      (su systemd de usuario arranca con la VM y sobrevive a las sesiones)
#   2. net.ipv4.ip_unprivileged_port_start=443 (pp-traefik.socket abre el 443)
#   3. journal persistente (logs de los contenedores; Alloy los lee)
#   4. tabla nftables pp-origin: el 443 solo desde Cloudflare
#   5. directorios del entorno con dueño petpassport
set -euo pipefail

[[ $# -eq 1 && $1 =~ ^(staging|prod)$ ]] || { echo "uso: $0 staging|prod" >&2; exit 2; }
ENV_NAME=$1
REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SVC_USER=${PP_SVC_USER:-petpassport}
SVC_HOME=/var/lib/petpassport
NFT_DIR=/etc/pp-origin
MIN_PODMAN=5.4.0

log() { printf '%s [host-setup:%s] %s\n' "$(date -u +%FT%TZ)" "$ENV_NAME" "$*"; }
die() { log "ERROR: $*"; exit 1; }
# shellcheck source=lib/cloudflare.sh
source "$REPO_DIR/infra/deploy/lib/cloudflare.sh"

[[ $EUID -eq 0 ]] || die "ejecutar como root (solo este script; deploy.sh no)"
for c in podman nft jq curl openssl loginctl; do command -v "$c" >/dev/null || die "falta $c (RUNBOOK §1)"; done
v=$(podman version --format '{{.Client.Version}}')
[[ $(printf '%s\n%s\n' "$MIN_PODMAN" "$v" | sort -V | head -1) == "$MIN_PODMAN" ]] ||
  die "podman $v < $MIN_PODMAN (Notify=healthy; ADR-026)"

# --- 1. Usuario de servicio ---------------------------------------------------
# UID normal (>= 1000), no --system: journald solo da journal propio a esos
# usuarios, y sin él ni `journalctl --user` ni Alloy (que corre como
# petpassport) verían los logs. Sin contraseña: se entra con sudo -iu.
if ! id "$SVC_USER" >/dev/null 2>&1; then
  log "1/5 creando usuario $SVC_USER"
  useradd --create-home --home-dir "$SVC_HOME" --shell /bin/bash "$SVC_USER"
  passwd -l "$SVC_USER" >/dev/null
fi
(($(id -u "$SVC_USER") >= 1000)) || die "$SVC_USER tiene UID de sistema: sin journal de usuario (RUNBOOK §1)"
# useradd suele asignar los rangos subordinados (sin ellos no hay rootless); si
# no, se toma el primer bloque de 65536 libre tras los existentes.
next_range() { awk -F: 'BEGIN{m=100000} {e=$2+$3; if(e>m)m=e} END{printf "%d-%d", m, m+65535}' "$1"; }
grep -q "^$SVC_USER:" /etc/subuid || usermod --add-subuids "$(next_range /etc/subuid)" "$SVC_USER"
grep -q "^$SVC_USER:" /etc/subgid || usermod --add-subgids "$(next_range /etc/subgid)" "$SVC_USER"
loginctl enable-linger "$SVC_USER"
# `sudo -iu petpassport` no abre sesión de logind: sin esto, systemctl --user y
# journalctl --user no encuentran el bus del usuario.
profile=$SVC_HOME/.bash_profile
# shellcheck disable=SC2016 # $(id -u) se expande al iniciar sesión, no aquí.
grep -qs XDG_RUNTIME_DIR "$profile" ||
  { echo 'export XDG_RUNTIME_DIR=/run/user/$(id -u)  # host-setup.sh' >>"$profile"; chown "$SVC_USER:" "$profile"; }
log "1/5 $SVC_USER: subuid $(grep "^$SVC_USER:" /etc/subuid | cut -d: -f2-), linger activo"

# --- 2. Puerto 443 para rootless ------------------------------------------------
# Solo se baja: si el host ya permite puertos menores, no se toca.
if (($(sysctl -n net.ipv4.ip_unprivileged_port_start) > 443)); then
  cat >/etc/sysctl.d/90-petpassport.conf <<'EOF'
# pp-traefik.socket (usuario petpassport) abre el 443 (ADR-026, host-setup.sh).
net.ipv4.ip_unprivileged_port_start=443
EOF
  sysctl -q -p /etc/sysctl.d/90-petpassport.conf
fi
log "2/5 ip_unprivileged_port_start=$(sysctl -n net.ipv4.ip_unprivileged_port_start)"

# --- 3. Journal persistente ---------------------------------------------------
if [[ ! -d /var/log/journal ]]; then
  mkdir -p /var/log/journal
  systemd-tmpfiles --create --prefix /var/log/journal
  systemctl restart systemd-journald
fi
log "3/5 journal persistente en /var/log/journal"

# --- 4. Filtro de origen ------------------------------------------------------
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cf_ips "$REPO_DIR" >"$WORK/cf-ips" || die "no se pudieron obtener los rangos de Cloudflare"
cf4=$(grep -v ':' "$WORK/cf-ips" | paste -sd, - | sed 's/,/, /g')
cf6=$(grep ':' "$WORK/cf-ips" | paste -sd, - | sed 's/,/, /g')
sed -e "s|@CF_IPV4@|$cf4|" -e "s|@CF_IPV6@|$cf6|" "$REPO_DIR/infra/deploy/host/pp-origin.nft" >"$WORK/pp-origin.nft"
nft -c -f "$WORK/pp-origin.nft" || die "tabla nftables pp-origin inválida"
mkdir -p "$NFT_DIR"
install -m 644 "$WORK/pp-origin.nft" "$NFT_DIR/pp-origin.nft"
# deploy.sh compara con esta lista y avisa si Cloudflare publica rangos nuevos.
install -m 644 "$WORK/cf-ips" "$NFT_DIR/cf-ips"
install -m 644 "$REPO_DIR/infra/deploy/host/pp-origin-firewall.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable pp-origin-firewall.service >/dev/null 2>&1
systemctl reload-or-restart pp-origin-firewall.service
log "4/5 nftables pp-origin cargada ($(wc -l <"$WORK/cf-ips") rangos)"

# --- 5. Directorios -------------------------------------------------------------
install -d -m 700 -o "$SVC_USER" -g "$SVC_USER" /etc/petpassport "$SVC_HOME/$ENV_NAME" "$SVC_HOME/$ENV_NAME/certs"
[[ -f /etc/petpassport/$ENV_NAME.env ]] && chown "$SVC_USER:" "/etc/petpassport/$ENV_NAME.env" && chmod 600 "/etc/petpassport/$ENV_NAME.env"
find "$SVC_HOME/$ENV_NAME/certs" -type f -exec chown "$SVC_USER:" {} + -exec chmod 600 {} +
log "5/5 listo. Siguiente: como $SVC_USER, $REPO_DIR/infra/deploy/deploy.sh $ENV_NAME <versión> (RUNBOOK §3)"
