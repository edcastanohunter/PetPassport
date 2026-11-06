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

export interface PushSubscriptionItem {
  id: string
  endpoint: string
  status: string
  created_at: string
  last_seen_at?: string
}

export async function getVapidPublicKey(): Promise<string> {
  const res = await fetch('/api/v1/push/vapid-public-key')
  const data = await res.json()
  return data.key ?? ''
}

export async function getPushSubscriptions(): Promise<PushSubscriptionItem[]> {
  const res = await fetch('/api/v1/me/push-subscriptions')
  if (!res.ok) return []
  return res.json()
}

export async function registerPushSubscription(sub: {
  endpoint: string
  keys: { p256dh: string; auth: string }
  user_agent?: string
}): Promise<{ id: string }> {
  const res = await fetch('/api/v1/me/push-subscriptions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(sub),
  })
  if (!res.ok) throw new Error('Error al registrar suscripción')
  return res.json()
}

export async function deletePushSubscription(id: string): Promise<void> {
  await fetch(`/api/v1/me/push-subscriptions/${id}`, {
    method: 'DELETE',
  })
}
