import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AdminAIPage from './AdminAIPage.vue'
import es from '@/app/i18n/locales/es.json'

const GET = vi.fn()
const PUT = vi.fn()

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, PUT }),
  }
})

const mockBudget = {
  monthly_budget_usd: 500,
  current_spend_usd: 125.5,
  alert_threshold_percent: 80,
  status: 'active',
}

const mockUsage = {
  month: '2026-11',
  total_calls: 1540,
  total_cost_usd: 125.5,
  by_model: [
    {
      model: 'claude-3-5-sonnet',
      calls: 1540,
      total_tokens: 3500000,
      cost_usd: 125.5,
    },
  ],
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminAIPage, { global: { plugins: [i18n] } })
}

describe('AdminAIPage (FE-011, OCR-008)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockImplementation((path: string) => {
      if (path.includes('/budget'))
        return Promise.resolve({ data: mockBudget, response: { status: 200 } })
      if (path.includes('/usage'))
        return Promise.resolve({ data: mockUsage, response: { status: 200 } })
      return Promise.resolve({ data: {}, response: { status: 200 } })
    })

    PUT.mockResolvedValue({
      data: { ...mockBudget, monthly_budget_usd: 800 },
      response: { status: 200 },
    })
  })

  it('muestra presupuesto y consumo por modelo y permite actualizar', async () => {
    const w = mountPage()
    await flushPromises()

    expect(GET).toHaveBeenCalledWith('/admin/ai/budget')
    expect(GET).toHaveBeenCalledWith('/admin/ai/usage')

    expect(w.text()).toContain('$125.50')
    expect(w.text()).toContain('$500.00 USD')
    expect(w.text()).toContain('claude-3-5-sonnet')

    // Actualizar presupuesto
    const input = w.get('input[type="number"]')
    await input.setValue(800)
    await w.get('form').trigger('submit')
    await flushPromises()

    expect(PUT).toHaveBeenCalledWith('/admin/ai/budget', {
      body: {
        monthly_budget_usd: 800,
        alert_threshold_percent: 80,
      },
    })
  })
})
