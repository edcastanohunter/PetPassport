import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import es from '@/app/i18n/locales/es.json'
import { ApiError } from '@/shared/api/client'
import OrgBillingPage from './OrgBillingPage.vue'

const GET = vi.fn()
const POST = vi.fn()
const PATCH = vi.fn()
const DELETE = vi.fn()

vi.mock('vue-router', () => ({ useRoute: () => ({ params: { orgId: 'org-1' } }) }))
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST, PATCH, DELETE }) }
})

const account = {
  id: 'acc-1',
  country: 'CO',
  currency: 'COP',
  organizations: ['org-1'],
  created_at: '2026-11-01T00:00:00Z',
  legal_entity: {
    kind: 'company',
    legal_name: 'Clínica Sur SAS',
    tax_id: { country: 'CO', value: '900123456-7', kind: 'NIT' },
    address: 'Cra 1 # 2-3',
    email: 'facturas@sur.co',
  },
  payment_method: { kind: 'card', brand: 'VISA', last4: '4242', expires_at: '2030-12-31' },
}

function subscription(status: string) {
  return {
    id: 'sub-1',
    billing_account_id: 'acc-1',
    kind: 'clinic',
    plan: { code: 'pro', version: 1, name: 'Clínica Pro' },
    seats: { purchased: 5, used: 4 },
    cycle: 'monthly',
    status,
    current_period: { start: '2026-11-01T00:00:00Z', end: '2026-12-01T00:00:00Z' },
    dunning: status === 'grace' ? { grace_ends_at: '2026-11-15T00:00:00Z' } : undefined,
    cancel_at_period_end: false,
    created_at: '2026-11-01T00:00:00Z',
  }
}

const invoice = {
  id: 'inv-1',
  account_id: 'acc-1',
  charge_id: 'ch-1',
  number: 'PP-1',
  status: 'accepted',
  total: 357000,
  currency: 'COP',
  has_pdf: true,
  has_xml: true,
  credit_notes: [],
  created_at: '2026-11-01T00:00:00Z',
}

function routeGet(status: string, withSub = true) {
  GET.mockImplementation((path: string) => {
    switch (path) {
      case '/organizations/{orgId}/billing':
        return Promise.resolve({
          data: withSub ? { account, subscription: subscription(status) } : {},
        })
      case '/billing-accounts/{id}/invoices':
        return Promise.resolve({ data: { data: [invoice] } })
      case '/billing-accounts/{id}/payments':
        return Promise.resolve({ data: { data: [] } })
      case '/plans':
        return Promise.resolve({
          data: {
            data: [
              { code: 'pro', name: 'Clínica Pro', kind: 'clinic', pricing: { model: 'per_seat' } },
              {
                code: 'starter',
                name: 'Clínica Starter',
                kind: 'clinic',
                pricing: { model: 'flat' },
              },
            ],
          },
        })
      case '/billing-accounts/{id}/organizations':
        return Promise.resolve({
          data: {
            data: [
              {
                organization_id: 'org-1',
                name: 'Sede Sur',
                seat_pool: 'own',
                joined_at: '2026-11-01T00:00:00Z',
              },
            ],
            invitations: [],
          },
        })
      case '/organizations/{orgId}/billing-invitations':
        return Promise.resolve({ data: { data: invitations } })
      default:
        return Promise.resolve({ data: undefined })
    }
  })
}

let invitations: unknown[] = []

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(OrgBillingPage, {
    global: { plugins: [i18n], stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })
}

