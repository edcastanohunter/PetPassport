import http from 'k6/http'
import crypto from 'k6/crypto'
import encoding from 'k6/encoding'
import { fail } from 'k6'

const CLIENT_ID = 'petpassport-web'

export function endpoints(domain) {
  return {
    api: `https://api.${domain}/api/v1`,
    auth: `https://auth.${domain}/realms/petpassport/protocol/openid-connect`,
    redirect: `https://app.${domain}/auth/callback`,
  }
}

export function parseUsers(raw) {
  if (!raw) return []
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
    fail(`login sin code para ${user.username}`)
  }
  return tokenRequest(ep, {
    grant_type: 'authorization_code',
    client_id: CLIENT_ID,
    code,
    redirect_uri: ep.redirect,
    code_verifier: verifier,
  })
}

export function authHeaders(session, allowlistKey = 'load-test-nightly') {
  return {
    Authorization: `Bearer ${session.access_token}`,
    'Content-Type': 'application/json',
    'X-RateLimit-Allowlist': allowlistKey,
    'X-Allowlist-Rate-Limit': allowlistKey,
  }
}

function tokenRequest(ep, params) {
  const res = http.post(
    `${ep.auth}/token`,
    Object.entries(params)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
      .join('&'),
    {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      tags: { kind: 'auth' },
    },
  )
  if (res.status !== 200) {
    fail(`token endpoint error ${res.status}: ${res.body}`)
  }
  const body = JSON.parse(res.body)
  return {
    access_token: body.access_token,
    refresh_token: body.refresh_token,
    expires_at: Date.now() + (body.expires_in - 30) * 1000,
  }
}

export function fresh(ep, session) {
  if (Date.now() < session.expires_at) return session
  return tokenRequest(ep, {
    grant_type: 'refresh_token',
    client_id: CLIENT_ID,
    refresh_token: session.refresh_token,
  })
}
