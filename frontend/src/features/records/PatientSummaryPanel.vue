<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { api, type PetClinicalSummary } from '@/shared/api/client'
import { useDerivedView } from '@/shared/composables/useDerivedView'
import FreshnessIndicator from '@/shared/ui/FreshnessIndicator.vue'

// Resumen clínico del paciente (PP-F2-PRD-009, FE-003 columna derecha y FE-005
// pestaña Resumen): alertas del perfil, peso actual, última consulta firmada,
// próximas dosis (llegan con el carnet, M2.2) y controles pendientes. Vista
// derivada con indicador de frescura (FE-014).
const props = withDefaults(
  defineProps<{ petId: string; compact?: boolean; showAccess?: boolean }>(),
  {
    compact: false,
    showAccess: true,
  },
)
const emit = defineEmits<{ loaded: [PetClinicalSummary] }>()
const { t, te, d, n } = useI18n()

const view = useDerivedView<PetClinicalSummary>(async () => {
  const res = await api().GET('/pets/{petId}/summary', { params: { path: { petId: props.petId } } })
  if (res.error) throw res.error
  if (res.data) emit('loaded', res.data)
  return res
})
void view.reload()

function typeLabel(k: string) {
  const key = `encounters.type.${k}`
  return te(key) ? t(key) : k
}

defineExpose({ afterCommand: view.afterCommand, data: view.data })
</script>

<template>
  <section class="space-y-4 text-sm" data-testid="patient-summary">
    <FreshnessIndicator
      :lag-seconds="view.lagSeconds.value"
      :loading="view.loading.value"
      @reload="view.reload()"
    />
    <p v-if="view.error.value" class="text-red-700 dark:text-red-300" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <template v-if="view.data.value">
      <div
        v-if="showAccess && view.data.value.access === 'custody'"
        class="rounded-lg border border-amber-300 bg-amber-50 p-3 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100"
        role="note"
        data-testid="custody-banner"
      >
        <span class="pi pi-lock mr-1" aria-hidden="true" />{{ t('records.custodyBanner') }}
      </div>

      <div>
        <h3 class="font-semibold">{{ t('records.summary.alerts') }}</h3>
        <ul
          v-if="view.data.value.alerts.length"
          class="mt-1 space-y-1"
          data-testid="summary-alerts"
        >
          <li
            v-for="(a, i) in view.data.value.alerts"
            :key="i"
            class="rounded bg-red-50 px-2 py-1 text-red-800 dark:bg-red-950 dark:text-red-200"
          >
            <span class="pi pi-exclamation-circle mr-1" aria-hidden="true" />{{ a }}
          </li>
        </ul>
        <p v-else class="text-surface-500">{{ t('records.summary.noAlerts') }}</p>
      </div>

      <div>
        <h3 class="font-semibold">{{ t('records.summary.weight') }}</h3>
        <p v-if="view.data.value.current_weight">
          <span class="tabular-nums"
            >{{
              n(view.data.value.current_weight.weight_kg, { maximumFractionDigits: 2 })
            }}
            kg</span
          >
          <span class="text-surface-500">
            · {{ d(view.data.value.current_weight.measured_at, 'short') }}</span
          >
        </p>
        <p v-else class="text-surface-500">{{ t('records.summary.noWeight') }}</p>
      </div>

      <div>
        <h3 class="font-semibold">{{ t('records.summary.lastEncounter') }}</h3>
        <p v-if="view.data.value.last_encounter">
          {{ d(view.data.value.last_encounter.started_at, 'short') }} ·
          {{ typeLabel(view.data.value.last_encounter.type) }} —
          {{ view.data.value.last_encounter.reason }}
        </p>
        <p v-else class="text-surface-500">{{ t('records.summary.noEncounters') }}</p>
      </div>

      <div v-if="!compact || view.data.value.next_due.length">
        <h3 class="font-semibold">{{ t('records.summary.nextDue') }}</h3>
        <p class="text-surface-500">{{ t('records.summary.nextDueSoon') }}</p>
      </div>

      <div>
        <h3 class="font-semibold">{{ t('records.summary.followUps') }}</h3>
        <ul v-if="view.data.value.open_follow_ups.length" class="mt-1 space-y-1">
          <li v-for="f in view.data.value.open_follow_ups" :key="f.encounter_id">
            {{ d(f.due_date + 'T12:00:00', 'short') }} — {{ f.reason }}
          </li>
        </ul>
        <p v-else class="text-surface-500">{{ t('records.summary.noFollowUps') }}</p>
      </div>
    </template>
  </section>
</template>
