<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LanguageSwitcher from './LanguageSwitcher.vue'

// Pie común (SEC-009): el aviso de privacidad y los términos quedan a un clic
// desde cualquier contexto (público, tutor, organización y administración).
const { t } = useI18n()
</script>

<template>
  <footer
    class="no-print border-t border-surface-200 text-xs text-surface-500 dark:border-surface-800"
  >
    <div class="mx-auto flex max-w-6xl flex-wrap items-center gap-4 px-4 py-4">
      <span>© PetPassport</span>
      <RouterLink to="/legal/privacy" class="hover:underline">{{ t('legal.privacy') }}</RouterLink>
      <RouterLink to="/legal/terms" class="hover:underline">{{ t('legal.terms') }}</RouterLink>
      <LanguageSwitcher class="ml-auto" />
    </div>
  </footer>
</template>