describe('OrgBillingPage (PP-F5-FE-003)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    for (const m of [GET, POST, PATCH, DELETE]) m.mockReset()
    POST.mockResolvedValue({ data: {} })
  })

  it('muestra plan, estado, asientos, método de pago y facturas con importes en pesos', async () => {
    routeGet('active')
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="billing-summary"]').text()).toContain('Clínica Pro')
    expect(w.get('[data-testid="sub-status"]').text()).toBe('Activa')
    expect(w.text()).toContain('4 de 5 asientos en uso')
    expect(w.get('[data-testid="pm-summary"]').text()).toContain('4242')
    // 357000 pesos, no 3570 (la API no usa centavos).
    expect(w.get('[data-testid="invoices"]').text()).toMatch(/357\.000/)
    expect(w.find('[data-testid="billing-status-banner"]').exists()).toBe(false)
  })

  it('en gracia muestra el banner persistente y reintenta el cobro', async () => {
    routeGet('grace')
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="billing-status-banner"]').text()).toContain(
      'No pudimos cobrar tu suscripción',
    )
    await w.get('[data-testid="retry-charge"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/subscriptions/{id}/retry-charge', {
      params: { path: { id: 'sub-1' } },
    })
  })

  it('en restricted explica que la lectura sigue disponible (RN-32)', async () => {
    routeGet('restricted')
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="billing-status-banner"]').text()).toContain(
      'Puedes consultar toda la información',
    )
  })

  it('cambia de plan solo tras ver la vista previa calculada por el backend', async () => {
    routeGet('active')
    POST.mockImplementation((path: string) =>
      Promise.resolve(
        path.endsWith('/preview')
          ? {
              data: {
                currency: 'COP',
                subtotal: 300000,
                tax: 57000,
                total: 357000,
                lines: [{ description: 'Clínica Pro × 4', amount: 300000 }],
                next_charge_at: '2026-12-01T00:00:00Z',
              },
            }
          : { data: {} },
      ),
    )
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="confirm-change"]').attributes('disabled')).toBeDefined()
    await w.get('#seats-change').setValue(4)
    await w.get('[data-testid="run-preview"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="preview"]').text()).toMatch(/357\.000/)
    await w.get('[data-testid="confirm-change"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/subscriptions/{id}/change', {
      params: { path: { id: 'sub-1' } },
      body: { plan_code: 'pro', cycle: 'monthly', seats: 4 },
    })
  })

  it('cancelar al final del periodo envía el motivo', async () => {
    routeGet('active')
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="open-cancel"]').trigger('click')
    await w.get('#cancel-reason').setValue('Cerramos la sede')
    await w.get('[data-testid="cancel-form"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/subscriptions/{id}/cancel', {
      params: { path: { id: 'sub-1' } },
      body: { at_period_end: true, reason: 'Cerramos la sede' },
    })
  })

  it('retirar un método de pago en uso muestra el conflicto', async () => {
    routeGet('active')
    DELETE.mockResolvedValue({
      error: new ApiError({ code: 'PAYMENT_METHOD_IN_USE' }, 409),
    })
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="pm-remove"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="payment-method"]').text()).toContain(
      'respalda una suscripción vigente',
    )
  })

  it('registra el método de pago con el token del proveedor (nunca datos de tarjeta)', async () => {
    routeGet('active')
    POST.mockImplementation((path: string) =>
      Promise.resolve(
        path.endsWith('/setup')
          ? { data: { provider: 'fake', client_config: { mode: 'fake' } } }
          : { data: { kind: 'card', last4: '4242' } },
      ),
    )
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="pm-change"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/billing-accounts/{id}/payment-methods', {
      params: { path: { id: 'acc-1' } },
      body: {
        provider_token: 'tok_ok',
        acceptance_token: undefined,
        personal_auth_token: undefined,
      },
    })
  })

  it('sin suscripción ofrece ver los planes', async () => {
    routeGet('active', false)
    const w = mountPage()
    await flushPromises()
    expect(w.find('[data-testid="billing-empty"]').exists()).toBe(true)
  })
  it('PRD-008: invita una organización por su código público', async () => {
    routeGet('active')
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="account-orgs"]').text()).toContain('Sede Sur')
    expect(w.find('[data-testid="detach-org"]').exists()).toBe(false) // la última no se retira
    await w.get('#invite-code').setValue(' norte01 ')
    await w.get('[data-testid="invite-org"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/billing-accounts/{id}/organizations', {
      params: { path: { id: 'acc-1' } },
      body: { public_code: 'NORTE01', seat_pool: 'own' },
    })
  })

  it('PRD-008: la organización invitada acepta aunque aún no tenga suscripción', async () => {
    invitations = [
      {
        id: 'inv-9',
        account_id: 'acc-9',
        account_name: 'Cadena Vet SAS',
        organization_id: 'org-1',
        organization_name: 'Sede Sur',
        seat_pool: 'shared',
        status: 'pending',
        created_at: '2026-11-10T00:00:00Z',
      },
    ]
    routeGet('active', false)
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="billing-invitations"]').text()).toContain('Cadena Vet SAS')
    await w.get('[data-testid="accept-invitation"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/billing-invitations/{invitationId}/accept', {
      params: { path: { invitationId: 'inv-9' } },
    })
    expect(w.find('[data-testid="billing-invitations"]').exists()).toBe(false)
    invitations = []
  })
})
