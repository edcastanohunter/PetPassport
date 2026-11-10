import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import StatusPage from './StatusPage.vue'
import { useStatusStore } from './statusStore'
import es from '@/app/i18n/locales/es.json'

const i18n = createI18n({
  legacy: false,
  locale: 'es',
  messages: { es },
})

describe('StatusPage (PP-F5-PRD-015, FE-009, ARQ-012)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('renders overall operational status card and components', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'operational',
      updated_at: '2026-11-10T12:00:00Z',
      components: [
        {
          id: 'web-app',
          name: 'Aplicación Web',
          status: 'operational',
          uptime_percentage_90d: 99.98,
        },
        { id: 'api', name: 'API REST', status: 'operational', uptime_percentage_90d: 99.95 },
      ],
      active_incidents: [],
      scheduled_maintenances: [],
    })

    const wrapper = mount(StatusPage, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    expect(wrapper.find('[data-testid="status-page"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="overall-status"]').text()).toContain(
      'Todos los sistemas operando con normalidad',
    )
    expect(wrapper.find('[data-testid="component-web-app"]').text()).toContain('Aplicación Web')
    expect(wrapper.find('[data-testid="component-web-app"]').text()).toContain('99.98%')
    expect(wrapper.find('[data-testid="component-api"]').text()).toContain('API REST')
  })

  it('renders active incidents when present', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'major_outage',
      updated_at: '2026-11-10T12:00:00Z',
      components: [
        { id: 'api', name: 'API REST', status: 'major_outage', uptime_percentage_90d: 98.5 },
      ],
      active_incidents: [
        {
          id: 'inc-1',
          name: 'Pérdida de conectividad en cluster EKS',
          status: 'investigating',
          severity: 'S1',
          impact: 'Servicios temporalmente no disponibles',
          created_at: '2026-11-10T12:05:00Z',
          updated_at: '2026-11-10T12:10:00Z',
          updates: [
            {
              id: 'u-1',
              status: 'investigating',
              body: 'Investigando conectividad en la zona us-east-1a.',
              created_at: '2026-11-10T12:10:00Z',
            },
          ],
        },
      ],
      scheduled_maintenances: [],
    })

    const wrapper = mount(StatusPage, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    expect(wrapper.find('[data-testid="active-incidents"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="active-incidents"]').text()).toContain(
      'Pérdida de conectividad en cluster EKS',
    )
    expect(wrapper.find('[data-testid="active-incidents"]').text()).toContain(
      'Investigando conectividad',
    )
  })

  it('renders scheduled maintenances when announced', () => {
    const store = useStatusStore()
    store.setMockStatus({
      status: 'operational',
      updated_at: '2026-11-10T12:00:00Z',
      components: [],
      active_incidents: [],
      scheduled_maintenances: [
        {
          id: 'm-1',
          name: 'Actualización Kubernetes v1.31',
          status: 'scheduled',
          scheduled_for: '2026-11-15T02:00:00Z',
          scheduled_until: '2026-11-15T04:00:00Z',
          impact: 'Sin corte esperado',
          description: 'Actualización periódica de seguridad en nodos trabajadores',
        },
      ],
    })

    const wrapper = mount(StatusPage, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: true },
      },
    })

    expect(wrapper.find('[data-testid="scheduled-maintenance"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="scheduled-maintenance"]').text()).toContain(
      'Actualización Kubernetes v1.31',
    )
  })
})
