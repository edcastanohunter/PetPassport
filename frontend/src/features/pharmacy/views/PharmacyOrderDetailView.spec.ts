import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PharmacyOrderDetailView from './PharmacyOrderDetailView.vue'
import type { PharmacyOrderDetail } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, POST }),
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({
    params: {
      orgId: '01930000-0000-7000-8000-00000000c004',
      orderId: '01930000-0000-7000-8000-00000000o001',
    },
  }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockPharmacyOrder: PharmacyOrderDetail = {
  id: '01930000-0000-7000-8000-00000000o001',
  quote_request_id: '01930000-0000-7000-8000-00000000d001',
  prescription_id: '01930000-0000-7000-8000-00000000e001',
  tutor_name: 'Carlos Tutor',
  tutor_phone: '+573001234567',
  pet_name: 'Toby',
  pet_species: 'Canino',
  status: 'placed',
  fulfillment_type: 'delivery',
  delivery_address: {
    street: 'Calle 100',
    number: '# 15-20',
    city: 'Bogotá',
  },
  lines: [
    {
      item_id: 'item-1',
      prescription_item_id: '01930000-0000-7000-8000-00000000e002',
      product_name: 'Amoxicilina 250mg',
      substitution: false,
      quantity: 1,
      unit_price_cents: 15000,
      total_price_cents: 15000,
    },
  ],
  delivery_fee_cents: 5000,
  total_cents: 20000,
  status_history: [
    {
      status: 'placed',
      changed_at: '2026-11-09T10:00:00Z',
    },
  ],
  created_at: '2026-11-09T10:00:00Z',
}

describe('PharmacyOrderDetailView (FE-008)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: mockPharmacyOrder,
      error: undefined,
      response: { status: 200 },
    })
    POST.mockResolvedValue({
      data: { status: 'confirmed' },
      error: undefined,
      response: { status: 200 },
    })
  })

  it('muestra la información del pedido y permite confirmarlo con ETA', async () => {
    const wrapper = mount(PharmacyOrderDetailView)
    await flushPromises()

    expect(wrapper.text()).toContain('Gestión del Pedido')
    expect(wrapper.text()).toContain('Carlos Tutor')
    expect(wrapper.text()).toContain('+573001234567')
    expect(wrapper.text()).toContain('Calle 100')

    // Abrir modal de confirmación
    const confirmBtn = wrapper.findAll('button').find((b) => b.text().includes('Confirmar con ETA'))
    expect(confirmBtn).toBeDefined()
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Confirmar Aceptación del Pedido')

    // Confirmar pedido
    const submitConfirmBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Confirmar Pedido'))
    expect(submitConfirmBtn).toBeDefined()
    await submitConfirmBtn!.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledTimes(1)
  })
})
