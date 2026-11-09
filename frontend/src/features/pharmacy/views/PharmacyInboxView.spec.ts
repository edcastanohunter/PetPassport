import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PharmacyInboxView from './PharmacyInboxView.vue'
import type { PharmacyInboxResponse } from '@/shared/api/client'

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
  useRoute: () => ({ params: { orgId: '01930000-0000-7000-8000-00000000c004' } }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockInboxResponse: PharmacyInboxResponse = {
  items: [
    {
      quote_request_id: '01930000-0000-7000-8000-00000000d001',
      prescription_id: '01930000-0000-7000-8000-00000000e001',
      pet_name: 'Max',
      pet_species: 'Canino',
      status: 'pending',
      fulfillment_type: 'delivery',
      delivery_city: 'Bogotá',
      items_count: 2,
      invited_at: '2026-11-09T10:00:00Z',
      expires_at: '2026-11-10T10:00:00Z',
    },
  ],
  total: 1,
}

describe('PharmacyInboxView (FE-007)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: mockInboxResponse,
      error: undefined,
      response: { status: 200 },
    })
    POST.mockResolvedValue({
      data: { quote_id: '01930000-0000-7000-8000-00000000q001' },
      error: undefined,
      response: { status: 201 },
    })
  })

  it('muestra las solicitudes recibidas y permite enviar una cotización', async () => {
    const wrapper = mount(PharmacyInboxView)
    await flushPromises()

    expect(wrapper.text()).toContain('Solicitudes de Cotización Recibidas')
    expect(wrapper.text()).toContain('Canino')
    expect(wrapper.text()).toContain('Pendiente de Respuesta')

    // Abrir modal de cotización
    const quoteBtn = wrapper.findAll('button').find((b) => b.text().includes('Cotizar Oferta'))
    expect(quoteBtn).toBeDefined()
    await quoteBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Enviar Cotización a Tutor')

    // Enviar cotización
    const submitBtn = wrapper.findAll('button').find((b) => b.text().includes('Enviar Cotización'))
    expect(submitBtn).toBeDefined()
    await submitBtn!.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledTimes(1)
  })

  it('permite declinar una solicitud con motivo', async () => {
    const wrapper = mount(PharmacyInboxView)
    await flushPromises()

    const declineBtn = wrapper.findAll('button').find((b) => b.text().includes('Declinar'))
    expect(declineBtn).toBeDefined()
    await declineBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Declinar Solicitud de Cotización')

    const confirmDeclineBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Confirmar Declinación'))
    expect(confirmDeclineBtn).toBeDefined()
    await confirmDeclineBtn!.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledTimes(1)
  })
})
