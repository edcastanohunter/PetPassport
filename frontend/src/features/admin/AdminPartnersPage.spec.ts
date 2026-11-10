import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AdminPartnersPage from './AdminPartnersPage.vue'
import es from '@/app/i18n/locales/es.json'

const GET = vi.fn()
const POST = vi.fn()

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, POST }),
  }
})

const mockPartners = [
  {
    organization_id: '01930000-0000-7000-8000-00000000c001',
    display_name: 'Droguería San Mateo',
    mode: 'api' as const,
    status: 'active' as const,
    webhook_configured: true,
    total_quotes: 42,
    total_orders: 28,
    created_at: '2026-11-01T10:00:00Z',
  },
  {
    organization_id: '01930000-0000-7000-8000-00000000c002',
    display_name: 'Farmacia La Mascota',
    mode: 'portal' as const,
    status: 'suspended' as const,
    webhook_configured: false,
    total_quotes: 10,
    total_orders: 5,
    created_at: '2026-11-02T10:00:00Z',
  },
]

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminPartnersPage, { global: { plugins: [i18n] } })
}

describe('AdminPartnersPage (FE-011, PRD-018)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockResolvedValue({
      data: { items: mockPartners, total: 2 },
      response: { status: 200 },
    })
    POST.mockResolvedValue({
      data: { status: 'ok' },
      response: { status: 200 },
    })
  })

  it('lista partners y permite suspender y reactivar con motivo', async () => {
    const w = mountPage()
    await flushPromises()

    expect(GET).toHaveBeenCalledWith('/admin/partners')
    expect(w.text()).toContain('Droguería San Mateo')
    expect(w.text()).toContain('Farmacia La Mascota')

    // Click en Suspender
    const suspendBtn = w.findAll('button').find((b) => b.text().includes('Suspender'))
    expect(suspendBtn).toBeDefined()
    await suspendBtn?.trigger('click')
    await flushPromises()

    // Abre ReasonDialog
    expect(w.text()).toContain('Suspender Partner B2B')

    // Escribir motivo
    const textarea = w.get('textarea')
    await textarea.setValue('Incumplimiento reiterado de SLAs contractuales')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/admin/partners/{orgId}/suspend',
      expect.objectContaining({
        params: { path: { orgId: '01930000-0000-7000-8000-00000000c001' } },
      }),
    )
  })
})
