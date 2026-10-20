import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import HomePage from './HomePage.vue'

vi.mock('@/shared/api/client', () => ({
  createApiClient: () => ({ GET: async () => ({ data: { status: 'ok' } }) }),
}))

describe('HomePage', () => {
  it('muestra el estado de la API', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'es',
      messages: { es: { home: { title: 'T', subtitle: 'S', status: 'Estado' } } },
    })
    const wrapper = mount(HomePage, { global: { plugins: [i18n], stubs: { RouterLink: true } } })
    await new Promise((r) => setTimeout(r, 0))
    expect(wrapper.get('[data-testid="api-status"]').text()).toContain('ok')
  })
})
