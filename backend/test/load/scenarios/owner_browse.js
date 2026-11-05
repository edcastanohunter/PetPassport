import http from 'k6/http'
import { check, sleep } from 'k6'
import { authHeaders, fresh } from '../lib/session.js'

export function runOwnerBrowse(ep, sessions, state) {
  const session = sessions[(__VU - 1) % sessions.length]
  const curSession = fresh(ep, session)
  const headers = authHeaders(curSession)

  // 1. GET /me (perfil del tutor)
  const resMe = http.get(`${ep.api}/me`, {
    headers,
    tags: { kind: 'api_read', route: '/api/v1/me' },
  })
  check(resMe, { 'status is 200': (r) => r.status === 200 })

  // 2. GET /pets (listado de mascotas del tutor)
  const resPets = http.get(`${ep.api}/pets`, {
    headers,
    tags: { kind: 'api_read', route: '/api/v1/pets' },
  })
  check(resPets, { 'status is 200': (r) => r.status === 200 })

  let petId = null
  try {
    const pets = JSON.parse(resPets.body).items || []
    if (pets.length > 0) {
      petId = pets[0].id
    }
  } catch {}

  if (petId) {
    // 3. GET /pets/{id} (ficha)
    const resPet = http.get(`${ep.api}/pets/${petId}`, {
      headers,
      tags: { kind: 'api_read', route: '/api/v1/pets/{id}' },
    })
    check(resPet, { 'status is 200': (r) => r.status === 200 })

    // 4. GET /pets/{id}/timeline (timeline clínico)
    const resTimeline = http.get(`${ep.api}/pets/${petId}/timeline`, {
      headers,
      tags: { kind: 'api_read', route: '/api/v1/pets/{id}/timeline' },
    })
    check(resTimeline, { 'status is 200 or 404': (r) => r.status === 200 || r.status === 404 })

    // 5. GET /pets/{id}/vaccination-card (carnet de vacunación)
    const resCard = http.get(`${ep.api}/pets/${petId}/vaccination-card`, {
      headers,
      tags: { kind: 'api_read', route: '/api/v1/pets/{id}/vaccination-card' },
    })
    check(resCard, { 'status is 200 or 404': (r) => r.status === 200 || r.status === 404 })
  }

  sleep(1)
}
