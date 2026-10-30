<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NotificationPreferences } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { scheduleErrorKey } from '@/features/scheduling/scheduleFormat'
import { CATEGORIES, getPreferences, savePreferences } from './notificationsApi'

// Preferencias de notificación (FE-012, NOT-004): un interruptor por categoría
// de email. «Seguridad» siempre está activa. El resumen diario solo lo ven los
// administradores de una clínica. Se consultan al enviar, no al programar.
const { t, te } = useI18n()
const session = useSessionStore()

const prefs = ref<NotificationPreferences | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const saved = ref(false)
const saving = ref(false)

const isClinicAdmin = computed(() => session.memberships.some((m) => m.role === 'clinic_admin'))

onMounted(async () => {
  try {
    prefs.value = await getPreferences()
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
})

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
