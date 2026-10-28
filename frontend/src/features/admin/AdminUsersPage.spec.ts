import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AdminUsersPage from './AdminUsersPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError, type AdminUserRow } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }), setTokenProvider: vi.fn() }
})

const row = (id: string, email: string, extra: Partial<AdminUserRow> = {}): AdminUserRow => ({
  id,
  email,
  display_name: 'Bea',
  status: 'active',
  global_roles: ['pet_owner'],
  created_at: '2026-10-01T10:00:00Z',
  last_login_at: null,
  pet_count: 2,
  memberships: [
    {
      organization_id: 'org-1',
      display_name: 'Clínica Sur',
      type: 'clinic',
      role: 'veterinarian',
      org_status: 'active',
    },
  ],
  ...extra,
})

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(AdminUsersPage, { global: { plugins: [i18n] } })
}

describe('AdminUsersPage (FE-009, PRD-018)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    POST.mockReset()
    useSessionStore().me = { id: 'admin-1' } as never
    GET.mockResolvedValue({
      data: {
        data: [
          row('u-1', 'bea@example.com'),
          row('admin-1', 'root@example.com', { global_roles: ['super_admin'] }),
        ],
        next_cursor: null,
      },
    })
  })

  it('busca con filtros y muestra roles, membresías y mascotas', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('#user-search').setValue('  bea ')
    await w.get('#user-status').setValue('active')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(GET).toHaveBeenLastCalledWith('/admin/users', {
      params: { query: { q: 'bea', status: 'active', cursor: undefined } },
    })
    const first = w.findAll('[data-testid="user-row"]')[0]!
    expect(first.text()).toContain('Clínica Sur · Veterinario/a')
    expect(first.text()).toContain('pet_owner')
  })

  it('no permite suspender la propia cuenta', async () => {
    const w = mountPage()
    await flushPromises()
    const buttons = w.findAll('[data-testid="suspend-user"]')
    expect(buttons[0]!.attributes('disabled')).toBeUndefined()
    expect(buttons[1]!.attributes('disabled')).toBeDefined()
  })

  it('suspende con motivo y actualiza la fila', async () => {
    POST.mockResolvedValue({
      data: row('u-1', 'bea@example.com', {
        status: 'suspended',
        status_reason: 'Fraude reportado',
      }),
    })
    const w = mountPage()
    await flushPromises()
    await w.findAll('[data-testid="suspend-user"]')[0]!.trigger('click')
    await w.get('#admin-reason').setValue('Fraude reportado')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/users/{userId}/suspend', {
      params: { path: { userId: 'u-1' } },
      body: { reason: 'Fraude reportado' },
    })
    expect(w.findAll('[data-testid="user-row"]')[0]!.text()).toContain('Suspendida')
    expect(w.find('[data-testid="reactivate-user"]').exists()).toBe(true)
  })

  it('Keycloak caído (503) se informa y el diálogo sigue abierto para reintentar', async () => {
    POST.mockRejectedValue(new ApiError({ code: 'UNAVAILABLE' }, 503))
    const w = mountPage()
    await flushPromises()
    await w.findAll('[data-testid="suspend-user"]')[0]!.trigger('click')
    await w.get('#admin-reason').setValue('Fraude reportado')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Servicio no disponible temporalmente')
    expect(w.find('[data-testid="reason-dialog"]').exists()).toBe(true)
  })
})
