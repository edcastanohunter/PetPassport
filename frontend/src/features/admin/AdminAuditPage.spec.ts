import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import AdminAuditPage from './AdminAuditPage.vue'
import es from '@/app/i18n/locales/es.json'

const GET = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET }), setTokenProvider: vi.fn() }
})

const ACTOR = '01930000-0000-7000-8000-00000000a001'

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: {
      es: { long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit' } },
    },
  })
  return mount(AdminAuditPage, { global: { plugins: [i18n] } })
}

describe('AdminAuditPage (FE-009)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    GET.mockReset()
    GET.mockResolvedValue({
      data: {
        data: [
          {
            id: 'a-1',
            occurred_at: '2026-10-26T10:00:00Z',
            actor_id: ACTOR,
            actor_roles: ['super_admin'],
            action: 'admin.org.verify',
            target_type: 'organization',
            target_id: 'org-1',
            reason: null,
            correlation_id: null,
          },
        ],
        next_cursor: 'c1',
      },
    })
  })

  it('carga sin filtros y pagina con el cursor', async () => {
    const w = mountPage()
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/admin/audit-log', {
      params: { query: { cursor: undefined } },
    })
    expect(w.get('[data-testid="audit-row"]').text()).toContain('admin.org.verify')
    await w.get('button.btn-secondary').trigger('click')
    await flushPromises()
    expect(GET).toHaveBeenLastCalledWith('/admin/audit-log', {
      params: { query: { cursor: 'c1' } },
    })
    expect(w.findAll('[data-testid="audit-row"]')).toHaveLength(2)
  })

  it('filtra por actor y rango; el día final se incluye completo', async () => {
    const w = mountPage()
    await flushPromises()
    await w.get('#audit-actor').setValue(ACTOR)
    await w.get('#audit-from').setValue('2026-10-01')
    await w.get('#audit-to').setValue('2026-10-26')
    await w.get('form').trigger('submit')
    await flushPromises()
    const query = GET.mock.lastCall![1].params.query
    expect(query.actor_id).toBe(ACTOR)
    expect(query.target_id).toBeUndefined()
    expect(new Date(query.to).getTime() - new Date(query.from).getTime()).toBe(
      new Date(2026, 9, 27).getTime() - new Date(2026, 9, 1).getTime(),
    )
  })

  it('id con formato inválido o rango invertido no llaman a la API', async () => {
    const w = mountPage()
    await flushPromises()
    GET.mockClear()
    await w.get('#audit-actor').setValue('no-es-uuid')
    await w.get('form').trigger('submit')
    expect(GET).not.toHaveBeenCalled()
    await w.get('#audit-actor').setValue('')
    await w.get('#audit-from').setValue('2026-10-26')
    await w.get('#audit-to').setValue('2026-10-01')
    await w.get('form').trigger('submit')
    expect(GET).not.toHaveBeenCalled()
    expect(w.text()).toContain('La fecha final debe ser posterior')
  })
})
