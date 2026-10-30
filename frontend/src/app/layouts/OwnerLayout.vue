<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '@/features/auth/sessionStore'
import ConsentDialog from '@/features/onboarding/ConsentDialog.vue'
import AppFooter from '@/shared/ui/AppFooter.vue'
import SkipLink from '@/shared/ui/SkipLink.vue'
import ContextSwitcher from '@/shared/ui/ContextSwitcher.vue'

// Layout Owner (FE-004): barra con selector de contexto ("Mis mascotas", una
// entrada por organización, "Administración" si aplica) y menú de cuenta.
const { t } = useI18n()
const session = useSessionStore()

const contexts = computed(() => {
  const items: { to: string; label: string }[] = [{ to: '/app', label: t('nav.myPets') }]
  for (const m of session.memberships) {
    items.push({ to: `/org/${m.organization_id}`, label: m.display_name })
  }
  if (session.hasGlobalRole('super_admin')) items.push({ to: '/admin', label: t('nav.admin') })
  return items
})
</script>

<template>
  <div
    class="min-h-screen flex flex-col bg-surface-0 text-surface-900 dark:bg-surface-950 dark:text-surface-0"
  >
    <SkipLink />
    <header class="border-b border-surface-200 dark:border-surface-800">
      <nav
        class="mx-auto max-w-6xl px-4 py-3 flex items-center justify-between gap-4"
        :aria-label="t('nav.main')"
      >
        <div class="flex items-center gap-4">
          <RouterLink to="/app" class="font-semibold text-lg">PetPassport</RouterLink>
          <ContextSwitcher :items="contexts" />
        </div>
        <ul class="flex items-center gap-3 text-sm">
          <li>
            <RouterLink to="/app/appointments" class="hover:underline">{{
              t('scheduling.nav.myAppointments')
            }}</RouterLink>
          </li>
          <li>
            <RouterLink to="/app/profile" class="hover:underline">{{
              t('nav.profile')
            }}</RouterLink>
          </li>
          <li>
            <button
              type="button"
              class="hover:underline"
              data-testid="logout"
              @click="session.logout()"
            >
              {{ t('nav.logout') }}
            </button>
          </li>
        </ul>
      </nav>
    </header>
    <p
      v-if="session.renewFailed"
      role="alert"
      class="bg-amber-100 text-amber-900 text-sm px-4 py-2 text-center"
    >
      {{ t('auth.sessionExpired') }}
      <button type="button" class="underline ml-2" @click="session.login($route.fullPath)">
        {{ t('auth.signInAgain') }}
      </button>
    </p>
    <main id="main" tabindex="-1" class="outline-none flex-1 mx-auto w-full max-w-6xl px-4 py-6">
      <slot />
    </main>
    <AppFooter />
    <ConsentDialog v-if="session.consentOutdated" />
  </div>
</template>
