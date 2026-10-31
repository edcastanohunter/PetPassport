<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { isApiError } from '@/shared/api/client'
import { unsubscribe } from './notificationsApi'

// Baja por enlace (FE-012, NOT-004): sin sesión; el token del correo identifica
// al usuario y la categoría. Idempotente, así que se ejecuta al abrir la página.
// Token inválido o caducado → mensaje neutro (el backend responde 404).
const { t } = useI18n()
const route = useRoute()

const state = ref<'working' | 'done' | 'invalid' | 'error'>('working')
const category = ref('')

onMounted(async () => {
  try {
    category.value = (await unsubscribe(String(route.params.token))).category
    state.value = 'done'
  } catch (e) {
    state.value = isApiError(e) && e.status === 404 ? 'invalid' : 'error'
  }
})
</script>

<template>
  <section class="mx-auto max-w-xl space-y-4 py-8" aria-live="polite">
    <h1 class="text-2xl font-semibold">{{ t('notifications.unsubscribe.title') }}</h1>
    <p v-if="state === 'working'" class="text-surface-600">{{ t('common.loading') }}</p>
    <div v-else-if="state === 'done'" data-testid="unsubscribed" class="space-y-3">
      <p>
        {{
          t('notifications.unsubscribe.done', {
            category: t(`notifications.category.${category}`).toLowerCase(),
          })
        }}
      </p>
      <p class="text-sm text-surface-600">{{ t('notifications.unsubscribe.undo') }}</p>
      <RouterLink to="/app/settings/notifications" class="underline">{{
        t('notifications.unsubscribe.manage')
      }}</RouterLink>
    </div>
    <p v-else-if="state === 'invalid'" role="alert" data-testid="unsubscribe-invalid">
      {{ t('notifications.unsubscribe.invalid') }}
    </p>
    <p v-else role="alert">{{ t('errors.GENERIC') }}</p>
  </section>
</template>
