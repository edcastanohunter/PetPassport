import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  searchPatients,
  searchClinical,
  suggestPatients,
  searchMe,
  suggestPetsMe,
} from './searchApi'
import { api, ApiError } from '@/shared/api/client'

vi.mock('@/shared/api/client', () => {
  const mockGet = vi.fn()
  return {
    api: () => ({
      GET: mockGet,
    }),
    ApiError: class MockApiError extends Error {
      status: number
      retryAfter?: number
      code: string
      constructor(p: any, status: number, retryAfter?: string | null) {
        super(p.detail || 'error')
        this.status = status
        this.code = p.code || 'ERROR'
        this.retryAfter = retryAfter ? Number(retryAfter) : undefined
      }
    },
  }
})

describe('searchApi.ts (PP-F3-API-001, API-002)', () => {
  const mockApi = api() as any

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('searchPatients performs GET and parses degraded mode header', async () => {
    mockApi.GET.mockResolvedValueOnce({
      data: {
        data: [{ pet: { id: 'p1', name: 'Luna' } }],
        facets: {},
        freshness: { lag_seconds: 0 },
      },
      response: {
        status: 200,
        headers: new Headers({ 'X-PP-Search-Mode': 'degraded' }),
      },
    })

    const res = await searchPatients('org-123', { q: 'Luna' })
    expect(mockApi.GET).toHaveBeenCalledWith(
      '/organizations/org-123/search/patients',
      expect.objectContaining({
        params: { query: { q: 'Luna' } },
      }),
    )
    expect(res.data[0].pet.name).toBe('Luna')
    expect(res.mode).toBe('degraded')
  })

  it('searchPatients propagates ApiError with Retry-After on 429', async () => {
    mockApi.GET.mockResolvedValueOnce({
      error: { code: 'RATE_LIMITED', detail: 'Too many requests' },
      response: {
        status: 429,
        headers: new Headers({ 'Retry-After': '45' }),
      },
    })

    await expect(searchPatients('org-123', { q: 'Spam' })).rejects.toThrow()
  })

  it('suggestPatients calls quick completion endpoint', async () => {
    mockApi.GET.mockResolvedValueOnce({
      data: {
        data: [{ pet_id: 'p1', name: 'Max', species: 'dog', access_level: 'linked' }],
      },
      response: {
        status: 200,
        headers: new Headers(),
      },
    })

    const res = await suggestPatients('org-123', 'Ma')
    expect(mockApi.GET).toHaveBeenCalledWith(
      '/organizations/org-123/suggest/patients',
      expect.objectContaining({
        params: { query: { q: 'Ma' } },
      }),
    )
    expect(res.data).toHaveLength(1)
    expect(res.data[0].name).toBe('Max')
  })

  it('searchMe calls /me/search with query parameters', async () => {
    mockApi.GET.mockResolvedValueOnce({
      data: {
        data: [],
        facets: {},
        freshness: { lag_seconds: 1 },
        mode: 'full',
      },
      response: {
        status: 200,
        headers: new Headers({ 'X-PP-Search-Mode': 'full' }),
      },
    })

    const res = await searchMe({ q: 'vacuna' })
    expect(mockApi.GET).toHaveBeenCalledWith(
      '/me/search',
      expect.objectContaining({
        params: { query: { q: 'vacuna' } },
      }),
    )
    expect(res.mode).toBe('full')
  })

  it('suggestPetsMe calls /me/suggest/pets', async () => {
    mockApi.GET.mockResolvedValueOnce({
      data: {
        data: [{ pet_id: 'p1', name: 'Luna', species: 'dog' }],
      },
      response: {
        status: 200,
        headers: new Headers(),
      },
    })

    const res = await suggestPetsMe('Lu')
    expect(mockApi.GET).toHaveBeenCalledWith(
      '/me/suggest/pets',
      expect.objectContaining({
        params: { query: { q: 'Lu' } },
      }),
    )
    expect(res.data[0].name).toBe('Luna')
  })
})
