import { api, type NotificationPreferences } from '@/shared/api/client'

// Preferencias de notificación y baja por enlace (API-010, NOT-004).

function must<T>(res: { data?: T; error?: unknown }): T {
  if (res.error) throw res.error
  return res.data as T
}

export const CATEGORIES = ['appointments', 'preventive_care', 'clinical', 'account'] as const
export type Category = (typeof CATEGORIES)[number]

export async function getPreferences(): Promise<NotificationPreferences> {
  return must(await api().GET('/me/notification-preferences'))
}

export async function savePreferences(
  body: NotificationPreferences,
): Promise<NotificationPreferences> {
  return must(await api().PUT('/me/notification-preferences', { body }))
}

export async function unsubscribe(token: string): Promise<{ category: string }> {
  return must(await api().POST('/unsubscribe/{token}', { params: { path: { token } } }))
}
