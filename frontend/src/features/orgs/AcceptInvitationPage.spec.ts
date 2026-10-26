import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AcceptInvitationPage from './AcceptInvitationPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError } from '@/shared/api/client'
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
  useRoute: () => ({ params: { token: 'tok_tok_tok_tok_tok_tok' }, fullPath: '/invitations/tok' }),
}))

const preview = {
  organization: {
    id: 'org-1',
    type: 'clinic',
    display_name: 'Clínica Sur',
    city: 'Medellín',
    country: 'CO',
    status: 'active',
  },
  role: 'veterinarian',
  email_masked: 'v***@sur.co',
  expires_at: '2026-11-02T10:00:00Z',
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(AcceptInvitationPage, { global: { plugins: [i18n] } })
}

describe('AcceptInvitationPage (FE-007, PRD-013)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    POST.mockReset()
    replace.mockReset()
    const s = useSessionStore()
    s.me = { email: 'otro@example.com' } as never
  })

  it('muestra la previsualización y al aceptar va a la organización', async () => {
    GET.mockResolvedValue({ data: preview })
    POST.mockResolvedValue({ data: { id: 'u', memberships: [], consent_outdated: false } })
    const w = mountPage()
    await flushPromises()
    expect(w.text()).toContain('Clínica Sur')
    expect(w.text()).toContain('v***@sur.co')
    await w.get('[data-testid="accept-invitation"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/invitations/{token}/accept', {
      params: { path: { token: 'tok_tok_tok_tok_tok_tok' } },
    })
    expect(replace).toHaveBeenCalledWith({ name: 'org-summary', params: { orgId: 'org-1' } })
  })

  it('email distinto: mensaje claro y cambiar de cuenta', async () => {
    GET.mockResolvedValue({ data: preview })
    POST.mockRejectedValue(new ApiError({ code: 'INVITATION_EMAIL_MISMATCH' }, 403))
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="accept-invitation"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="invitation-mismatch"]').text()).toContain('otro@example.com')
  })

  it('token inválido o caducado → 404 como enlace inválido', async () => {
    GET.mockRejectedValue(new ApiError({ code: 'NOT_FOUND' }, 404))
    const w = mountPage()
    await flushPromises()
    expect(w.find('[data-testid="invitation-invalid"]').exists()).toBe(true)
  })
})
