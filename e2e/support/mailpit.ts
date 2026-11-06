import { expect } from '@playwright/test'
import { env } from './env'

// Lectura de correos en Mailpit (API v1): espera el último mensaje para un
// destinatario y extrae el primer enlace que cumpla el patrón.
export interface MessageSummary {
  ID: string
  Subject: string
}

async function latestFor(to: string, subject?: RegExp): Promise<MessageSummary | undefined> {
  const q = encodeURIComponent(`to:"${to}"`)
  const res = await fetch(`${env.mailpitUrl}/api/v1/search?query=${q}&limit=20`)
  if (!res.ok) throw new Error(`mailpit search: ${res.status}`)
  const { messages } = (await res.json()) as { messages: MessageSummary[] }
  return messages.find((m) => !subject || subject.test(m.Subject))
}

export async function waitForMessage(to: string, subject?: RegExp): Promise<MessageSummary> {
  let found: MessageSummary | undefined
  await expect
    .poll(async () => (found = await latestFor(to, subject)), {
      message: `correo para ${to}`,
      timeout: 30_000,
      intervals: [500, 1000, 2000],
    })
    .toBeTruthy()
  return found!
}

export async function waitForLink(to: string, linkPattern: RegExp, subject?: RegExp): Promise<string> {
  const found = await waitForMessage(to, subject)
  const res = await fetch(`${env.mailpitUrl}/api/v1/message/${found.ID}`)
  const msg = (await res.json()) as { Text: string; HTML: string }
  const body = `${msg.Text}\n${msg.HTML}`.replace(/&amp;/g, '&')
  const link = body.match(linkPattern)?.[0]
  if (!link) throw new Error(`sin enlace ${linkPattern} en el correo a ${to}`)
  return link
}
