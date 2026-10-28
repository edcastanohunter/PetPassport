// Gate de carga de F2 (PP-F2-QA-007): 100 VUs con mezcla 70 % lecturas /
// 30 % comandos contra staging; p95 API < 300 ms y p95 del retraso de las
// proyecciones asíncronas < 2 s, con el worker procesando la carga.
//
//   K6_DOMAIN=staging.<dominio> K6_USERS='a@x:pw,b@x:pw' \
//   K6_PROMETHEUS=http://localhost:9091 k6 run tests/load/k6/f2-mix.js
//
// El retraso se lee de `projection_lag_seconds{name}` en el Prometheus de
// staging (raspa el worker cada 15 s; 127.0.0.1:9091 en la VM, por túnel
// SSH: ver README). Los comandos son ediciones de perfil
// de las mascotas de los usuarios de carga (generan eventos que el worker
// debe consumir); un 409 por edición concurrente de la misma mascota es un
// resultado válido y se cuenta aparte.
import http from 'k6/http'
import { check, fail, sleep } from 'k6'
import { Counter, Trend } from 'k6/metrics'
import { endpoints, fresh, login, parseUsers } from './lib/session.js'

const DOMAIN = __ENV.K6_DOMAIN || fail('K6_DOMAIN requerido')
const USERS = parseUsers(__ENV.K6_USERS || fail('K6_USERS requerido (email:password,...)'))
const PROMETHEUS = __ENV.K6_PROMETHEUS || fail('K6_PROMETHEUS requerido (sin él no hay gate de lag)')
const EP = endpoints(DOMAIN)

const server5xx = new Counter('server_errors_5xx')
const rateLimited = new Counter('rate_limited_429')
const versionConflicts = new Counter('version_conflicts_409')
const lag = new Trend('projection_lag', true) // ms, por proyección (tag name)
const lagSamples = new Counter('projection_lag_samples')

const STEADY = __ENV.K6_DURATION || '5m'

export const options = {
  scenarios: {
    api: {
      executor: 'ramping-vus',
      exec: 'mix',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 100 },
        { duration: STEADY, target: 100 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
    lag: {
      executor: 'constant-vus',
      exec: 'sampleLag',
      vus: 1,
      duration: `${parseDuration(STEADY) + 60}s`,
    },
  },
  thresholds: {
    'http_req_duration{kind:api}': ['p(95)<300'],
    projection_lag: ['p(95)<2000'],
    // Sin muestras el gate de lag pasaría en vacío (1 cada 5 s durante la carga).
    projection_lag_samples: ['count>30'],
    server_errors_5xx: ['count==0'],
    rate_limited_429: ['count==0'],
    checks: ['rate>0.99'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

function parseDuration(d) {
  const m = /^(\d+)(s|m|h)$/.exec(d)
  if (!m) fail(`K6_DURATION inválido: ${d} (p. ej. 5m)`)
  return Number(m[1]) * { s: 1, m: 60, h: 3600 }[m[2]]
}

export function setup() {
  return { sessions: USERS.map((u) => login(EP, u)) }
}

let session

function request(method, path, name, body, headers = {}) {
  const res = http.request(method, `${EP.api}${path}`, body === undefined ? null : JSON.stringify(body), {
    headers: { Authorization: `Bearer ${session.access}`, 'Content-Type': 'application/json', ...headers },
    tags: { kind: 'api', name },
  })
  if (res.status >= 500) server5xx.add(1, { name })
  if (res.status === 429) rateLimited.add(1, { name })
  return res
}

function read(path, name) {
  const res = request('GET', path, name)
  check(res, { [`${name} 200`]: (r) => r.status === 200 })
  return res
}

// 70 % lecturas (las de la SPA) / 30 % comandos (PATCH de perfil con If-Match).
export function mix(data) {
  if (!session) session = data.sessions[(__VU - 1) % data.sessions.length]
  session = fresh(EP, session)

  const pets = read('/pets', 'GET /pets')
  const items = pets.status === 200 ? pets.json('data') || [] : []
  if (!items.length) fail('usuario de carga sin mascotas (precondición del README)')
  const pet = items[(__VU + __ITER) % items.length]

  if (Math.random() < 0.3) {
    const current = read(`/pets/${pet.id}`, 'GET /pets/{id}')
    const etag = current.headers.Etag || current.headers.ETag
    const res = request('PATCH', `/pets/${pet.id}`, 'PATCH /pets/{id}', { breed: `k6-${__VU}-${__ITER}` }, {
      'If-Match': (etag || '').replace(/"/g, ''),
    })
    if (res.status === 409) versionConflicts.add(1)
    check(res, { 'PATCH /pets/{id} 200|409': (r) => r.status === 200 || r.status === 409 })
  } else {
    read('/me', 'GET /me')
    read(`/pets/${pet.id}`, 'GET /pets/{id}')
  }
  sleep(1)
}

// Muestrea cada 5 s el retraso de cada proyección asíncrona (consulta
// instantánea a Prometheus; el gauge se actualiza en cada scrape).
export function sampleLag() {
  const res = http.get(`${PROMETHEUS}/api/v1/query?query=projection_lag_seconds`, {
    tags: { kind: 'prometheus' },
  })
  if (res.status !== 200) fail(`Prometheus → ${res.status} (¿túnel SSH?)`)
  for (const r of res.json('data.result') || []) {
    lag.add(Number(r.value[1]) * 1000, { name: r.metric.name })
    lagSamples.add(1)
  }
  sleep(5)
}

export function handleSummary(data) {
  const m = data.metrics
  const api = m['http_req_duration{kind:api}']
  const v = (metric, stat) => (metric && metric.values[stat] !== undefined ? metric.values[stat] : 0)
  const ok = Object.values(data.metrics).every(
    (metric) => !metric.thresholds || Object.values(metric.thresholds).every((t) => t.ok),
  )
  const md = [
    `### k6 F2 — mezcla 70/30 en staging (${DOMAIN})`,
    '',
    '| Métrica | Valor | Umbral |',
    '|---|---|---|',
    `| VUs máx. | ${v(m.vus_max, 'max')} | 100 |`,
    `| Peticiones API | ${v(m.http_reqs, 'count')} | |`,
    `| p95 API | ${v(api, 'p(95)').toFixed(1)} ms | < 300 ms |`,
    `| p95 lag de proyección | ${v(m.projection_lag, 'p(95)').toFixed(0)} ms | < 2000 ms |`,
    `| máx. lag de proyección | ${v(m.projection_lag, 'max').toFixed(0)} ms | |`,
    `| muestras de lag | ${v(m.projection_lag_samples, 'count')} | > 30 |`,
    `| 409 (edición concurrente) | ${v(m.version_conflicts_409, 'count')} | informativo |`,
    `| 5xx | ${v(m.server_errors_5xx, 'count')} | 0 |`,
    `| 429 | ${v(m.rate_limited_429, 'count')} | 0 |`,
    `| checks OK | ${(v(m.checks, 'rate') * 100).toFixed(2)} % | > 99 % |`,
    '',
    `**Resultado: ${ok ? 'PASA' : 'NO PASA'}**`,
    '',
  ].join('\n')
  return {
    stdout: md,
    'tests/load/k6/report-f2.md': md,
    'tests/load/k6/summary-f2.json': JSON.stringify(data, null, 2),
  }
}
