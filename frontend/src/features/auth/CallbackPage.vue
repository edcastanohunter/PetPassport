<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionStore } from './sessionStore'

// /auth/callback: procesa signinRedirectCallback y vuelve a returnTo (FE-002).
const router = useRouter()
const session = useSessionStore()
const { t } = useI18n()
const error = ref(false)

onMounted(async () => {
  try {
    const returnTo = await session.handleCallback()
    const me = await session.loadMe()
    if (me?.onboarding_required) {
      await router.replace({ name: 'onboarding', query: { returnTo } })
    } else {
      await router.replace(returnTo)
    }
  } catch {
    error.value = true
  }
})
</script>

<template>
  <section class="py-16 text-center" aria-live="polite">
    <p v-if="!error">{{ t('auth.signingIn') }}</p>
    <div v-else class="space-y-4">
      <p class="text-red-600">{{ t('auth.callbackError') }}</p>
      <button type="button" class="underline" @click="session.login('/app')">
        {{ t('auth.retry') }}
      </button>
    </div>
  </section>
</template>
