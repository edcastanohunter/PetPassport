<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <div
    class="min-h-screen flex flex-col bg-surface-0 text-surface-900 dark:bg-surface-950 dark:text-surface-0"
  >
    <header class="border-b border-surface-200 dark:border-surface-800">
      <nav
        class="mx-auto max-w-5xl px-4 py-3 flex items-center justify-between"
        :aria-label="t('nav.main')"
      >
        <RouterLink to="/" class="font-semibold text-lg">PetPassport</RouterLink>
        <ul class="flex gap-4 text-sm">
          <li>
            <RouterLink to="/legal/terms">{{ t('nav.terms') }}</RouterLink>
          </li>
          <li>
            <RouterLink to="/legal/privacy">{{ t('nav.privacy') }}</RouterLink>
          </li>
        </ul>
      </nav>
    </header>
    <main id="main" class="flex-1 mx-auto w-full max-w-5xl px-4 py-8">
      <slot />
    </main>
    <footer class="border-t border-surface-200 dark:border-surface-800 text-xs text-surface-500">
      <div class="mx-auto max-w-5xl px-4 py-4">© PetPassport</div>
    </footer>
  </div>
</template>
