import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import NotificationSettingsPage from './NotificationSettingsPage.vue'
import UnsubscribePage from './UnsubscribePage.vue'
import InboxPage from './InboxPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const PUT = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, PUT, POST }), setTokenProvider: vi.fn() }
})
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { token: 'tok_tok_tok_tok_tok_tok' } }),
}))

const prefs = () => ({
  email: {
    appointments: true,
    preventive_care: true,
    clinical: true,
    account: true,
    security: true,
  },
  digest: { clinic_daily_agenda: false },
})

function i18n() {
  return createI18n({ legacy: false, locale: 'es', messages: { es } })
}
const stubs = { RouterLink: { template: '<a><slot /></a>' } }

describe('NotificationSettingsPage (FE-012, NOT-004)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    PUT.mockReset()
  })

  it('carga, permite apagar una categoría y guarda; security queda fija', async () => {
    GET.mockResolvedValue({ data: prefs() })
    PUT.mockImplementation((_url: string, { body }: { body: unknown }) =>
      Promise.resolve({ data: body }),
    )
    const w = mount(NotificationSettingsPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    const security = w.get('[data-testid="pref-security"]')
    expect((security.element as HTMLInputElement).disabled).toBe(true)
    expect((security.element as HTMLInputElement).checked).toBe(true)
    await w.get('[data-testid="pref-preventive_care"]').setValue(false)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(PUT).toHaveBeenCalledTimes(1)
    expect(PUT.mock.calls[0][1].body.email.preventive_care).toBe(false)
    expect(w.find('[data-testid="saved"]').exists()).toBe(true)
  })

  it('el resumen diario solo se ofrece a administradores de clínica', async () => {
    GET.mockResolvedValue({ data: prefs() })
    const w = mount(NotificationSettingsPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(w.find('[data-testid="pref-digest"]').exists()).toBe(false)

    const s = useSessionStore()
    s.me = {
      memberships: [{ organization_id: 'o', role: 'clinic_admin', display_name: 'C' }],
    } as never
    const w2 = mount(NotificationSettingsPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(w2.find('[data-testid="pref-digest"]').exists()).toBe(true)
  })
})

describe('UnsubscribePage (FE-012)', () => {
  beforeEach(() => {
    POST.mockReset()
  })

  it('da de baja con el token y confirma la categoría', async () => {
    POST.mockResolvedValue({ data: { category: 'appointments' } })
    const w = mount(UnsubscribePage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/unsubscribe/{token}', {
      params: { path: { token: 'tok_tok_tok_tok_tok_tok' } },
    })
    expect(w.get('[data-testid="unsubscribed"]').text()).toContain('citas')
  })

  it('token inválido (404) muestra un mensaje neutro', async () => {
    POST.mockImplementation(() => Promise.reject(new ApiError({ code: 'NOT_FOUND' }, 404)))
    const w = mount(UnsubscribePage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(w.find('[data-testid="unsubscribe-invalid"]').exists()).toBe(true)
  })
})

describe('InboxPage (FE-012, PRD-020)', () => {
  beforeEach(() => {
    GET.mockReset()
    POST.mockReset()
  })

  const proposal = {
    id: 'a1',
    kind: 'appointment_proposal',
    ref_id: 'a1',
    pet_id: 'p1',
    pet_name: 'Luna',
    organization_id: 'o1',
    title: 'Clínica Sur',
    body: 'Control',
    status: 'pending',
    created_at: '2026-11-01T10:00:00Z',
  }
  const request = {
    id: 'r1',
    kind: 'care_link_request',
    ref_id: 'r1',
    pet_id: 'p1',
    pet_name: 'Luna',
    organization_id: 'o2',
    title: 'Clínica Norte',
    body: 'Atendemos a Luna',
    status: 'pending',
    created_at: '2026-11-02T10:00:00Z',
    expires_at: '2026-11-16T10:00:00Z',
  }

  it('lista propuestas de cita y las acepta', async () => {
    GET.mockResolvedValueOnce({ data: { data: [proposal] } }).mockResolvedValueOnce({
      data: { data: [] },
    })
    POST.mockResolvedValue({ data: {} })
    const w = mount(InboxPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(w.findAll('[data-testid="inbox-item"]')).toHaveLength(1)
    expect(w.text()).toContain('Clínica Sur propone otra franja para Luna')
    await w.get('button').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/appointments/{appointmentId}/accept-proposal', {
      params: { path: { appointmentId: 'a1' } },
    })
    expect(w.find('[data-testid="inbox-empty"]').exists()).toBe(true)
  })

  it('aprueba una solicitud de vínculo de una clínica', async () => {
    GET.mockResolvedValueOnce({ data: { data: [request] } }).mockResolvedValueOnce({
      data: { data: [] },
    })
    POST.mockResolvedValue({ data: {} })
    const w = mount(InboxPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    expect(w.get('[data-testid="inbox-carelink"]').text()).toContain(
      'Clínica Norte solicita acceso a la ficha de Luna',
    )
    await w.get('[data-testid="inbox-approve"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/pets/{petId}/care-link-requests/{requestId}/approve', {
      params: { path: { petId: 'p1', requestId: 'r1' } },
    })
    expect(w.get('[data-testid="inbox-done"]').text()).toBe('Acceso aprobado.')
    expect(w.find('[data-testid="inbox-empty"]').exists()).toBe(true)
  })

  it('rechaza la solicitud y, si ya caducó, recarga y muestra el error', async () => {
    GET.mockResolvedValue({ data: { data: [request] } })
    POST.mockResolvedValueOnce({ data: {} }).mockResolvedValueOnce({
      error: new ApiError({ code: 'CARE_LINK_REQUEST_NOT_PENDING' }, 409),
    })
    const w = mount(InboxPage, { global: { plugins: [i18n()], stubs } })
    await flushPromises()
    await w.get('[data-testid="inbox-decline"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenLastCalledWith('/pets/{petId}/care-link-requests/{requestId}/decline', {
      params: { path: { petId: 'p1', requestId: 'r1' } },
    })
    await w.get('[data-testid="inbox-approve"]').trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('La solicitud ya fue respondida o caducó.')
  })
})
