import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AdminRegulatoryPage from './AdminRegulatoryPage.vue'
import es from '@/app/i18n/locales/es.json'

const GET = vi.fn()

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET }),
  }
})

const mockRulesCO = {
  country: 'CO',
  max_validity_days: 30,
  retention_years: 5,
  requires_controlled_registry: true,
  allowed_substitutions: true,
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminRegulatoryPage, { global: { plugins: [i18n] } })
}

describe('AdminRegulatoryPage (FE-011, DOM-012)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: mockRulesCO,
      response: { status: 200 },
    })
  })

  it('carga y muestra reglas regulatorias para el país seleccionado', async () => {
    const w = mountPage()
    await flushPromises()

    expect(GET).toHaveBeenCalledWith(
      '/admin/regulatory-rules/{country}',
      expect.objectContaining({ params: { path: { country: 'CO' } } }),
    )
    expect(w.text()).toContain('30 días')
    expect(w.text()).toContain('5 años')
    expect(w.text()).toContain('Exigido por Ley')
    expect(w.text()).toContain('Permitidas (Bioequivalentes)')
  })
})
