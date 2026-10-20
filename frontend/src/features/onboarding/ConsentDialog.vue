<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type LegalCurrent } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

// Modal de re-consentimiento (FE-003: 403 CONSENT_VERSION_OUTDATED → modal).
const { t } = useI18n()
const session = useSessionStore()
const legal = ref<LegalCurrent | null>(null)
const terms = ref(false)
const privacy = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)

onMounted(async () => {
  const { data } = await api().GET('/legal/current')
  legal.value = data ?? null
})

async function accept() {
  if (!legal.value || busy.value) return
  busy.value = true
  error.value = null
  try {
    await api().POST('/me/consent', {
      body: {
        terms_version: legal.value.terms_version,
        privacy_version: legal.value.privacy_version,
      },
    })
    await session.loadMe(true)
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="consent-title"
  >
    <div class="w-full max-w-md space-y-4 rounded bg-surface-0 p-6 text-surface-900 shadow-lg">
      <h2 id="consent-title" class="text-lg font-semibold">{{ t('consent.title') }}</h2>
      <p class="text-sm text-surface-500">{{ t('consent.detail') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <template v-if="legal">
        <label class="flex items-start gap-2 text-sm">
          <input v-model="terms" type="checkbox" class="mt-1" />
          <span>
            <i18n-t keypath="onboarding.acceptTerms" tag="span">
              <template #link
                ><RouterLink to="/legal/terms" target="_blank" class="underline">{{
                  t('legal.terms')
                }}</RouterLink></template
              >
            </i18n-t>
            ({{ legal.terms_version }})
          </span>
        </label>
        <label class="flex items-start gap-2 text-sm">
          <input v-model="privacy" type="checkbox" class="mt-1" />
          <span>
            <i18n-t keypath="onboarding.acceptPrivacy" tag="span">
              <template #link
                ><RouterLink to="/legal/privacy" target="_blank" class="underline">{{
                  t('legal.privacy')
                }}</RouterLink></template
              >
            </i18n-t>
            ({{ legal.privacy_version }})
          </span>
        </label>
      </template>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" :disabled="busy" @click="session.logout()">
          {{ t('nav.logout') }}
        </button>
        <button
          type="button"
          class="btn-primary"
          :disabled="!terms || !privacy || busy"
          @click="accept"
        >
          {{ t('consent.accept') }}
        </button>
      </div>
    </div>
  </div>
</template>
