<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  api,
  ApiError,
  isApiError,
  type EncounterSummary,
  type EncounterType,
  type Pet,
  type PetClinicalSummary,
  type TimelineItem,
} from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { useCommand } from '@/shared/composables/useCommand'
import { openEncounter } from '@/features/encounters/encounterApi'
import PatientSummaryPanel from '@/features/records/PatientSummaryPanel.vue'
import TimelineList from '@/features/records/TimelineList.vue'
import WeightPanel from '@/features/records/WeightPanel.vue'

// Ficha de paciente (PP-F2-FE-005, PRD-009; F1 FE-008): pestañas Resumen,
// Historial, Consultas, Carnet, Citas y Peso. Contacto del tutor solo con
// vínculo activo (pet:guardian_contact:read). Si el acceso es solo por
// custodia (vínculo revocado, ADR-010) la ficha F1 responde 404, pero la
// historia sigue con lo propio y un aviso lo explica.
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const { key, renew } = useCommand()

const TABS = ['summary', 'history', 'encounters', 'card', 'appointments', 'weight'] as const
type Tab = (typeof TABS)[number]

const orgId = computed(() => String(route.params.orgId))
const petId = computed(() => String(route.params.petId))
const membership = computed(() => session.membershipFor(orgId.value))
const suspended = computed(() => membership.value?.org_status === 'suspended')
const tab = computed<Tab>(() => {
  const q = String(route.query.tab ?? '')
  return (TABS as readonly string[]).includes(q) ? (q as Tab) : 'summary'
})
const pet = ref<Pet | null>(null)
const summary = shallowRef<PetClinicalSummary | null>(null)
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')
const encounters = ref<EncounterSummary[]>([])
const encCursor = ref<string | undefined>()

const custody = computed(() => summary.value?.access === 'custody')
const canOpen = computed(() => summary.value?.access === 'linked' && !!membership.value)

onMounted(async () => {
  if (suspended.value) return
  try {
    const { data, error } = await api().GET('/organizations/{orgId}/patients/{petId}', {
      params: { path: { orgId: orgId.value, petId: petId.value } },
    })
    if (error) throw error
    pet.value = data ?? null
    state.value = 'ready'
  } catch (e) {
    // Sin vínculo la ficha F1 no responde; con custodia la historia sigue.
    state.value = e instanceof ApiError && e.status === 404 ? 'notFound' : 'error'
  }
  void loadEncounters()
})

async function loadEncounters(cursor?: string) {
  try {
    const { data, error } = await api().GET('/pets/{petId}/encounters', {
      params: { path: { petId: petId.value }, query: cursor ? { cursor } : {} },
    })
    if (error) throw error
    encounters.value = cursor ? [...encounters.value, ...(data?.data ?? [])] : (data?.data ?? [])
    encCursor.value = data?.next_cursor
  } catch {
    encounters.value = cursor ? encounters.value : []
  }
}

const orgNames = computed(() => {
  const m: Record<string, string> = {}
  if (membership.value) m[orgId.value] = membership.value.display_name
  for (const e of encounters.value)
    if (e.organization_name) m[e.organization_id] ??= e.organization_name
  return m
})

const speciesLabel = computed(() => {
  const s = pet.value?.species ?? summary.value?.last_encounter?.pet.species
  if (!s) return ''
  const k = `pets.species.${s}`
  return te(k) ? t(k) : s
})
const petName = computed(() => pet.value?.name ?? summary.value?.last_encounter?.pet.name ?? '')

function selectTab(next: Tab) {
  void router.replace({ query: { ...route.query, tab: next } })
}
function onTabKey(ev: KeyboardEvent, i: number) {
  const delta = ev.key === 'ArrowRight' ? 1 : ev.key === 'ArrowLeft' ? -1 : 0
  if (!delta) return
  ev.preventDefault()
  const next = TABS[(i + delta + TABS.length) % TABS.length]!
  selectTab(next)
  document.getElementById(`tab-${next}`)?.focus()
}

function linkFor(item: TimelineItem) {
  if (item.kind !== 'encounter' && item.kind !== 'addendum') return null
  return { name: 'org-encounter', params: { orgId: orgId.value, encounterId: item.ref_id } }
}

// Abrir consulta (PRD-001): tipo + motivo; solo con vínculo activo.
const opening = ref(false)
const openType = ref<EncounterType>('consultation')
const openReason = ref('')
const openError = ref<string | null>(null)
const TYPES: EncounterType[] = [
  'consultation',
  'follow_up',
  'emergency',
  'vaccination',
  'surgery',
  'procedure',
  'other',
]
async function startEncounter() {
  if (!openReason.value.trim()) return
  openError.value = null
  try {
    const e = await openEncounter(orgId.value, key.value, {
      pet_id: petId.value,
      type: openType.value,
      reason: openReason.value.trim(),
      ...(membership.value?.role === 'veterinarian' && session.me
        ? { attending_vet_id: session.me.id }
        : {}),
    })
    renew()
    await router.push({ name: 'org-encounter', params: { orgId: orgId.value, encounterId: e.id } })
  } catch (e) {
    openError.value = isApiError(e)
      ? t(`errors.${e.code}`, t('errors.GENERIC'))
      : t('errors.GENERIC')
  }
}
function typeLabel(k: string) {
  const key = `encounters.type.${k}`
  return te(key) ? t(key) : k
}
</script>

