import {
  api,
  type AddendumReason,
  type Attachment,
  type DocumentJob,
  type DownloadLink,
  type Encounter,
  type EncounterType,
} from '@/shared/api/client'

// Llamadas de encuentros (API-001/002/003, M2.1). Cada comando lleva la
// versión conocida en If-Match (ARQ-008) y devuelve la consulta actualizada
// con su nueva versión; las descargas piden `Accept: application/json` para
// recibir la URL prefirmada en lugar de un 302 que fetch no deja leer.

function must<T>(res: { data?: T; error?: unknown }): T {
  if (res.error) throw res.error
  return res.data as T
}

export async function getEncounter(id: string): Promise<Encounter> {
  return must(
    await api().GET('/encounters/{encounterId}', { params: { path: { encounterId: id } } }),
  )
}

export async function openEncounter(
  orgId: string,
  key: string,
  body: { pet_id: string; type: EncounterType; reason: string; attending_vet_id?: string },
): Promise<Encounter> {
  return must(
    await api().POST('/organizations/{orgId}/encounters', {
      params: { path: { orgId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export async function signEncounter(id: string, version: number): Promise<Encounter> {
  return must(
    await api().POST('/encounters/{encounterId}/sign', {
      params: { path: { encounterId: id }, header: { 'If-Match': version } },
    }),
  )
}

export async function discardEncounter(id: string, reason: string): Promise<void> {
  const res = await api().POST('/encounters/{encounterId}/discard', {
    params: { path: { encounterId: id } },
    body: { reason },
  })
  if (res.error) throw res.error
}

export async function addAddendum(
  id: string,
  key: string,
  body: { text: string; reason: AddendumReason; attachment_ids?: string[] },
): Promise<Encounter> {
  return must(
    await api().POST('/encounters/{encounterId}/addenda', {
      params: { path: { encounterId: id }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export async function uploadAttachment(
  id: string,
  file: File,
  opts: { description?: string; visibleToGuardian?: boolean } = {},
): Promise<Attachment> {
  const form = new FormData()
  form.append('file', file)
  if (opts.description) form.append('description', opts.description)
  if (opts.visibleToGuardian !== undefined) {
    form.append('visible_to_guardian', String(opts.visibleToGuardian))
  }
  return must(
    await api().POST('/encounters/{encounterId}/attachments', {
      params: { path: { encounterId: id } },
      // openapi-fetch envía FormData tal cual (multipart); el tipo generado
      // declara string por binario: elenco controlado.
      body: form as unknown as { file: string },
    }),
  ) as Attachment
}

export async function setAttachmentVisibility(
  id: string,
  attachmentId: string,
  visible: boolean,
  reason?: string,
): Promise<Attachment> {
  return must(
    await api().PATCH('/encounters/{encounterId}/attachments/{attachmentId}', {
      params: { path: { encounterId: id, attachmentId } },
      body: { visible_to_guardian: visible, ...(reason ? { reason } : {}) },
    }),
  )
}

export async function deleteAttachment(id: string, attachmentId: string): Promise<void> {
  const res = await api().DELETE('/encounters/{encounterId}/attachments/{attachmentId}', {
    params: { path: { encounterId: id, attachmentId } },
  })
  if (res.error) throw res.error
}

/** URL prefirmada (15 min) de un adjunto; la descarga queda auditada. */
export async function attachmentLink(id: string, attachmentId: string): Promise<DownloadLink> {
  return must(
    await api().GET('/encounters/{encounterId}/attachments/{attachmentId}', {
      params: {
        path: { encounterId: id, attachmentId },
        header: { Accept: 'application/json' },
      },
    }),
  ) as DownloadLink
}

export type PdfState = { ready: true; link: DownloadLink } | { ready: false; job: DocumentJob }

/** PDF vigente: URL si está listo; si no, el trabajo en generación (202). */
export async function encounterPdf(id: string): Promise<PdfState> {
  const res = await api().GET('/encounters/{encounterId}/pdf', {
    params: { path: { encounterId: id }, header: { Accept: 'application/json' } },
  })
  if (res.error) throw res.error
  if (res.response.status === 202) return { ready: false, job: res.data as DocumentJob }
  return { ready: true, link: res.data as DownloadLink }
}

/** PDF del carnet (M2.2): misma semántica 200/202 que el de consulta. */
export async function cardPdf(petId: string): Promise<PdfState> {
  const res = await api().GET('/pets/{petId}/card/pdf', {
    params: { path: { petId }, header: { Accept: 'application/json' } },
  })
  if (res.error) throw res.error
  if (res.response.status === 202) return { ready: false, job: res.data as DocumentJob }
  return { ready: true, link: res.data as DownloadLink }
}

export async function documentJob(jobId: string): Promise<DocumentJob> {
  return must(await api().GET('/documents/{jobId}', { params: { path: { jobId } } }))
}
