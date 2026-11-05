// Escenario Spike: ráfaga x5 durante 2 min para evaluar resiliencia, breaker y recuperación (PP-F3-PRF-003)
import { runClinicDay } from './clinic_day.js'
import { runOwnerBrowse } from './owner_browse.js'
import { runSearchHeavy } from './search_heavy.js'

export const spikeStages = [
  { duration: '30s', target: 50 },   // Calentamiento a carga normal
  { duration: '1m', target: 100 },   // Carga nominal (100 req/s)
  { duration: '30s', target: 500 },  // Rampa a ráfaga x5
  { duration: '2m', target: 500 },   // Spike x5 sostenido durante 2 minutos
  { duration: '30s', target: 100 },  // Retorno a carga nominal
  { duration: '2m', target: 100 },   // Periodo de observación de recuperación
  { duration: '30s', target: 0 },    // Bajada final
]

export function runSpike(ep, sessions, state) {
  // Mezcla de tráfico durante el spike
  const r = Math.random()
  if (r < 0.6) {
    runOwnerBrowse(ep, sessions, state)
  } else if (r < 0.9) {
    runClinicDay(ep, sessions, state)
  } else {
    runSearchHeavy(ep, sessions, state)
  }
}
