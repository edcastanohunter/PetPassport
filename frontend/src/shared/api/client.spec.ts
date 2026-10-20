import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, createApiClient, onApiError, resetApiClient } from './client'

const problem = {
  type: 'https://docs.petpassport.app/errors/VALIDATION_FAILED',
  title: 'Datos inválidos',
  status: 422,
  code: 'VALIDATION_FAILED',
  detail: 'Uno o más campos no son válidos.',
  errors: [{ field: 'phone', code: 'INVALID' }],
}

describe('api client', () => {
  beforeEach(() => resetApiClient())
  afterEach(() => vi.restoreAllMocks())

  it('añade Authorization y X-Correlation-ID', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    const client = createApiClient({ getToken: () => 'tok', baseUrl: 'http://api.test/api/v1' })
    await client.GET('/healthz')
    const req = fetchMock.mock.calls[0][0] as Request
    expect(req.headers.get('Authorization')).toBe('Bearer tok')
    expect(req.headers.get('X-Correlation-ID')).toMatch(/^[0-9a-f-]{36}$/)
  })

  it('convierte problem+json en ApiError y notifica a los handlers', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(problem), {
        status: 422,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    )
    const seen: ApiError[] = []
    onApiError((e) => seen.push(e))
    const client = createApiClient({ getToken: () => undefined, baseUrl: 'http://api.test/api/v1' })
    await expect(client.PATCH('/me', { body: { phone: 'x' } })).rejects.toBeInstanceOf(ApiError)
    expect(seen).toHaveLength(1)
    expect(seen[0].code).toBe('VALIDATION_FAILED')
    expect(seen[0].status).toBe(422)
    expect(seen[0].fieldErrors()).toEqual({ phone: 'INVALID' })
  })

  it('401 sin cuerpo produce UNAUTHENTICATED', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))
    const client = createApiClient({ getToken: () => undefined, baseUrl: 'http://api.test/api/v1' })
    const err = await client.GET('/me').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).code).toBe('UNAUTHENTICATED')
  })
})
