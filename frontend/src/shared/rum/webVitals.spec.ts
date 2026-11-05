import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushQueue, initRUM, recordMetric } from './webVitals'

describe('RUM Web Vitals (PP-F3-FE-010)', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('records and flushes metrics via fetch or sendBeacon', () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }))

    recordMetric({
      name: 'LCP',
      value: 1400,
      route: '/app/pets',
      rating: 'good',
    })

    flushQueue('/api/v1/client-events')

    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/v1/client-events',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          events: [
            {
              name: 'LCP',
              value: 1400,
              route: '/app/pets',
              rating: 'good',
            },
          ],
        }),
      }),
    )
  })

  it('tolerates when observers are not supported or empty queue', () => {
    expect(() => initRUM()).not.toThrow()
    expect(() => flushQueue()).not.toThrow()
  })
})