<template>
  <section class="mx-auto max-w-5xl space-y-6">
    <RouterLink :to="{ name: 'org-patients', params: { orgId } }" class="text-sm underline">
      ← {{ t('orgs.nav.patients') }}
    </RouterLink>

    <p
      v-if="suspended"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ t('errors.ORGANIZATION_SUSPENDED') }}
    </p>
    <p v-else-if="state === 'error'" class="rounded-lg bg-red-50 p-4 text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>

    <template v-if="!suspended">
      <header class="flex flex-wrap items-center gap-4">
        <img
          v-if="pet?.photo"
          :src="pet.photo.url"
          :alt="t('pets.photoAlt', { name: pet.name })"
          class="h-20 w-20 rounded-full object-cover"
        />
        <div class="flex-1">
          <h1 class="text-2xl font-bold">{{ petName || t('records.patient') }}</h1>
          <p class="text-sm text-surface-500">
            {{ speciesLabel }}<template v-if="pet?.breed"> · {{ pet.breed }}</template>
            <template v-if="pet?.status === 'deceased'"> · {{ t('pets.inMemoriam') }}</template>
          </p>
        </div>
        <button
          v-if="canOpen && !opening"
          type="button"
          class="btn-primary"
          data-testid="open-encounter"
          @click="opening = true"
        >
          <span class="pi pi-plus mr-2" aria-hidden="true" />{{ t('encounters.open.button') }}
        </button>
      </header>

      <div
        v-if="custody"
        class="rounded-xl border border-amber-300 bg-amber-50 p-4 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100"
        role="note"
        data-testid="custody-page-banner"
      >
        <span class="pi pi-lock mr-1" aria-hidden="true" />{{ t('records.custodyBanner') }}
      </div>
      <p
        v-else-if="state === 'notFound' && !summary"
        class="rounded-lg bg-surface-200/40 p-6 text-center"
      >
        {{ t('pets.notFound') }}
      </p>

      <form
        v-if="opening"
        class="space-y-3 rounded-xl border border-surface-200 p-4 dark:border-surface-800"
        @submit.prevent="startEncounter"
      >
        <h2 class="font-semibold">{{ t('encounters.open.title') }}</h2>
        <p
          v-if="openError"
          class="rounded bg-red-50 p-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
          role="alert"
        >
          {{ openError }}
        </p>
        <div class="grid gap-3 sm:grid-cols-3">
          <label>
            <span class="label">{{ t('encounters.fields.type') }}</span>
            <select v-model="openType" class="input" @change="renew()">
              <option v-for="ty in TYPES" :key="ty" :value="ty">
                {{ t(`encounters.type.${ty}`) }}
              </option>
            </select>
          </label>
          <label class="sm:col-span-2">
            <span class="label">{{ t('encounters.fields.reason') }}</span>
            <input
              v-model="openReason"
              class="input"
              maxlength="200"
              required
              data-testid="open-reason"
              @input="renew()"
            />
          </label>
        </div>
        <div class="flex gap-2">
          <button
            type="submit"
            class="btn-primary"
            :disabled="!openReason.trim()"
            data-testid="open-submit"
          >
            {{ t('encounters.open.submit') }}
          </button>
          <button type="button" class="btn-secondary" @click="opening = false">
            {{ t('common.cancel') }}
          </button>
        </div>
      </form>

      <div
        role="tablist"
        class="flex flex-wrap gap-1 border-b border-surface-200 dark:border-surface-800"
        :aria-label="t('records.tabs.label')"
      >
        <button
          v-for="(tb, i) in TABS"
          :id="`tab-${tb}`"
          :key="tb"
          type="button"
          role="tab"
          class="-mb-px border-b-2 px-3 py-2 text-sm"
          :class="tab === tb ? 'border-blue-700 font-semibold' : 'border-transparent'"
          :aria-selected="tab === tb"
          :aria-controls="`panel-${tb}`"
          :tabindex="tab === tb ? 0 : -1"
          :data-testid="`tab-${tb}`"
          @click="selectTab(tb)"
          @keydown="onTabKey($event, i)"
        >
          {{ t(`records.tabs.${tb}`) }}
        </button>
      </div>

      <div :id="`panel-${tab}`" role="tabpanel" :aria-labelledby="`tab-${tab}`" tabindex="0">
        <div v-show="tab === 'summary'" class="grid gap-6 md:grid-cols-2">
          <PatientSummaryPanel :pet-id="petId" :show-access="false" @loaded="summary = $event" />
          <div class="space-y-4">
            <dl v-if="pet" class="card grid grid-cols-2 gap-4">
              <div>
                <dt class="text-sm text-surface-500">{{ t('pets.form.sex') }}</dt>
                <dd>{{ t(`pets.sex.${pet.sex}`) }}</dd>
              </div>
              <div>
                <dt class="text-sm text-surface-500">{{ t('pets.form.neutered') }}</dt>
                <dd>{{ t(`pets.neutered.${pet.neutered}`) }}</dd>
              </div>
              <div v-if="pet.birth_date">
                <dt class="text-sm text-surface-500">{{ t('pets.form.birthDate') }}</dt>
                <dd>
                  {{ pet.birth_date.date }}
                  <span v-if="pet.birth_date.approximate">({{ t('pets.approximate') }})</span>
                  <span v-if="pet.age"> · {{ t('pets.ageShort', pet.age) }}</span>
                </dd>
              </div>
              <div>
                <dt class="text-sm text-surface-500">{{ t('pets.microchip') }}</dt>
                <dd class="font-mono">{{ pet.microchip || t('pets.noMicrochip') }}</dd>
              </div>
              <div v-if="pet.markings" class="col-span-2">
                <dt class="text-sm text-surface-500">{{ t('pets.form.markings') }}</dt>
                <dd>{{ pet.markings }}</dd>
              </div>
            </dl>
            <div v-if="pet?.guardian_contact" class="card space-y-1" data-testid="guardian-contact">
              <h2 class="font-semibold">{{ t('orgs.patients.guardianContact') }}</h2>
              <p>{{ pet.guardian_contact.display_name }}</p>
              <p>
                <a :href="`mailto:${pet.guardian_contact.email}`" class="underline">{{
                  pet.guardian_contact.email
                }}</a>
              </p>
              <p v-if="pet.guardian_contact.phone">
                <a :href="`tel:${pet.guardian_contact.phone}`" class="underline">{{
                  pet.guardian_contact.phone
                }}</a>
              </p>
            </div>
          </div>
        </div>

        <TimelineList
          v-if="tab === 'history'"
          :pet-id="petId"
          :org-id="orgId"
          :org-names="orgNames"
          :link-for="linkFor"
        />

        <div v-if="tab === 'encounters'" class="space-y-3">
          <p
            v-if="encounters.length === 0"
            class="rounded-lg bg-surface-200/40 p-6 text-center text-sm"
          >
            {{ t('records.summary.noEncounters') }}
          </p>
          <table v-else class="w-full text-left text-sm" data-testid="pet-encounters">
            <thead>
              <tr class="text-surface-500">
                <th scope="col">{{ t('encounters.list.date') }}</th>
                <th scope="col">{{ t('encounters.fields.type') }}</th>
                <th scope="col">{{ t('encounters.fields.reason') }}</th>
                <th scope="col">{{ t('encounters.attendingVet') }}</th>
                <th scope="col">{{ t('encounters.list.status') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="e in encounters"
                :key="e.id"
                class="border-t border-surface-200 dark:border-surface-800"
              >
                <td :data-label="t('encounters.list.date')">
                  <RouterLink
                    :to="{ name: 'org-encounter', params: { orgId, encounterId: e.id } }"
                    class="underline"
                  >
                    {{ d(e.started_at, 'short') }}
                  </RouterLink>
                </td>
                <td :data-label="t('encounters.fields.type')">{{ typeLabel(e.type) }}</td>
                <td :data-label="t('encounters.fields.reason')">
                  {{ e.reason }}
                  <span
                    v-if="e.organization_id !== orgId"
                    class="ml-1 rounded bg-surface-200 px-2 dark:bg-surface-800"
                  >
                    {{ e.organization_name ?? t('records.timeline.otherClinic') }}
                  </span>
                </td>
                <td :data-label="t('encounters.attendingVet')">
                  {{ e.attending_vet_name ?? '—' }}
                </td>
                <td :data-label="t('encounters.list.status')">
                  {{ t(`encounters.status.${e.status}`) }}
                </td>
              </tr>
            </tbody>
          </table>
          <button
            v-if="encCursor"
            type="button"
            class="btn-secondary"
            @click="loadEncounters(encCursor)"
          >
            {{ t('orgs.patients.more') }}
          </button>
        </div>

        <p
          v-if="tab === 'card'"
          class="rounded-lg border border-dashed border-surface-200 p-6 text-center text-sm text-surface-500 dark:border-surface-800"
        >
          {{ t('records.tabs.cardSoon') }}
        </p>
        <p
          v-if="tab === 'appointments'"
          class="rounded-lg border border-dashed border-surface-200 p-6 text-center text-sm text-surface-500 dark:border-surface-800"
        >
          {{ t('records.tabs.appointmentsSoon') }}
        </p>
        <WeightPanel v-if="tab === 'weight'" :pet-id="petId" />
      </div>
    </template>
  </section>
</template>
