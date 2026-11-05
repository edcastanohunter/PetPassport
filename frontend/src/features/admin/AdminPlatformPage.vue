<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ReasonDialog from './ReasonDialog.vue'
import {
  getPlatformStats,
  getTopOrganizations,
  getSearchIndices,
  reindexIndex,
  retryIndexQueue,
  purgeCache,
  type PlatformDailyMetrics,
  type TopOrganizationItem,
  type SearchIndexSummary,
} from './platformApi'

const { t, d } = useI18n()

const loading = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

// Parámetros de consulta
const granularity = ref('daily')
const dateFrom = ref('')
const dateTo = ref('')

// Datos
const statsSeries = ref<PlatformDailyMetrics[]>([])
const topOrgs = ref<TopOrganizationItem[]>([])
const indices = ref<SearchIndexSummary[]>([])

// Diálogos de confirmación con motivo
type PendingAction =
  | { type: 'reindex'; alias: string }
  | { type: 'purge'; families: string[] }

const pendingAction = ref<PendingAction | null>(null)
const dialogBusy = ref(false)

// Métricas más recientes para las tarjetas KPI
const latestMetrics = computed(() => {
  if (statsSeries.value.length === 0) return null
  return statsSeries.value[statsSeries.value.length - 1]
})

async function loadData() {
  loading.value = true
  error.value = null
  try {
    const [stats, orgs, idxs] = await Promise.all([
      getPlatformStats({
        granularity: granularity.value,
        from: dateFrom.value || undefined,
        to: dateTo.value || undefined,
      }),
      getTopOrganizations({ metric: 'encounters_signed', period: '30d' }),
      getSearchIndices(),
    ])
    statsSeries.value = stats
    topOrgs.value = orgs
    indices.value = idxs
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : t('common.errorGeneric')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadData()
})

function triggerReindex(alias: string) {
  pendingAction.value = { type: 'reindex', alias }
}

function triggerPurge() {
  pendingAction.value = { type: 'purge', families: ['search', 'catalog'] }
}

async function triggerRetryQueue() {
  loading.value = true
  error.value = null
  notice.value = null
  try {
    const res = await retryIndexQueue()
    notice.value = t('admin.platform.retrySuccess', { count: res.retried })
    await loadData()
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : t('common.errorGeneric')
  } finally {
    loading.value = false
  }
}

