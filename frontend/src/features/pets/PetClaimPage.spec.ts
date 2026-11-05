import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import PetClaimPage from './PetClaimPage.vue'
import es from '@/app/i18n/locales/es.json'
import { useSessionStore } from '@/features/auth/sessionStore'

const replace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace }),
  useRoute: () => ({ params: { token: 'claim-tok-123' }, fullPath: '/pet-claims/claim-tok-123' }),
}))

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(PetClaimPage, {
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('PetClaimPage (PP-F3-FE-009, PP-F3-PRD-013)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    replace.mockReset()
    const session = useSessionStore()
    session.user = { access_token: 'fake-jwt', expired: false } as never
  })

  it('previsualiza la mascota y clínica emisora', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          pet: { name: 'Milo', species: 'dog', breed: 'Beagle', sex: 'male' },
          organization: { display_name: 'Veterinaria Central' },
          contact_masked: 'tu***@gmail.com',
        }),
    } as unknown as Response)

    const w = mountPage()
    await flushPromises()

    expect(w.get('[data-testid="claim-pet-name"]').text()).toBe('Milo')
    expect(w.text()).toContain('Veterinaria Central')
    expect(w.text()).toContain('tu***@gmail.com')
    expect(w.find('[data-testid="btn-accept-claim"]').exists()).toBe(true)
    expect(w.find('[data-testid="microchip-duplicate-alert"]').exists()).toBe(false)
  })

  it('muestra alerta de duplicado de microchip y opción de fusionar', async () => {
    global.fetch = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes('/accept')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ id: 'pet-existing-1' }),
        })
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            pet: { name: 'Milo', species: 'dog', breed: 'Beagle' },
            organization: { display_name: 'Veterinaria Central' },
            contact_masked: 'tu***@gmail.com',
            existing_match: { pet_id: 'pet-existing-1', name: 'Milo Original' },
          }),
      })
    })

    const w = mountPage()
    await flushPromises()

    expect(w.find('[data-testid="microchip-duplicate-alert"]').exists()).toBe(true)
    expect(w.text()).toContain('¿Es la misma mascota que Milo Original?')

    const mergeBtn = w.get('[data-testid="btn-merge-claim"]')
    expect(mergeBtn.text()).toContain('Fusionar con Milo Original')

    await mergeBtn.trigger('click')
    await flushPromises()

    expect(replace).toHaveBeenCalledWith({
      name: 'pet-detail',
      params: { petId: 'pet-existing-1' },
    })
  })

  it('muestra aviso de caducidad si el token es inválido o 404', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
    } as unknown as Response)

    const w = mountPage()
    await flushPromises()

    expect(w.text()).toContain('Invitación no válida')
    expect(w.text()).toContain('Esta invitación no existe, ha expirado o ya fue reclamada.')
  })
})
