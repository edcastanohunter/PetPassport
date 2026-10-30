import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import WeightChart from './WeightChart.vue'
import es from '@/app/i18n/locales/es.json'
import type { WeightHistory } from '@/shared/api/client'
import { nearestIndex, niceTicks } from './weightScale'

const history = (over: Partial<WeightHistory> = {}): WeightHistory => ({
  data: [
    { measured_at: '2026-09-20T10:00:00Z', weight_kg: 14, source: 'encounter' },
    { measured_at: '2026-07-01T10:00:00Z', weight_kg: 13.5, source: 'encounter' },
    { measured_at: '2026-10-20T10:00:00Z', weight_kg: 12.5, source: 'encounter' },
  ],
  trend: { delta_kg: -1.5, delta_pct: -10.7, window_days: 90, alert: true },
  freshness: { lag_seconds: 0 },
  ...over,
})

function mountChart(h: WeightHistory) {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(WeightChart, { props: { history: h }, global: { plugins: [i18n] } })
}

describe('WeightChart (PRD-025)', () => {
  it('línea ordenada por fecha, valor directo en el último punto', () => {
    const w = mountChart(history())
    expect(w.findAll('circle')).toHaveLength(3)
    expect(w.get('path.viz-line').attributes('d')).toMatch(/^M[\d.]+,[\d.]+L[\d.]+,[\d.]+L/)
    expect(w.get('.viz-end-label').text()).toBe('12,5 kg')
    // El último punto (más reciente) está a la derecha.
    const xs = w.findAll('circle').map((c) => Number(c.attributes('cx')))
    expect(xs).toEqual([...xs].sort((a, b) => a - b))
  })

  it('alerta > 10 % en 90 días con icono y texto, no solo color', () => {
    const w = mountChart(history())
    const alert = w.get('[data-testid="weight-alert"]')
    expect(alert.text()).toContain('superior al 10')
    expect(alert.find('.pi-exclamation-triangle').exists()).toBe(true)
    expect(w.get('[data-testid="weight-trend"]').text()).toContain('-1,5 kg')
    expect(
      mountChart(history({ trend: { window_days: 90, alert: false } }))
        .find('[data-testid="weight-alert"]')
        .exists(),
    ).toBe(false)
  })

  it('teclado: ←/→ recorren los puntos y muestran el tooltip', async () => {
    const w = mountChart(history())
    const svg = w.get('svg')
    expect(svg.attributes('aria-label')).toContain('3 registros')
    await svg.trigger('keydown', { key: 'ArrowLeft' })
    expect(w.get('[data-testid="weight-tooltip"]').text()).toContain('12,5 kg')
    await svg.trigger('keydown', { key: 'ArrowLeft' })
    expect(w.get('[data-testid="weight-tooltip"]').text()).toContain('14 kg')
    await svg.trigger('keydown', { key: 'Escape' })
    expect(w.find('[data-testid="weight-tooltip"]').exists()).toBe(false)
  })

  it('vista de tabla con todos los valores', async () => {
    const w = mountChart(history())
    await w.get('[data-testid="weight-table-toggle"]').trigger('click')
    const rows = w.findAll('[data-testid="weight-table"] tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0]!.text()).toContain('12,5')
  })

  it('un solo punto: sin línea; vacío: mensaje', () => {
    const one = mountChart(history({ data: [history().data[0]!] }))
    expect(one.find('path.viz-line').exists()).toBe(false)
    expect(one.findAll('circle')).toHaveLength(1)
    expect(mountChart(history({ data: [] })).text()).toContain('Sin registros de peso')
  })

  it('escalas: marcas 1/2/5 y punto más cercano', () => {
    expect(niceTicks(12.5, 14)).toEqual([12.5, 13, 13.5, 14])
    expect(niceTicks(3.2, 41)).toEqual([0, 10, 20, 30, 40, 50])
    expect(niceTicks(10, 10).length).toBeGreaterThan(1)
    expect(nearestIndex([0, 10, 20], 14)).toBe(1)
  })
})
