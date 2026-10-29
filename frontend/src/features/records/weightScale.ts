// Escalas del gráfico de peso (PRD-025): ejes lineales con marcas "bonitas"
// (1/2/5 × 10^n) y el índice del punto más cercano para el cursor.

export function niceTicks(min: number, max: number, count = 4): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return []
  if (min === max) {
    const pad = Math.max(Math.abs(min) * 0.1, 0.5)
    min -= pad
    max += pad
  }
  const raw = (max - min) / Math.max(count, 1)
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag
  const start = Math.floor(min / step) * step
  const end = Math.ceil(max / step) * step
  const ticks: number[] = []
  for (let v = start; v <= end + step / 2; v += step) ticks.push(Math.round(v * 1e6) / 1e6)
  return ticks
}

export function linear(d0: number, d1: number, r0: number, r1: number) {
  const span = d1 - d0 || 1
  return (v: number) => r0 + ((v - d0) / span) * (r1 - r0)
}

/** Índice del valor de `xs` (ordenado) más cercano a `x`. */
export function nearestIndex(xs: number[], x: number): number {
  if (xs.length === 0) return -1
  let best = 0
  for (let i = 1; i < xs.length; i++) {
    if (Math.abs(xs[i]! - x) < Math.abs(xs[best]! - x)) best = i
  }
  return best
}
