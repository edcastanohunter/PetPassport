import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QuoteBoardView from './QuoteBoardView.vue'
import type { QuoteRequestDetail } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, POST }),
  }
})

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '01930000-0000-7000-8000-00000000d001' } }),
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockQuoteDetail: QuoteRequestDetail = {
  id: '01930000-0000-7000-8000-00000000d001',
  prescription_id: '01930000-0000-7000-8000-00000000e001',
  status: 'open',
  fulfillment_type: 'delivery',
  created_at: '2026-11-09T10:00:00Z',
  expires_at: '2026-11-10T10:00:00Z',
  items: [
    {
      item_id: 'item-1',
      prescription_item_id: '01930000-0000-7000-8000-00000000e002',
      medication_name: 'Amoxicilina 250mg',
      dose: '250mg',
      quantity: 1,
      unit: 'caja',
      substitution_allowed: true,
    },
  ],
  invitations: [
    {
      pharmacy_id: '01930000-0000-7000-8000-00000000c004',
      pharmacy_name: 'Farmacia Aliada Norte',
      status: 'quoted',
      sent_at: '2026-11-09T10:01:00Z',
    },
  ],
  quotes: [
    {
      quote_id: '01930000-0000-7000-8000-00000000q001',
      pharmacy_id: '01930000-0000-7000-8000-00000000c004',
      pharmacy_name: 'Farmacia Aliada Norte',
      delivery_fee_cents: 5000,
      total_cents: 25000,
      lines: [
        {
          item_id: 'item-1',
          product_name: 'Amoxicilina Genfar 250mg',
          substitution: true,
          quantity: 1,
          unit_price_cents: 20000,
          total_price_cents: 20000,
        },
      ],
      created_at: '2026-11-09T10:05:00Z',
      expires_at: '2026-11-10T10:00:00Z',
      payment_on_delivery_only: true,
    },
  ],
}

describe('QuoteBoardView (FE-005, SEC-005)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: mockQuoteDetail,
      error: undefined,
      response: { status: 200 },
    })
    POST.mockResolvedValue({
      data: { order_id: '01930000-0000-7000-8000-00000000o001' },
      error: undefined,
      response: { status: 200 },
    })
  })

  it('muestra el tablero comparativo con las ofertas recibidas y sustituciones', async () => {
    const wrapper = mount(QuoteBoardView)
    await flushPromises()

    expect(wrapper.text()).toContain('Tablero de Cotizaciones Recibidas')
    expect(wrapper.text()).toContain('Farmacia Aliada Norte')
    expect(wrapper.text()).toContain('Sustituto')
    expect(wrapper.text()).toContain('Pago contra entrega')
  })

  it('exige el consentimiento de datos antes de confirmar la selección de cotización', async () => {
    const wrapper = mount(QuoteBoardView)
    await flushPromises()

    // Abrir modal de selección
    const selectBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Seleccionar esta Oferta'))
    expect(selectBtn).toBeDefined()
    await selectBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Confirmar Selección de Cotización')
    expect(wrapper.text()).toContain('Consentimiento de datos compartidos')

    // El botón Confirmar Pedido debe estar deshabilitado hasta que se marque el consentimiento
    const confirmBtn = wrapper.findAll('button').find((b) => b.text().includes('Confirmar Pedido'))
    expect(confirmBtn).toBeDefined()
    expect(confirmBtn!.attributes('disabled')).toBeDefined()

    // Marcar checkbox de consentimiento
    const consentCheckbox = wrapper.find('input[type="checkbox"]')
    await consentCheckbox.setValue(true)
    await flushPromises()

    expect(confirmBtn!.attributes('disabled')).toBeUndefined()
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledTimes(1)
    expect(push).toHaveBeenCalledWith({
      name: 'order-detail',
      params: { id: '01930000-0000-7000-8000-00000000o001' },
    })
  })
})
