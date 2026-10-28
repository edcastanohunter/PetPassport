#!/usr/bin/env bash
# Entorno de desarrollo con Podman rootless + Quadlet (docs/phase-1/09 §2, ADR-026).
#
#   dev-quadlet.sh up            renderiza infra/quadlet/dev, la instala y arranca todo
#   dev-quadlet.sh down          detiene los servicios (conserva volúmenes)
#   dev-quadlet.sh reset         down + borra volúmenes, red e imagen local
#   dev-quadlet.sh obs           arranca la observabilidad (otel, prometheus, grafana…)
#   dev-quadlet.sh migrate       relanza migrate up
#   dev-quadlet.sh export        respaldo de es.events contra el MinIO local (DAT-009)
#   dev-quadlet.sh logs [svc…]   journal de los servicios (por defecto api y frontend)
#   dev-quadlet.sh status        estado de las unidades
#
# Las unidades se instalan en ~/.config/containers/systemd/petpassport-dev y las
# gestiona `systemctl --user`: sin root, sin daemon y sin podman compose. En
# Windows/macOS el script se reejecuta dentro de la podman machine (que debe ser
# rootless: `podman machine set --rootful=false`).
set -euo pipefail

REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
UNIT_DIR=${XDG_CONFIG_HOME:-$HOME/.config}/containers/systemd/petpassport-dev
MIN_PODMAN=5.4.0
CORE=(traefik)
OBS=(otel-collector tempo prometheus alertmanager blackbox loki alloy grafana)

die() { echo "dev-quadlet: $*" >&2; exit 1; }
cmd=${1:-}
[[ -n $cmd ]] || die "uso: $0 up|down|reset|obs|migrate|export|logs [svc…]|status"
shift

# --- Fuera de Linux: reejecutar en la podman machine -------------------------
if [[ $(uname -s) != Linux ]]; then
  command -v podman >/dev/null || die "falta podman"
  case $REPO_DIR in
    /[a-zA-Z]/*) machine_dir=/mnt/$(tr '[:upper:]' '[:lower:]' <<<"${REPO_DIR:1:1}")${REPO_DIR:2} ;; # Git Bash: /c/… → /mnt/c/…
    *) machine_dir=$REPO_DIR ;; # macOS: la máquina monta $HOME en la misma ruta
  esac
  [[ $(podman machine inspect --format '{{.Rootful}}' 2>/dev/null) == false ]] ||
    die "la podman machine es rootful; ejecuta: podman machine stop && podman machine set --rootful=false && podman machine start"
  exec podman machine ssh "cd '$machine_dir' && infra/scripts/dev-quadlet.sh $cmd $*" </dev/null
fi

# --- Precondiciones ------------------------------------------------------------
[[ $EUID -ne 0 ]] || die "ejecutar como usuario normal (Podman rootless, ADR-026), no como root"
command -v podman >/dev/null || die "falta podman (README §Requisitos)"
v=$(podman version --format '{{.Client.Version}}')
[[ $(printf '%s\n%s\n' "$MIN_PODMAN" "$v" | sort -V | head -1) == "$MIN_PODMAN" ]] ||
  die "podman $v < $MIN_PODMAN (Notify=healthy, unidades .build)"
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
systemctl --user show-environment >/dev/null 2>&1 ||
  die "no hay sesión systemd de usuario (loginctl enable-linger $USER)"
QUADLET_BIN=
for q in /usr/libexec/podman/quadlet /usr/lib/podman/quadlet /usr/lib/systemd/user-generators/podman-user-generator; do
  [[ -x $q ]] && { QUADLET_BIN=$q; break; }
done
[[ -n $QUADLET_BIN ]] || die "no se encuentra el generador de Quadlet"

sc() { systemctl --user "$@"; }
units() { local s; for s in "$@"; do printf 'pp-dev-%s.service
' "$s"; done; }
installed() { sc list-units 'pp-dev-*' --all --plain --no-legend | awk '{print $1}'; }

install_units() {
  local port
  port=$(sysctl -n net.ipv4.ip_unprivileged_port_start)
  ((port <= 80)) || die "rootless no puede publicar 80/443: sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80 (y persístelo en /etc/sysctl.d; en la podman machine: podman machine ssh sudo …)"
  [[ -f $REPO_DIR/infra/traefik/certs/pp.localhost.pem ]] || die "faltan certificados locales: make dev-certs"
  mkdir -p "$UNIT_DIR"
  local f out
  for f in "$REPO_DIR"/infra/quadlet/dev/pp-dev*; do
    out=$UNIT_DIR/${f##*/}
    sed "s|@REPO_DIR@|$REPO_DIR|g" "$f" >"$out.tmp"
    if cmp -s "$out.tmp" "$out"; then rm "$out.tmp"; else mv "$out.tmp" "$out"; fi
  done
  # Unidades retiradas del repo.
  for f in "$UNIT_DIR"/pp-dev*; do
    [[ -e $REPO_DIR/infra/quadlet/dev/${f##*/} ]] || rm -f "$f"
  done
  QUADLET_UNIT_DIRS=$UNIT_DIR "$QUADLET_BIN" -user -dryrun >/dev/null 2>"$UNIT_DIR/../petpassport-dev.log" ||
    { cat "$UNIT_DIR/../petpassport-dev.log" >&2; die "Quadlet rechaza las unidades"; }
  sc daemon-reload
}

# Las descargas grandes (Keycloak) superan el TimeoutStartSec por defecto si
# ocurren dentro de la unidad: se hacen antes, con progreso visible.
pull_images() {
  grep -h '^Image=' "$UNIT_DIR"/*.container | cut -d= -f2- | grep -v '\.build$' | sort -u |
    while read -r image; do
      podman image exists "$image" || podman pull "$image" >/dev/null
    done
}

case $cmd in
  up)
    install_units
    pull_images
    echo "arrancando (la primera vez construye la imagen del backend y compila; puede tardar)…"
    # traefik arrastra api, frontend y keycloak; api, migrate/realm/buckets/postgres.
    mapfile -t todo < <(units "${CORE[@]}")
    sc start "${todo[@]}"
    echo "app: https://app.pp.localhost  api: https://api.pp.localhost  auth: https://auth.pp.localhost  mail: http://localhost:8025"
    ;;
  obs)
    install_units
    pull_images
    mapfile -t todo < <(units "${OBS[@]}")
    sc start "${todo[@]}"
    echo "grafana: http://localhost:3000  prometheus: http://localhost:9091"
    ;;
  down)
    mapfile -t running < <(installed)
    ((${#running[@]})) && sc stop "${running[@]}"
    sc reset-failed 'pp-dev-*' 2>/dev/null || true
    ;;
  reset)
    "$0" down
    podman volume ls --format '{{.Name}}' | grep '^pp-dev-' | xargs -r podman volume rm
    podman network rm -f pp-dev >/dev/null 2>&1 || true
    podman rmi -f localhost/petpassport-backend:dev >/dev/null 2>&1 || true
    ;;
  migrate)
    install_units
    sc restart pp-dev-migrate.service
    ;;
  export)
    install_units
    sc start pp-dev-events-export.service
    ;;
  logs)
    (($#)) || set -- api frontend
    args=()
    for s in "$@"; do args+=(-u "pp-dev-$s.service"); done
    exec journalctl --user -f -o cat "${args[@]}"
    ;;
  status)
    sc list-units 'pp-dev-*' --all --no-pager
    ;;
  *) die "comando desconocido: $cmd" ;;
esac
