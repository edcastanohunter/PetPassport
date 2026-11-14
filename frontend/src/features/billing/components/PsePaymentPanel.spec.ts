import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import type { BillingAccount, Subscription } from '@/shared/api/client'
import PsePaymentPanel from './PsePaymentPanel.vue'

const GET = vi.fn()
const POST = vi.fn()

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }) }
})

function account(kind: 'pse' | 'card', country = 'CO'): BillingAccount {
  return {
    id: 'acc-1',
    country,
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
    payment_method: { kind },
  } as BillingAccount
}

function subscription(chargeStatus: string): Subscription {
  return {
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
        number: '1',
        amount: 178500,
        currency: 'COP',
        status: chargeStatus,
        created_at: '2026-11-01T00:00:00Z',
      },
    ],
  } as Subscription
}

function mountPanel(acc: BillingAccount, sub: Subscription) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(PsePaymentPanel, {
    props: { account: acc, subscription: sub, orgId: 'org-1' },
    global: { plugins: [i18n] },
  })
}

describe('PsePaymentPanel (PP-F5-PRD-004, FE-003)', () => {
  beforeEach(() => {
    GET.mockReset()
    POST.mockReset()
    GET.mockResolvedValue({ data: { data: [{ code: '1', name: 'Banco que aprueba' }] } })
    POST.mockImplementation((path: string) =>
      path === '/billing-accounts/{id}/payment-methods/setup'
        ? Promise.resolve({
            data: {
              provider: 'wompi',
              client_config: { acceptance_token: 'acc', acceptance_permalink: 'https://x/t' },
            },
          })
        : Promise.resolve({
            data: { status: 'pending', redirect_url: 'https://bank.example/pse/1' },
          }),
    )
  })

  it('solo aparece con un cargo abierto en Colombia; con tarjeta, solo si falló', () => {
    expect(
      mountPanel(account('pse'), subscription('pending'))
        .find('[data-testid="pse-payment"]')
        .exists(),
    ).toBe(true)
    expect(
      mountPanel(account('card'), subscription('failed'))
        .find('[data-testid="pse-payment"]')
        .exists(),
    ).toBe(true)
    expect(
      mountPanel(account('card'), subscription('pending'))
        .find('[data-testid="pse-payment"]')
        .exists(),
    ).toBe(false)
    expect(
      mountPanel(account('pse'), subscription('paid')).find('[data-testid="pse-payment"]').exists(),
    ).toBe(false)
    expect(
      mountPanel(account('pse', 'MX'), subscription('failed'))
        .find('[data-testid="pse-payment"]')
        .exists(),
    ).toBe(false)
  })

  it('exige banco y aceptación de términos y redirige al banco', async () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    const w = mountPanel(account('pse'), subscription('failed'))
    await w.get('[data-testid="pse-start"]').trigger('click')
    await flushPromises()

    // Prellenado desde los datos fiscales: NIT sin dígito de verificación.
    expect(w.get<HTMLInputElement>('[data-testid="pse-id"]').element.value).toBe('900123456')
    const pay = w.get<HTMLButtonElement>('[data-testid="pse-pay"]')
    expect(pay.element.disabled).toBe(true)
    await w.get('[data-testid="pse-bank"]').setValue('1')
    expect(pay.element.disabled).toBe(true) // falta aceptar los términos
    await w.get('[data-testid="pse-accept"]').setValue(true)
    expect(pay.element.disabled).toBe(false)

    await w.get('[data-testid="pse-form"]').trigger('submit')
    await flushPromises()
    const call = POST.mock.calls.find(([p]) => p === '/charges/{id}/pse-payment')
    expect(call?.[1]).toMatchObject({
      params: { path: { id: 'ch-1' } },
      body: {
        bank_code: '1',
        payer_type: 'company',
        legal_id_type: 'NIT',
        legal_id: '900123456',
        acceptance_token: 'acc',
        return_path: '/org/org-1/settings/billing?pse=return',
      },
    })
    expect(assign).toHaveBeenCalledWith('https://bank.example/pse/1')
    vi.unstubAllGlobals()
  })

  it('un pago resuelto en el acto avisa y recarga; un rechazo lo explica', async () => {
    POST.mockImplementation((path: string) =>
      path === '/billing-accounts/{id}/payment-methods/setup'
        ? Promise.resolve({
            data: { provider: 'fake', client_config: { acceptance_token: 'acc' } },
          })
        : Promise.resolve({ data: { status: 'paid' } }),
    )
    const w = mountPanel(account('pse'), subscription('failed'))
    await w.get('[data-testid="pse-start"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="pse-bank"]').setValue('1')
    await w.get('[data-testid="pse-accept"]').setValue(true)
    await w.get('[data-testid="pse-form"]').trigger('submit')
    await flushPromises()
    expect(w.emitted('changed')).toHaveLength(1)
    expect(w.text()).toContain(es.billing.pse.paid)

    POST.mockImplementation((path: string) =>
      path === '/billing-accounts/{id}/payment-methods/setup'
        ? Promise.resolve({
            data: { provider: 'fake', client_config: { acceptance_token: 'acc' } },
          })
        : Promise.resolve({ data: { status: 'failed' } }),
    )
    const r = mountPanel(account('pse'), subscription('failed'))
    await r.get('[data-testid="pse-start"]').trigger('click')
    await flushPromises()
    await r.get('[data-testid="pse-bank"]').setValue('1')
    await r.get('[data-testid="pse-accept"]').setValue(true)
    await r.get('[data-testid="pse-form"]').trigger('submit')
    await flushPromises()
    expect(r.get('[role="alert"]').text()).toBe(es.billing.pse.declined)
  })
})
