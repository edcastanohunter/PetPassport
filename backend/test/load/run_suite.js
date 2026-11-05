// Suite de pruebas de carga F3 M3.3 (PP-F3-PRF-003)
// Ejecución: K6_DOMAIN=staging.<domain> K6_USERS='...' k6 run backend/test/load/run_suite.js
import http from 'k6/http'
import { check, fail, sleep } from 'k6'
import { Trend, Counter } from 'k6/metrics'
import { endpoints, fresh, login, parseUsers } from './lib/session.js'
import { runOwnerBrowse } from './scenarios/owner_browse.js'
import { runClinicDay } from './scenarios/clinic_day.js'
import { runSearchHeavy } from './scenarios/search_heavy.js'
import { runSpike, spikeStages } from './scenarios/spike.js'
import { runSoak, soakStages } from './scenarios/soak.js'

const DOMAIN = __ENV.K6_DOMAIN || 'staging.petpassport.local'
const RAW_USERS = __ENV.K6_USERS || ''
const PROMETHEUS = __ENV.K6_PROMETHEUS || ''
const SCENARIO_MODE = __ENV.K6_SCENARIO || 'nominal' // nominal | spike | soak

const EP = endpoints(DOMAIN)

// Métricas de SLO
const projectionLagTrend = new Trend('projection_lag', true)
const searchIndexLagTrend = new Trend('search_index_lag', true)

function getScenariosConfig() {
  if (SCENARIO_MODE === 'spike') {
    return {
      spike_test: {
        executor: 'ramping-vus',
        exec: 'execSpike',
        startVUs: 0,
        stages: spikeStages,
        gracefulRampDown: '15s',
      },
    }
  }

  if (SCENARIO_MODE === 'soak') {
    return {
      soak_test: {
        executor: 'ramping-vus',
        exec: 'execSoak',
        startVUs: 0,
        stages: soakStages,
        gracefulRampDown: '30s',
      },
    }
  }

  // Modo nominal (100 req/s, 60% browse, 30% clinic, 10% search)
  const duration = __ENV.K6_DURATION || '5m'
  return {
    owner_browse: {
      executor: 'ramping-vus',
      exec: 'execOwnerBrowse',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 60 },
        { duration, target: 60 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
    clinic_day: {
      executor: 'ramping-vus',
      exec: 'execClinicDay',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 30 },
        { duration, target: 30 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
    search_heavy: {
      executor: 'ramping-vus',
      exec: 'execSearchHeavy',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },
        { duration, target: 10 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
    sample_lag: {
      executor: 'constant-vus',
      exec: 'execSampleLag',
      vus: 1,
      duration: '6m',
    },
  }
}

export const options = {
  scenarios: getScenariosConfig(),
  thresholds: {
    // SLO 1: Disponibilidad API (no-5xx >= 99.9%)
    http_req_failed: ['rate<0.001'],
    // SLO 2: Latencia API lectura (GET no búsqueda): p95 < 250 ms, p99 < 600 ms
    'http_req_duration{kind:api_read}': ['p(95)<250', 'p(99)<600'],
    // SLO 3: Latencia API comandos: p95 < 400 ms
    'http_req_duration{kind:api_command}': ['p(95)<400'],
    // SLO 4: Latencia búsqueda (/search/*): p95 < 300 ms
    'http_req_duration{kind:api_search}': ['p(95)<300'],
    // SLO 5: Latencia autocompletado (/suggest/*): p95 < 150 ms
    'http_req_duration{kind:api_suggest}': ['p(95)<150'],
    // SLO 6: Retraso de proyecciones asíncronas: p95 < 2 s
    projection_lag: ['p(95)<2000'],
    // SLO 7: Retraso de indexación: p95 < 5 s
    search_index_lag: ['p(95)<5000'],
    // Verificación general de checks
    checks: ['rate>0.99'],
  },
}

export function setup() {
  const users = parseUsers(RAW_USERS)
  const sessions = []

  if (users.length > 0) {
    for (const u of users) {
      sessions.push(login(EP, u))
    }
  } else {
    // Sesión de desarrollo simulada si no se pasan credenciales
    sessions.push({
      access_token: 'mock-k6-token',
      refresh_token: 'mock-k6-refresh',
      expires_at: Date.now() + 3600 * 1000,
    })
  }

  return {
    sessions,
    orgId: __ENV.K6_ORG_ID || '018f0000-0000-7000-a000-000000000001',
    petId: __ENV.K6_PET_ID || '018f0000-0000-7000-b000-000000000001',
  }
}

export function execOwnerBrowse(data) {
  runOwnerBrowse(EP, data.sessions, data)
}

export function execClinicDay(data) {
  runClinicDay(EP, data.sessions, data)
}

export function execSearchHeavy(data) {
  runSearchHeavy(EP, data.sessions, data)
}

export function execSpike(data) {
  runSpike(EP, data.sessions, data)
}

export function execSoak(data) {
  runSoak(EP, data.sessions, data)
}

export function execSampleLag() {
  if (!PROMETHEUS) {
    sleep(5)
    return
  }

  // Consulta projection_lag_seconds
  try {
    const res = http.get(
      `${PROMETHEUS}/api/v1/query?query=max(projection_lag_seconds)`,
      { tags: { kind: 'prometheus' } },
    )
    if (res.status === 200) {
      const data = JSON.parse(res.body)
      const val = parseFloat(data.data.result[0]?.value[1] || '0')
      projectionLagTrend.add(val * 1000)
    }
  } catch {}

  // Consulta search_index_lag_seconds
  try {
    const res = http.get(
      `${PROMETHEUS}/api/v1/query?query=max(search_index_lag_seconds)`,
      { tags: { kind: 'prometheus' } },
    )
    if (res.status === 200) {
      const data = JSON.parse(res.body)
      const val = parseFloat(data.data.result[0]?.value[1] || '0')
      searchIndexLagTrend.add(val * 1000)
    }
  } catch {}

  sleep(5)
}
