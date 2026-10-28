<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/shared/api/client'
import { useAdminStore, type AuditFilter } from './adminStore'

// Auditoría (FE-009, SEC-005): tabla de audit_log filtrable por actor,
// objetivo y rango de fechas (más reciente primero, paginada por cursor).
const { t, d } = useI18n()
const store = useAdminStore()

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const form = ref({ actor_id: '', target_id: '', from: '', to: '' })
const fieldErrors = ref<Record<string, string>>({})
const error = ref<string | null>(null)

// Las fechas del formulario son locales (input date); el rango es [from, to+1d).
function toFilter(): AuditFilter | null {
  const errs: Record<string, string> = {}
  for (const k of ['actor_id', 'target_id'] as const) {
    const v = form.value[k].trim()
    if (v && !UUID.test(v)) errs[k] = 'INVALID_FORMAT'
  }
  const from = form.value.from ? new Date(`${form.value.from}T00:00:00`) : null
  const to = form.value.to ? new Date(`${form.value.to}T00:00:00`) : null
  if (to) to.setDate(to.getDate() + 1)
  if (from && to && from >= to) errs.to = 'INVALID'
  fieldErrors.value = errs
  if (Object.keys(errs).length) return null
  return {
    actor_id: form.value.actor_id.trim(),
    target_id: form.value.target_id.trim(),
    from: from?.toISOString(),
    to: to?.toISOString(),
  }
}

async function load(more = false) {
  const filter = toFilter()
  if (!filter) return
  error.value = null
  try {
    await store.loadAudit(filter, more)
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  }
}

onMounted(() => load())
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('admin.nav.audit') }}</h1>

    <form
      class="card grid gap-3 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_auto_auto_auto] lg:items-end"
      novalidate
      @submit.prevent="load()"
    >
      <div>
        <label class="label" for="audit-actor">{{ t('admin.audit.actor') }}</label>
        <input
          id="audit-actor"
          v-model="form.actor_id"
          class="input"
          :aria-invalid="!!fieldErrors.actor_id"
        />
        <p v-if="fieldErrors.actor_id" class="text-sm text-red-600">
          {{ t(`fieldErrors.${fieldErrors.actor_id}`) }}
        </p>
      </div>
      <div>
        <label class="label" for="audit-target">{{ t('admin.audit.target') }}</label>
        <input
          id="audit-target"
          v-model="form.target_id"
          class="input"
          :aria-invalid="!!fieldErrors.target_id"
        />
        <p v-if="fieldErrors.target_id" class="text-sm text-red-600">
          {{ t(`fieldErrors.${fieldErrors.target_id}`) }}
        </p>
      </div>
      <div>
        <label class="label" for="audit-from">{{ t('admin.audit.from') }}</label>
        <input id="audit-from" v-model="form.from" type="date" class="input" />
      </div>
      <div>
        <label class="label" for="audit-to">{{ t('admin.audit.to') }}</label>
        <input
          id="audit-to"
          v-model="form.to"
          type="date"
          class="input"
          :aria-invalid="!!fieldErrors.to"
        />
        <p v-if="fieldErrors.to" class="text-sm text-red-600">
          {{ t('admin.audit.badRange') }}
        </p>
      </div>
      <button type="submit" class="btn-primary" data-testid="filter-audit">
        {{ t('admin.audit.filter') }}
      </button>
    </form>

    <p
      v-if="error"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>

    <p v-if="!store.audit.length" class="text-surface-500">{{ t('admin.audit.empty') }}</p>
    <div v-else class="overflow-x-auto">
      <table class="table-cards w-full text-left text-sm">
        <thead class="text-surface-500">
          <tr>
            <th class="py-2">{{ t('admin.audit.when') }}</th>
            <th>{{ t('admin.audit.action') }}</th>
            <th>{{ t('admin.audit.actor') }}</th>
            <th>{{ t('admin.audit.target') }}</th>
            <th>{{ t('admin.audit.reason') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="e in store.audit"
            :key="e.id"
            class="border-t border-surface-200 align-top dark:border-surface-700"
            data-testid="audit-row"
          >
            <td class="py-2 whitespace-nowrap" :data-label="t('admin.audit.when')">
              {{ d(new Date(e.occurred_at), 'long') }}
            </td>
            <td class="font-mono text-xs" :data-label="t('admin.audit.action')">{{ e.action }}</td>
            <td class="font-mono text-xs" :data-label="t('admin.audit.actor')">
              {{ e.actor_id ?? t('admin.audit.system') }}
              <span v-if="e.actor_roles.length" class="block text-surface-500">{{
                e.actor_roles.join(', ')
              }}</span>
            </td>
            <td class="font-mono text-xs" :data-label="t('admin.audit.target')">
              <span v-if="e.target_type" class="block text-surface-500">{{ e.target_type }}</span>
              {{ e.target_id ?? '—' }}
            </td>
            <td :data-label="t('admin.audit.reason')">{{ e.reason ?? '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="store.auditCursor" type="button" class="btn-secondary" @click="load(true)">
      {{ t('admin.loadMore') }}
    </button>
  </section>
</template>
