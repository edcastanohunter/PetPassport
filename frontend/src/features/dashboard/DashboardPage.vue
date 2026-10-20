<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '@/features/auth/sessionStore'

// Dashboard del tutor (/app). La lista de mascotas llega en M1.2 (FE-006).
const { t } = useI18n()
const session = useSessionStore()
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-bold">
      {{ t('dashboard.title', { name: session.me?.display_name ?? '' }) }}
    </h1>
    <p class="text-surface-500">{{ t('dashboard.petsSoon') }}</p>
  </section>
</template>
