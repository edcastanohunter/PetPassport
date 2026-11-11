<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { useStatusStore } from './statusStore'
import type { ServiceStatus } from './statusTypes'

const { t } = useI18n()
const store = useStatusStore()

onMounted(() => {
  if (!store.lastFetched) {
    void store.fetchStatus()
  }
})

function statusBadgeClass(status: ServiceStatus) {
  switch (status) {
    case 'operational':
      return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300'
    case 'degraded_performance':
      return 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300'
    case 'partial_outage':
    case 'major_outage':
      return 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300'
    case 'maintenance':
      return 'bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300'
    default:
      return 'bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-300'
  }
}

function statusLabel(status: ServiceStatus) {
  switch (status) {
    case 'operational':
      return t('status.operational')
    case 'degraded_performance':
      return t('status.degraded')
    case 'partial_outage':
      return t('status.partialOutage')
    case 'major_outage':
      return t('status.majorOutage')
    case 'maintenance':
      return t('status.maintenance')
    default:
      return status
  }
}
</script>

<template>
  <div class="max-w-4xl mx-auto py-8 px-4" data-testid="status-page">
    <div
      class="flex items-center justify-between mb-8 pb-4 border-b border-surface-200 dark:border-surface-800"
    >
      <div>
        <h1 class="text-2xl font-bold">{{ t('status.pageTitle') }}</h1>
        <p class="text-sm text-surface-500 dark:text-surface-400 mt-1">
          {{ t('status.lastUpdated') }}:
          {{
            store.statusData?.updated_at
              ? new Date(store.statusData.updated_at).toLocaleString()
              : '—'
          }}
        </p>
      </div>
      <RouterLink to="/" class="text-sm font-medium text-primary-600 hover:underline">
        {{ t('status.backToApp') }}
      </RouterLink>
    </div>

    <!-- Overall System Status Card -->
    <div
      class="p-6 rounded-xl mb-8 flex items-center justify-between shadow-sm border"
      :class="
        store.isOperational
          ? 'bg-emerald-50 border-emerald-200 dark:bg-emerald-950/40 dark:border-emerald-800'
          : 'bg-amber-50 border-amber-200 dark:bg-amber-950/40 dark:border-amber-800'
      "
      data-testid="overall-status"
    >
      <div class="flex items-center gap-4">
        <div
          class="w-10 h-10 rounded-full flex items-center justify-center font-bold text-lg"
          :class="store.isOperational ? 'bg-emerald-500 text-white' : 'bg-amber-500 text-white'"
        >
          {{ store.isOperational ? '✓' : '!' }}
        </div>
        <div>
          <h2 class="text-lg font-semibold">
            {{
              store.isOperational
                ? t('status.allSystemsOperational')
                : t('status.someSystemsDegraded')
            }}
          </h2>
          <p class="text-sm opacity-90">
            {{
              store.isOperational
                ? t('status.allOperationalSubtitle')
                : t('status.degradedSubtitle')
            }}
          </p>
        </div>
      </div>
      <button
        type="button"
        class="text-xs px-3 py-1.5 border rounded-lg bg-surface-0 dark:bg-surface-900 shadow-sm hover:bg-surface-50"
        data-testid="refresh-status-btn"
        @click="store.fetchStatus()"
      >
        {{ t('common.refresh') }}
      </button>
    </div>

    <!-- Active Incidents Section -->
    <section v-if="store.hasActiveIncident" class="mb-8" data-testid="active-incidents">
      <h3 class="text-lg font-semibold mb-3 text-red-600 dark:text-red-400">
        {{ t('status.activeIncidentsTitle') }}
      </h3>
      <div
        v-for="inc in store.statusData?.active_incidents"
        :key="inc.id"
        class="p-4 rounded-lg border border-red-200 bg-red-50/50 dark:border-red-900 dark:bg-red-950/30 mb-3"
      >
        <div class="flex items-center justify-between">
          <span class="font-bold text-base">[{{ inc.severity }}] {{ inc.name }}</span>
          <span class="text-xs font-semibold px-2 py-0.5 rounded uppercase bg-red-200 text-red-900">
            {{ inc.status }}
          </span>
        </div>
        <p class="text-sm mt-2 text-surface-700 dark:text-surface-300">{{ inc.impact }}</p>
        <div
          v-if="inc.updates && inc.updates.length > 0"
          class="mt-4 space-y-2 border-t pt-2 text-xs"
        >
          <div v-for="u in inc.updates" :key="u.id" class="text-surface-600 dark:text-surface-400">
            <span class="font-semibold">{{ new Date(u.created_at).toLocaleTimeString() }}:</span>
            {{ u.body }}
          </div>
        </div>
      </div>
    </section>

    <!-- Scheduled Maintenance Section -->
    <section
      v-if="
        store.statusData?.scheduled_maintenances &&
        store.statusData.scheduled_maintenances.length > 0
      "
      class="mb-8"
      data-testid="scheduled-maintenance"
    >
      <h3 class="text-lg font-semibold mb-3 text-blue-600 dark:text-blue-400">
        {{ t('status.scheduledMaintenanceTitle') }}
      </h3>
      <div
        v-for="m in store.statusData.scheduled_maintenances"
        :key="m.id"
        class="p-4 rounded-lg border border-blue-200 bg-blue-50/50 dark:border-blue-900 dark:bg-blue-950/30 mb-3"
      >
        <div class="flex items-center justify-between">
          <span class="font-bold text-base">{{ m.name }}</span>
          <span
            class="text-xs font-semibold px-2 py-0.5 rounded uppercase bg-blue-200 text-blue-900"
          >
            {{ m.status }}
          </span>
        </div>
        <p class="text-sm mt-1 text-surface-600 dark:text-surface-400">{{ m.description }}</p>
        <p class="text-xs text-surface-500 mt-2">
          {{ new Date(m.scheduled_for).toLocaleString() }} —
          {{ new Date(m.scheduled_until).toLocaleString() }}
        </p>
      </div>
    </section>

    <!-- Components Status Grid -->
    <section class="mb-8" data-testid="components-status">
      <h3 class="text-lg font-semibold mb-4">{{ t('status.componentsTitle') }}</h3>
      <div
        class="rounded-xl border border-surface-200 dark:border-surface-800 divide-y divide-surface-200 dark:divide-surface-800 overflow-hidden shadow-sm"
      >
        <div
          v-for="comp in store.statusData?.components"
          :key="comp.id"
          class="p-4 flex items-center justify-between bg-surface-0 dark:bg-surface-900"
          :data-testid="`component-${comp.id}`"
        >
          <div>
            <div class="font-medium text-sm">{{ comp.name }}</div>
            <div class="text-xs text-surface-500 mt-0.5">
              {{ t('status.uptime90d') }}: {{ comp.uptime_percentage_90d }}%
            </div>
          </div>
          <span
            class="text-xs font-medium px-2.5 py-1 rounded-full"
            :class="statusBadgeClass(comp.status)"
          >
            {{ statusLabel(comp.status) }}
          </span>
        </div>
      </div>
    </section>

    <!-- External Status Page Link & Subscriptions -->
    <div
      class="p-4 rounded-xl border border-surface-200 dark:border-surface-800 bg-surface-50 dark:bg-surface-900/50 flex flex-col sm:flex-row items-center justify-between gap-4 text-sm"
    >
      <span>{{ t('status.externalPageNote') }}</span>
      <a
        href="https://status.petpassport.app"
        target="_blank"
        rel="noopener noreferrer"
        class="btn-primary text-xs shrink-0"
      >
        {{ t('status.openExternalPage') }} ↗
      </a>
    </div>
  </div>
</template>
