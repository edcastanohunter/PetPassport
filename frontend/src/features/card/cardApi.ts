import {
  api,
  type Card,
  type CardEntry,
  type CardEntryKind,
  type CreatedShare,
  type NextDueProposal,
  type Share,
  type Vaccine,
} from '@/shared/api/client'

// Llamadas del carnet (API-004/005/006, M2.2). Los comandos de escritura
// llevan Idempotency-Key (ARQ-008); `must` propaga el problem+json tal cual.

function must<T>(res: { data?: T; error?: unknown }): T {
  if (res.error) throw res.error
  return res.data as T
}

export interface EntryInput {
  kind: CardEntryKind
  product: { name: string; catalog_id?: string | null }
  administered_at: string
  lot?: string
  dose_label?: string
  next_due_date?: string
}

export async function getCard(petId: string): Promise<Card> {
  return must(await api().GET('/pets/{petId}/card', { params: { path: { petId } } }))
}

export async function recordEntry(
  petId: string,
  key: string,
  body: EntryInput,
): Promise<CardEntry> {
  return must(
    await api().POST('/pets/{petId}/card/entries', {
      params: { path: { petId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export async function declareEntry(
  petId: string,
  key: string,
  body: Omit<EntryInput, 'next_due_date'>,
): Promise<CardEntry> {
  return must(
    await api().POST('/pets/{petId}/card/declarations', {
      params: { path: { petId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

type EntryAction = 'verify' | 'dispute'
export async function reviewEntry(
  petId: string,
  entryId: string,
  action: EntryAction,
  note?: string,
) {
  const path =
    action === 'verify'
      ? '/pets/{petId}/card/entries/{entryId}/verify'
      : '/pets/{petId}/card/entries/{entryId}/dispute'
  const res = await api().POST(path, { params: { path: { petId, entryId } }, body: { note } })
  if (res.error) throw res.error
}

export async function voidEntry(petId: string, entryId: string, reason: string) {
  const res = await api().POST('/pets/{petId}/card/entries/{entryId}/void', {
    params: { path: { petId, entryId } },
    body: { reason },
  })
  if (res.error) throw res.error
}

export async function proposeNextDue(
  petId: string,
  q: { kind: CardEntryKind; product_catalog_id?: string; administered_at: string },
): Promise<NextDueProposal> {
  return must(
    await api().GET('/pets/{petId}/card/next-due/proposal', {
      params: { path: { petId }, query: q },
    }),
  )
}

export async function listVaccines(species?: string, q?: string): Promise<Vaccine[]> {
  const r = must(
    await api().GET('/catalogs/vaccines', {
      params: { query: { ...(species ? { species } : {}), ...(q ? { q } : {}) } },
    }),
  )
  return r.data
}

export async function listShares(petId: string): Promise<Share[]> {
  return must(await api().GET('/pets/{petId}/shares', { params: { path: { petId } } })).data
}

export async function createShare(
  petId: string,
  key: string,
  body: { scope: 'vaccination_card' | 'full_record'; expires_in_days: number; pin?: string },
): Promise<CreatedShare> {
  return must(
    await api().POST('/pets/{petId}/shares', {
      params: { path: { petId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export async function revokeShare(petId: string, shareId: string) {
  const res = await api().DELETE('/pets/{petId}/shares/{shareId}', {
    params: { path: { petId, shareId } },
  })
  if (res.error) throw res.error
}
