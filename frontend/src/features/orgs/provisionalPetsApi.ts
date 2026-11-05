import { useSessionStore } from '@/features/auth/sessionStore'

export interface ProvisionalPetItem {
  pet_id: string
  name: string
  species: string
  breed?: string
  sex: string
  microchip?: string
  created_at: string
  claim_status: 'pending' | 'claimed' | 'expired'
  claim_expires_at?: string
  resend_count: number
  contact_masked?: string
}

export interface RegisterProvisionalPayload {
  pet: {
    name: string
    species: string
    breed?: string
    sex: string
    neutered: string
    birth_date?: { year: number; month?: number; day?: number }
    microchip?: string
  }
  guardian_contact: {
    email?: string
    phone?: string
  }
  send_invitation: boolean
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

export async function listProvisionalPets(
  orgId: string,
  status?: string,
): Promise<ProvisionalPetItem[]> {
  const q = status ? `?status=${encodeURIComponent(status)}` : ''
  const res = await authFetch<{ data: ProvisionalPetItem[] }>(
    `/api/v1/organizations/${orgId}/provisional-pets${q}`,
  )
  return res.data ?? []
}

export async function registerProvisionalPet(
  orgId: string,
  payload: RegisterProvisionalPayload,
): Promise<{ pet_id: string }> {
  return authFetch<{ pet_id: string }>(`/api/v1/organizations/${orgId}/provisional-pets`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function resendClaim(orgId: string, petId: string): Promise<void> {
  const session = useSessionStore()
  const res = await fetch(`/api/v1/organizations/${orgId}/provisional-pets/${petId}/resend-claim`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${session.accessToken}`,
    },
  })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.detail || data?.title || `HTTP ${res.status}`)
  }
}

export async function importProvisionalPets(
  orgId: string,
  file: File,
): Promise<{ imported: number; errors: { row: number; error: string }[] }> {
  const session = useSessionStore()
  const formData = new FormData()
  formData.append('file', file)

  const res = await fetch(`/api/v1/organizations/${orgId}/provisional-pets/import`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${session.accessToken}`,
    },
    body: formData,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.detail || data?.title || `HTTP ${res.status}`)
  }
  return res.json()
}
