import { api, ApiError } from '@/shared/api/client'
import type {
  ImportBatch,
  ListMedicationsParams,
  Medication,
  MedicationSuggestion,
  MedicationUsageStats,
  SuggestMedicationItem,
} from './medicationsTypes'

type DynamicMethod = <T>(
  path: string,
  options?: Record<string, unknown>,
) => Promise<{
  data?: T
  error?: Record<string, unknown>
  response?: Response
}>

const rawGet: DynamicMethod = (path, options) => {
  const getFn = api().GET as unknown as DynamicMethod
  return getFn(path, options)
}

const rawPost: DynamicMethod = (path, options) => {
  const postFn = api().POST as unknown as DynamicMethod
  return postFn(path, options)
}

const rawPut: DynamicMethod = (path, options) => {
  const putFn = api().PUT as unknown as DynamicMethod
  return putFn(path, options)
}

function handleResponse<T>(
  data: T | undefined,
  error: Record<string, unknown> | undefined,
  response?: Response,
): T {
  if (error || !data) {
    const status = response?.status ?? 500
    const retryAfter = response?.headers.get('Retry-After')
    throw new ApiError(error ?? {}, status, retryAfter)
  }
  return data
}

/**
 * Autocompletado de medicamentos por texto y especie (PP-F3-API-002, FE-003).
 */
