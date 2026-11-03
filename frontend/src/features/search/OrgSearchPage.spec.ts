import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import OrgSearchPage from './OrgSearchPage.vue'
import * as searchApi from './searchApi'

const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })

vi.mock('./searchApi', () => ({
  searchPatients: vi.fn(),
  searchClinical: vi.fn(),
}))

const mockRoute = {
  params: { orgId: 'org-test-123' },
  query: { q: 'Luna', tab: 'patients' },
}
const mockRouter = {
  replace: vi.fn(),
  push: vi.fn(),
}

vi.mock('vue-router', () => ({
  useRoute: () => mockRoute,
  useRouter: () => mockRouter,
}))

function mountPage() {
  return mount(OrgSearchPage, {
    global: { plugins: [i18n] },
  })
}

describe('OrgSearchPage.vue (PP-F3-FE-001, FE-011)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('performs patient search and displays results with highlighted text', async () => {
    vi.mocked(searchApi.searchPatients).mockResolvedValueOnce({
      data: [
        {
          pet: {
            id: 'pet-1',
            name: 'Luna',
            species: 'dog',
            breed: 'Beagle',
            sex: 'female',
            status: 'active',
            microchip_suffix: '5678',
          },
          guardian_display_name: 'Ana Gomez',
          access_level: 'linked',
          highlight: {
            name: '[[mark]]Luna[[/mark]]',
          },
        },
      ],
      facets: {
        species: [{ key: 'dog', count: 1 }],
      },
      freshness: { lag_seconds: 0 },
      mode: 'full',
    })

    const wrapper = mountPage()
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    expect(searchApi.searchPatients).toHaveBeenCalledWith('org-test-123', expect.any(Object))
    const card = wrapper.find('[data-testid="patient-card"]')
    expect(card.exists()).toBe(true)
    expect(card.find('mark').text()).toBe('Luna')
    expect(card.text()).toContain('Beagle')
    expect(card.text()).toContain('5678')
  })

  it('switches to clinical tab and renders grouped clinical hits', async () => {
    vi.mocked(searchApi.searchPatients).mockResolvedValueOnce({
      data: [],
      facets: {},
      freshness: { lag_seconds: 0 },
      mode: 'full',
    })

    vi.mocked(searchApi.searchClinical).mockResolvedValueOnce({
      data: [
        {
          pet: { id: 'pet-1', name: 'Luna', species: 'dog' },
          hits: [
            {
              doc_kind: 'encounter',
              ref_id: 'enc-1',
              occurred_at: '2026-10-15T10:00:00Z',
              organization: { name: 'Clinica Central' },
              encounter_type: 'consultation',
              diagnoses: [{ text: 'Otitis', certainty: 'confirmed' }],
              highlight: {
                text: ['Secreción en [[mark]]oído derecho[[/mark]]'],
              },
            },
          ],
        },
      ],
      facets: {
        species: [{ key: 'dog', count: 1 }],
        encounter_type: [{ key: 'consultation', count: 1 }],
      },
      freshness: { lag_seconds: 0 },
      mode: 'full',
    })

    const wrapper = mountPage()
    await wrapper.vm.$nextTick()

    const clinicalTab = wrapper.find('[data-testid="tab-clinical"]')
    await clinicalTab.trigger('click')
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    expect(searchApi.searchClinical).toHaveBeenCalled()
    const group = wrapper.find('[data-testid="clinical-group"]')
    expect(group.exists()).toBe(true)
    expect(group.text()).toContain('Luna')
    expect(group.text()).toContain('Otitis')

    const snippet = wrapper.find('[data-testid="highlighted-snippet"]')
    expect(snippet.exists()).toBe(true)
    expect(snippet.find('mark').text()).toBe('oído derecho')
  })
})
