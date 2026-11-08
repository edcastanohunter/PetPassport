import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import GuardianPrescriptionsPage from './GuardianPrescriptionsPage.vue'
import es from '@/app/i18n/locales/es.json'
import type { PrescriptionDetail, PrescriptionSummary } from '@/shared/api/client'

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
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockRxIssued: PrescriptionSummary = {
  id: 'rx-101',
  version: 1,
  pet_id: 'pet-1',
  guardian_id: 'guardian-1',
  country: 'CO',
  source: 'issued',
  status: 'issued',
  issuer_id: 'vet-1',
  issuer_org_id: 'org-1',
  issued_at: '2026-11-01T10:00:00Z',
  valid_until: '2026-12-01',
  refills_allowed: 3,
  refills_used: 1,
  items_count: 2,
  has_controlled: false,
  pdf_object_key: 'prescriptions/rx-101.pdf',
  archived_by_guardian: false,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
}

const mockRxExternal: PrescriptionSummary = {
  id: 'rx-102',
  version: 2,
  pet_id: 'pet-1',
  guardian_id: 'guardian-1',
  country: 'CO',
  source: 'external',
  status: 'confirmed',
  valid_until: '2026-11-20',
  refills_allowed: 0,
  refills_used: 0,
  items_count: 1,
  has_controlled: true,
  archived_by_guardian: false,
  created_at: '2026-11-05T12:00:00Z',
  updated_at: '2026-11-05T12:00:00Z',
}

function mountComponent() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })

  return mount(GuardianPrescriptionsPage, {
    global: {
      plugins: [pinia, i18n],
      stubs: {
        RouterLink: true,
      },
    },
  })
}

describe('GuardianPrescriptionsPage (PP-F4-FE-003, PP-F4-PRD-007, PP-F4-QA-003)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: { data: [mockRxIssued, mockRxExternal] },
      response: { status: 200 },
    })
    POST.mockResolvedValue({ data: { success: true }, response: { status: 200 } })
  })

  it('renderiza título, filtros y tarjetas de prescripción', async () => {
    const w = mountComponent()
    await flushPromises()

    expect(w.text()).toContain('Mis Recetas Médicas')
    expect(w.find('[data-testid="btn-upload-external"]').exists()).toBe(true)
    expect(w.find('[data-testid="select-pet-filter"]').exists()).toBe(true)
    expect(w.find('[data-testid="select-status-filter"]').exists()).toBe(true)
    expect(w.find('[data-testid="select-source-filter"]').exists()).toBe(true)

    // Tarjeta 1: Receta emitida
    const card1 = w.find('[data-testid="prescription-card-rx-101"]')
    expect(card1.exists()).toBe(true)
    expect(card1.text()).toContain('Emitida por clínica')
    expect(card1.text()).toContain('Verificada')
    expect(card1.text()).toContain('1 de 3 utilizados')

    // Tarjeta 2: Receta externa
    const card2 = w.find('[data-testid="prescription-card-rx-102"]')
    expect(card2.exists()).toBe(true)
    expect(card2.text()).toContain('Receta externa')
    expect(card2.text()).toContain('Externa no verificada')
    expect(card2.text()).toContain('Controlado')
  })

  it('permite filtrar por estado y origen disparando recarga', async () => {
    const w = mountComponent()
    await flushPromises()

    // Cambiar filtro de origen a external
    const sourceSelect = w.find<HTMLSelectElement>('[data-testid="select-source-filter"]')
    await sourceSelect.setValue('external')
    await flushPromises()

    expect(GET).toHaveBeenCalledWith(
      '/me/prescriptions',
      expect.objectContaining({
        params: {
          query: expect.objectContaining({
            source: 'external',
          }),
        },
      }),
    )

    // Cambiar filtro de estado a confirmed
    const statusSelect = w.find<HTMLSelectElement>('[data-testid="select-status-filter"]')
    await statusSelect.setValue('confirmed')
    await flushPromises()

    expect(GET).toHaveBeenCalledWith(
      '/me/prescriptions',
      expect.objectContaining({
        params: {
          query: expect.objectContaining({
            status: 'confirmed',
          }),
        },
      }),
    )
  })

  it('abre el modal de detalle completo con medicamentos e indicaciones', async () => {
    const mockDetail: PrescriptionDetail = {
      id: 'rx-101',
      version: 1,
      pet_id: 'pet-1',
      guardian_id: 'guardian-1',
      country: 'CO',
      source: 'issued',
      status: 'issued',
      issuer_id: 'vet-1',
      issuer_org_id: 'org-1',
      issued_at: '2026-11-01T10:00:00Z',
      valid_until: '2026-12-01',
      refills_allowed: 3,
      refills_used: 1,
      items: [
        {
          name: 'Cefalexina 500mg',
          dose: { value: 500, unit: 'mg' },
          route: 'oral',
          frequency: 'cada 12 horas',
          duration_days: 10,
          quantity: { value: 20, unit: 'tablets' },
          substitution_allowed: true,
          instructions: 'Dar con abundante alimento',
          controlled: false,
        },
      ],
      has_controlled: false,
      verifications_count: 2,
      archived_by_guardian: false,
      created_at: '2026-11-01T10:00:00Z',
      updated_at: '2026-11-01T10:00:00Z',
    }

    GET.mockImplementation((url: string) => {
      if (url.includes('/prescriptions/{prescriptionId}')) {
        return Promise.resolve({ data: mockDetail, response: { status: 200 } })
      }
      return Promise.resolve({ data: { data: [mockRxIssued] }, response: { status: 200 } })
    })

    const w = mountComponent()
    await flushPromises()

    const detailBtn = w.find(
      '[data-testid="prescription-card-rx-101"] [data-testid="btn-view-detail"]',
    )
    await detailBtn.trigger('click')
    await flushPromises()

    const modal = w.find('[data-testid="modal-prescription-detail"]')
    expect(modal.exists()).toBe(true)
    expect(modal.text()).toContain('Cefalexina 500mg')
    expect(modal.text()).toContain('Dar con abundante alimento')
    expect(modal.text()).toContain('Dosis: 500 mg')
  })

  it('permite archivar una receta abriendo confirmación y llamando al endpoint', async () => {
    const w = mountComponent()
    await flushPromises()

    const archiveBtn = w.find(
      '[data-testid="prescription-card-rx-101"] [data-testid="btn-archive"]',
    )
    await archiveBtn.trigger('click')

    const confirmModal = w.find('[data-testid="modal-archive-confirm"]')
    expect(confirmModal.exists()).toBe(true)

    const confirmBtn = w.find('[data-testid="btn-confirm-archive"]')
    await confirmBtn.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/prescriptions/{prescriptionId}/archive',
      expect.objectContaining({
        params: { path: { prescriptionId: 'rx-101' } },
      }),
    )
    expect(w.text()).toContain('Receta archivada de tu lista')
  })

  it('navega a cotización al hacer click en "Cotizar en farmacias"', async () => {
    const w = mountComponent()
    await flushPromises()

    const quoteBtn = w.find('[data-testid="prescription-card-rx-101"] [data-testid="btn-quote"]')
    await quoteBtn.trigger('click')

    expect(push).toHaveBeenCalledWith({
      path: '/app/quotes/new',
      query: { prescription_id: 'rx-101' },
    })
  })
})
