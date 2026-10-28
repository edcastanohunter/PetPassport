<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const props = defineProps<{ kind: 'terms' | 'privacy' }>()
const { t } = useI18n()
// Versión vigente sembrada en identity.consent_versions (PRD-020). El texto
// definitivo lo aporta legal (prerrequisito humano de M1.5) en locales/*.json.
const version = '2026-10-draft'
</script>

<template>
  <article class="prose max-w-none">
    <h1 class="text-2xl font-bold">{{ t(`legal.${props.kind}`) }}</h1>
    <p class="text-sm text-surface-500">{{ t('legal.version') }} {{ version }}</p>
    <p>{{ t('legal.pendingReview') }}</p>
  </article>
</template>
