<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { api, type TimelineItem, type TimelinePage } from '@/shared/api/client'
import { useDerivedView } from '@/shared/composables/useDerivedView'
import FreshnessIndicator from '@/shared/ui/FreshnessIndicator.vue'

// Historial de la mascota (PP-F2-PRD-008/009, FE-005/FE-009): timeline con
// filtros por tipo y clínica, paginación por cursor e indicador de frescura
// (FE-014). El servidor ya aplica audiencia y custodia; aquí solo se marca lo
// que viene de otras clínicas.
const props = defineProps<{
  petId: string
  /** Organización del contexto: los ítems de otras se marcan. */
  orgId?: string
  orgNames?: Record<string, string>
  linkFor?: (item: TimelineItem) => RouteLocationRaw | null
}>()
const { t, te, d } = useI18n()

const KINDS = [
  'encounter',
  'addendum',
  'vaccine',
  'preventive',
  'appointment',
  'guardianship',
  'care_link',
] as const
type Kind = (typeof KINDS)[number]
const kind = ref<Kind | ''>('')
const organization = ref('')
const items = ref<TimelineItem[]>([])
const cursor = ref<string | undefined>()
const loadingMore = ref(false)

function query(c?: string) {
  return {
    ...(kind.value ? { kind: kind.value } : {}),
    ...(organization.value ? { organization_id: organization.value } : {}),
    ...(c ? { cursor: c } : {}),
  }
}

const view = useDerivedView<TimelinePage>(async () => {
  const res = await api().GET('/pets/{petId}/timeline', {
    params: { path: { petId: props.petId }, query: query() },
  })
  if (res.error) throw res.error
  return res
})

watch(view.data, (page) => {
  items.value = page?.data ?? []
  cursor.value = page?.next_cursor
})
watch([kind, organization], () => void view.reload())
void view.reload()

async function more() {
  if (!cursor.value) return
  loadingMore.value = true
  try {
    const { data, error } = await api().GET('/pets/{petId}/timeline', {
      params: { path: { petId: props.petId }, query: query(cursor.value) },
    })
    if (error) throw error
    items.value = [...items.value, ...(data?.data ?? [])]
    cursor.value = data?.next_cursor
  } finally {
    loadingMore.value = false
  }
}

function title(item: TimelineItem) {
  const k = `records.timeline.titles.${item.title}`
  return te(k) ? t(k) : t(`records.timeline.kinds.${item.kind}`)
}
function other(item: TimelineItem) {
  return !!props.orgId && !!item.organization_id && item.organization_id !== props.orgId
}
function orgName(id?: string) {
  if (!id) return ''
  return props.orgNames?.[id] ?? t('records.timeline.otherClinic')
}

defineExpose({ afterCommand: view.afterCommand })
</script>

<template>
  <section class="space-y-4" data-testid="timeline">
    <div class="flex flex-wrap items-end gap-3">
      <label>
        <span class="label">{{ t('records.timeline.filterKind') }}</span>
        <select v-model="kind" class="input" data-testid="timeline-kind">
          <option value="">{{ t('records.timeline.all') }}</option>
          <option v-for="k in KINDS" :key="k" :value="k">
            {{ t(`records.timeline.kinds.${k}`) }}
          </option>
        </select>
      </label>
      <label v-if="orgNames && Object.keys(orgNames).length > 1">
        <span class="label">{{ t('records.timeline.filterClinic') }}</span>
        <select v-model="organization" class="input">
          <option value="">{{ t('records.timeline.all') }}</option>
          <option v-for="(name, id) in orgNames" :key="id" :value="id">{{ name }}</option>
        </select>
      </label>
    </div>

    <FreshnessIndicator
      :lag-seconds="view.lagSeconds.value"
      :loading="view.loading.value"
      @reload="view.reload()"
    />

    <p
      v-if="view.error.value"
      class="rounded bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ t('errors.GENERIC') }}
    </p>
    <p
      v-else-if="!view.loading.value && items.length === 0"
      class="rounded-lg bg-surface-200/40 p-6 text-center text-sm"
    >
      {{ t('records.timeline.empty') }}
    </p>

    <ol class="space-y-2" :class="{ 'opacity-60': view.loading.value }">
      <li
        v-for="item in items"
        :key="`${item.kind}-${item.ref_id}-${item.occurred_at}`"
        class="rounded-lg border border-surface-200 p-3 dark:border-surface-800"
        data-testid="timeline-item"
      >
        <p class="flex flex-wrap items-center gap-2 text-xs text-surface-500">
          <time :datetime="item.occurred_at">{{ d(item.occurred_at, 'long') }}</time>
          <span>· {{ t(`records.timeline.kinds.${item.kind}`) }}</span>
          <span v-if="item.organization_id && orgNames">· {{ orgName(item.organization_id) }}</span>
          <span
            v-if="other(item)"
            class="rounded bg-surface-200 px-2 text-surface-900 dark:bg-surface-800 dark:text-surface-0"
            data-testid="other-clinic"
            >{{ t('records.timeline.otherClinicBadge') }}</span
          >
        </p>
        <component
          :is="linkFor?.(item) ? 'RouterLink' : 'p'"
          :to="linkFor?.(item) ?? undefined"
          class="mt-1 block font-medium"
          :class="{ underline: !!linkFor?.(item) }"
        >
          {{ title(item) }}
        </component>
        <p v-if="item.summary" class="text-sm">{{ item.summary }}</p>
      </li>
    </ol>
    <button v-if="cursor" type="button" class="btn-secondary" :disabled="loadingMore" @click="more">
      {{ t('orgs.patients.more') }}
    </button>
  </section>
</template>
