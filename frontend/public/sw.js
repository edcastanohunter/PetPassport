// Service Worker PetPassport (PP-F3-FE-008, PP-F3-ARQ-015)

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', (event) => {
  if (!event.data) return
  try {
    const payload = event.data.json()
    const title = payload.title || 'PetPassport'
    const options = {
      body: payload.body || '',
      icon: '/icon-192.png',
      badge: '/badge-72.png',
      data: {
        url: payload.url || '/',
        category: payload.category || 'general',
      },
    }
    event.waitUntil(self.registration.showNotification(title, options))
  } catch {
    const text = event.data.text()
    event.waitUntil(self.registration.showNotification('PetPassport', { body: text }))
  }
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const targetUrl = (event.notification.data && event.notification.data.url) || '/'
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windowClients) => {
      for (const client of windowClients) {
        if (client.url.includes(targetUrl) && 'focus' in client) {
          return client.focus()
        }
      }
      if (self.clients.openWindow) {
        return self.clients.openWindow(targetUrl)
      }
    }),
  )
})

self.addEventListener('pushsubscriptionchange', (event) => {
  event.waitUntil(
    self.registration.pushManager.subscribe(event.oldSubscription.options).then((subscription) => {
      return fetch('/api/v1/me/push-subscriptions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          endpoint: subscription.endpoint,
          keys: {
            p256dh: btoa(
              String.fromCharCode.apply(
                null,
                Array.from(new Uint8Array(subscription.getKey('p256dh'))),
              ),
            ),
            auth: btoa(
              String.fromCharCode.apply(
                null,
                Array.from(new Uint8Array(subscription.getKey('auth'))),
              ),
            ),
          },
        }),
      })
    }),
  )
})
