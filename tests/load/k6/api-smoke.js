// Prueba de carga ligera de la API (PP-F1 criterio de salida 5, QA-006):
// 50 VUs durante 5 min contra staging; p95 < 300 ms y 0 respuestas 5xx.
//
//   K6_DOMAIN=staging.<dominio> K6_USERS='a@x:pw,b@x:pw' k6 run tests/load/k6/api-smoke.js
//
// Login con authorization code + PKCE como la SPA (el realm no tiene direct
// grants), igual que infra/deploy/smoke.sh. Cada VU usa un usuario de K6_USERS
// (round-robin) y renueva su token con el refresh token antes de que caduque
// (accessTokenLifespan 300 s). Precondiciones y lectura del informe: README.md.
import http from 'k6/http'
import crypto from 'k6/crypto'
import encoding from 'k6/encoding'
import { check, fail, sleep } from 'k6'
import { Counter } from 'k6/metrics'

const DOMAIN = __ENV.K6_DOMAIN || fail('K6_DOMAIN requerido')
const USERS = (__ENV.K6_USERS || fail('K6_USERS requerido (email:password,...)'))
  .split(',')
  .map((pair) => {
    const i = pair.indexOf(':')
    return { username: pair.slice(0, i), password: pair.slice(i + 1) }
  })
const API = `https://api.${DOMAIN}/api/v1`
const AUTH = `https://auth.${DOMAIN}/realms/petpassport/protocol/openid-connect`
const REDIRECT = `https://app.${DOMAIN}/auth/callback`
const CLIENT_ID = 'petpassport-web'

const server5xx = new Counter('server_errors_5xx')
const rateLimited = new Counter('rate_limited_429')

export const options = {
  scenarios: {
    api: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50 },
        { duration: '5m', target: 50 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    // Umbrales de docs/phase-1/10 §6 (QA-006).
    'http_req_duration{kind:api}': ['p(95)<300'],
    server_errors_5xx: ['count==0'],
    // Un 429 invalida la medición (límite del WAF/Traefik sin excepción): ver README.
    rate_limited_429: ['count==0'],
    checks: ['rate>0.99'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

function b64url(buf) {
  return encoding.b64encode(buf, 'rawurl')
}

function login(user) {
  const verifier = b64url(crypto.randomBytes(48))
  const challenge = b64url(crypto.sha256(verifier, 'binary'))
  const state = b64url(crypto.randomBytes(16))
  const page = http.get(
    `${AUTH}/auth?client_id=${CLIENT_ID}&response_type=code&scope=openid` +
      `&redirect_uri=${encodeURIComponent(REDIRECT)}&code_challenge=${challenge}` +
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
  if (!location.startsWith(REDIRECT) || !code) {
    fail(`login sin code para ${user.username} (¿credenciales, OTP u onboarding?)`)
  }
  return tokenRequest({
    grant_type: 'authorization_code',
    client_id: CLIENT_ID,
    code,
    redirect_uri: REDIRECT,
    code_verifier: verifier,
  })
}

function tokenRequest(body) {
  const res = http.post(`${AUTH}/token`, body, { tags: { kind: 'auth' } })
  if (res.status !== 200) fail(`token endpoint → ${res.status}`)
  const t = res.json()
  return { access: t.access_token, refresh: t.refresh_token, at: Date.now() }
}

export function setup() {
  // Un login por usuario: los VUs comparten la sesión de su usuario.
  return { sessions: USERS.map(login) }
}

// Estado por VU (cada VU es un runtime JS aislado).
let session

function apiGet(path, name) {
  const res = http.get(`${API}${path}`, {
    headers: { Authorization: `Bearer ${session.access}` },
    tags: { kind: 'api', name },
  })
  if (res.status >= 500) server5xx.add(1, { name })
  if (res.status === 429) rateLimited.add(1, { name })
  check(res, { [`${name} 200`]: (r) => r.status === 200 })
  return res
}

export default function (data) {
  if (!session) session = data.sessions[(__VU - 1) % data.sessions.length]
  if (Date.now() - session.at > 240_000) {
    session = tokenRequest({
      grant_type: 'refresh_token',
      client_id: CLIENT_ID,
      refresh_token: session.refresh,
    })
  }

  // Lecturas más frecuentes de la SPA tras el login (FE-005/FE-006).
  apiGet('/me', 'GET /me')
  const pets = apiGet('/pets', 'GET /pets')
  const items = pets.status === 200 ? pets.json('data') || [] : []
  if (items.length) apiGet(`/pets/${items[__ITER % items.length].id}`, 'GET /pets/{id}')
  apiGet('/legal/current', 'GET /legal/current')
  sleep(1)
}

export function handleSummary(data) {
  const m = data.metrics
  const api = m['http_req_duration{kind:api}']
  const v = (metric, stat) => (metric && metric.values[stat] !== undefined ? metric.values[stat] : 0)
  const ok = Object.values(data.metrics).every(
    (metric) => !metric.thresholds || Object.values(metric.thresholds).every((t) => t.ok),
  )
  const md = [
    `### k6 — API staging (${DOMAIN})`,
    '',
    '| Métrica | Valor | Umbral |',
    '|---|---|---|',
    `| VUs máx. | ${v(m.vus_max, 'max')} | 50 |`,
    `| Peticiones API | ${v(m.http_reqs, 'count')} | |`,
    `| p95 API | ${v(api, 'p(95)').toFixed(1)} ms | < 300 ms |`,
    `| p99 API | ${v(api, 'p(99)').toFixed(1)} ms | |`,
    `| 5xx | ${v(m.server_errors_5xx, 'count')} | 0 |`,
    `| 429 | ${v(m.rate_limited_429, 'count')} | 0 |`,
    `| checks OK | ${(v(m.checks, 'rate') * 100).toFixed(2)} % | > 99 % |`,
    '',
    `**Resultado: ${ok ? 'PASA' : 'NO PASA'}**`,
    '',
  ].join('\n')
  return {
    stdout: md,
    'tests/load/k6/report.md': md,
    'tests/load/k6/summary.json': JSON.stringify(data, null, 2),
  }
}
