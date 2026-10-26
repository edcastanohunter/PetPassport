#!/usr/bin/env bash
# Smoke test post-despliegue (PP-F1-INF-006): smoke.sh <dominio>
#
# Recorre el camino real de un usuario a través del WAF:
#   1. GET https://api.<dominio>/api/v1/healthz              → 200
#   2. login del usuario sintético con authorization code + PKCE
#      (mismo flujo que la SPA; no requiere direct grants en el realm)
#   3. GET /api/v1/me con el access token                    → 200
#   4. (si SMOKE_ORIGIN_IP) conexión directa al origen sin la cabecera del
#      WAF                                                    → rechazada
#
# Variables: SMOKE_USER, SMOKE_PASSWORD (usuario sintético pet_owner, sin OTP,
# onboarding hecho; RUNBOOK §2). Requiere curl, jq y openssl.
set -euo pipefail

DOMAIN=${1:?uso: smoke.sh <dominio>}
: "${SMOKE_USER:?SMOKE_USER}" "${SMOKE_PASSWORD:?SMOKE_PASSWORD}"
API=https://api.$DOMAIN
AUTH=https://auth.$DOMAIN/realms/petpassport
REDIRECT=https://app.$DOMAIN/auth/callback

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
JAR=$WORK/cookies

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }
ok() { echo "SMOKE ok: $*"; }
curl_() { curl -sS --max-time 15 --retry 2 --retry-connrefused "$@"; }

# 1. Liveness pública a través del WAF.
code=$(curl_ -o /dev/null -w '%{http_code}' "$API/api/v1/healthz") || fail "healthz inalcanzable"
[[ $code == 200 ]] || fail "healthz → $code"
ok "healthz"

# 2. Login PKCE.
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
verifier=$(openssl rand 48 | b64url)
challenge=$(printf '%s' "$verifier" | openssl dgst -sha256 -binary | b64url)
state=$(openssl rand 16 | b64url)

login_page=$(curl_ -c "$JAR" -b "$JAR" -G "$AUTH/protocol/openid-connect/auth" \
  --data-urlencode client_id=petpassport-web \
  --data-urlencode response_type=code \
  --data-urlencode scope=openid \
  --data-urlencode "redirect_uri=$REDIRECT" \
  --data-urlencode "code_challenge=$challenge" \
  --data-urlencode code_challenge_method=S256 \
  --data-urlencode "state=$state") || fail "página de login inalcanzable"
action=$(grep -o 'id="kc-form-login"[^>]*action="[^"]*"' <<<"$login_page" | sed 's/.*action="\([^"]*\)".*/\1/' |
  sed 's/&amp;/\&/g')
[[ -n $action ]] || action=$(grep -o 'action="[^"]*login-actions/authenticate[^"]*"' <<<"$login_page" |
  head -1 | sed 's/action="\([^"]*\)"/\1/; s/&amp;/\&/g')
[[ $action == https://auth.$DOMAIN/* ]] || fail "formulario de login no encontrado"

location=$(curl_ -c "$JAR" -b "$JAR" -o /dev/null -w '%{redirect_url}' \
  --data-urlencode "username=$SMOKE_USER" --data-urlencode "password=$SMOKE_PASSWORD" \
  "$action") || fail "POST de credenciales"
[[ $location == "$REDIRECT"* ]] || fail "login sin redirección a la app (¿credenciales u OTP?)"
auth_code=$(sed -n 's/.*[?&]code=\([^&]*\).*/\1/p' <<<"$location")
returned_state=$(sed -n 's/.*[?&]state=\([^&]*\).*/\1/p' <<<"$location")
[[ -n $auth_code && $returned_state == "$state" ]] || fail "redirección sin code/state válido"

token=$(curl_ "$AUTH/protocol/openid-connect/token" \
  --data-urlencode grant_type=authorization_code \
  --data-urlencode client_id=petpassport-web \
  --data-urlencode "code=$auth_code" \
  --data-urlencode "redirect_uri=$REDIRECT" \
  --data-urlencode "code_verifier=$verifier" | jq -r '.access_token // empty') || fail "token endpoint"
[[ -n $token ]] || fail "sin access_token"
ok "login PKCE"

# 3. /me con el token (valida JWKS, iss, aud y la BD).
code=$(curl_ -o "$WORK/me.json" -w '%{http_code}' -H "Authorization: Bearer $token" "$API/api/v1/me") ||
  fail "/me inalcanzable"
[[ $code == 200 ]] || fail "/me → $code"
jq -e '.id' "$WORK/me.json" >/dev/null || fail "/me sin id"
ok "GET /me"

# 4. El origen no responde sin pasar por el WAF (DoD M1.4).
if [[ -n ${SMOKE_ORIGIN_IP:-} ]]; then
  code=$(curl -sk --max-time 5 -o /dev/null -w '%{http_code}' \
    --resolve "api.$DOMAIN:443:$SMOKE_ORIGIN_IP" "$API/api/v1/healthz" || true)
  [[ $code == 000 || $code == 404 ]] || fail "el origen respondió $code sin X-Origin-Auth"
  ok "origen cerrado (directo → $code)"
fi
echo "SMOKE OK"
