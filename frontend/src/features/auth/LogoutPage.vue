<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionStore } from './sessionStore'

// /auth/logout: callback post-logout de Keycloak (IAM-013). Descarta el estado local.
const router = useRouter()
const session = useSessionStore()
const { t } = useI18n()

onMounted(async () => {
  await session.completeLogout()
  await router.replace({ name: 'home' })
})
</script>

<template>
  <section class="py-16 text-center" aria-live="polite">
    <p>{{ t('auth.signedOut') }}</p>
  </section>
</template>
