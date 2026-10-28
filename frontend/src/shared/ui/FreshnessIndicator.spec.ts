import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import FreshnessIndicator from './FreshnessIndicator.vue'
import { onFreshness, parseFreshness } from '@/shared/api/freshness'
import { createApiClient, resetApiClient } from '@/shared/api/client'
import { useDerivedView } from '@/shared/composables/useDerivedView'

// PP-F2-FE-014: indicador de frescura de vistas derivadas.

function mountIndicator(lagSeconds: number | null) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(FreshnessIndicator, { props: { lagSeconds }, global: { plugins: [i18n] } })
}

function response(lag?: string) {
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (lag !== undefined) headers.set('X-PP-Freshness', lag)
  return new Response('{}', { status: 200, headers })
}

describe('parseFreshness', () => {
  it.each([
    ['lag=4', 4],
    ['lag=0.5', 0.5],
    [' lag = 12 ; other=1', 12],
    ['other=1, lag=2', 2],
    ['lag=-1', null],
    ['lag=x', null],
    ['', null],
    [null, null],
  ])('%s → %s', (value, want) => {
    expect(parseFreshness(value)).toBe(want)
  })
})

describe('FreshnessIndicator', () => {
  it('oculto si no hay cabecera o el retraso es < 3 s', () => {
    expect(mountIndicator(null).find('[data-testid="freshness"]').exists()).toBe(false)
    expect(mountIndicator(2.9).find('[data-testid="freshness"]').exists()).toBe(false)
  })

  it('muestra "Actualizando… (hace N s)" y emite reload', async () => {
    const w = mountIndicator(7.4)
    const box = w.get('[data-testid="freshness"]')
    expect(box.text()).toContain('Actualizando… (hace 7 s)')
    expect(box.attributes('aria-live')).toBe('polite')
    await w.get('[data-testid="freshness-reload"]').trigger('click')
    expect(w.emitted('reload')).toHaveLength(1)
  })
})

describe('cliente API', () => {
  beforeEach(() => resetApiClient())
  afterEach(() => vi.restoreAllMocks())

  it('publica X-PP-Freshness de las respuestas', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(response('lag=5'))
    const seen: number[] = []
    onFreshness((e) => seen.push(e.lagSeconds))
    const client = createApiClient({ getToken: () => undefined, baseUrl: 'http://api.test/api/v1' })
    await client.GET('/healthz')
    expect(seen).toEqual([5])
  })
})

describe('useDerivedView', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('tras un comando recarga hasta 3 veces cada 2 s mientras siga retrasada', async () => {
    const load = vi.fn().mockImplementation(async () => ({ data: 1, response: response('lag=10') }))
    const scope = effectScope()
    const view = scope.run(() => useDerivedView(load))!
    const done = view.afterCommand()
    await vi.advanceTimersByTimeAsync(4000)
    await done
    expect(load).toHaveBeenCalledTimes(3)
    expect(view.lagSeconds.value).toBe(10)
    scope.stop()
  })

  it('deja de sondear en cuanto la vista está al día', async () => {
    const load = vi
      .fn()
      .mockResolvedValueOnce({ data: 1, response: response('lag=6') })
      .mockResolvedValue({ data: 2, response: response('lag=0') })
    const scope = effectScope()
    const view = scope.run(() => useDerivedView(load))!
    const done = view.afterCommand()
    await vi.advanceTimersByTimeAsync(10_000)
    await done
    expect(load).toHaveBeenCalledTimes(2)
    expect(view.data.value).toBe(2)
    expect(view.lagSeconds.value).toBe(0)
    scope.stop()
  })
})
