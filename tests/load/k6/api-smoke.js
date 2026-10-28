// Prueba de carga ligera de la API (PP-F1 criterio de salida 5, QA-006):
// 50 VUs durante 5 min contra staging; p95 < 300 ms y 0 respuestas 5xx.
//
//   K6_DOMAIN=staging.<dominio> K6_USERS='a@x:pw,b@x:pw' k6 run tests/load/k6/api-smoke.js
//
// Login PKCE y renovación de token en lib/session.js. Cada VU usa un usuario
// de K6_USERS (round-robin). Precondiciones y lectura del informe: README.md.
import http from 'k6/http'
import { check, fail, sleep } from 'k6'
import { Counter } from 'k6/metrics'
import { endpoints, fresh, login, parseUsers } from './lib/session.js'

const DOMAIN = __ENV.K6_DOMAIN || fail('K6_DOMAIN requerido')
const USERS = parseUsers(__ENV.K6_USERS || fail('K6_USERS requerido (email:password,...)'))
const EP = endpoints(DOMAIN)
const API = EP.api

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

export function setup() {
  // Un login por usuario: los VUs comparten la sesión de su usuario.
  return { sessions: USERS.map((u) => login(EP, u)) }
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
  session = fresh(EP, session)

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
