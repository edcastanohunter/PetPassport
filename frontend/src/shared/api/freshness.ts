// Frescura de vistas derivadas (PP-F2-FE-014, API-013): las respuestas de
// timeline, resumen, peso, estadísticas y bandeja llevan `X-PP-Freshness:
// lag=<s>` con el retraso de su proyección asíncrona (ADR-009). No es un
// error: la UI muestra "Actualizando…" si el retraso es apreciable.

export const FRESHNESS_HEADER = 'X-PP-Freshness'

/** Umbral por debajo del cual el indicador se oculta (FE-014). */
export const FRESHNESS_VISIBLE_FROM_SECONDS = 3

/** Interpreta `lag=<s>` (admite decimales y otros pares `k=v` separados por `;` o `,`). */
export function parseFreshness(value: string | null | undefined): number | null {
  if (!value) return null
  for (const part of value.split(/[;,]/)) {
    const [k, v] = part.split('=').map((s) => s.trim())
    if (k?.toLowerCase() === 'lag' && v !== undefined && v !== '') {
      const n = Number(v)
      return Number.isFinite(n) && n >= 0 ? n : null
    }
  }
  return null
}

/** Retraso en segundos de una respuesta, o null si no es una vista derivada. */
export function freshnessOf(response: Response | undefined): number | null {
  return parseFreshness(response?.headers.get(FRESHNESS_HEADER))
}

export interface FreshnessEvent {
  url: string
  lagSeconds: number
}
type Listener = (e: FreshnessEvent) => void
const listeners = new Set<Listener>()

/** Suscribe a las cabeceras de frescura vistas por el cliente API. */
export function onFreshness(listener: Listener) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Lo invoca el middleware del cliente API en cada respuesta con cabecera. */
export function emitFreshness(e: FreshnessEvent) {
  for (const l of listeners) l(e)
}

/** Solo pruebas. */
export function resetFreshnessListeners() {
  listeners.clear()
}
