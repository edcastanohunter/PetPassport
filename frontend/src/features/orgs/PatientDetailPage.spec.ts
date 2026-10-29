import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import PatientDetailPage from './PatientDetailPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const POST = vi.fn()
const replace = vi.fn()
const query: Record<string, string> = {}
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }), setTokenProvider: vi.fn() }
})
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace, push: vi.fn() }),
  useRoute: () => ({ params: { orgId: 'org-1', petId: 'pet-1' }, query }),
}))

const res = () => ({ status: 200, headers: new Headers({ 'X-PP-Freshness': 'lag=0' }) }) as Response
const signed = {
  id: 'enc-1',
  version: 7,
  pet: { id: 'pet-1', name: 'Luna', species: 'dog' },
  organization_id: 'org-1',
  type: 'consultation',
  status: 'signed',
  started_at: '2026-10-20T09:30:00Z',
  reason: 'Vómitos',
  abandoned: false,
  last_activity_at: '2026-10-20T10:30:00Z',
}

function routeGets(access: 'linked' | 'custody') {
  GET.mockImplementation((path: string) => {
    switch (path) {
      case '/organizations/{orgId}/patients/{petId}':
        return access === 'linked'
          ? Promise.resolve({
              data: {
                id: 'pet-1',
                name: 'Luna',
                species: 'dog',
                sex: 'female',
                neutered: 'yes',
                status: 'active',
              },
            })
          : Promise.resolve({ error: new ApiError({ code: 'NOT_FOUND' }, 404) })
      case '/pets/{petId}/summary':
        return Promise.resolve({
          data: {
            access,
            alerts: [],
            next_due: [],
            open_follow_ups: [],
            last_encounter: signed,
            freshness: { lag_seconds: 0 },
          },
          response: res(),
        })
      case '/pets/{petId}/encounters':
        return Promise.resolve({ data: { data: [signed] } })
      default:
        return Promise.resolve({ data: undefined, response: res() })
    }
  })
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: {
      es: {
        short: { year: 'numeric', month: 'short', day: 'numeric' },
        long: {
          year: 'numeric',
          month: 'short',
          day: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
        },
      },
    },
  })
  return mount(PatientDetailPage, {
    global: { plugins: [i18n], stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })
}

describe('PatientDetailPage (FE-005, PRD-009)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    POST.mockReset()
    replace.mockReset()
    for (const k of Object.keys(query)) delete query[k]
    useSessionStore().me = {
      id: 'vet-1',
      memberships: [
        {
          organization_id: 'org-1',
          role: 'veterinarian',
          type: 'clinic',
          display_name: 'Clínica Sol',
          org_status: 'active',
        },
      ],
    } as never
  })

  it('con vínculo: pestañas y botón Abrir consulta, sin aviso de custodia', async () => {
    routeGets('linked')
    const w = mountPage()
    await flushPromises()
    expect(w.findAll('[role="tab"]').map((t) => t.text())).toEqual([
      'Resumen',
      'Historial',
      'Consultas',
      'Carnet',
      'Citas',
      'Peso',
    ])
    expect(w.find('[data-testid="open-encounter"]').exists()).toBe(true)
    expect(w.find('[data-testid="custody-page-banner"]').exists()).toBe(false)
  })

  it('solo custodia (vínculo revocado): aviso, sin abrir consulta, historia propia accesible', async () => {
    routeGets('custody')
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="custody-page-banner"]').text()).toContain(
      'Acceso limitado a sus propias consultas',
    )
    expect(w.find('[data-testid="open-encounter"]').exists()).toBe(false)
    expect(w.text()).not.toContain('La mascota no existe')
    expect(w.get('h1').text()).toBe('Luna')
  })

  it('las pestañas cambian por query y con flechas', async () => {
    routeGets('linked')
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="tab-summary"]').trigger('keydown', { key: 'ArrowRight' })
    expect(replace).toHaveBeenCalledWith({ query: { tab: 'history' } })
  })
})
