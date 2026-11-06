import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AdminPlatformPage from './AdminPlatformPage.vue'
import es from '@/app/i18n/locales/es.json'
import * as platformApi from './platformApi'

vi.mock('./platformApi', () => ({
  getPlatformStats: vi.fn(),
  getTopOrganizations: vi.fn(),
  getSearchIndices: vi.fn(),
  reindexIndex: vi.fn(),
  retryIndexQueue: vi.fn(),
  purgeCache: vi.fn(),
}))

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminPlatformPage, {
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('AdminPlatformPage (PP-F3-FE-006)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()

    vi.mocked(platformApi.getPlatformStats).mockResolvedValue([
      {
        date: '2026-11-04',
        users_active: 120,
        pets: 350,
        encounters_signed: 42,
        organizations: { active: 15 },
      },
      {
        date: '2026-11-05',
        users_active: 125,
        pets: 360,
        encounters_signed: 50,
        organizations: { active: 16 },
      },
    ])

    vi.mocked(platformApi.getTopOrganizations).mockResolvedValue([
      {
        organization_id: 'org-1',
        display_name: 'Clínica Veterinaria San Roque',
        metric: 'encounters_signed',
        count: 85,
      },
    ])

    vi.mocked(platformApi.getSearchIndices).mockResolvedValue([
      {
        alias: 'clinical-encounters',
        physical: 'clinical-encounters-v2',
        docs: 1500,
        size_bytes: 4500000,
        mapping_version: 2,
        repo_mapping_version: 2,
        drift: false,
        lag_seconds: 0,
      },
    ])
  })

  it('muestra las tarjetas KPI y las métricas más recientes', async () => {
    const w = mountPage()
    await flushPromises()

    expect(w.get('[data-testid="kpi-users"]').text()).toBe('125')
    expect(w.get('[data-testid="kpi-pets"]').text()).toBe('360')
    expect(w.get('[data-testid="kpi-encounters"]').text()).toBe('50')
    expect(w.get('[data-testid="kpi-orgs"]').text()).toBe('16')
  })

  it('lista la tabla de top organizaciones y los índices de búsqueda', async () => {
    const w = mountPage()
    await flushPromises()

    expect(w.text()).toContain('Clínica Veterinaria San Roque')
    expect(w.text()).toContain('85')
    expect(w.text()).toContain('clinical-encounters')
    expect(w.text()).toContain('Actualizado')
  })

  it('permite reintentar la cola de índices asíncronos', async () => {
    vi.mocked(platformApi.retryIndexQueue).mockResolvedValue({ retried: 3 })
    const w = mountPage()
    await flushPromises()

    const retryBtn = w.findAll('button').find((b) => b.text().includes('Reintentar cola'))
    expect(retryBtn).toBeDefined()
    await retryBtn?.trigger('click')
    await flushPromises()

    expect(platformApi.retryIndexQueue).toHaveBeenCalled()
    expect(w.text()).toContain('Se reintentaron 3 eventos en la cola')
  })
})