export async function suggestMedications(
  q: string,
  species?: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<SuggestMedicationItem[]> {
  const query: Record<string, string | number | undefined> = {
    q: q.trim(),
    species: species || undefined,
    limit: limit ?? 8,
  }

  const { data, error, response } = await rawGet<{ data: SuggestMedicationItem[] }>(
    '/suggest/medications',
    { params: { query }, signal },
  )

  const res = handleResponse(data, error, response)
  return res.data ?? []
}

/**
 * Lista medicamentos del catálogo con filtros (PP-F3-API-003).
 */
export async function listMedications(
  params: ListMedicationsParams,
  signal?: AbortSignal,
): Promise<{ data: Medication[]; total?: number }> {
  const query: Record<string, string | number | boolean | undefined> = {
    q: params.q?.trim() || undefined,
    species: params.species || undefined,
    form: params.form || undefined,
    prescription_required: params.prescription_required,
    status: params.status || undefined,
    limit: params.limit,
    offset: params.offset,
  }

  const { data, error, response } = await rawGet<{ data: Medication[]; total?: number }>(
    '/catalogs/medications',
    { params: { query }, signal },
  )

  return handleResponse(data, error, response)
}

/**
 * Obtiene el detalle de un medicamento por ID (PP-F3-API-003).
 */
export async function getMedication(
  id: string,
): Promise<{ data: Medication; merged_from?: string }> {
  const { data, error, response } = await rawGet<{ data: Medication; merged_from?: string }>(
    `/catalogs/medications/${id}`,
  )
  return handleResponse(data, error, response)
}

/**
 * Registra una sugerencia de medicamento por parte de un veterinario (PP-F3-API-003, FE-003).
 */
export async function createSuggestion(payload: {
  organization_id: string
  name: string
  notes?: string
  photo_object_key?: string
}): Promise<MedicationSuggestion> {
  const { data, error, response } = await rawPost<MedicationSuggestion>(
    '/catalogs/medications/suggestions',
    { body: payload },
  )
  return handleResponse(data, error, response)
}

/**
 * Lista las sugerencias de medicamentos de una organización (PP-F3-API-003, FE-004).
 */
export async function listOrgSuggestions(
  orgId: string,
  status?: string,
): Promise<MedicationSuggestion[]> {
  const query = status ? { status } : undefined
  const { data, error, response } = await rawGet<{ data: MedicationSuggestion[] }>(
    `/organizations/${orgId}/catalogs/medications/suggestions`,
    { params: { query } },
  )
  const res = handleResponse(data, error, response)
  return res.data ?? []
}

/**
 * Administración: Crea un nuevo medicamento en el catálogo (PP-F3-API-003, FE-004).
 */
export async function adminCreateMedication(
  medication: Partial<Medication> & { reason: string },
): Promise<Medication> {
  const { data, error, response } = await rawPost<Medication>('/admin/catalogs/medications', {
    body: medication,
  })
  return handleResponse(data, error, response)
}

/**
 * Administración: Actualiza un medicamento existente (PP-F3-API-003, FE-004).
 */
export async function adminUpdateMedication(
  id: string,
  medication: Partial<Medication> & { reason: string },
): Promise<Medication> {
  const { data, error, response } = await rawPut<Medication>(`/admin/catalogs/medications/${id}`, {
    body: medication,
  })
  return handleResponse(data, error, response)
}

/**
 * Administración: Descontinúa un medicamento (PP-F3-API-003, FE-004).
 */
export async function adminDiscontinueMedication(id: string, reason: string): Promise<Medication> {
  const { data, error, response } = await rawPost<Medication>(
    `/admin/catalogs/medications/${id}/discontinue`,
    { body: { reason } },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Fusiona un medicamento en otro (PP-F3-API-003, FE-004).
 */
export async function adminMergeMedication(
  id: string,
  targetId: string,
  reason: string,
): Promise<Medication> {
  const { data, error, response } = await rawPost<Medication>(
    `/admin/catalogs/medications/${id}/merge`,
    { body: { into: targetId, reason } },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Importación en lote (PP-F3-API-003, DAT-002, FE-004).
 */
export async function adminImportMedications(payload: {
  source: string
  dry_run?: boolean
  rows: unknown[]
}): Promise<ImportBatch> {
  const { data, error, response } = await rawPost<ImportBatch>(
    '/admin/catalogs/medications/import',
    { body: payload },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Consulta el estado de un lote de importación (PP-F3-API-003, FE-004).
 */
export async function adminGetImportBatch(batchId: string): Promise<ImportBatch> {
  const { data, error, response } = await rawGet<ImportBatch>(
    `/admin/catalogs/medications/imports/${batchId}`,
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Revierte un lote de importación (PP-F3-API-003, FE-004).
 */
export async function adminRevertImportBatch(
  batchId: string,
  reason: string,
): Promise<ImportBatch> {
  const { data, error, response } = await rawPost<ImportBatch>(
    `/admin/catalogs/medications/imports/${batchId}/revert`,
    { body: { reason } },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Lista sugerencias enviadas por profesionales (PP-F3-API-003, FE-004).
 */
export async function adminListSuggestions(status?: string): Promise<MedicationSuggestion[]> {
  const query = status ? { status } : undefined
  const { data, error, response } = await rawGet<{ data: MedicationSuggestion[] }>(
    '/admin/catalogs/medications/suggestions',
    { params: { query } },
  )
  const res = handleResponse(data, error, response)
  return res.data ?? []
}

/**
 * Administración: Aprueba una sugerencia registrando el fármaco (PP-F3-API-003, FE-004).
 */
export async function adminApproveSuggestion(
  id: string,
  payload: Partial<Medication> & { reason: string },
): Promise<Medication> {
  const { data, error, response } = await rawPost<Medication>(
    `/admin/catalogs/medications/suggestions/${id}/approve`,
    { body: payload },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Rechaza una sugerencia con motivo (PP-F3-API-003, FE-004).
 */
export async function adminRejectSuggestion(
  id: string,
  reason: string,
): Promise<MedicationSuggestion> {
  const { data, error, response } = await rawPost<MedicationSuggestion>(
    `/admin/catalogs/medications/suggestions/${id}/reject`,
    { body: { reason } },
  )
  return handleResponse(data, error, response)
}

/**
 * Administración: Obtiene métricas de adopción del catálogo (PP-F3-API-003, FE-004).
 */
export async function adminGetUsageStats(): Promise<MedicationUsageStats> {
  const { data, error, response } = await rawGet<MedicationUsageStats>(
    '/admin/catalogs/medications/usage',
  )
  return handleResponse(data, error, response)
}
