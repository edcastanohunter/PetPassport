import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AdminMedicationsPage from './AdminMedicationsPage.vue'
import es from '@/app/i18n/locales/es.json'
import * as medApi from '@/features/medications/medicationsApi'
import type {
  Medication,
  MedicationSuggestion,
  MedicationUsageStats,
} from '@/features/medications/medicationsTypes'

vi.mock('@/features/medications/medicationsApi', () => ({
  listMedications: vi.fn(),
  adminCreateMedication: vi.fn(),
  adminUpdateMedication: vi.fn(),
  adminDiscontinueMedication: vi.fn(),
  adminMergeMedication: vi.fn(),
  adminListSuggestions: vi.fn(),
  adminApproveSuggestion: vi.fn(),
  adminRejectSuggestion: vi.fn(),
  adminImportMedications: vi.fn(),
  adminGetImportBatch: vi.fn(),
  adminRevertImportBatch: vi.fn(),
  adminGetUsageStats: vi.fn(),
}))

const mockMed: Medication = {
  id: 'med-1',
  name: 'Amoxicilina 500mg',
  generic_name: 'Amoxicilina trihidrato',
  form: 'tablet',
  concentration: '500 mg',
  active_ingredients: [{ name: 'Amoxicilina', concentration: '500 mg' }],
  presentation: 'Caja x 30 comprimidos',
  species: ['dog', 'cat'],
  routes: ['oral'],
  prescription_required: true,
  controlled: false,
  status: 'active',
  source: { kind: 'manual' },
  usage_count: 42,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
}

const mockSug: MedicationSuggestion = {
  id: 'sug-1',
  proposed_by: 'vet-1',
  organization_id: 'org-1',
  name: 'Meloxicam 2mg',
  notes: 'Antiinflamatorio canino',
  status: 'pending',
  created_at: '2026-11-02T10:00:00Z',
}

const mockStats: MedicationUsageStats = {
  total_plan_items: 100,
  catalog_linked_items: 80,
  free_text_items: 20,
  adoption_rate_pct: 80.0,
  top_medications: [{ catalog_id: 'med-1', name: 'Amoxicilina 500mg', usage_count: 42 }],
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminMedicationsPage, { global: { plugins: [i18n] } })
}

describe('AdminMedicationsPage (PP-F3-FE-004)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.mocked(medApi.listMedications).mockResolvedValue({ data: [mockMed], total: 1 })
    vi.mocked(medApi.adminListSuggestions).mockResolvedValue([mockSug])
    vi.mocked(medApi.adminGetUsageStats).mockResolvedValue(mockStats)
  })

  it('renderiza el catálogo y filtra por texto', async () => {
    const w = mountPage()
    await flushPromises()

    expect(w.find('[data-testid="admin-medications-page"]').exists()).toBe(true)
    expect(medApi.listMedications).toHaveBeenCalled()
    expect(w.text()).toContain('Amoxicilina 500mg')
    expect(w.text()).toContain('tablet')

    const filterInput = w.get('[data-testid="filter-q"]')
    await filterInput.setValue('Amox')
    await flushPromises()
  })

  it('permite cambiar a la pestaña de sugerencias y aprobar', async () => {
    vi.mocked(medApi.adminApproveSuggestion).mockResolvedValue({
      ...mockMed,
      name: 'Meloxicam 2mg',
    })
    const w = mountPage()
    await flushPromises()

    // Cambiar a pestaña Sugerencias
    await w.get('[data-testid="tab-suggestions"]').trigger('click')
    await flushPromises()

    expect(medApi.adminListSuggestions).toHaveBeenCalledWith('pending')
    expect(w.text()).toContain('Meloxicam 2mg')
    expect(w.text()).toContain('Antiinflamatorio canino')

    // Abrir modal de aprobación
    await w.get('[data-testid="btn-approve-sug"]').trigger('click')
    expect(w.text()).toContain('Aprobar sugerencia')

    // Confirmar aprobación
    const approveForm = w.get('form')
    await approveForm.trigger('submit')
    await flushPromises()

    expect(medApi.adminApproveSuggestion).toHaveBeenCalledWith(
      'sug-1',
      expect.objectContaining({ name: 'Meloxicam 2mg' }),
    )
  })

  it('permite cambiar a la pestaña de importación y simular lote', async () => {
    vi.mocked(medApi.adminImportMedications).mockResolvedValue({
      id: 'batch-1',
      source: 'ica',
      total_rows: 5,
      created: 4,
      updated: 1,
      skipped: 0,
      errors: [],
      created_by: 'admin-1',
      created_at: '2026-11-03T10:00:00Z',
    })

    const w = mountPage()
    await flushPromises()

    // Cambiar a pestaña Importación
    await w.get('[data-testid="tab-import"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid="section-import"]').exists()).toBe(true)
    const dryRunEl = w.get('[data-testid="import-dryrun-checkbox"]').element as HTMLInputElement
    expect(dryRunEl.checked).toBe(true)
  })

  it('muestra la pestaña de analítica con tasas de adopción y top 10', async () => {
    const w = mountPage()
    await flushPromises()

    // Cambiar a pestaña Stats
    await w.get('[data-testid="tab-stats"]').trigger('click')
    await flushPromises()

    expect(medApi.adminGetUsageStats).toHaveBeenCalled()
    expect(w.text()).toContain('80.0%')
    expect(w.text()).toContain('42')
  })
})
