<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '@/features/auth/sessionStore'
import AppFooter from '@/shared/ui/AppFooter.vue'

// Layout Admin (FE-004/FE-009): consola de super_admin con organizaciones,
// usuarios y auditoría. El guard exige requiresGlobalRole('super_admin') y el
// backend repite la decisión (admin:*) en cada request.
const { t } = useI18n()
const session = useSessionStore()

const sections = [
  { name: 'admin-organizations', label: 'admin.nav.organizations' },
  { name: 'admin-users', label: 'admin.nav.users' },
  { name: 'admin-audit', label: 'admin.nav.audit' },
] as const
</script>

<template>
  <div
    class="flex min-h-screen flex-col bg-surface-0 text-surface-900 dark:bg-surface-950 dark:text-surface-0"
  >
    <header class="border-b border-surface-200 dark:border-surface-800">
      <nav
        class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-4 py-3"
        :aria-label="t('nav.main')"
      >
        <div class="flex items-center gap-4">
          <RouterLink to="/app" class="text-lg font-semibold">PetPassport</RouterLink>
          <span class="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-900">
            {{ t('nav.admin') }}
          </span>
        </div>
        <ul class="flex items-center gap-3 text-sm">
          <li>
            <RouterLink to="/app" class="hover:underline">{{ t('nav.myPets') }}</RouterLink>
          </li>
          <li>
            <button type="button" class="hover:underline" @click="session.logout()">
              {{ t('nav.logout') }}
            </button>
          </li>
        </ul>
      </nav>
      <nav class="mx-auto max-w-6xl px-4 pb-2" :aria-label="t('admin.nav.label')">
        <ul class="flex flex-wrap gap-3 text-sm" data-testid="admin-nav">
          <li v-for="s in sections" :key="s.name">
            <RouterLink
              :to="{ name: s.name }"
              class="hover:underline"
              active-class="font-semibold underline"
            >
              {{ t(s.label) }}
            </RouterLink>
          </li>
        </ul>
      </nav>
    </header>
    <main id="main" class="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
      <slot />
    </main>
    <AppFooter />
  </div>
</template>
