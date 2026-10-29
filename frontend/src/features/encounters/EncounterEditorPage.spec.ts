import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import EncounterEditorPage from './EncounterEditorPage.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError, type Encounter } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

const GET = vi.fn()
const POST = vi.fn()
const PATCH = vi.fn()
const PUT = vi.fn()
const DELETE = vi.fn()
const push = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST, PATCH, PUT, DELETE }), setTokenProvider: vi.fn() }
})
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  useRoute: () => ({ params: { orgId: 'org-1', encounterId: 'enc-1' }, query: {} }),
}))

const VET = 'vet-1'
function encounter(over: Partial<Encounter> = {}): Encounter {
  return {
    id: 'enc-1',
    version: 3,
    pet: { id: 'pet-1', name: 'Luna', species: 'dog' },
    organization_id: 'org-1',
    type: 'consultation',
    status: 'draft',
    started_at: '2026-10-20T09:30:00Z',
    reason: 'Vómitos',
    attending_vet: { id: VET, display_name: 'Dra. Pérez' },
    diagnoses: [{ text: 'Gastroenteritis', certainty: 'presumptive' }],
    plan: {},
    attachments: [],
    addenda: [],
    created_at: '2026-10-20T09:30:00Z',
    updated_at: '2026-10-20T09:30:00Z',
    ...over,
  }
}
const response = (status = 200) => ({ status, headers: new Headers() }) as Response

function routeGets(enc: Encounter) {
  GET.mockImplementation((path: string) => {
    if (path === '/encounters/{encounterId}')
      return Promise.resolve({ data: enc, response: response() })
    if (path === '/organizations/{orgId}/members') return Promise.resolve({ data: { data: [] } })
    if (path === '/pets/{petId}/summary')
      return Promise.resolve({
        data: { alerts: [], next_due: [], open_follow_ups: [], freshness: { lag_seconds: 0 } },
        response: response(),
      })
    return Promise.resolve({ data: undefined, response: response() })
  })
}

function login(role: string, userId = VET) {
  const s = useSessionStore()
  s.me = {
    id: userId,
    memberships: [
      {
        organization_id: 'org-1',
        role,
        type: 'clinic',
        display_name: 'Clínica Sol',
        org_status: 'active',
      },
    ],
  } as never
}

function mountPage() {
  const fmt = {
    short: { year: 'numeric', month: 'short', day: 'numeric' },
    long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
    time: { hour: '2-digit', minute: '2-digit', second: '2-digit' },
  } as const
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: fmt },
  })
  return mount(EncounterEditorPage, {
    global: { plugins: [i18n], stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })
}

describe('EncounterEditorPage (FE-003/FE-004)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    for (const f of [GET, POST, PATCH, PUT, DELETE, push]) f.mockReset()
  })
  afterEach(() => vi.useRealTimers())

  it('recepción: A/P y control en solo lectura con aviso (E4)', async () => {
    login('receptionist', 'rec-1')
    routeGets(encounter())
    const w = mountPage()
    await flushPromises()
    expect(w.text()).toContain('solo los edita un veterinario')
    const fieldsets = w.findAll('fieldset')
    expect(fieldsets.length).toBeGreaterThan(0)
    for (const f of fieldsets) expect(f.attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="subjective"]').attributes('disabled')).toBeUndefined()
  })

  it('autoguardado: PATCH con If-Match a los 5 s y aviso "Guardado"', async () => {
    vi.useFakeTimers()
    login('veterinarian')
    routeGets(encounter())
    PATCH.mockImplementation(() =>
      Promise.resolve({ data: encounter({ version: 4, reason: 'Vómitos y diarrea' }) }),
    )
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="reason"]').setValue('Vómitos y diarrea')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(PATCH).toHaveBeenCalledTimes(1)
    const [path, opts] = PATCH.mock.calls[0]!
    expect(path).toBe('/encounters/{encounterId}')
    expect(opts.params.header['If-Match']).toBe(3)
    expect(opts.body.reason).toBe('Vómitos y diarrea')
    expect(PUT).not.toHaveBeenCalled()
    expect(w.get('[data-testid="autosave-status"]').text()).toContain('Guardado')
  })

  it('409 detiene el autoguardado y ofrece diferencias y recarga', async () => {
    vi.useFakeTimers()
    login('veterinarian')
    routeGets(encounter())
    PATCH.mockResolvedValue({ error: new ApiError({ code: 'CONFLICT_VERSION' }, 409) })
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="reason"]').setValue('Otro motivo')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(w.find('[data-testid="conflict"]').exists()).toBe(true)
    await w.get('[data-testid="conflict"] button').trigger('click') // ver diferencias
    await flushPromises()
    expect(w.get('[data-testid="conflict-sections"]').text()).toContain('Motivo')
    await w.get('[data-testid="conflict-reload"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="conflict"]').exists()).toBe(false)
    expect((w.get('[data-testid="reason"]').element as HTMLInputElement).value).toBe('Vómitos')
  })

  it('ENCOUNTER_INCOMPLETE marca los ítems del checklist que indica el servidor', async () => {
    login('veterinarian')
    routeGets(encounter())
    POST.mockResolvedValue({
      error: new ApiError(
        { code: 'ENCOUNTER_INCOMPLETE', errors: [{ field: 'started_at', code: 'REQUIRED' }] },
        422,
      ),
    })
    const w = mountPage()
    await flushPromises()
    await w.get('[data-testid="sign-open"]').trigger('click')
    expect(w.text()).toContain('Al firmar, la consulta no podrá modificarse')
    await w.get('[data-testid="sign-confirm"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/encounters/{encounterId}/sign', {
      params: { path: { encounterId: 'enc-1' }, header: { 'If-Match': 3 } },
    })
    expect(w.get('[data-testid="check-started_at"]').classes()).toContain('font-semibold')
    expect(w.get('[data-testid="check-content"]').classes()).not.toContain('font-semibold')
  })

  it('el botón Firmar exige ser el responsable', async () => {
    login('veterinarian', 'otro-vet')
    routeGets(encounter())
    const w = mountPage()
    await flushPromises()
    expect(w.get('[data-testid="sign-open"]').attributes('disabled')).toBeDefined()
  })

  it('firmada: solo lectura con sello, hash abreviado y adendas para el responsable', async () => {
    login('veterinarian')
    routeGets(
      encounter({
        status: 'signed',
        signature: {
          signed_by: { id: VET, display_name: 'Dra. Pérez' },
          signed_at: '2026-10-20T10:30:00Z',
          stream_version: 7,
          content_hash: 'ab'.repeat(32),
        },
        addenda: [
          {
            seq: 1,
            text: 'Resultados normales',
            reason: 'lab_results',
            signed_by: { id: VET, display_name: 'Dra. Pérez' },
            signed_at: '2026-10-22T10:30:00Z',
            attachments: [],
          },
        ],
      }),
    )
    const w = mountPage()
    await flushPromises()
    expect(w.find('[data-testid="soap-nav"]').exists()).toBe(false)
    const seal = w.get('[data-testid="signature-seal"]').text()
    expect(seal).toContain('Dra. Pérez')
    expect(seal).toContain('abababab…abababab')
    expect(w.get('[data-testid="addenda"]').text()).toContain('Adenda 1')
    expect(w.find('[data-testid="addendum-open"]').exists()).toBe(true)
  })
})
