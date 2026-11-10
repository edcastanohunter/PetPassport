import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import CheckoutModal from './CheckoutModal.vue'
import type { BillingPlan } from '@/shared/api/client'

const mockPlan: BillingPlan = {
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
    monthly: 10000000, // $100.000 COP
    annual: 100000000, // $1.000.000 COP (ahorro)
    tiers: [{ up_to: 10, monthly_price: 2000000 }],
  },
  quotas: { active_veterinarians: 5 },
}

function mountCheckout(props: {
  modelValue: boolean
  plan: BillingPlan | null
  initialCycle?: 'monthly' | 'annual'
  orgId?: string
  accountId?: string
}) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(CheckoutModal, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('CheckoutModal (PP-F5-FE-002)', () => {
  it('renderiza desglose de precios y cambio de ciclo mensual/anual', async () => {
    const w = mountCheckout({ modelValue: true, plan: mockPlan })
    expect(w.find('[data-testid="checkout-modal"]').exists()).toBe(true)
    expect(w.text()).toContain('Clínica Estándar')
    expect(w.text()).toContain('Facturación mensual')
    expect(w.text()).toContain('Facturación anual')
    expect(w.text()).toContain('Subtotal')
    expect(w.text()).toContain('Impuestos (IVA)')
    expect(w.text()).toContain('Total a pagar')

    // Conmutar a ciclo anual
    const annualBtn = w.findAll('button').find((b) => b.text().includes('anual'))
    expect(annualBtn).toBeDefined()
    await annualBtn!.trigger('click')
    expect(w.text()).toContain('Ahorra')
  })

  it('permite cambiar cantidad de asientos', async () => {
    const w = mountCheckout({ modelValue: true, plan: mockPlan })
    const seatsInput = w.get<HTMLInputElement>('#seats-input')
    expect(seatsInput.element.value).toBe('3')

    await seatsInput.setValue(5)
    expect(seatsInput.element.value).toBe('5')
  })
})
