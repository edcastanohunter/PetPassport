<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, type OrgStats } from '@/shared/api/client'
import { getOrgStats } from './careLinkApi'

// Estadísticas básicas de la clínica (PRD-026): mes actual frente al anterior.
// Vista derivada: se muestra el retraso de la proyección cuando es notable.
const props = defineProps<{ orgId: string }>()
const { t } = useI18n()

const stats = ref<OrgStats | null>(null)
const error = ref<string | null>(null)

onMounted(async () => {
  try {
    stats.value = await getOrgStats(props.orgId)
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  }
})

const rows = computed(() => {
  const s = stats.value
  if (!s) return []
  const c = s.current
  const p = s.previous
  return [
    { key: 'encounters', cur: c.encounters_signed, prev: p.encounters_signed },
    { key: 'confirmed', cur: c.appointments.confirmed, prev: p.appointments.confirmed },
    { key: 'completed', cur: c.appointments.completed, prev: p.appointments.completed },
    { key: 'cancelled', cur: c.appointments.cancelled, prev: p.appointments.cancelled },
    { key: 'noShow', cur: c.appointments.no_show, prev: p.appointments.no_show },
    { key: 'newPatients', cur: c.new_patients, prev: p.new_patients },
    { key: 'vaccinations', cur: c.vaccinations, prev: p.vaccinations },
  ]
})
</script>

<template>
  <section class="card space-y-2" data-testid="org-stats">
    <h2 class="text-lg font-semibold">{{ t('orgs.stats.title') }}</h2>
    <p v-if="error" role="alert" class="text-sm text-red-700">{{ error }}</p>
    <template v-if="stats">
      <table class="w-full text-left text-sm">
        <thead class="text-surface-500">
          <tr>
            <th scope="col">
              <span class="sr-only">{{ t('orgs.stats.metric') }}</span>
            </th>
            <th scope="col">{{ stats.current.month }}</th>
            <th scope="col">{{ stats.previous.month }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.key" data-testid="stats-row">
            <th scope="row" class="font-normal">{{ t(`orgs.stats.${r.key}`) }}</th>
            <td class="font-semibold">{{ r.cur }}</td>
            <td>{{ r.prev }}</td>
          </tr>
          <tr>
            <th scope="row" class="font-normal">{{ t('orgs.stats.openFollowUps') }}</th>
            <td class="font-semibold" colspan="2">{{ stats.open_follow_ups }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="stats.freshness.lag_seconds > 60" class="text-xs text-surface-500">
        {{ t('orgs.stats.lag', { seconds: Math.round(stats.freshness.lag_seconds) }) }}
      </p>
    </template>
  </section>
</template>
