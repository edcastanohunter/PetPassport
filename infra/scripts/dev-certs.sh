#!/usr/bin/env bash
# Genera certificados locales de confianza con mkcert para *.pp.localhost (docs/phase-1/09 §2).
set -euo pipefail
cd "$(dirname "$0")/../traefik/certs"
if ! command -v mkcert >/dev/null; then
  echo "mkcert no está instalado: https://github.com/FiloSottile/mkcert" >&2
  exit 1
fi
mkcert -install
mkcert -cert-file pp.localhost.pem -key-file pp.localhost-key.pem "pp.localhost" "*.pp.localhost" localhost 127.0.0.1
echo "certificados generados en infra/traefik/certs/"
