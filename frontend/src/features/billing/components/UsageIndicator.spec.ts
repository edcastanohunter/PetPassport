import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import UsageIndicator from './UsageIndicator.vue'

function mountUsage(props: {
  label: string
  used: number
  limit: number
  unit?: string
  softLimitPercent?: number
}) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(UsageIndicator, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('UsageIndicator (PP-F5-DOM-008, FE-004)', () => {
  it('renderiza progreso y texto correctamente bajo el límite', () => {
    const w = mountUsage({ label: 'Veterinarios', used: 2, limit: 5, unit: 'asientos' })
    expect(w.text()).toContain('Veterinarios')
    expect(w.text()).toContain('2 / 5 asientos')
    const bar = w.get('[role="progressbar"]')
    expect(bar.attributes('aria-valuenow')).toBe('2')
    expect(bar.attributes('aria-valuemax')).toBe('5')
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('muestra aviso de límite blando cuando se supera el 90%', () => {
    const w = mountUsage({ label: 'Citas', used: 95, limit: 100 })
    expect(w.text()).toContain('95 / 100')
    const alert = w.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('superado el 90%')
  })

  it('indica estado lleno cuando se alcanza o supera el 100%', () => {
    const w = mountUsage({ label: 'Asientos', used: 5, limit: 5 })
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.text()).toContain('5 / 5')
  })
})
