import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import es from '@/app/i18n/locales/es.json'
import PricingPage from './PricingPage.vue'
import type { BillingPlan } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
const push = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, POST }),
  }
})

const mockPlans: BillingPlan[] = [
  {
    code: 'clinic_standard',
    version: 1,
    kind: 'clinic',
    name: 'Clínica Estándar',
    status: 'published',
    trial_days: 14,
    features: ['Historial clínico', '5 Asientos'],
    seats: { min: 1, max: 10, included: 3 },
    pricing: {
      model: 'per_seat',
      currency: 'COP',
      monthly: 10000000,
      annual: 100000000,
      tiers: [],
    },
    quotas: { active_veterinarians: 5 },
  },
]

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(PricingPage, { global: { plugins: [i18n] } })
}

describe('PricingPage (PP-F5-FE-001)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
    GET.mockResolvedValue({ data: { data: mockPlans }, error: null })
  })

  it('carga y muestra los planes de clínica por defecto', async () => {
    const w = mountPage()
    await flushPromises()

    expect(GET).toHaveBeenCalledWith('/plans', {
      params: { query: { kind: 'clinic' } },
    })

    const cards = w.findAll('[data-testid="plan-card"]')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('Clínica Estándar')
    expect(cards[0].text()).toContain('Historial clínico')
  })

  it('permite cambiar entre pestañas de clínica y farmacia', async () => {
    const w = mountPage()
    await flushPromises()

    const pharmacyTab = w.findAll('button').find((b) => b.text().includes('Farmacias'))
    expect(pharmacyTab).toBeDefined()
    await pharmacyTab!.trigger('click')
    await flushPromises()

    expect(GET).toHaveBeenCalledWith('/plans', {
      params: { query: { kind: 'pharmacy' } },
    })
  })

  it('redirige a onboarding/login si usuario anónimo hace clic en CTA', async () => {
    const w = mountPage()
    await flushPromises()

    const ctaBtn = w.get('[data-testid="plan-card"] button')
    expect(ctaBtn.text()).toContain('Iniciar Sesión')
    await ctaBtn.trigger('click')

    expect(push).toHaveBeenCalledWith({
      path: '/onboarding',
      query: { returnTo: '/pricing' },
    })
  })
})
