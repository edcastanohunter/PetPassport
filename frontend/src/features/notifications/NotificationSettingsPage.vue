<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NotificationPreferences } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { scheduleErrorKey } from '@/features/scheduling/scheduleFormat'
import {
  CATEGORIES,
  getPreferences,
  savePreferences,
  getVapidPublicKey,
  getPushSubscriptions,
  registerPushSubscription,
  deletePushSubscription,
  type PushSubscriptionItem,
} from './notificationsApi'

// Preferencias de notificación y Web Push (PP-F3-FE-008, PP-F3-DOM-011, NOT-004):
const { t, te } = useI18n()
const session = useSessionStore()

const prefs = ref<NotificationPreferences | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const saved = ref(false)
const saving = ref(false)

const pushPermission = ref<NotificationPermission>(typeof Notification !== 'undefined' ? Notification.permission : 'default')
const pushDevices = ref<PushSubscriptionItem[]>([])
const pushSubscribing = ref(false)
const isSubscribedOnThisDevice = ref(false)

const isClinicAdmin = computed(() => session.memberships.some((m) => m.role === 'clinic_admin'))

onMounted(async () => {
  try {
    prefs.value = await getPreferences()
    await loadDevices()
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
})

async function loadDevices() {
  try {
    pushDevices.value = await getPushSubscriptions()
    if ('serviceWorker' in navigator) {
      const reg = await navigator.serviceWorker.ready
      const sub = await reg.pushManager.getSubscription()
      isSubscribedOnThisDevice.value = !!sub
    }
  } catch {
    // fallback
  }
}

async function toggleThisDevicePush() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    alert('Tu navegador no soporta notificaciones Web Push.')
    return
  }
  pushSubscribing.value = true
  try {
    const perm = await Notification.requestPermission()
    pushPermission.value = perm
    if (perm !== 'granted') {
      alert('Permiso de notificaciones no concedido.')
      return
    }

    const reg = await navigator.serviceWorker.ready
    const existing = await reg.pushManager.getSubscription()

    if (existing) {
      // Unsubscribe
      await existing.unsubscribe()
      isSubscribedOnThisDevice.value = false
    } else {
      // Subscribe
      const vapidKey = await getVapidPublicKey()
      const convertedKey = urlBase64ToUint8Array(vapidKey)
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: convertedKey,
      })
      const p256dh = sub.getKey('p256dh')
      const authKey = sub.getKey('auth')
      if (p256dh && authKey) {
        await registerPushSubscription({
          endpoint: sub.endpoint,
          keys: {
            p256dh: btoa(String.fromCharCode.apply(null, Array.from(new Uint8Array(p256dh)))),
            auth: btoa(String.fromCharCode.apply(null, Array.from(new Uint8Array(authKey)))),
          },
          user_agent: navigator.userAgent,
        })
      }
      isSubscribedOnThisDevice.value = true
    }
    await loadDevices()
  } catch (err) {
    console.error('Error toggling push:', err)
  } finally {
    pushSubscribing.value = false
  }
}

function urlBase64ToUint8Array(base64String: string) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = window.atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}

async function removeDevice(id: string) {
  try {
    await deletePushSubscription(id)
    await loadDevices()
  } catch {
    // fallback
  }
}

