import http from 'k6/http'
import { check, sleep } from 'k6'
import { authHeaders, fresh } from '../lib/session.js'

export function runClinicDay(ep, sessions, state) {
  const session = sessions[(__VU - 1) % sessions.length]
  const curSession = fresh(ep, session)
  const headers = authHeaders(curSession)

  const orgId = state.orgId || '018f0000-0000-7000-a000-000000000001'

  // 1. Agenda de la clínica
  const resAgenda = http.get(`${ep.api}/organizations/${orgId}/agenda/slots?date=2026-11-05`, {
    headers,
    tags: { kind: 'api_read', route: '/api/v1/organizations/{id}/agenda/slots' },
  })
  check(resAgenda, { 'agenda status 200 or 404': (r) => r.status === 200 || r.status === 404 })

  // 2. Búsqueda de pacientes en el espacio clínico
  const query = state.sampleTerms ? state.sampleTerms[__ITER % state.sampleTerms.length] : 'Luna'
  const resSearch = http.get(`${ep.api}/organizations/${orgId}/search/patients?q=${encodeURIComponent(query)}`, {
    headers,
    tags: { kind: 'api_search', route: '/api/v1/organizations/{id}/search/patients' },
  })
  check(resSearch, { 'patient search ok': (r) => r.status === 200 || r.status === 404 })

  // 3. Crear borrador de encuentro (comando POST)
  const petId = state.petId || '018f0000-0000-7000-b000-000000000001'
  const createPayload = JSON.stringify({
    pet_id: petId,
    type: 'routine_checkup',
    reason: 'Control periódico y vacunación anual',
  })

  const resCreate = http.post(`${ep.api}/organizations/${orgId}/encounters`, createPayload, {
    headers: Object.assign({}, headers, {
      'Idempotency-Key': `k6-enc-${__VU}-${__ITER}-${Date.now()}`,
    }),
    tags: { kind: 'api_command', route: '/api/v1/organizations/{id}/encounters' },
  })
  check(resCreate, { 'create encounter ok': (r) => r.status === 201 || r.status === 400 || r.status === 403 })

  let encounterId = null
  let etag = null
  if (resCreate.status === 201) {
    try {
      const enc = JSON.parse(resCreate.body)
      encounterId = enc.id
      etag = resCreate.headers['ETag'] || enc.version
    } catch {}
  }

  if (encounterId) {
    // 4. Editar encuentro (comando PATCH)
    const patchPayload = JSON.stringify({
      subjective: 'Mascota activa sin síntomas adversos.',
      objective: {
        narrative: 'Constantes vitales normales. Peso 12.4 kg.',
      },
    })
    const patchHeaders = Object.assign({}, headers, {
      'If-Match': etag ? `${etag}` : undefined,
    })
    const resPatch = http.patch(`${ep.api}/organizations/${orgId}/encounters/${encounterId}`, patchPayload, {
      headers: patchHeaders,
      tags: { kind: 'api_command', route: '/api/v1/organizations/{id}/encounters/{id}' },
    })
    check(resPatch, { 'patch encounter ok': (r) => r.status === 200 || r.status === 412 })

    // 5. Firmar encuentro (comando POST sign)
    const signPayload = JSON.stringify({
      confirmation: true,
    })
    const resSign = http.post(`${ep.api}/organizations/${orgId}/encounters/${encounterId}/sign`, signPayload, {
      headers,
      tags: { kind: 'api_command', route: '/api/v1/organizations/{id}/encounters/{id}/sign' },
    })
    check(resSign, { 'sign encounter ok': (r) => r.status === 200 || r.status === 400 })
  }

  // 6. Registrar vacuna en carnet (comando POST)
  const vacPayload = JSON.stringify({
    pet_id: petId,
    vaccine_code: 'RABIES',
    product_name: 'Rabisin-D',
    batch_number: 'LOT-2026-X',
    administered_at: new Date().toISOString(),
  })
  const resVac = http.post(`${ep.api}/organizations/${orgId}/vaccinations`, vacPayload, {
    headers: Object.assign({}, headers, {
      'Idempotency-Key': `k6-vac-${__VU}-${__ITER}-${Date.now()}`,
    }),
    tags: { kind: 'api_command', route: '/api/v1/organizations/{id}/vaccinations' },
  })
  check(resVac, { 'vaccination ok': (r) => r.status === 201 || r.status === 400 || r.status === 403 })

  sleep(1)
}
