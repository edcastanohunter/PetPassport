import { api, ApiError } from '@/shared/api/client'
import type {
  ClinicalSearchResponse,
  PatientSearchResponse,
  SearchMode,
  SuggestPatientsResponse,
  SuggestPetsMeResponse,
} from './searchTypes'

// Parámetros de búsqueda clínica y de pacientes (PP-F3-API-001, API-002, FE-001, FE-002, FE-011).

export interface SearchPatientsParams {
  q?: string
  species?: string
  status?: string
  cursor?: string
  size?: number
}

export interface SearchClinicalParams {
  q?: string
  species?: string
  type?: string
  professional_id?: string
  from?: string
  to?: string
  confirmed_only?: boolean
  organization_scope?: 'all' | 'mine'
  cursor?: string
  size?: number
}

export interface SearchMeParams {
  q?: string
  pet_id?: string
  kind?: string
  from?: string
  to?: string
  cursor?: string
  size?: number
}

function parseSearchMode(response: Response | undefined): SearchMode {
  const header = response?.headers.get('X-PP-Search-Mode')
  return header === 'degraded' ? 'degraded' : 'full'
}

/**
 * Busca pacientes en el espacio de una organización (PP-F3-API-001, FE-001).
 */
export async function searchPatients(
  orgId: string,
  params: SearchPatientsParams,
  signal?: AbortSignal,
): Promise<PatientSearchResponse> {
  const query: Record<string, string | number | undefined> = {
    q: params.q?.trim() || undefined,
    species: params.species || undefined,
    status: params.status || undefined,
    cursor: params.cursor || undefined,
    size: params.size,
  }

  const { data, error, response } = await (api().GET as any)(
    `/organizations/${orgId}/search/patients`,
    {
      params: { query },
      signal,
    },
  )

  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }

  const mode = parseSearchMode(response)
  return {
    ...data,
    mode: (data.mode as SearchMode) || mode,
  }
}

/**
 * Busca en el historial clínico de una organización (PP-F3-API-001, FE-001).
 */
export async function searchClinical(
  orgId: string,
  params: SearchClinicalParams,
  signal?: AbortSignal,
): Promise<ClinicalSearchResponse> {
  const query: Record<string, string | number | boolean | undefined> = {
    q: params.q?.trim() || undefined,
    species: params.species || undefined,
    type: params.type || undefined,
    professional_id: params.professional_id || undefined,
    from: params.from || undefined,
    to: params.to || undefined,
    confirmed_only: params.confirmed_only ? true : undefined,
    organization_scope: params.organization_scope || undefined,
    cursor: params.cursor || undefined,
    size: params.size,
  }

  const { data, error, response } = await (api().GET as any)(
    `/organizations/${orgId}/search/clinical`,
    {
      params: { query },
      signal,
    },
  )

  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }

  const mode = parseSearchMode(response)
  return {
    ...data,
    mode: (data.mode as SearchMode) || mode,
  }
}

/**
 * Autocompletado rápido de pacientes para la barra global (PP-F3-API-002, FE-001).
 */
export async function suggestPatients(
  orgId: string,
  q: string,
  signal?: AbortSignal,
): Promise<SuggestPatientsResponse> {
  const { data, error, response } = await (api().GET as any)(
    `/organizations/${orgId}/suggest/patients`,
    {
      params: { query: { q: q.trim() } },
      signal,
    },
  )

  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }

  return data
}

/**
 * Búsqueda del tutor sobre sus mascotas e historiales (PP-F3-API-001, FE-002).
 */
export async function searchMe(
  params: SearchMeParams,
  signal?: AbortSignal,
): Promise<ClinicalSearchResponse> {
  const query: Record<string, string | number | undefined> = {
    q: params.q?.trim() || undefined,
    pet_id: params.pet_id || undefined,
    kind: params.kind || undefined,
    from: params.from || undefined,
    to: params.to || undefined,
    cursor: params.cursor || undefined,
    size: params.size,
  }

  const { data, error, response } = await (api().GET as any)('/me/search', {
    params: { query },
    signal,
  })

  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }

  const mode = parseSearchMode(response)
  return {
    ...data,
    mode: (data.mode as SearchMode) || mode,
  }
}

/**
 * Autocompletado rápido de mascotas del tutor (PP-F3-API-002, FE-002).
 */
export async function suggestPetsMe(
  q: string,
  signal?: AbortSignal,
): Promise<SuggestPetsMeResponse> {
  const { data, error, response } = await (api().GET as any)('/me/suggest/pets', {
    params: { query: { q: q.trim() } },
    signal,
  })

  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }

  return data
}
