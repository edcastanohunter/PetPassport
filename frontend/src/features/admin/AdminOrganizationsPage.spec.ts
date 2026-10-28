import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AdminOrganizationsPage from './AdminOrganizationsPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError, type Organization } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }), setTokenProvider: vi.fn() }
})

const org = {
  id: 'org-1',
  version: 1,
  type: 'clinic',
  legal_name: 'Veterinaria Sur S.A.S.',
  display_name: 'Clínica Sur',
  tax_id: '900123456',
  address: { country: 'CO', region: 'Antioquia', city: 'Medellín', line1: 'Cra 1 # 2-3' },
  contact: { email: 'hola@sur.co', phone: '+573001112233' },
  public_code: '7K3MQ2',
  status: 'pending_verification',
  member_count: 1,
  founder: { id: 'u-1', email: 'ana@sur.co', display_name: 'Ana' },
  created_at: '2026-10-20T10:00:00Z',
  updated_at: '2026-10-20T10:00:00Z',
} as Organization

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(AdminOrganizationsPage, { global: { plugins: [i18n] } })
}

describe('AdminOrganizationsPage (FE-009, PRD-017)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    POST.mockReset()
    GET.mockResolvedValue({ data: { data: [org], next_cursor: null } })
  })

  it('abre la cola de verificación y muestra el detalle con fundador', async () => {
    const w = mountPage()
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/admin/organizations', {
      params: { query: { status: 'pending_verification', q: undefined, cursor: undefined } },
    })
    await w.get('[data-testid="org-row"]').trigger('click')
    const detail = w.get('[data-testid="org-detail"]')
    expect(detail.text()).toContain('Veterinaria Sur S.A.S.')
    expect(w.get('[data-testid="org-founder"]').text()).toContain('ana@sur.co')
    expect(w.find('[data-testid="action-verify"]').exists()).toBe(true)
    expect(w.find('[data-testid="action-reject"]').exists()).toBe(true)
    expect(w.find('[data-testid="action-suspend"]').exists()).toBe(false)
  })

  it('verifica sin motivo (opcional) y la saca de la cola', async () => {
    POST.mockResolvedValue({ data: { ...org, status: 'active' } })
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="org-row"]').trigger('click')
    await w.get('[data-testid="action-verify"]').trigger('click')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/organizations/{orgId}/verify', {
      params: { path: { orgId: 'org-1' } },
      body: {},
    })
    expect(w.find('[data-testid="org-row"]').exists()).toBe(false)
    expect(w.text()).toContain('Clínica Sur verificada')
  })

  it('rechazar exige motivo ≥ 10 caracteres antes de llamar a la API', async () => {
    POST.mockResolvedValue({ data: { ...org, status: 'rejected' } })
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="org-row"]').trigger('click')
    await w.get('[data-testid="action-reject"]').trigger('click')
    await w.get('#admin-reason').setValue('corto')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    expect(POST).not.toHaveBeenCalled()
    await w.get('#admin-reason').setValue('  NIT no corresponde a la razón social  ')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/admin/organizations/{orgId}/reject', {
      params: { path: { orgId: 'org-1' } },
      body: { reason: 'NIT no corresponde a la razón social' },
    })
  })

  it('transición inválida (otro admin actuó) muestra el error y recarga', async () => {
    POST.mockRejectedValue(new ApiError({ code: 'INVALID_STATUS_TRANSITION' }, 409))
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="org-row"]').trigger('click')
    await w.get('[data-testid="action-verify"]').trigger('click')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('no está en un estado que permita esta acción')
    expect(GET).toHaveBeenCalledTimes(2)
    expect(w.find('[data-testid="reason-dialog"]').exists()).toBe(false)
  })

  it('pestaña suspendidas ofrece reactivar', async () => {
    const w = mountPage()
    await flushPromises()
    GET.mockResolvedValue({ data: { data: [{ ...org, status: 'suspended' }], next_cursor: null } })
    await w.get('[data-testid="tab-suspended"]').trigger('click')
    await flushPromises()
    expect(GET).toHaveBeenLastCalledWith('/admin/organizations', {
      params: { query: { status: 'suspended', q: undefined, cursor: undefined } },
    })
    await w.get('[data-testid="org-row"]').trigger('click')
    expect(w.find('[data-testid="action-reactivate"]').exists()).toBe(true)
  })
})
