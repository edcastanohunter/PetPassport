// Sesión de los usuarios de carga (compartido por api-smoke.js y f2-mix.js).
// Login con authorization code + PKCE como la SPA (el realm no tiene direct
// grants), igual que infra/deploy/smoke.sh; renovación con el refresh token
// antes de que caduque (accessTokenLifespan 300 s).
import http from 'k6/http'
import crypto from 'k6/crypto'
import encoding from 'k6/encoding'
import { fail } from 'k6'

const CLIENT_ID = 'petpassport-web'

/** URLs de staging a partir de K6_DOMAIN. */
export function endpoints(domain) {
  return {
    api: `https://api.${domain}/api/v1`,
    auth: `https://auth.${domain}/realms/petpassport/protocol/openid-connect`,
    redirect: `https://app.${domain}/auth/callback`,
  }
}

/** K6_USERS = 'email:password,email:password' (la contraseña puede contener ':'). */
export function parseUsers(raw) {
  return raw.split(',').map((pair) => {
    const i = pair.indexOf(':')
    return { username: pair.slice(0, i), password: pair.slice(i + 1) }
  })
}

function b64url(buf) {
  return encoding.b64encode(buf, 'rawurl')
}

export function login(ep, user) {
  const verifier = b64url(crypto.randomBytes(48))
  const challenge = b64url(crypto.sha256(verifier, 'binary'))
  const state = b64url(crypto.randomBytes(16))
  const page = http.get(
    `${ep.auth}/auth?client_id=${CLIENT_ID}&response_type=code&scope=openid` +
      `&redirect_uri=${encodeURIComponent(ep.redirect)}&code_challenge=${challenge}` +
      `&code_challenge_method=S256&state=${state}`,
    { tags: { kind: 'auth' } },
  )
  const action = page.html().find('#kc-form-login').attr('action')
  if (!action) fail(`formulario de login no encontrado para ${user.username}`)
  const res = http.post(
    action.replace(/&amp;/g, '&'),
    { username: user.username, password: user.password },
    { redirects: 0, tags: { kind: 'auth' } },
  )
  const location = res.headers.Location || ''
  const code = (location.match(/[?&]code=([^&]+)/) || [])[1]
  if (!location.startsWith(ep.redirect) || !code) {
    fail(`login sin code para ${user.username} (¿credenciales, OTP u onboarding?)`)
  }
  return tokenRequest(ep, {
    grant_type: 'authorization_code',
    client_id: CLIENT_ID,
    code,
    redirect_uri: ep.redirect,
    code_verifier: verifier,
  })
}

function tokenRequest(ep, body) {
  const res = http.post(`${ep.auth}/token`, body, { tags: { kind: 'auth' } })
  if (res.status !== 200) fail(`token endpoint → ${res.status}`)
  const t = res.json()
  return { access: t.access_token, refresh: t.refresh_token, at: Date.now() }
}

/** Devuelve la sesión renovada si tiene más de 4 min (el access token dura 5). */
export function fresh(ep, session) {
  if (Date.now() - session.at <= 240_000) return session
  return tokenRequest(ep, { grant_type: 'refresh_token', client_id: CLIENT_ID, refresh_token: session.refresh })
}
