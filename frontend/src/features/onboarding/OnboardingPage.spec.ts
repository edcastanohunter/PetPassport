import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import OnboardingPage from './OnboardingPage.vue'
import es from '@/app/i18n/locales/es.json'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const POST = vi.fn()
const replace = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }), setTokenProvider: vi.fn() }
})
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace }),
  useRoute: () => ({ query: { returnTo: '/app' } }),
}))

const legal = {
  terms_version: '2026-10-draft',
  privacy_version: '2026-10-draft',
  terms_url: '/legal/terms',
  privacy_url: '/legal/privacy',
}

describe('OnboardingPage', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    POST.mockReset()
    replace.mockReset()
    GET.mockResolvedValue({ data: legal })
    const s = useSessionStore()
    s.user = { access_token: 't', expired: false } as never
    s.me = {
      id: 'x',
      email: 'ana@example.com',
      email_verified: true,
      display_name: 'Ana',
      locale: 'es',
      global_roles: [],
      memberships: [],
      onboarding_required: true,
      consent_outdated: true,
      status: 'active',
    }
  })

  it('recorre los 3 pasos y envía las versiones vigentes', async () => {
    POST.mockResolvedValue({
      data: { ...useSessionStore().me, onboarding_required: false, consent_outdated: false },
    })
    const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
    const w = mount(OnboardingPage, { global: { plugins: [i18n], stubs: { RouterLink: true } } })
    await flushPromises()
    expect(w.find('[data-testid="step-consent"]').exists()).toBe(true)
    expect((w.get('[data-testid="next"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid="accept-terms"]').setValue(true)
    await w.get('[data-testid="accept-privacy"]').setValue(true)
    await w.get('[data-testid="step-consent"]').trigger('submit')
    expect(w.find('[data-testid="step-basics"]').exists()).toBe(true)
    await w.get('#display_name').setValue('Ana Tutora')
    await w.get('#country').setValue('CO')
    await w.get('#phone').setValue('+57 300 123 4567')
    await w.get('[data-testid="step-basics"]').trigger('submit')
    expect(w.find('[data-testid="step-path"]').exists()).toBe(true)
    await w.get('[data-testid="path-owner"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/me/onboarding', {
      body: {
        display_name: 'Ana Tutora',
        country: 'CO',
        locale: 'es',
        phone: '+57 300 123 4567',
        accept_terms_version: '2026-10-draft',
        accept_privacy_version: '2026-10-draft',
      },
    })
    expect(replace).toHaveBeenCalledWith('/app')
    expect(useSessionStore().me?.onboarding_required).toBe(false)
  })
})
