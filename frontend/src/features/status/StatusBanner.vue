<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { useStatusStore } from './statusStore'

const { t } = useI18n()
const store = useStatusStore()
const dismissed = ref(false)

const shouldShow = computed(() => {
  if (dismissed.value) return false
  return store.bannerSeverity !== null
})

const severityClasses = computed(() => {
  switch (store.bannerSeverity) {
    case 'critical':
      return 'bg-red-600 text-white dark:bg-red-700'
    case 'warning':
      return 'bg-amber-500 text-slate-900 dark:bg-amber-600 dark:text-white'
    case 'info':
    default:
      return 'bg-blue-600 text-white dark:bg-blue-700'
  }
})

function dismiss() {
  dismissed.value = true
}
</script>

<template>
  <aside
    v-if="shouldShow"
    class="w-full px-4 py-2.5 flex items-center justify-between text-sm transition-colors duration-200"
    :class="severityClasses"
    role="status"
    aria-live="polite"
    data-testid="status-banner"
  >
    <div class="mx-auto max-w-5xl w-full flex items-center justify-between gap-4">
      <div class="flex items-center gap-2 min-w-0">
        <span class="font-bold shrink-0">
          <template v-if="store.activeIncident">
            [{{ store.activeIncident.severity }}] {{ store.activeIncident.name }}
          </template>
          <template v-else-if="store.activeMaintenance">
            [{{ t('status.maintenanceTitle') }}] {{ store.activeMaintenance.name }}
          </template>
          <template v-else>
            {{ t('status.serviceDegraded') }}
          </template>
        </span>
        <span class="truncate opacity-90 hidden sm:inline">
          <template v-if="store.activeIncident"> — {{ store.activeIncident.impact }} </template>
          <template v-else-if="store.activeMaintenance">
            — {{ store.activeMaintenance.description }}
          </template>
        </span>
      </div>

      <div class="flex items-center gap-3 shrink-0">
        <RouterLink
          to="/status"
          class="underline font-semibold hover:opacity-80 transition-opacity whitespace-nowrap"
          data-testid="status-banner-link"
        >
          {{ t('status.viewStatus') }}
        </RouterLink>
        <button
          type="button"
          class="p-1 hover:bg-black/10 rounded transition-colors"
          :aria-label="t('common.close')"
          data-testid="status-banner-dismiss"
          @click="dismiss"
        >
          ✕
        </button>
      </div>
    </div>
  </aside>
</template>
