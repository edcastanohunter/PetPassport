import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PdfButton from './PdfButton.vue'
import es from '@/app/i18n/locales/es.json'

const GET = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET }) }
})
const open = vi.fn()
vi.mock('./openUrl', () => ({ openUrl: (u: string) => open(u) }))

function mountButton() {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(PdfButton, { props: { encounterId: 'enc-1' }, global: { plugins: [i18n] } })
}

// PP-F2-FE-004: "Generando…" con poll a /documents/{job} y luego descarga.
describe('PdfButton', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    GET.mockReset()
    open.mockReset()
  })
  afterEach(() => vi.useRealTimers())

  it('listo: pide JSON y abre la URL prefirmada', async () => {
    GET.mockResolvedValue({
      data: { url: 'https://s3/v1.pdf', expires_at: '2026-10-28T10:15:00Z' },
      response: { status: 200 },
    })
    const w = mountButton()
    await w.get('[data-testid="pdf-button"]').trigger('click')
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/encounters/{encounterId}/pdf', {
      params: { path: { encounterId: 'enc-1' }, header: { Accept: 'application/json' } },
    })
    expect(open).toHaveBeenCalledWith('https://s3/v1.pdf')
  })

  it('202: muestra Generando…, consulta el trabajo y ofrece el enlace', async () => {
    GET.mockImplementation((path: string) => {
      if (path === '/encounters/{encounterId}/pdf')
        return Promise.resolve({
          data: { id: 'job-1', status: 'pending' },
          response: { status: 202 },
        })
      return Promise.resolve({
        data:
          GET.mock.calls.filter((c) => c[0] === '/documents/{jobId}').length < 2
            ? { id: 'job-1', status: 'rendering' }
            : { id: 'job-1', status: 'ready', url: 'https://s3/v2.pdf' },
      })
    })
    const w = mountButton()
    await w.get('[data-testid="pdf-button"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="pdf-button"]').text()).toContain('Generando…')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.find('[data-testid="pdf-link"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(w.get('[data-testid="pdf-link"]').attributes('href')).toBe('https://s3/v2.pdf')
  })
})
