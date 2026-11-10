import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import PlanLimitModal from './PlanLimitModal.vue'

function mountModal(props: {
  modelValue: boolean
  resourceName: string
  currentUsage: number
  limit: number
  planName?: string
}) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(PlanLimitModal, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('PlanLimitModal (PP-F5-FE-004)', () => {
  it('no es visible si modelValue es false', () => {
    const w = mountModal({
      modelValue: false,
      resourceName: 'veterinarios',
      currentUsage: 5,
      limit: 5,
    })
    expect(w.find('[data-testid="plan-limit-modal"]').exists()).toBe(false)
  })

  it('muestra el recurso agotado y emite eventos de cerrar y upgrade', async () => {
    const w = mountModal({
      modelValue: true,
      resourceName: 'veterinarios',
      currentUsage: 5,
      limit: 5,
    })
    expect(w.find('[data-testid="plan-limit-modal"]').exists()).toBe(true)
    expect(w.text()).toContain('Límite del plan alcanzado')
    expect(w.text()).toContain('veterinarios')
    expect(w.text()).toContain('5 / 5')

    const buttons = w.findAll('button')
    // Click cancelar
    await buttons[0].trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([false])

    // Click upgrade
    await buttons[1].trigger('click')
    expect(w.emitted('upgrade')).toHaveLength(1)
  })
})
