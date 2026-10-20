<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createApiClient } from '@/shared/api/client'

const { t } = useI18n()
const apiStatus = ref<'checking' | 'ok' | 'down'>('checking')

onMounted(async () => {
  try {
    const { data } = await createApiClient().GET('/healthz')
    apiStatus.value = data?.status === 'ok' ? 'ok' : 'down'
  } catch {
    apiStatus.value = 'down'
  }
})
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-3xl font-bold">{{ t('home.title') }}</h1>
    <p class="text-lg text-surface-500">{{ t('home.subtitle') }}</p>
    <p class="text-sm" data-testid="api-status">
      {{ t('home.status') }}:
      <span :class="apiStatus === 'ok' ? 'text-green-600' : 'text-red-600'">{{ apiStatus }}</span>
    </p>
  </section>
</template>
