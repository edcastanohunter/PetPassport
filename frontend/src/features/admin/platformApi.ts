import { useSessionStore } from '@/features/auth/sessionStore'

export interface PlatformDailyMetrics {
  date: string
  users_active?: number
  pets?: number
  encounters_signed?: number
  vaccinations?: number
  organizations?: Record<string, number>
  [key: string]: unknown
}

export interface TopOrganizationItem {
  organization_id: string
  display_name: string
  metric: string
  count: number
}

export interface SearchIndexSummary {
  alias: string
  physical: string
  docs: number
  size_bytes: number
  mapping_version: number
  repo_mapping_version: number
  drift: boolean
  lag_seconds: number
}

async function authFetch<T>(input: string, init?: RequestInit): Promise<T> {
  const session = useSessionStore()
  const headers = new Headers(init?.headers)
  if (session.accessToken) {
    headers.set('Authorization', `Bearer ${session.accessToken}`)
  }
  if (!headers.has('Content-Type') && init?.body && typeof init.body === 'string') {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(input, { ...init, headers })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.detail || data?.title || `HTTP ${res.status}`)
  }
  return res.json()
}

export async function getPlatformStats(params?: {
  granularity?: string
  from?: string
  to?: string
}): Promise<PlatformDailyMetrics[]> {
  const q = new URLSearchParams()
  if (params?.granularity) q.set('granularity', params.granularity)
  if (params?.from) q.set('from', params.from)
  if (params?.to) q.set('to', params.to)
  const qs = q.toString()
  const url = `/api/v1/admin/platform/stats${qs ? `?${qs}` : ''}`
  const data = await authFetch<{ series: PlatformDailyMetrics[] }>(url)
  return data.series ?? []
}

export async function getTopOrganizations(params?: {
  metric?: string
  period?: string
}): Promise<TopOrganizationItem[]> {
  const q = new URLSearchParams()
  if (params?.metric) q.set('metric', params.metric)
  if (params?.period) q.set('period', params.period)
  const qs = q.toString()
  const url = `/api/v1/admin/platform/top-organizations${qs ? `?${qs}` : ''}`
  const data = await authFetch<{ data: TopOrganizationItem[] }>(url)
  return data.data ?? []
}

export async function getSearchIndices(): Promise<SearchIndexSummary[]> {
  return authFetch<SearchIndexSummary[]>('/api/v1/admin/search/indices')
}

export async function reindexIndex(alias: string, reason: string): Promise<{ job_id: string }> {
  return authFetch<{ job_id: string }>(`/api/v1/admin/search/indices/${alias}/reindex`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}

export async function retryIndexQueue(): Promise<{ retried: number }> {
  return authFetch<{ retried: number }>('/api/v1/admin/search/index-queue/retry', {
    method: 'POST',
  })
}

export async function purgeCache(
  reason: string,
  families: string[] = ['search', 'catalog'],
): Promise<void> {
  await authFetch<void>('/api/v1/admin/cache/purge', {
    method: 'POST',
    body: JSON.stringify({ reason, families }),
  })
}
