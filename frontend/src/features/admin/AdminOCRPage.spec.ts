import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AdminOCRPage from './AdminOCRPage.vue'
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

const mockMonitor = {
  total_24h: 340,
  success_rate_percent: 98.5,
  avg_latency_ms: 1250,
  queue_depth: 3,
}

const mockSamples = [
  {
    id: '01930000-0000-7000-8000-00000000s001',
    prescription_id: '01930000-0000-7000-8000-00000000p001',
    status: 'confirmed',
    confidence: 0.94,
    latency_ms: 1180,
    created_at: '2026-11-09T10:00:00Z',
  },
]

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
  })
  return mount(AdminOCRPage, { global: { plugins: [i18n] } })
}

describe('AdminOCRPage (FE-011, OCR-010)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockImplementation((path: string) => {
      if (path.includes('/monitor'))
        return Promise.resolve({ data: mockMonitor, response: { status: 200 } })
      if (path.includes('/samples'))
        return Promise.resolve({
          data: { items: mockSamples, total: 1 },
          response: { status: 200 },
        })
      return Promise.resolve({ data: {}, response: { status: 200 } })
    })

    POST.mockResolvedValue({
      data: { prescription_id: '01930000-0000-7000-8000-00000000p001', status: 'revoked' },
      response: { status: 200 },
    })
  })

  it('muestra telemetría OCR y permite revocar receta de emergencia con motivo', async () => {
    const w = mountPage()
    await flushPromises()

    expect(GET).toHaveBeenCalledWith('/admin/ocr/monitor')
    expect(GET).toHaveBeenCalledWith('/admin/ocr/samples')

    expect(w.text()).toContain('340') // total_24h
    expect(w.text()).toContain('98.5%') // success_rate_percent
    expect(w.text()).toContain('1250 ms') // avg_latency_ms
    expect(w.text()).toContain('94%') // confidence

    // Revocar receta
    const revokeBtn = w.findAll('button').find((b) => b.text().includes('Revocar Receta'))
    expect(revokeBtn).toBeDefined()
    await revokeBtn?.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('Revocar Receta Médica de Emergencia')

    const textarea = w.get('textarea')
    await textarea.setValue('Dosis letal identificada en extracción errónea')
    await w.get('[data-testid="reason-dialog"]').trigger('submit')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/admin/prescriptions/{prescriptionId}/revoke',
      expect.objectContaining({
        params: { path: { prescriptionId: '01930000-0000-7000-8000-00000000p001' } },
        body: { reason: 'Dosis letal identificada en extracción errónea' },
      }),
    )
  })
})