async function confirmAction(reason: string) {
  if (!pendingAction.value) return
  dialogBusy.value = true
  error.value = null
  notice.value = null
  try {
    if (pendingAction.value.type === 'reindex') {
      const res = await reindexIndex(pendingAction.value.alias, reason)
      notice.value = t('admin.platform.reindexSuccess', { alias: pendingAction.value.alias, job: res.job_id })
    } else if (pendingAction.value.type === 'purge') {
      await purgeCache(reason, pendingAction.value.families)
      notice.value = t('admin.platform.purgeSuccess')
    }
    pendingAction.value = null
    await loadData()
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : t('common.errorGeneric')
  } finally {
    dialogBusy.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">{{ t('admin.platform.title') }}</h1>
        <p class="text-sm text-surface-500">{{ t('admin.platform.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="btn-secondary text-sm"
          :disabled="loading"
          @click="loadData"
        >
          <span class="pi pi-refresh mr-1" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
        <button
          type="button"
          class="btn-secondary text-sm"
          :disabled="loading"
          @click="triggerPurge"
        >
          <span class="pi pi-trash mr-1" />
          {{ t('admin.platform.purgeCache') }}
        </button>
      </div>
    </header>

    <div
      v-if="notice"
      class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200"
      role="status"
    >
      {{ notice }}
    </div>

    <div
      v-if="error"
      class="rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ error }}
    </div>

    <!-- KPI Cards -->
    <section aria-labelledby="kpi-heading" class="space-y-3">
      <h2 id="kpi-heading" class="text-lg font-semibold">{{ t('admin.platform.kpi') }}</h2>
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <div class="card p-4 space-y-1">
          <span class="text-xs font-medium text-surface-500 uppercase">{{ t('admin.platform.usersActive') }}</span>
          <p class="text-2xl font-bold" data-testid="kpi-users">
            {{ latestMetrics?.users_active ?? '—' }}
          </p>
        </div>
        <div class="card p-4 space-y-1">
          <span class="text-xs font-medium text-surface-500 uppercase">{{ t('admin.platform.pets') }}</span>
          <p class="text-2xl font-bold" data-testid="kpi-pets">
            {{ latestMetrics?.pets ?? '—' }}
          </p>
        </div>
        <div class="card p-4 space-y-1">
          <span class="text-xs font-medium text-surface-500 uppercase">{{ t('admin.platform.encounters') }}</span>
          <p class="text-2xl font-bold" data-testid="kpi-encounters">
            {{ latestMetrics?.encounters_signed ?? '—' }}
          </p>
        </div>
        <div class="card p-4 space-y-1">
          <span class="text-xs font-medium text-surface-500 uppercase">{{ t('admin.platform.orgs') }}</span>
          <p class="text-2xl font-bold" data-testid="kpi-orgs">
            {{ (latestMetrics?.organizations as Record<string, number>)?.active ?? '—' }}
          </p>
        </div>
      </div>
    </section>

    <!-- Filtros de serie temporal -->
    <section class="card p-4 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-surface-200 pb-3 dark:border-surface-800">
        <h2 class="text-lg font-semibold">{{ t('admin.platform.seriesTitle') }}</h2>
        <div class="flex flex-wrap items-center gap-2">
          <select v-model="granularity" class="input py-1 text-sm" @change="loadData">
            <option value="daily">{{ t('admin.platform.daily') }}</option>
            <option value="weekly">{{ t('admin.platform.weekly') }}</option>
            <option value="monthly">{{ t('admin.platform.monthly') }}</option>
          </select>
          <input
            v-model="dateFrom"
            type="date"
            class="input py-1 text-sm"
            :placeholder="t('admin.platform.dateFrom')"
            @change="loadData"
          />
          <input
            v-model="dateTo"
            type="date"
            class="input py-1 text-sm"
            :placeholder="t('admin.platform.dateTo')"
            @change="loadData"
          />
        </div>
      </div>

      <!-- Tabla de puntos de serie -->
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr class="border-b border-surface-200 text-surface-500 dark:border-surface-800">
              <th class="py-2">{{ t('common.date') }}</th>
              <th class="py-2">{{ t('admin.platform.usersActive') }}</th>
              <th class="py-2">{{ t('admin.platform.pets') }}</th>
              <th class="py-2">{{ t('admin.platform.encounters') }}</th>
              <th class="py-2">{{ t('admin.platform.orgs') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="point in statsSeries"
              :key="point.date"
              class="border-b border-surface-100 dark:border-surface-900"
            >
              <td class="py-2 font-mono text-xs">{{ point.date }}</td>
              <td class="py-2">{{ point.users_active ?? 0 }}</td>
              <td class="py-2">{{ point.pets ?? 0 }}</td>
              <td class="py-2">{{ point.encounters_signed ?? 0 }}</td>
              <td class="py-2">{{ (point.organizations as Record<string, number>)?.active ?? 0 }}</td>
            </tr>
            <tr v-if="statsSeries.length === 0">
              <td colspan="5" class="py-4 text-center text-surface-400">
                {{ t('admin.platform.noStats') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Top Organizaciones -->
    <section class="card p-4 space-y-3">
      <h2 class="text-lg font-semibold">{{ t('admin.platform.topOrgs') }}</h2>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr class="border-b border-surface-200 text-surface-500 dark:border-surface-800">
              <th class="py-2">#</th>
              <th class="py-2">{{ t('admin.platform.orgName') }}</th>
              <th class="py-2">{{ t('admin.platform.metric') }}</th>
              <th class="py-2 text-right">{{ t('admin.platform.total') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(org, i) in topOrgs"
              :key="org.organization_id"
              class="border-b border-surface-100 dark:border-surface-900"
            >
              <td class="py-2 font-semibold text-surface-500">{{ i + 1 }}</td>
              <td class="py-2 font-medium">{{ org.display_name }}</td>
              <td class="py-2 text-surface-500">{{ org.metric }}</td>
              <td class="py-2 text-right font-mono font-semibold">{{ org.count }}</td>
            </tr>
            <tr v-if="topOrgs.length === 0">
              <td colspan="4" class="py-4 text-center text-surface-400">
                {{ t('admin.platform.noTopOrgs') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Estado de índices y proyecciones -->
    <section class="card p-4 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-surface-200 pb-3 dark:border-surface-800">
        <div>
          <h2 class="text-lg font-semibold">{{ t('admin.platform.indices') }}</h2>
          <p class="text-xs text-surface-500">{{ t('admin.platform.indicesHint') }}</p>
        </div>
        <button
          type="button"
          class="btn-secondary text-sm"
          :disabled="loading"
          @click="triggerRetryQueue"
        >
          <span class="pi pi-sync mr-1" />
          {{ t('admin.platform.retryQueue') }}
        </button>
      </div>

      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr class="border-b border-surface-200 text-surface-500 dark:border-surface-800">
              <th class="py-2">{{ t('admin.platform.alias') }}</th>
              <th class="py-2">{{ t('admin.platform.physical') }}</th>
              <th class="py-2 text-right">{{ t('admin.platform.docs') }}</th>
              <th class="py-2 text-right">{{ t('admin.platform.lag') }}</th>
              <th class="py-2 text-center">{{ t('admin.platform.drift') }}</th>
              <th class="py-2 text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="idx in indices"
              :key="idx.alias"
              class="border-b border-surface-100 dark:border-surface-900"
            >
              <td class="py-2 font-medium font-mono text-xs">{{ idx.alias }}</td>
              <td class="py-2 font-mono text-xs text-surface-500">{{ idx.physical || '—' }}</td>
              <td class="py-2 text-right font-mono text-xs">{{ idx.docs }}</td>
              <td class="py-2 text-right font-mono text-xs">
                <span :class="{ 'text-amber-600 font-bold': idx.lag_seconds > 10 }">
                  {{ idx.lag_seconds }}s
                </span>
              </td>
              <td class="py-2 text-center">
                <span
                  class="rounded px-2 py-0.5 text-xs font-medium"
                  :class="idx.drift ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800'"
                >
                  {{ idx.drift ? t('admin.platform.driftYes') : t('admin.platform.driftNo') }}
                </span>
              </td>
              <td class="py-2 text-right">
                <button
                  type="button"
                  class="btn-secondary py-1 text-xs"
                  @click="triggerReindex(idx.alias)"
                >
                  {{ t('admin.platform.reindex') }}
                </button>
              </td>
            </tr>
            <tr v-if="indices.length === 0">
              <td colspan="6" class="py-4 text-center text-surface-400">
                {{ t('admin.platform.noIndices') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Diálogo de motivo para auditoría -->
    <ReasonDialog
      v-if="pendingAction"
      :title="pendingAction.type === 'reindex' ? t('admin.platform.reindexConfirm') : t('admin.platform.purgeConfirm')"
      :target="pendingAction.type === 'reindex' ? pendingAction.alias : 'platform_cache'"
      :busy="dialogBusy"
      @confirm="confirmAction"
      @cancel="pendingAction = null"
    />
  </div>
</template>
