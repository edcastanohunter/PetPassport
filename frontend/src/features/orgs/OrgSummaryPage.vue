<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { ADMIN_ROLES, CLINIC_ROLES } from '@/shared/validation/schemas'
import OrgStatsCard from './OrgStatsCard.vue'
import { useOrgStore } from './orgStore'

// Resumen de organización (FE-007): banner de estado (pendiente /
// suspendida / rechazada con motivo), código público con copiar y tarjeta
// imprimible (QR en F2) y accesos rápidos según rol y tipo.
const { t } = useI18n()
const route = useRoute()
const session = useSessionStore()
const store = useOrgStore()

const orgId = computed(() => String(route.params.orgId))
const error = ref<string | null>(null)
const copied = ref(false)
const membership = computed(() => session.membershipFor(orgId.value))
const isAdmin = computed(() => ADMIN_ROLES.includes(membership.value?.role ?? ''))
const seesStats = computed(
  () =>
    membership.value?.type === 'clinic' &&
    ['clinic_admin', 'veterinarian'].includes(membership.value.role),
)
const seesPatients = computed(
  () => membership.value?.type === 'clinic' && CLINIC_ROLES.includes(membership.value.role),
)

async function load() {
  error.value = null
  try {
    await store.load(orgId.value)
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  }
}

onMounted(load)
watch(orgId, load)

const org = computed(() => store.current)
const bannerClass = computed(() => {
  switch (org.value?.status) {
    case 'pending_verification':
      return 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200'
    case 'suspended':
    case 'rejected':
      return 'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200'
    default:
      return ''
  }
})

async function copyCode() {
  if (!org.value) return
  try {
    await navigator.clipboard.writeText(org.value.public_code)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    copied.value = false
  }
}

function printCard() {
  window.print()
}
</script>

<template>
  <section class="space-y-6">
    <p
      v-if="error"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>

    <template v-if="org">
      <header class="no-print">
        <h1 class="text-2xl font-bold">{{ org.display_name }}</h1>
        <p class="text-sm text-surface-500">
          {{ t(`orgs.type.${org.type}`) }} · {{ org.legal_name }}
        </p>
      </header>

      <div
        v-if="org.status !== 'active'"
        class="no-print rounded-xl border p-4"
        :class="bannerClass"
        role="status"
        data-testid="org-status-banner"
      >
        <p class="font-semibold">{{ t(`orgs.status.${org.status}`) }}</p>
        <p class="text-sm">{{ t(`orgs.status.${org.status}_hint`) }}</p>
        <p v-if="org.status_reason" class="mt-1 text-sm">
          {{ t('orgs.statusReason') }}: {{ org.status_reason }}
        </p>
      </div>

      <div
        class="print-card rounded-xl border border-surface-200 bg-white p-6 text-center shadow-sm dark:border-surface-700 dark:bg-surface-800"
        data-testid="public-code-card"
      >
        <p class="text-sm text-surface-500">{{ t('orgs.publicCode') }}</p>
        <p class="my-2 font-mono text-4xl font-bold tracking-[0.3em]">{{ org.public_code }}</p>
        <p class="text-sm">{{ t('orgs.publicCodeHint', { name: org.display_name }) }}</p>
        <div class="no-print mt-4 flex justify-center gap-3">
          <button type="button" class="btn-secondary" data-testid="copy-code" @click="copyCode">
            {{ copied ? t('orgs.copied') : t('orgs.copy') }}
          </button>
          <button type="button" class="btn-secondary" @click="printCard">
            {{ t('orgs.print') }}
          </button>
        </div>
      </div>

      <OrgStatsCard v-if="seesStats && org.status === 'active'" class="no-print" :org-id="orgId" />

      <nav class="no-print grid gap-3 sm:grid-cols-3" :aria-label="t('orgs.quickLinks')">
        <RouterLink
          v-if="seesPatients"
          :to="{ name: 'org-patients', params: { orgId } }"
          class="card"
        >
          <span class="block font-semibold">{{ t('orgs.nav.patients') }}</span>
          <span class="block text-sm text-surface-500">{{ t('orgs.nav.patientsHint') }}</span>
        </RouterLink>
        <RouterLink v-if="isAdmin" :to="{ name: 'org-members', params: { orgId } }" class="card">
          <span class="block font-semibold">{{ t('orgs.nav.members') }}</span>
          <span class="block text-sm text-surface-500">
            {{ t('orgs.memberCount', { n: org.member_count ?? 0 }) }}
          </span>
        </RouterLink>
        <RouterLink v-if="isAdmin" :to="{ name: 'org-settings', params: { orgId } }" class="card">
          <span class="block font-semibold">{{ t('orgs.nav.settings') }}</span>
          <span class="block text-sm text-surface-500">{{ t('orgs.nav.settingsHint') }}</span>
        </RouterLink>
      </nav>
    </template>
  </section>
</template>
