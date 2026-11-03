import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import SearchDegradationBanner from './SearchDegradationBanner.vue'

const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })

function mountBanner(props: any) {
  return mount(SearchDegradationBanner, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('SearchDegradationBanner.vue (PP-F3-FE-011)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders nothing when mode is full and lag is low', () => {
    const wrapper = mountBanner({
      mode: 'full',
      lagSeconds: 2,
    })
    expect(wrapper.find('[data-testid="search-degraded-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="search-lag-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="search-ratelimit-banner"]').exists()).toBe(false)
  })

  it('renders degraded banner when mode is degraded and emits retry on button click', async () => {
    const wrapper = mountBanner({
      mode: 'degraded',
    })
    const banner = wrapper.find('[data-testid="search-degraded-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('solo nombre y microchip')

    const retryBtn = wrapper.find('[data-testid="search-retry-btn"]')
    await retryBtn.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('automatically emits retry after 30 seconds countdown in degraded mode', async () => {
    const wrapper = mountBanner({
      mode: 'degraded',
    })
    expect(wrapper.emitted('retry')).toBeUndefined()

    // Avanzar 30 segundos
    vi.advanceTimersByTime(30000)
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('renders freshness banner when lag is greater than 10 seconds', async () => {
    const wrapper = mountBanner({
      mode: 'full',
      lagSeconds: 15,
    })
    const lagBanner = wrapper.find('[data-testid="search-lag-banner"]')
    expect(lagBanner.exists()).toBe(true)
    expect(lagBanner.text()).toContain('15')

    const reloadBtn = wrapper.find('[data-testid="search-reload-freshness-btn"]')
    await reloadBtn.trigger('click')
    expect(wrapper.emitted('reload')).toHaveLength(1)
  })

  it('renders rate limit banner with countdown when retryAfter is set', async () => {
    const wrapper = mountBanner({
      retryAfter: 10,
    })
    const rlBanner = wrapper.find('[data-testid="search-ratelimit-banner"]')
    expect(rlBanner.exists()).toBe(true)
    expect(rlBanner.text()).toContain('10')

    vi.advanceTimersByTime(2000)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="search-ratelimit-banner"]').text()).toContain('8')
  })
})
