<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, isApiError, type Encounter } from '@/shared/api/client'
import EncounterDetail from '@/features/encounters/EncounterDetail.vue'
import { getEncounter } from '@/features/encounters/encounterApi'

// Detalle de consulta para el tutor (PP-F2-FE-009, PRD-008): todo lo firmado,
// adendas, adjuntos visibles y PDF; indica clínica y profesional. Lectura
// auditada en servidor (SEC-005).
const { t } = useI18n()
const route = useRoute()
const petId = computed(() => String(route.params.petId))
const encounterId = computed(() => String(route.params.encounterId))

const encounter = shallowRef<Encounter | null>(null)
const orgName = ref<string | undefined>()
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')

onMounted(async () => {
  try {
    const e = await getEncounter(encounterId.value)
    encounter.value = e
    state.value = 'ready'
    const { data } = await api().GET('/pets/{petId}', { params: { path: { petId: petId.value } } })
    orgName.value = data?.care_links.find(
      (l) => l.organization_id === e.organization_id,
    )?.display_name
  } catch (e) {
    state.value = isApiError(e) && e.status === 404 ? 'notFound' : 'error'
  }
})
</script>

<template>
  <section class="mx-auto max-w-3xl space-y-6">
    <RouterLink :to="{ name: 'pet-history', params: { petId } }" class="text-sm underline">
      ← {{ t('records.history.back') }}
    </RouterLink>
    <p v-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('encounters.notFound') }}
    </p>
    <p
      v-else-if="state === 'error'"
      class="rounded bg-red-50 p-3 text-sm text-red-700"
      role="alert"
    >
      {{ t('errors.GENERIC') }}
    </p>
    <EncounterDetail v-if="encounter" :encounter="encounter" :organization-name="orgName" />
  </section>
</template>
