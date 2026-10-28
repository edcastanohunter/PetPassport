#!/usr/bin/env bash
# Gate de cobertura de dominio (PP-F2-QA-007): `internal/<ctx>/domain` ≥ 90 %
# en los contextos de F2. Un contexto sin paquete domain/ (aún sin agregados)
# se omite con aviso; en cuanto el hito lo crea, el gate aplica.
#
#   scripts/domain-coverage.sh [umbral]      # desde la raíz del repo
set -euo pipefail

THRESHOLD="${1:-90}"
CONTEXTS=(clinical scheduling notifications)

cd "$(dirname "$0")/../backend"
fail=0
for ctx in "${CONTEXTS[@]}"; do
  dir="internal/$ctx/domain"
  if ! compgen -G "$dir/*.go" >/dev/null; then
    echo "::notice::$dir no existe todavía; gate omitido"
    continue
  fi
  profile="$(mktemp)"
  go test -count=1 -coverprofile="$profile" "./$dir/..." >/dev/null
  pct="$(go tool cover -func="$profile" | awk '/^total:/ { sub("%", "", $3); print $3 }')"
  rm -f "$profile"
  if awk -v p="$pct" -v t="$THRESHOLD" 'BEGIN { exit !(p + 0 < t + 0) }'; then
    echo "::error::$dir cobertura ${pct}% < ${THRESHOLD}%"
    fail=1
  else
    echo "$dir cobertura ${pct}% (≥ ${THRESHOLD}%)"
  fi
done
exit "$fail"
