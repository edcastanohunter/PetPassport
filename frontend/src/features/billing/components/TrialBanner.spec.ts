import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import TrialBanner from './TrialBanner.vue'

function mountBanner(props: {
  status: 'trialing' | 'grace' | string
  daysRemaining?: number
  planName?: string
}) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(TrialBanner, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('TrialBanner (PP-F5-DOM-004, FE-001)', () => {
  it('renderiza banner de periodo de prueba con días restantes', async () => {
    const w = mountBanner({ status: 'trialing', daysRemaining: 7, planName: 'Clínica Pro' })
    expect(w.find('[data-testid="trial-banner"]').exists()).toBe(true)
    expect(w.text()).toContain('Periodo de prueba activo')
    expect(w.text()).toContain('7 días de prueba')
    expect(w.text()).toContain('Suscribirse ahora')

    await w.get('button').trigger('click')
    expect(w.emitted('upgrade')).toHaveLength(1)
  })

  it('renderiza advertencia de periodo de gracia y botón de pago', async () => {
    const w = mountBanner({ status: 'grace', daysRemaining: 2 })
    expect(w.text()).toContain('Problema con el pago')
    expect(w.text()).toContain('2 días para actualizar')
    expect(w.text()).toContain('Actualizar método de pago')
  })

  it('no muestra nada para estados activos o normales', () => {
    const w = mountBanner({ status: 'active' })
    expect(w.find('[data-testid="trial-banner"]').exists()).toBe(false)
  })
})
