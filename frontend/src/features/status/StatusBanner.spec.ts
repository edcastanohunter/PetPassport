import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import StatusBanner from './StatusBanner.vue'
import { useStatusStore } from './statusStore'
import es from '@/app/i18n/locales/es.json'

const i18n = createI18n({
  legacy: false,
  locale: 'es',
  messages: { es },
})

describe('StatusBanner (PP-F5-PRD-015, FE-009)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('does not render when all systems are operational and no incidents', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'operational',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [],
      scheduled_maintenances: [],
    })

    const wrapper = mount(StatusBanner, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    expect(wrapper.find('[data-testid="status-banner"]').exists()).toBe(false)
  })

  it('renders critical alert banner during S1 incident', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'major_outage',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [
        {
          id: 'inc-1',
          name: 'Interrupción en API de pagos',
          status: 'investigating',
          severity: 'S1',
          impact: 'Cobros no disponibles',
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          updates: [],
        },
      ],
      scheduled_maintenances: [],
    })

    const wrapper = mount(StatusBanner, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    const banner = wrapper.find('[data-testid="status-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('Interrupción en API de pagos')
    expect(banner.classes()).toContain('bg-red-600')
  })

  it('renders maintenance banner when scheduled maintenance is in progress', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'maintenance',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [],
      scheduled_maintenances: [
        {
          id: 'm-1',
          name: 'Ventana de corte K8s',
          status: 'in_progress',
          scheduled_for: new Date().toISOString(),
          scheduled_until: new Date().toISOString(),
          impact: 'Solo lectura temporal',
          description: 'Migración blue/green',
        },
      ],
    })

    const wrapper = mount(StatusBanner, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    const banner = wrapper.find('[data-testid="status-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('Ventana de corte K8s')
    expect(banner.classes()).toContain('bg-blue-600')
  })

  it('can be dismissed by clicking the close button', async () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'degraded_performance',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [],
      scheduled_maintenances: [],
    })

    const wrapper = mount(StatusBanner, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    expect(wrapper.find('[data-testid="status-banner"]').exists()).toBe(true)
    await wrapper.find('[data-testid="status-banner-dismiss"]').trigger('click')
    expect(wrapper.find('[data-testid="status-banner"]').exists()).toBe(false)
  })
})
