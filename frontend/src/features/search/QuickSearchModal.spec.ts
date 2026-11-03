import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import QuickSearchModal from './QuickSearchModal.vue'
import * as searchApi from './searchApi'

const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })

vi.mock('./searchApi', () => ({
  suggestPatients: vi.fn(),
}))

const mockRouter = {
  push: vi.fn(),
}
vi.mock('vue-router', () => ({
  useRouter: () => mockRouter,
}))

function mountModal(props: any) {
  return mount(QuickSearchModal, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('QuickSearchModal.vue (PP-F3-FE-001)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('opens on button click and shows input', async () => {
    const wrapper = mountModal({ orgId: 'org-123' })

    expect(wrapper.find('[data-testid="quick-search-backdrop"]').exists()).toBe(false)
    const trigger = wrapper.find('[data-testid="quick-search-trigger"]')
    await trigger.trigger('click')

    expect(wrapper.find('[data-testid="quick-search-backdrop"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-search-input"]').exists()).toBe(true)
  })

  it('debounces and suggests results on input', async () => {
    vi.useFakeTimers()
    const mockSuggest = vi.mocked(searchApi.suggestPatients).mockResolvedValueOnce({
      data: [
        {
          pet_id: 'p1',
          name: 'Luna',
          species: 'dog',
          microchip_suffix: '1234',
          access_level: 'custody',
          guardian_display_name: 'Ana',
        },
      ],
    })

    const wrapper = mountModal({ orgId: 'org-123' })
    await wrapper.find('[data-testid="quick-search-trigger"]').trigger('click')

    const input = wrapper.find('[data-testid="quick-search-input"]')
    await input.setValue('Lun')

    expect(mockSuggest).not.toHaveBeenCalled()
    vi.advanceTimersByTime(150)
    expect(mockSuggest).toHaveBeenCalledWith('org-123', 'Lun', expect.any(AbortSignal))

    await vi.runAllTimersAsync()
    await wrapper.vm.$nextTick()

    const item = wrapper.find('[data-testid="suggest-item"]')
    expect(item.exists()).toBe(true)
    expect(item.text()).toContain('Luna')
    expect(item.text()).toContain('1234')
    expect(wrapper.find('[data-testid="limited-access-badge"]').exists()).toBe(true)

    vi.useRealTimers()
  })

  it('navigates to patient page when result is clicked', async () => {
    vi.useFakeTimers()
    vi.mocked(searchApi.suggestPatients).mockResolvedValueOnce({
      data: [
        {
          pet_id: 'p1',
          name: 'Luna',
          species: 'dog',
          access_level: 'linked',
        },
      ],
    })

    const wrapper = mountModal({ orgId: 'org-123' })
    await wrapper.find('[data-testid="quick-search-trigger"]').trigger('click')
    await wrapper.find('[data-testid="quick-search-input"]').setValue('Luna')
    vi.advanceTimersByTime(150)
    await vi.runAllTimersAsync()
    await wrapper.vm.$nextTick()

    const item = wrapper.find('[data-testid="suggest-item"]')
    await item.trigger('click')

    expect(mockRouter.push).toHaveBeenCalledWith({
      name: 'org-patient',
      params: { orgId: 'org-123', petId: 'p1' },
    })

    vi.useRealTimers()
  })

  it('navigates to full search page on Enter when no item is selected', async () => {
    const wrapper = mountModal({ orgId: 'org-123' })
    await wrapper.find('[data-testid="quick-search-trigger"]').trigger('click')
    const input = wrapper.find('[data-testid="quick-search-input"]')
    await input.setValue('Beagle')
    await input.trigger('keydown', { key: 'Enter' })

    expect(mockRouter.push).toHaveBeenCalledWith({
      name: 'org-search',
      params: { orgId: 'org-123' },
      query: { q: 'Beagle' },
    })
  })
})
