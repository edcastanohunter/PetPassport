import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import es from '@/app/i18n/locales/es.json'
import AdminBillingPage from './AdminBillingPage.vue'

const GET = vi.fn()
const POST = vi.fn()
const PUT = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST, PUT }) }
})

const detail = {
  account: {
    id: 'acc-1',
    country: 'CO',
    currency: 'COP',
    organizations: ['org-1'],
    created_at: '2026-11-01T00:00:00Z',
    legal_entity: {
      kind: 'company',
      legal_name: 'Clínica Sur SAS',
      tax_id: { country: 'CO', value: '900123456-7', kind: 'NIT' },
      address: 'Cra 1',
      email: 'f@sur.co',
    },
  },
  subscriptions: [
    {
      id: 'sub-1',
      billing_account_id: 'acc-1',
      kind: 'clinic',
      plan: { code: 'pro', version: 1, name: 'Clínica Pro' },
      cycle: 'monthly',
      status: 'grace',
      current_period: { start: '2026-11-01T00:00:00Z', end: '2026-12-01T00:00:00Z' },
      cancel_at_period_end: false,
      created_at: '2026-11-01T00:00:00Z',
      charges: [
        {
          id: 'ch-1',
          number: 1,
          amount: 357000,
          currency: 'COP',
          status: 'failed',
          created_at: '2026-11-01T00:00:00Z',
        },
      ],
    },
  ],
  payments: [],
  invoices: [],
  discrepancies: [],
  audit: [
    {
      id: 'au-1',
      occurred_at: '2026-11-05T10:00:00Z',
      action: 'admin.billing.subscription.adjust',
      reason: 'Compensación por caída',
    },
  ],
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(AdminBillingPage, { global: { plugins: [i18n] } })
}

describe('AdminBillingPage (PP-F5-FE-005)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    for (const m of [GET, POST, PUT]) m.mockReset()
    GET.mockImplementation((path: string) => {
      if (path === '/admin/billing/accounts')
        return Promise.resolve({
          data: {
            data: [
              {
                id: 'acc-1',
                legal_name: 'Clínica Sur SAS',
                country: 'CO',
                currency: 'COP',
                mrr: 357000,
                status: 'active',
                created_at: '2026-11-01T00:00:00Z',
                subscriptions: [{ id: 'sub-1', kind: 'clinic', plan_code: 'pro', status: 'grace' }],
              },
            ],
          },
        })
      if (path === '/admin/billing/accounts/{id}') return Promise.resolve({ data: detail })
      if (path === '/admin/coupons') return Promise.resolve({ data: { data: [] } })
      if (path === '/admin/plans')
        return Promise.resolve({
          data: {
            data: [
              { code: 'pro', version: 2, kind: 'clinic', name: 'Clínica Pro', status: 'draft' },
              {
                code: 'pro',
                version: 1,
                kind: 'clinic',
                name: 'Clínica Pro',
                status: 'published',
                effective_from: '2026-11-01',
              },
            ],
          },
        })
      return Promise.resolve({ data: { data: [] } })
    })
    POST.mockResolvedValue({ data: {} })
  })

  it('filtra cuentas y abre el detalle con suscripciones y cargos', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('#ba-q').setValue('Sur')
    await w.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="account-row"]').text()).toMatch(/MRR.*357\.000/)
    expect(GET).toHaveBeenCalledWith('/admin/billing/accounts', {
      params: { query: { q: 'Sur', status: undefined, country: undefined, limit: 100 } },
    })
    await w.get('[data-testid="account-row"]').trigger('click')
    await flushPromises()
    const aside = w.get('[data-testid="account-detail"]')
    expect(aside.text()).toContain('Clínica Pro')
    expect(aside.text()).toMatch(/357\.000/)
  })

  it('un ajuste exige motivo de ≥ 10 caracteres y envía los campos de la acción', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="account-row"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="sub-action-credit"]').trigger('click')
    await w.get('[data-testid="ba-amount"]').setValue('50000')
    await w.get('[data-testid="ba-reason"]').setValue('corto')
    await w.get('[data-testid="billing-action-dialog"]').trigger('submit')
    expect(POST).not.toHaveBeenCalled()
    await w.get('[data-testid="ba-reason"]').setValue('Compensación por caída del servicio')
    await w.get('[data-testid="billing-action-dialog"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/subscriptions/{id}/adjust', {
      params: { path: { id: 'sub-1' } },
      body: expect.objectContaining({
        kind: 'credit',
        amount: 50000,
        reason: 'Compensación por caída del servicio',
      }),
    })
  })

  it('crea un cupón con el código en mayúsculas', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="tab-coupons"]').trigger('click')
    await flushPromises()
    await w.get('#c-code').setValue('bienvenida')
    await w.get('#c-value').setValue('20')
    await w.get('#c-reason').setValue('Campaña de lanzamiento en Medellín')
    await w.get('[data-testid="coupon-form"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/coupons', {
      body: expect.objectContaining({ code: 'BIENVENIDA', kind: 'pct', value: 20 }),
    })
  })

  it('el detalle muestra las acciones de administración con su motivo', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="account-row"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="account-audit"]').text()).toContain('Compensación por caída')
  })

  it('publica una versión en borrador con fecha y motivo (API-001)', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="tab-plans"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="retire-pro@1"]').exists()).toBe(true)
    await w.get('[data-testid="publish-pro@2"]').trigger('click')
    await w.get('[data-testid="ba-effective_from"]').setValue('2027-01-15')
    await w.get('[data-testid="ba-reason"]').setValue('Nueva lista de precios 2027')
    await w.get('[data-testid="billing-action-dialog"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/plans/{code}/publish', {
      params: { path: { code: 'pro' } },
      body: { version: 2, effective_from: '2027-01-15', reason: 'Nueva lista de precios 2027' },
    })
  })
})
