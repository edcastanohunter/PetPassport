# Rangos de Cloudflare (uno por línea). Lo usan host-setup.sh (nftables) y
# deploy.sh (trustedIPs de Traefik): se refrescan si es posible; si no, la
# lista del repo (infra/waf/cloudflare-ips.txt). Falla si el formato no cuadra.
# shellcheck shell=bash
cf_ips() {
  local repo_dir=$1 fresh list
  if fresh=$( { curl -fsS --max-time 5 https://www.cloudflare.com/ips-v4 && echo &&
               curl -fsS --max-time 5 https://www.cloudflare.com/ips-v6; } 2>/dev/null ) &&
     [[ $(grep -c '/' <<<"$fresh") -ge 10 ]]; then
    list=$(grep '/' <<<"$fresh")
  else
    list=$(grep -v '^#' "$repo_dir/infra/waf/cloudflare-ips.txt" | grep '/')
  fi
  grep -Eqv '^[0-9a-fA-F:./]+$' <<<"$list" && { echo "rangos de Cloudflare con formato inesperado" >&2; return 1; }
  printf '%s\n' "$list"
}
