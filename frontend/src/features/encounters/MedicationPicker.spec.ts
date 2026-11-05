import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MedicationPicker from './MedicationPicker.vue'
import es from '@/app/i18n/locales/es.json'
import type { MedicationDraft } from './encounterDraft'

const suggestMedicationsMock = vi.fn()
const createSuggestionMock = vi.fn()

vi.mock('@/features/medications/medicationsApi', () => ({
  suggestMedications: (...args: unknown[]) => suggestMedicationsMock(...args),
  createSuggestion: (...args: unknown[]) => createSuggestionMock(...args),
}))

function mountPicker(props: {
  modelValue: MedicationDraft
  species?: string
  orgId?: string
  disabled?: boolean
}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(MedicationPicker, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('MedicationPicker (PP-F3-FE-003)', () => {
  const emptyMed: MedicationDraft = {
    catalog_id: null,
    free_text: false,
    catalog_name_snapshot: null,
    medication_name: '',
    presentation: '',
    dose: null,
    dose_unit: 'mg',
    route: 'oral',
    once: false,
    every: 24,
    unit: 'hours',
    duration_days: null,
    instructions: '',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  it('muestra chip cuando está vinculado a catálogo y permite cambiar', async () => {
    const catalogMed: MedicationDraft = {
      ...emptyMed,
      catalog_id: 'med-123',
      medication_name: 'Amoxicilina 500mg',
      catalog_name_snapshot: 'Amoxicilina 500mg',
    }
    const w = mountPicker({ modelValue: catalogMed })

    expect(w.find('[data-testid="medication-catalog-chip"]').exists()).toBe(true)
    expect(w.text()).toContain('Amoxicilina 500mg')
    expect(w.text()).toContain('Catálogo')

    // Click en cambiar limpia el catálogo
    await w.get('[data-testid="medication-clear-btn"]').trigger('click')
    expect(w.emitted('update:modelValue')).toBeTruthy()
    const emitted = w.emitted('update:modelValue')![0][0] as MedicationDraft
    expect(emitted.catalog_id).toBeNull()
    expect(emitted.medication_name).toBe('')
  })

  it('busca al tipear y selecciona un ítem del catálogo con defaults', async () => {
    suggestMedicationsMock.mockResolvedValue([
      {
        catalog_id: 'med-amox',
        name: 'Amoxicilina Clavulánico 250mg',
        form: 'tablet',
        concentration: '250mg',
        presentation: 'Caja x 10',
        prescription_required: true,
        default_route: 'oral',
        default_dose_unit: 'tablet',
      },
    ])

    const w = mountPicker({ modelValue: emptyMed, species: 'dog', orgId: 'org-1' })
    const input = w.get('[data-testid="medication-autocomplete-input"]')

    await input.setValue('Amox')
    vi.advanceTimersByTime(250)
    await flushPromises()

    expect(suggestMedicationsMock).toHaveBeenCalledWith('Amox', 'dog')
    expect(w.find('[data-testid="medication-suggestions-dropdown"]').exists()).toBe(true)
    expect(w.text()).toContain('Amoxicilina Clavulánico 250mg')
    expect(w.text()).toContain('Receta')

    // Seleccionar la sugerencia
    await w.get('[data-testid="suggestion-catalog-option"]').trigger('click')
    expect(w.emitted('update:modelValue')).toBeTruthy()
    const emitted = w.emitted('update:modelValue')![0][0] as MedicationDraft
    expect(emitted.catalog_id).toBe('med-amox')
    expect(emitted.medication_name).toBe('Amoxicilina Clavulánico 250mg')
    expect(emitted.route).toBe('oral')
    expect(emitted.dose_unit).toBe('tablet')
    expect(emitted.free_text).toBe(false)
  })

  it('permite seleccionar como texto libre', async () => {
    suggestMedicationsMock.mockResolvedValue([])
    const w = mountPicker({ modelValue: emptyMed, orgId: 'org-1' })
    const input = w.get('[data-testid="medication-autocomplete-input"]')

    await input.setValue('Compuesto Raro')
    vi.advanceTimersByTime(250)
    await flushPromises()

    expect(w.find('[data-testid="suggestion-freetext-option"]').exists()).toBe(true)
    await w.get('[data-testid="suggestion-freetext-option"]').trigger('click')

    expect(w.emitted('update:modelValue')).toBeTruthy()
    const emitted = w.emitted('update:modelValue')![0][0] as MedicationDraft
    expect(emitted.catalog_id).toBeNull()
    expect(emitted.medication_name).toBe('Compuesto Raro')
    expect(emitted.free_text).toBe(true)
  })

  it('abre modal para sugerir al catálogo y envía solicitud', async () => {
    createSuggestionMock.mockResolvedValue({ id: 'sug-1' })
    const w = mountPicker({ modelValue: emptyMed, orgId: 'org-1' })
    const input = w.get('[data-testid="medication-autocomplete-input"]')

    await input.setValue('Nuevo Fármaco')
    vi.advanceTimersByTime(250)
    await flushPromises()

    await w.get('[data-testid="suggestion-propose-option"]').trigger('click')
    expect(w.find('[data-testid="suggest-medication-modal"]').exists()).toBe(true)

    // Llenar formulario modal y enviar
    await w.get('[data-testid="suggest-notes-input"]').setValue('Dosis sugerida 1mg/kg')
    await w.get('[data-testid="suggest-submit-btn"]').trigger('submit')
    await flushPromises()

    expect(createSuggestionMock).toHaveBeenCalledWith({
      organization_id: 'org-1',
      name: 'Nuevo Fármaco',
      notes: 'Dosis sugerida 1mg/kg',
    })
  })
})
