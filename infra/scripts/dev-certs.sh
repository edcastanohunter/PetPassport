#!/usr/bin/env bash
# Genera certificados locales de confianza con mkcert para *.pp.localhost (docs/phase-1/09 §2)
# y la CA de clientes mTLS del borde B2B (PP-F4-INF-001). Nada de esto se versiona (.gitignore).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../traefik" && pwd)"

cd "$ROOT/certs"
if ! command -v mkcert >/dev/null; then
  echo "mkcert no está instalado: https://github.com/FiloSottile/mkcert" >&2
  exit 1
fi
mkcert -install
mkcert -cert-file pp.localhost.pem -key-file pp.localhost-key.pem "pp.localhost" "*.pp.localhost" localhost 127.0.0.1
echo "certificados generados en infra/traefik/certs/"

cd "$ROOT/clients-ca"
if [[ -f client-ca.pem && -f client-ca-key.pem ]]; then
  echo "CA de clientes mTLS ya existe en infra/traefik/clients-ca/ (bórrala para regenerarla)"
  exit 0
fi
if ! command -v openssl >/dev/null; then
  echo "openssl no está instalado: necesario para la CA de clientes mTLS" >&2
  exit 1
fi
openssl genrsa -out client-ca-key.pem 4096 2>/dev/null
chmod 600 client-ca-key.pem
MSYS_NO_PATHCONV=1 openssl req -x509 -new -nodes -key client-ca-key.pem -sha256 -days 1825 \
  -out client-ca.pem \
  -subj "/C=CO/ST=Bogota/O=PetPassport/OU=Partners/CN=PetPassport Partner Client CA (dev)"
echo "CA de clientes mTLS generada en infra/traefik/clients-ca/"
