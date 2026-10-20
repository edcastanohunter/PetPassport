<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const props = defineProps<{ kind: 'terms' | 'privacy' }>()
const { t } = useI18n()
// Contenido placeholder: los textos legales reales llegan en M1.5 (PRD-020).
const version = '2026-10-draft'
</script>

<template>
  <article class="prose max-w-none">
    <h1 class="text-2xl font-bold">{{ t(`legal.${props.kind}`) }}</h1>
    <p class="text-sm text-surface-500">{{ t('legal.version') }} {{ version }}</p>
    <p>Placeholder — pendiente de revisión legal (PP-F1-PRD-020).</p>
  </article>
</template>
