import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import UploadExternalPrescriptionPage from './UploadExternalPrescriptionPage.vue'
import es from '@/app/i18n/locales/es.json'

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
  useRoute: () => ({ params: { petId: 'pet-123' }, query: {} }),
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

// Mock de URL object methods en jsdom
if (!globalThis.URL.createObjectURL) {
  globalThis.URL.createObjectURL = vi.fn(() => 'blob:mock-preview-url')
}
if (!globalThis.URL.revokeObjectURL) {
  globalThis.URL.revokeObjectURL = vi.fn()
}

function mountComponent() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })

  return mount(UploadExternalPrescriptionPage, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        RouterLink: true,
      },
    },
  })
}

describe('UploadExternalPrescriptionPage (PP-F4-FE-002, PP-F4-OCR-009, PP-F4-QA-003)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({ data: {} })
    POST.mockResolvedValue({ data: {} })
  })

  it('renderiza la zona de carga de archivos en fase upload', () => {
    const w = mountComponent()
    expect(w.text()).toContain('Subir Receta Externa')
    expect(w.find('[data-testid="dropzone-input"]').exists()).toBe(true)
  })

  it('rechaza archivos con formato no permitido', async () => {
    const w = mountComponent()
    const input = w.find<HTMLInputElement>('[data-testid="dropzone-input"]')

    const badFile = new File(['binary'], 'malware.exe', { type: 'application/octet-stream' })
    Object.defineProperty(input.element, 'files', {
      value: [badFile],
      writable: false,
    })

    await input.trigger('change')
    expect(w.text()).toContain('Formato no compatible')
  })

  it('procesa selección de archivo válido y permite iniciar análisis', async () => {
    const w = mountComponent()
    const input = w.find<HTMLInputElement>('[data-testid="dropzone-input"]')

    const file = new File(['dummy-content'], 'receta.jpg', { type: 'image/jpeg' })
    Object.defineProperty(input.element, 'files', {
      value: [file],
      writable: false,
    })

    await input.trigger('change')
    expect(w.text()).toContain('Páginas preparadas (1 de 5)')

    POST.mockResolvedValueOnce({
      data: {
        id: 'rx-456',
        pet_id: 'pet-123',
        source: 'external',
        status: 'draft',
      },
      response: { status: 201 },
    })

    const btn = w.find('[data-testid="btn-start-analysis"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/pets/{petId}/prescriptions/external',
      expect.objectContaining({
        params: { path: { petId: 'pet-123' } },
      }),
    )
    expect(w.text()).toContain('Analizando tu receta')
  })

  it('maneja 503 AI_BUDGET_EXCEEDED cambiando a captura manual', async () => {
    const w = mountComponent()
    const input = w.find<HTMLInputElement>('[data-testid="dropzone-input"]')
    const file = new File(['dummy'], 'receta.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', {
      value: [file],
      writable: false,
    })
    await input.trigger('change')

    POST.mockResolvedValueOnce({
      error: { code: 'AI_BUDGET_EXCEEDED', message: 'Presupuesto diario excedido' },
      response: { status: 503 },
    })

    const btn = w.find('[data-testid="btn-start-analysis"]')
    await btn.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('Servicio de IA saturado')
    expect(w.find('[data-testid="btn-continue-manual"]').exists()).toBe(true)

    // Al hacer click en continuar manual, entra en fase manual
    await w.find('[data-testid="btn-continue-manual"]').trigger('click')
    expect(w.text()).toContain('1. Datos del Profesional y Emisión')
    expect(w.text()).toContain('2. Medicamentos Detectados')
  })

  it('pasa a fase review cuando el OCR termina con status done', async () => {
    vi.useFakeTimers()
    const w = mountComponent()

    const input = w.find<HTMLInputElement>('[data-testid="dropzone-input"]')
    const file = new File(['img'], 'rx.jpg', { type: 'image/jpeg' })
    Object.defineProperty(input.element, 'files', {
      value: [file],
      writable: false,
    })
    await input.trigger('change')

    POST.mockResolvedValueOnce({
      data: { id: 'rx-789', status: 'draft' },
      response: { status: 201 },
    })

    GET.mockResolvedValueOnce({
      data: {
        status: 'done',
        extraction: {
          header: {
            vet_name: 'Dra. María Ospina',
            clinic_name: 'Clínica Veterinaria Santa Fe',
            issued_on: '2026-11-05',
          },
          items: [
            {
              raw_text: 'Amoxicilina 250mg cada 12h por 7 dias',
              product: {
                text: 'Amoxicilina 250mg',
                candidates: [
                  { catalog_id: 'cat-med-1', name: 'Amoxicilina 250mg comprimidos', score: 0.94 },
                ],
              },
              dose: { value: 250, unit: 'mg' },
              frequency: 'cada 12 horas',
              duration: '7',
              quantity: '14 comprimidos',
              confidence: { overall: 0.92 },
              bbox: [0.1, 0.2, 0.8, 0.1],
            },
          ],
        },
      },
    })

    await w.find('[data-testid="btn-start-analysis"]').trigger('click')
    await flushPromises()

    // Avanzar temporizador del polling (1500 ms)
    await vi.advanceTimersByTimeAsync(1600)
    await flushPromises()

    // Avanzar timeout de transición (600 ms)
    await vi.advanceTimersByTimeAsync(700)
    await flushPromises()

    expect(w.text()).toContain('1. Datos del Profesional y Emisión')
    expect(w.text()).toContain('Amoxicilina 250mg comprimidos')
    expect(w.text()).toContain('Alta confianza')

    vi.useRealTimers()
  })

  it('exige la atestación obligatoria antes de permitir confirmar la receta', async () => {
    const w = mountComponent()

    // Simular fase review
    const vm = w.vm as unknown as {
      currentPhase: string
      prescriptionId: string
      items: Array<Record<string, unknown>>
    }
    vm.currentPhase = 'review'
    vm.prescriptionId = 'rx-confirm-1'
    vm.items = [
      {
        id: 'item-1',
        name: 'Metronidazol 250mg',
        dose_value: 250,
        dose_unit: 'mg',
        route: 'oral',
        frequency: 'cada 12 horas',
        duration_days: 5,
        quantity_value: 10,
        quantity_unit: 'comp',
        substitution_allowed: true,
        instructions: 'Con comida',
        controlled: false,
        candidates: [],
      },
    ]
    await w.vm.$nextTick()

    const confirmBtn = w.find('[data-testid="btn-confirm-prescription"]')
    expect(confirmBtn.exists()).toBe(true)

    // El botón está deshabilitado si no está marcada la atestación
    expect(confirmBtn.attributes('disabled')).toBeDefined()

    // Marcar casilla de atestación
    const checkbox = w.find<HTMLInputElement>('[data-testid="checkbox-attestation"]')
    await checkbox.setValue(true)
    await w.vm.$nextTick()

    expect(confirmBtn.attributes('disabled')).toBeUndefined()

    POST.mockResolvedValueOnce({
      data: { id: 'rx-confirm-1', status: 'confirmed' },
      response: { status: 200 },
    })

    await confirmBtn.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/prescriptions/{prescriptionId}/confirm',
      expect.objectContaining({
        params: { path: { prescriptionId: 'rx-confirm-1' } },
        body: expect.objectContaining({
          attestation: true,
          items: expect.arrayContaining([
            expect.objectContaining({
              name: 'Metronidazol 250mg',
            }),
          ]),
        }),
      }),
    )

    expect(push).toHaveBeenCalledWith({
      path: '/app/prescriptions',
      query: { confirmed: 'rx-confirm-1' },
    })
  })

  it('maneja estado reupload_requested mostrando aviso y consejos', async () => {
    vi.useFakeTimers()
    const w = mountComponent()

    const input = w.find<HTMLInputElement>('[data-testid="dropzone-input"]')
    const file = new File(['img'], 'rx-blurry.jpg', { type: 'image/jpeg' })
    Object.defineProperty(input.element, 'files', {
      value: [file],
      writable: false,
    })
    await input.trigger('change')

    POST.mockResolvedValueOnce({
      data: { id: 'rx-reupload', status: 'draft' },
      response: { status: 201 },
    })

    GET.mockResolvedValueOnce({
      data: {
        status: 'reupload_requested',
        reason: 'La imagen es demasiado borrosa y tiene reflejos de luz.',
      },
    })

    await w.find('[data-testid="btn-start-analysis"]').trigger('click')
    await flushPromises()

    await vi.advanceTimersByTimeAsync(1600)
    await flushPromises()

    expect(w.text()).toContain('La imagen es demasiado borrosa')
    expect(w.find('[data-testid="btn-reupload"]').exists()).toBe(true)

    vi.useRealTimers()
  })
})
