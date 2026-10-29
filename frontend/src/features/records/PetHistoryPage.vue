<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type EncounterSummary, type Pet, type TimelineItem } from '@/shared/api/client'
import TimelineList from './TimelineList.vue'

// Historial del tutor (PP-F2-FE-009, PRD-008): tarjetas por consulta firmada
// (clínica, profesional, diagnósticos, adendas) y timeline con filtros e
// indicador de frescura. El servidor solo entrega consultas firmadas al tutor.
const { t, te, d } = useI18n()
const route = useRoute()
const petId = computed(() => String(route.params.petId))

const pet = ref<Pet | null>(null)
const cards = ref<EncounterSummary[]>([])
const cursor = ref<string | undefined>()
const state = ref<'loading' | 'ready' | 'error'>('loading')
const view = ref<'cards' | 'timeline'>('cards')

async function loadCards(c?: string) {
  const { data, error } = await api().GET('/pets/{petId}/encounters', {
    params: { path: { petId: petId.value }, query: c ? { cursor: c } : {} },
  })
  if (error) throw error
  cards.value = c ? [...cards.value, ...(data?.data ?? [])] : (data?.data ?? [])
  cursor.value = data?.next_cursor
}

onMounted(async () => {
  try {
    const [p] = await Promise.all([
      api().GET('/pets/{petId}', { params: { path: { petId: petId.value } } }),
      loadCards(),
    ])
    if (p.error) throw p.error
    pet.value = p.data ?? null
    state.value = 'ready'
  } catch {
    state.value = 'error'
  }
})

const orgNames = computed(() => {
  const m: Record<string, string> = {}
  for (const l of pet.value?.care_links ?? []) m[l.organization_id] = l.display_name
  for (const e of cards.value) if (e.organization_name) m[e.organization_id] ??= e.organization_name
  return m
})

function linkFor(item: TimelineItem) {
  if (item.kind !== 'encounter' && item.kind !== 'addendum') return null
  return { name: 'pet-encounter', params: { petId: petId.value, encounterId: item.ref_id } }
}
function typeLabel(k: string) {
  const key = `encounters.type.${k}`
  return te(key) ? t(key) : k
}
</script>

<template>
  <section class="mx-auto max-w-3xl space-y-6">
    <RouterLink :to="{ name: 'pet-detail', params: { petId } }" class="text-sm underline">
      ← {{ pet?.name ?? t('nav.myPets') }}
    </RouterLink>
    <h1 class="text-2xl font-bold">{{ t('records.history.title', { name: pet?.name ?? '' }) }}</h1>

    <p v-if="state === 'error'" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>

    <div class="flex gap-2" role="group" :aria-label="t('records.history.view')">
      <button
        v-for="v in ['cards', 'timeline'] as const"
        :key="v"
        type="button"
        class="rounded border px-3 py-1 text-sm"
        :class="
          view === v
            ? 'border-blue-700 bg-blue-700 text-white'
            : 'border-surface-200 dark:border-surface-800'
        "
        :aria-pressed="view === v"
        @click="view = v"
      >
        {{ t(`records.history.views.${v}`) }}
      </button>
    </div>

    <template v-if="view === 'cards'">
      <p
        v-if="state === 'ready' && cards.length === 0"
        class="rounded-lg bg-surface-200/40 p-6 text-center text-sm"
      >
        {{ t('records.history.empty') }}
      </p>
      <ul class="space-y-3" data-testid="history-cards">
        <li v-for="e in cards" :key="e.id">
          <RouterLink
            :to="{ name: 'pet-encounter', params: { petId, encounterId: e.id } }"
            class="card block space-y-1"
            data-testid="history-card"
          >
            <p class="text-xs text-surface-500">
              {{ d(e.signed_at ?? e.started_at, 'short') }} · {{ typeLabel(e.type) }}
              <template v-if="e.organization_name"> · {{ e.organization_name }}</template>
            </p>
            <p class="font-semibold">{{ e.reason }}</p>
            <p v-if="e.attending_vet_name" class="text-sm">{{ e.attending_vet_name }}</p>
            <p v-if="e.diagnoses?.length" class="text-sm">
              {{ t('records.history.diagnoses') }}: {{ e.diagnoses.join(' · ') }}
            </p>
            <p v-if="e.addenda_count" class="text-xs text-surface-500">
              {{ t('records.history.addenda', { n: e.addenda_count }) }}
            </p>
          </RouterLink>
        </li>
      </ul>
      <button v-if="cursor" type="button" class="btn-secondary" @click="loadCards(cursor)">
        {{ t('orgs.patients.more') }}
      </button>
    </template>

    <TimelineList v-else :pet-id="petId" :org-names="orgNames" :link-for="linkFor" />
  </section>
</template>
