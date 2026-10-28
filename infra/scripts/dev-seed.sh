#!/usr/bin/env bash
# Semillas de desarrollo (docs/phase-1/09 §2): usuarios en Keycloak vía Admin API.
# Las mascotas y organizaciones se siembran por la API de negocio desde M1.2/M1.3.
# Uso: infra/scripts/dev-seed.sh [keycloak_url] (por defecto http://localhost:8080; con make dev-seed, el 8180 publicado)
set -euo pipefail

KC_URL="${1:-http://localhost:8080}"
KC_ADMIN_USER="${KC_ADMIN_USER:-admin}"
KC_ADMIN_PASSWORD="${KC_ADMIN_PASSWORD:-admin}"
REALM=petpassport
SEED_PASSWORD="${SEED_PASSWORD:-Password-Dev-2026}"

token() {
  curl -fsS -X POST "$KC_URL/realms/master/protocol/openid-connect/token" \
    -d grant_type=password -d client_id=admin-cli \
    -d "username=$KC_ADMIN_USER" -d "password=$KC_ADMIN_PASSWORD" | sed -E 's/.*"access_token":"([^"]+)".*/\1/'
}

TOKEN=$(token)
auth=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

ensure_user() { # email first last role
  local email=$1 first=$2 last=$3 role=${4:-}
  local existing
  existing=$(curl -fsS "${auth[@]}" "$KC_URL/admin/realms/$REALM/users?email=$email&exact=true")
  local id
  if [ "$existing" = "[]" ]; then
    curl -fsS "${auth[@]}" -X POST "$KC_URL/admin/realms/$REALM/users" -d "{
      \"username\": \"$email\", \"email\": \"$email\", \"emailVerified\": true, \"enabled\": true,
      \"firstName\": \"$first\", \"lastName\": \"$last\",
      \"credentials\": [{\"type\": \"password\", \"value\": \"$SEED_PASSWORD\", \"temporary\": false}]
    }" >/dev/null
    echo "creado   $email"
  else
    echo "existe   $email"
  fi
  id=$(curl -fsS "${auth[@]}" "$KC_URL/admin/realms/$REALM/users?email=$email&exact=true" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  if [ -n "$role" ]; then
    local roleJson
    roleJson=$(curl -fsS "${auth[@]}" "$KC_URL/admin/realms/$REALM/roles/$role")
    curl -fsS "${auth[@]}" -X POST "$KC_URL/admin/realms/$REALM/users/$id/role-mappings/realm" -d "[$roleJson]" >/dev/null
    echo "rol      $email -> $role"
  fi
}

ensure_user owner@dev.local Ana Tutora
ensure_user vet@dev.local Carlos Veterinario
ensure_user admin@dev.local Platform Admin super_admin

echo "Contraseña de las cuentas semilla: $SEED_PASSWORD"
