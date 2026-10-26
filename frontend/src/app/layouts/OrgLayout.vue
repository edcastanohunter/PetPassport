<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '@/features/auth/sessionStore'
import { ADMIN_ROLES, CLINIC_ROLES } from '@/shared/validation/schemas'
import ConsentDialog from '@/features/onboarding/ConsentDialog.vue'
import AppFooter from '@/shared/ui/AppFooter.vue'

// Layout Org (FE-004/FE-007): selector de contexto (Mis mascotas + una
// entrada por organización + Administración) y navegación de la organización
// según tipo y rol. La UI no es la autoridad: el backend repite cada decisión.
const { t } = useI18n()
const route = useRoute()
const session = useSessionStore()

const orgId = computed(() => String(route.params.orgId ?? ''))
const membership = computed(() => session.membershipFor(orgId.value))

const contexts = computed(() => {
  const items: { to: string; label: string }[] = [{ to: '/app', label: t('nav.myPets') }]
  for (const m of session.memberships) {
    items.push({ to: `/org/${m.organization_id}`, label: m.display_name })
  }
  if (session.hasGlobalRole('super_admin')) items.push({ to: '/admin', label: t('nav.admin') })
  return items
})

const sections = computed(() => {
  const m = membership.value
  if (!m) return []
  const items = [{ name: 'org-summary', label: t('orgs.nav.summary') }]
  if (m.type === 'clinic' && CLINIC_ROLES.includes(m.role)) {
    items.push({ name: 'org-patients', label: t('orgs.nav.patients') })
  }
  if (ADMIN_ROLES.includes(m.role)) {
    items.push({ name: 'org-members', label: t('orgs.nav.members') })
    items.push({ name: 'org-settings', label: t('orgs.nav.settings') })
  }
  return items
})
</script>

<template>
  <div
    class="flex min-h-screen flex-col bg-surface-0 text-surface-900 dark:bg-surface-950 dark:text-surface-0"
  >
    <header class="no-print border-b border-surface-200 dark:border-surface-800">
      <nav
        class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-4 py-3"
        :aria-label="t('nav.main')"
      >
        <div class="flex items-center gap-4">
          <RouterLink to="/app" class="text-lg font-semibold">PetPassport</RouterLink>
          <ul class="hidden gap-3 text-sm sm:flex" data-testid="context-switcher">
            <li v-for="c in contexts" :key="c.to">
              <RouterLink :to="c.to" class="hover:underline" active-class="font-semibold">{{
                c.label
              }}</RouterLink>
            </li>
          </ul>
        </div>
        <ul class="flex items-center gap-3 text-sm">
          <li>
            <RouterLink to="/app/profile" class="hover:underline">{{
              t('nav.profile')
            }}</RouterLink>
          </li>
          <li>
            <button type="button" class="hover:underline" @click="session.logout()">
              {{ t('nav.logout') }}
            </button>
          </li>
        </ul>
      </nav>
      <nav v-if="membership" class="mx-auto max-w-6xl px-4 pb-2" :aria-label="t('orgs.nav.label')">
        <p class="text-sm text-surface-500">
          {{ membership.display_name }} · {{ t(`orgs.role.${membership.role}`) }}
        </p>
        <ul class="mt-1 flex flex-wrap gap-3 text-sm" data-testid="org-nav">
          <li v-for="s in sections" :key="s.name">
            <RouterLink
              :to="{ name: s.name, params: { orgId } }"
              class="hover:underline"
              exact-active-class="font-semibold underline"
            >
              {{ s.label }}
            </RouterLink>
          </li>
        </ul>
      </nav>
    </header>
    <main id="main" class="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
      <slot />
    </main>
    <AppFooter />
    <ConsentDialog v-if="session.consentOutdated" />
  </div>
</template>
