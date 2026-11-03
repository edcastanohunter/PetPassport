import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import OwnerSearchPage from './OwnerSearchPage.vue'
import * as searchApi from './searchApi'

const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })

vi.mock('./searchApi', () => ({
  searchMe: vi.fn(),
}))

const mockRoute = {
  query: { q: '' },
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
  return mount(OwnerSearchPage, {
    global: { plugins: [i18n] },
  })
}

describe('OwnerSearchPage.vue (PP-F3-FE-002, FE-011)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders suggested shortcuts and clicking one triggers search', async () => {
    vi.mocked(searchApi.searchMe).mockResolvedValueOnce({
      data: [],
      facets: {},
      freshness: { lag_seconds: 0 },
      mode: 'full',
    })

    const wrapper = mountPage()
    await wrapper.vm.$nextTick()

    const shortcuts = wrapper.findAll('[data-testid="shortcut-btn"]')
    expect(shortcuts.length).toBeGreaterThanOrEqual(2)

    // Click on "Última vacuna de rabia"
    await shortcuts[0].trigger('click')
    expect(mockRouter.replace).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({ q: 'última vacuna de rabia' }),
      }),
    )
  })

  it('renders owner search results with card and history buttons', async () => {
    vi.mocked(searchApi.searchMe).mockResolvedValueOnce({
      data: [
        {
          pet: { id: 'pet-123', name: 'Max', species: 'dog' },
          hits: [
            {
              doc_kind: 'encounter',
              ref_id: 'enc-99',
              occurred_at: '2026-09-01T08:00:00Z',
              organization: { name: 'Hospital Veterinario' },
              encounter_type: 'vaccination',
              diagnoses: [],
              highlight: {
                text: ['Vacuna antirrábica aplicada'],
              },
            },
          ],
        },
      ],
      facets: {},
      freshness: { lag_seconds: 0 },
      mode: 'full',
    })

    const wrapper = mountPage()
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    const group = wrapper.find('[data-testid="owner-pet-group"]')
    expect(group.exists()).toBe(true)
    expect(group.text()).toContain('Max')

    const cardBtn = wrapper.find('[data-testid="open-card-btn"]')
    expect(cardBtn.exists()).toBe(true)
    await cardBtn.trigger('click')
    expect(mockRouter.push).toHaveBeenCalledWith({
      name: 'pet-card',
      params: { petId: 'pet-123' },
    })

    const histBtn = wrapper.find('[data-testid="open-history-btn"]')
    expect(histBtn.exists()).toBe(true)
    await histBtn.trigger('click')
    expect(mockRouter.push).toHaveBeenCalledWith({
      name: 'pet-history',
      params: { petId: 'pet-123' },
    })
  })
})