async function save() {
  if (!prefs.value || saving.value) return
  saving.value = true
  saved.value = false
  error.value = null
  try {
    prefs.value = await savePreferences(prefs.value)
    saved.value = true
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="space-y-4 max-w-2xl">
    <h1 class="text-2xl font-semibold">{{ t('notifications.title') }}</h1>
    <p class="text-sm text-surface-600">{{ t('notifications.intro') }}</p>

    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-700">{{ t(error) }}</p>

    <form v-if="prefs" class="space-y-4" @submit.prevent="save">
      <fieldset class="card space-y-3">
        <legend class="font-medium">{{ t('notifications.emailLegend') }}</legend>
        <label v-for="c in CATEGORIES" :key="c" class="flex items-start gap-3">
          <input v-model="prefs.email[c]" type="checkbox" class="mt-1" :data-testid="`pref-${c}`" />
          <span>
            <span class="block font-medium">{{ t(`notifications.category.${c}`) }}</span>
            <span class="block text-sm text-surface-600">{{
              t(`notifications.categoryHint.${c}`)
            }}</span>
          </span>
        </label>
        <label class="flex items-start gap-3">
          <input type="checkbox" class="mt-1" checked disabled data-testid="pref-security" />
          <span>
            <span class="block font-medium">{{ t('notifications.category.security') }}</span>
            <span class="block text-sm text-surface-600">{{
              t('notifications.categoryHint.security')
            }}</span>
          </span>
        </label>
      </fieldset>

      <fieldset v-if="isClinicAdmin" class="card space-y-3">
        <legend class="font-medium">{{ t('notifications.digestLegend') }}</legend>
        <label class="flex items-start gap-3">
          <input
            v-model="prefs.digest.clinic_daily_agenda"
            type="checkbox"
            class="mt-1"
            data-testid="pref-digest"
          />
          <span>
            <span class="block font-medium">{{ t('notifications.digest.clinicDailyAgenda') }}</span>
            <span class="block text-sm text-surface-600">{{
              t('notifications.digest.clinicDailyAgendaHint')
            }}</span>
          </span>
        </label>
      </fieldset>

      <!-- Web Push en dispositivos (PP-F3-FE-008) -->
      <fieldset class="card space-y-4">
        <legend class="font-medium">Notificaciones Web Push (PWA)</legend>
        <div class="flex items-center justify-between p-3 bg-surface-50 rounded-lg">
          <div>
            <div class="font-medium text-surface-900">Activar en este dispositivo</div>
            <div class="text-xs text-surface-500">
              Permiso actual:
              <span
                :class="{
                  'text-green-700 font-medium': pushPermission === 'granted',
                  'text-amber-700 font-medium': pushPermission === 'default',
                  'text-red-700 font-medium': pushPermission === 'denied',
                }"
              >
                {{ pushPermission }}
              </span>
            </div>
          </div>
          <button
            type="button"
            class="px-3 py-1.5 text-sm rounded font-medium border"
            :class="
              isSubscribedOnThisDevice
                ? 'bg-red-50 text-red-700 border-red-200 hover:bg-red-100'
                : 'bg-primary-50 text-primary-700 border-primary-200 hover:bg-primary-100'
            "
            :disabled="pushSubscribing"
            @click="toggleThisDevicePush"
          >
            {{
              pushSubscribing
                ? 'Procesando...'
                : isSubscribedOnThisDevice
                  ? 'Desactivar en este dispositivo'
                  : 'Activar en este dispositivo'
            }}
          </button>
        </div>

        <div v-if="pushDevices.length > 0" class="space-y-2">
          <div class="text-xs font-semibold uppercase tracking-wider text-surface-500">
            Dispositivos registrados ({{ pushDevices.length }})
          </div>
          <ul class="divide-y divide-surface-200 text-sm">
            <li
              v-for="dev in pushDevices"
              :key="dev.id"
              class="py-2 flex items-center justify-between"
            >
              <div>
                <div class="font-mono text-xs text-surface-700 truncate max-w-sm">
                  {{ dev.endpoint }}
                </div>
                <div class="text-xs text-surface-400">
                  Registrado: {{ new Date(dev.created_at).toLocaleDateString() }} · Estado:
                  <span
                    :class="dev.status === 'active' ? 'text-green-600 font-medium' : 'text-surface-400'"
                  >
                    {{ dev.status }}
                  </span>
                </div>
              </div>
              <button
                type="button"
                class="text-xs text-red-600 hover:underline px-2 py-1"
                @click="removeDevice(dev.id)"
              >
                Eliminar
              </button>
            </li>
          </ul>
        </div>
      </fieldset>

      <div class="flex items-center gap-3">
        <button type="submit" class="btn-primary" :disabled="saving">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
        <p v-if="saved" role="status" class="text-sm text-green-700" data-testid="saved">
          {{ t('notifications.saved') }}
        </p>
      </div>
    </form>
  </section>
</template>
