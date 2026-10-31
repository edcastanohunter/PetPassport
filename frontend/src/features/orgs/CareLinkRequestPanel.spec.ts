import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CareLinkRequestPanel from './CareLinkRequestPanel.vue'
import OrgStatsCard from './OrgStatsCard.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST }) }
})

function mountWith<T>(component: T, props: Record<string, unknown>) {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(component as never, { props, global: { plugins: [i18n] } })
}

const luna = { pet_id: 'p1', name: 'Luna', species: 'dog', guardian_masked: 'A*** T***' }
const pending = {
  id: 'r1',
  pet_id: 'p1',
  pet_name: 'Luna',
  organization_id: 'o1',
  status: 'pending',
  expires_at: '2026-11-16T10:00:00Z',
  created_at: '2026-11-02T10:00:00Z',
}

describe('CareLinkRequestPanel (FE-008, PRD-020)', () => {
  beforeEach(() => {
    GET.mockReset()
    POST.mockReset()
    GET.mockImplementation((path: string) =>
      Promise.resolve(
        path.endsWith('/care-link-requests') ? { data: { data: [] } } : { data: { data: [luna] } },
      ),
    )
  })

  it('busca por microchip, muestra el tutor enmascarado y envía la solicitud', async () => {
    POST.mockResolvedValue({ data: pending })
    const w = mountWith(CareLinkRequestPanel, { orgId: 'o1' })
    await flushPromises()
    await w.get('#lookup-term').setValue(' 982000123456789 ')
    await w.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/organizations/{orgId}/pet-lookup', {
      params: { path: { orgId: 'o1' }, query: { microchip: '982000123456789' } },
    })
    expect(w.get('[data-testid="lookup-result"]').text()).toContain('A*** T***')

    await w.get('[data-testid="lookup-result"]').trigger('click')
    await w.get('#request-message').setValue('Atendemos a Luna')
    await w.get('form:not([role="search"])').trigger('submit')
    await flushPromises()
    expect(POST).toHaveBeenCalledTimes(1)
    const [path, opts] = POST.mock.calls[0]
    expect(path).toBe('/organizations/{orgId}/care-link-requests')
    expect(opts.body).toEqual({ pet_id: 'p1', message: 'Atendemos a Luna' })
    expect(opts.params.header['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(w.find('[data-testid="request-sent"]').exists()).toBe(true)
  })

  it('busca por email del tutor y avisa si no hay resultados', async () => {
    GET.mockImplementation((path: string) =>
      Promise.resolve(
        path.endsWith('/care-link-requests') ? { data: { data: [] } } : { data: { data: [] } },
      ),
    )
    const w = mountWith(CareLinkRequestPanel, { orgId: 'o1' })
    await flushPromises()
    await w.findAll('input[type="radio"]')[1].setValue(true)
    await w.get('#lookup-term').setValue('nadie@example.com')
    await w.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/organizations/{orgId}/pet-lookup', {
      params: { path: { orgId: 'o1' }, query: { guardian_email: 'nadie@example.com' } },
    })
    expect(w.find('[data-testid="lookup-empty"]').exists()).toBe(true)
  })

  it('muestra el error del servidor (límite de búsquedas)', async () => {
    GET.mockImplementation((path: string) =>
      path.endsWith('/care-link-requests')
        ? Promise.resolve({ data: { data: [] } })
        : Promise.resolve({ error: new ApiError({ code: 'RATE_LIMITED' }, 429) }),
    )
    const w = mountWith(CareLinkRequestPanel, { orgId: 'o1' })
    await flushPromises()
    await w.get('#lookup-term').setValue('982000123456789')
    await w.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="request-error"]').text()).toBe(
      'Demasiadas solicitudes. Espera un momento.',
    )
  })

  it('lista las solicitudes con su estado y caducidad', async () => {
    GET.mockImplementation(() => Promise.resolve({ data: { data: [pending] } }))
    const w = mountWith(CareLinkRequestPanel, { orgId: 'o1' })
    await flushPromises()
    const row = w.get('[data-testid="request-row"]')
    expect(row.text()).toContain('Luna')
    expect(row.text()).toContain('Pendiente')
    expect(row.text()).toContain('Caduca el')
  })
})

describe('OrgStatsCard (PRD-026)', () => {
  beforeEach(() => {
    GET.mockReset()
  })

  const month = (m: string, n: number) => ({
    month: m,
    encounters_signed: n,
    appointments: { confirmed: n, completed: n, cancelled: 0, no_show: 1 },
    new_patients: 2,
    vaccinations: 3,
  })

  it('compara el mes actual con el anterior', async () => {
    GET.mockResolvedValue({
      data: {
        current: month('2026-11', 12),
        previous: month('2026-10', 9),
        open_follow_ups: 4,
        freshness: { lag_seconds: 0 },
      },
    })
    const w = mountWith(OrgStatsCard, { orgId: 'o1' })
    await flushPromises()
    const rows = w.findAll('[data-testid="stats-row"]')
    expect(rows).toHaveLength(7)
    expect(rows[0].text()).toContain('Consultas firmadas')
    expect(rows[0].text()).toContain('12')
    expect(rows[0].text()).toContain('9')
    expect(w.text()).toContain('Controles pendientes')
    expect(w.text()).not.toContain('retraso')
  })

  it('avisa si la proyección va retrasada', async () => {
    GET.mockResolvedValue({
      data: {
        current: month('2026-11', 1),
        previous: month('2026-10', 1),
        open_follow_ups: 0,
        freshness: { lag_seconds: 125 },
      },
    })
    const w = mountWith(OrgStatsCard, { orgId: 'o1' })
    await flushPromises()
    expect(w.text()).toContain('125 s de retraso')
  })
})
