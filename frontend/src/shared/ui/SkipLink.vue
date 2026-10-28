<script setup lang="ts">
import { useI18n } from 'vue-i18n'
// Enlace "saltar al contenido" (WCAG 2.4.1): primer elemento enfocable de cada layout.
const { t } = useI18n()
</script>

<template>
  <a
    href="#main"
    class="sr-only z-50 rounded bg-blue-700 px-4 py-2 text-white focus:not-sr-only focus:fixed focus:left-2 focus:top-2"
    >{{ t('nav.skipToContent') }}</a
  >
</template>
