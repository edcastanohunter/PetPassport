import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QuoteWizardView from './QuoteWizardView.vue'
import type { PrescriptionQuoteOptions } from '@/shared/api/client'

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
  useRoute: () => ({ params: { id: '01930000-0000-7000-8000-00000000e001' } }),
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockQuoteOptions: PrescriptionQuoteOptions = {
  prescription_id: '01930000-0000-7000-8000-00000000e001',
  items: [
    {
      prescription_item_id: '01930000-0000-7000-8000-00000000e002',
      medication_name: 'Amoxicilina 250mg',
      dose: '250mg',
      quantity: 1,
      unit: 'caja',
      is_controlled: false,
    },
    {
      prescription_item_id: '01930000-0000-7000-8000-00000000e003',
      medication_name: 'Tramadol 50mg',
      dose: '50mg',
      quantity: 1,
      unit: 'frasco',
      is_controlled: true,
    },
  ],
  pharmacies: [
    {
      pharmacy_id: '01930000-0000-7000-8000-00000000c004',
      name: 'Farmacia Aliada Centro',
      address: 'Calle 10 # 20-30',
      rating: 4.9,
      active: true,
      fulfillment_types: ['delivery', 'pickup'],
      average_quote_time_seconds: 600,
      on_time_delivery_rate: 0.98,
    },
  ],
}

describe('QuoteWizardView (FE-004, RN-26)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: mockQuoteOptions,
      error: undefined,
      response: { status: 200 },
    })
    POST.mockResolvedValue({
      data: { id: '01930000-0000-7000-8000-00000000d001' },
      error: undefined,
      response: { status: 201 },
    })
  })

  it('bloquea medicamentos controlados y muestra advertencia regulatoria RN-26', async () => {
    const wrapper = mount(QuoteWizardView)
    await flushPromises()

    expect(wrapper.text()).toContain('Amoxicilina 250mg')
    expect(wrapper.text()).toContain('Tramadol 50mg')
    expect(wrapper.text()).toContain('Controlado · No cotizable')
    expect(wrapper.text()).toContain('Regulación sobre sustancias controladas')

    // El checkbox del ítem controlado debe estar deshabilitado
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    const controlledCheckbox = checkboxes.find((cb) => cb.attributes('disabled') !== undefined)
    expect(controlledCheckbox).toBeDefined()
  })

  it('permite avanzar por los pasos del wizard y enviar la solicitud', async () => {
    const wrapper = mount(QuoteWizardView)
    await flushPromises()

    // Paso 1 -> Avanzar
    const nextBtnStep1 = wrapper.find('button[type="button"]:not([disabled])')
    expect(nextBtnStep1.text()).toContain('Modalidad de Entrega')
    await nextBtnStep1.trigger('click')
    await flushPromises()

    // Paso 2: Llenar dirección de delivery
    const streetInput = wrapper.find('input[placeholder="Ej. Calle 123"]')
    await streetInput.setValue('Carrera 7 # 45-10')

    const nextBtnStep2 = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Seleccionar Farmacias'))
    expect(nextBtnStep2).toBeDefined()
    await nextBtnStep2!.trigger('click')
    await flushPromises()

    // Paso 3: Farmacias -> Avanzar a Resumen
    expect(wrapper.text()).toContain('Farmacia Aliada Centro')
    expect(wrapper.text()).toContain('★ 4.9')
    const nextBtnStep3 = wrapper
      .findAll('button')
      .find((b) => b.text().includes('Revisar y Confirmar'))
    expect(nextBtnStep3).toBeDefined()
    await nextBtnStep3!.trigger('click')
    await flushPromises()

    // Paso 4: Confirmar y Enviar
    const submitBtn = wrapper.findAll('button').find((b) => b.text().includes('Confirmar y Enviar'))
    expect(submitBtn).toBeDefined()
    await submitBtn!.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledTimes(1)
    expect(push).toHaveBeenCalledWith({
      name: 'quote-request-board',
      params: { id: '01930000-0000-7000-8000-00000000d001' },
    })
  })
})
