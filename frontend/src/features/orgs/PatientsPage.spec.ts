import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import PatientsPage from './PatientsPage.vue'
import es from '@/app/i18n/locales/es.json'
import * as provisionalApi from './provisionalPetsApi'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const push = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET }), setTokenProvider: vi.fn() }
})
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  useRoute: () => ({ params: { orgId: 'org-1' } }),
}))
vi.mock('./provisionalPetsApi', () => ({
  listProvisionalPets: vi.fn(),
  registerProvisionalPet: vi.fn(),
  resendClaim: vi.fn(),
  importProvisionalPets: vi.fn(),
}))

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: {
      es: {
        short: { year: 'numeric', month: 'short', day: 'numeric' },
      },
    },
  })
  return mount(PatientsPage, {
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
        CareLinkRequestPanel: { template: '<div />' },
      },
    },
  })
}

describe('PatientsPage (PP-F3-FE-009)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()

    const session = useSessionStore()
    session.me = {
      id: 'vet-1',
      memberships: [
        {
          organization_id: 'org-1',
          role: 'veterinarian',
          type: 'clinic',
          display_name: 'Clínica Veterinaria San Roque',
          org_status: 'active',
        },
      ],
    } as never

    GET.mockResolvedValue({
      data: {
        data: [],
        next_cursor: null,
      },
    })

    vi.mocked(provisionalApi.listProvisionalPets).mockResolvedValue([
      {
        pet_id: 'pet-prov-1',
        name: 'Rocky',
        species: 'dog',
        breed: 'Bulldog',
        sex: 'male',
        microchip: '985141000123456',
        created_at: '2026-11-05T12:00:00Z',
        claim_status: 'pending',
        resend_count: 1,
        contact_masked: 'tu***@example.com',
      },
    ])
  })

  it('permite cambiar a la pestaña de pacientes provisionales y ver la lista', async () => {
    const w = mountPage()
    await flushPromises()

    const provTab = w.get('[data-testid="tab-provisional"]')
    await provTab.trigger('click')
    await flushPromises()

    expect(provisionalApi.listProvisionalPets).toHaveBeenCalledWith('org-1', undefined)
    expect(w.text()).toContain('Rocky')
    expect(w.text()).toContain('985141000123456')
    expect(w.text()).toContain('tu***@example.com')
    expect(w.find('[data-testid="btn-resend-claim"]').exists()).toBe(true)
  })

  it('permite reenviar la invitación de reclamo', async () => {
    vi.mocked(provisionalApi.resendClaim).mockResolvedValue()
    const w = mountPage()
    await flushPromises()

    await w.get('[data-testid="tab-provisional"]').trigger('click')
    await flushPromises()

    const resendBtn = w.get('[data-testid="btn-resend-claim"]')
    await resendBtn.trigger('click')
    await flushPromises()

    expect(provisionalApi.resendClaim).toHaveBeenCalledWith('org-1', 'pet-prov-1')
    expect(w.text()).toContain('Invitación reenviada correctamente')
  })

  it('abre el modal de nuevo paciente provisional y registra', async () => {
    vi.mocked(provisionalApi.registerProvisionalPet).mockResolvedValue({ pet_id: 'pet-new-1' })
    const w = mountPage()
    await flushPromises()

    await w.get('[data-testid="btn-new-provisional"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('Nuevo paciente sin cuenta')
    const nameInput = w.find('input[required]')
    await nameInput.setValue('Toby')

    await w.find('form.card').trigger('submit')
    await flushPromises()

    expect(provisionalApi.registerProvisionalPet).toHaveBeenCalledWith(
      'org-1',
      expect.objectContaining({
        pet: expect.objectContaining({ name: 'Toby' }),
      }),
    )
  })
})
