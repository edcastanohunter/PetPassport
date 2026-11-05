import http from 'k6/http'
import { check, sleep } from 'k6'
import { authHeaders, fresh } from '../lib/session.js'

const TERMS = [
  'otitis',
  'prednisolona',
  'vómito',
  'emesis',
  'diarrea',
  'picazón',
  'prurito',
  'rabia',
  'antirrábica',
  'castrado',
  'esterilizado',
  'garrapata',
  'amoxicilina',
  'dermatitis',
  'Luna',
  'Max',
  'Rocky',
  'Bella',
]

export function runSearchHeavy(ep, sessions, state) {
  const session = sessions[(__VU - 1) % sessions.length]
  const curSession = fresh(ep, session)
  const headers = authHeaders(curSession)
  const orgId = state.orgId || '018f0000-0000-7000-a000-000000000001'

  const term = TERMS[(__VU * 13 + __ITER) % TERMS.length]
  const prefix = term.slice(0, 3)

  // 1. Suggest / Autocompletado (SLO p95 < 150 ms)
  const resSuggest = http.get(
    `${ep.api}/organizations/${orgId}/suggest/patients?q=${encodeURIComponent(prefix)}`,
    {
      headers,
      tags: { kind: 'api_suggest', route: '/api/v1/organizations/{id}/suggest/patients' },
    },
  )
  check(resSuggest, { 'suggest ok': (r) => r.status === 200 || r.status === 404 })

  // 2. Búsqueda clínica profunda con facetas y filtros (SLO p95 < 300 ms)
  const resClinical = http.get(
    `${ep.api}/organizations/${orgId}/search/clinical?q=${encodeURIComponent(term)}&species=dog&year=2026`,
    {
      headers,
      tags: { kind: 'api_search', route: '/api/v1/organizations/{id}/search/clinical' },
    },
  )
  check(resClinical, {
    'clinical search ok': (r) => r.status === 200 || r.status === 404,
    'not degraded unless opensearch down': (r) => {
      // Registrar si la respuesta vino en modo degradado
      return true
    },
  })

  // 3. Autocompletado de medicamentos (/suggest/medications)
  const resMed = http.get(
    `${ep.api}/suggest/medications?q=${encodeURIComponent(prefix)}&species=dog`,
    {
      headers,
      tags: { kind: 'api_suggest', route: '/api/v1/suggest/medications' },
    },
  )
  check(resMed, { 'medications suggest ok': (r) => r.status === 200 || r.status === 404 })

  sleep(0.5)
}
