<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES, setLocale } from '@/app/i18n'

// Idioma de la interfaz (FE-010). Con sesión, la preferencia persistente es
// me.locale (Perfil); este selector cambia la UI de inmediato.
const { t, locale } = useI18n()
const value = computed({
  get: () => locale.value,
  set: (v: string) => setLocale(v),
})
// Autónimos: cada idioma se nombra en su propio idioma (con su `lang`).
const NAMES: Record<string, string> = { es: 'Español', en: 'English' }
</script>

<template>
  <label class="inline-flex items-center gap-2">
    <span class="sr-only">{{ t('nav.language') }}</span>
    <span class="pi pi-globe" aria-hidden="true" />
    <select v-model="value" class="tap rounded bg-transparent text-sm" data-testid="language">
      <option v-for="l in SUPPORTED_LOCALES" :key="l" :value="l" :lang="l">{{ NAMES[l] }}</option>
    </select>
  </label>
</template>
