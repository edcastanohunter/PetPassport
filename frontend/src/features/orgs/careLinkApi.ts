import {
  api,
  type CareLinkRequestItem,
  type OrgStats,
  type PetLookupResult,
} from '@/shared/api/client'

// Solicitud de vínculo por la clínica y estadísticas (API-009, API-011, M2.5).
// Los comandos que crean llevan Idempotency-Key (ARQ-008); `must` propaga el
// problem+json tal cual.

function must<T>(res: { data?: T; error?: unknown }): T {
  if (res.error) throw res.error
  return res.data as T
}

export async function lookupPets(
  orgId: string,
  q: { microchip?: string; guardian_email?: string },
): Promise<PetLookupResult[]> {
  const res = must(
    await api().GET('/organizations/{orgId}/pet-lookup', {
      params: { path: { orgId }, query: q },
    }),
  )
  return res.data
}

export async function requestCareLink(
  orgId: string,
  key: string,
  body: { pet_id: string; message?: string },
): Promise<CareLinkRequestItem> {
  return must(
    await api().POST('/organizations/{orgId}/care-link-requests', {
      params: { path: { orgId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export async function listCareLinkRequests(orgId: string): Promise<CareLinkRequestItem[]> {
  const res = must(
    await api().GET('/organizations/{orgId}/care-link-requests', {
      params: { path: { orgId }, query: {} },
    }),
  )
  return res.data
}

export async function getOrgStats(orgId: string, month?: string): Promise<OrgStats> {
  return must(
    await api().GET('/organizations/{orgId}/stats', {
      params: { path: { orgId }, query: month ? { month } : {} },
    }),
  )
}
