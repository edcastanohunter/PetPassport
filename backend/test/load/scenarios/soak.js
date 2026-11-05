// Escenario Soak: 2 horas sostenidas a carga nominal (100 req/s, 20 cmd/s)
// para detección de fugas de memoria, conexiones o degradación en el Event Store (PP-F3-PRF-003).
import { runClinicDay } from './clinic_day.js'
import { runOwnerBrowse } from './owner_browse.js'
import { runSearchHeavy } from './search_heavy.js'

export const soakStages = [
  { duration: '2m', target: 100 },    // Calentamiento a 100 VUs / 100 req/s
  { duration: '2h', target: 100 },    // 2 horas sostenidas
  { duration: '2m', target: 0 },      // Rampa de bajada
]

export function runSoak(ep, sessions, state) {
  const r = Math.random()
  if (r < 0.6) {
    runOwnerBrowse(ep, sessions, state)
  } else if (r < 0.9) {
    runClinicDay(ep, sessions, state)
  } else {
    runSearchHeavy(ep, sessions, state)
  }
}
