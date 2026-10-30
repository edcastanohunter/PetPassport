<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, isApiError, type EncounterSummary } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { discardEncounter } from './encounterApi'

// Consultas de la organización (PP-F2-API-001, FE-004): filtros por estado,
// incluida la bandeja de borradores abandonados
// (`/org/:orgId/encounters?filter=abandoned`) con retomar y descartar.
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const FILTERS = ['draft', 'abandoned', 'signed', 'discarded'] as const
type Filter = (typeof FILTERS)[number] | ''

const orgId = computed(() => String(route.params.orgId))
const filter = computed<Filter>(() => {
  const f = String(route.query.filter ?? '')
  return (FILTERS as readonly string[]).includes(f) ? (f as Filter) : ''
})
const role = computed(() => session.membershipFor(orgId.value)?.role)
const items = ref<EncounterSummary[]>([])
const cursor = ref<string | undefined>()
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const discarding = ref<string | null>(null)
const reason = ref('')

async function fetch(c?: string) {
  const { data, error: apiError } = await api().GET('/organizations/{orgId}/encounters', {
    params: {
      path: { orgId: orgId.value },
      query: { ...(filter.value ? { status: filter.value } : {}), ...(c ? { cursor: c } : {}) },
    },
  })
  if (apiError) throw apiError
  return data!
}

async function load() {
  state.value = 'loading'
  try {
    const page = await fetch()
    items.value = page.data
    cursor.value = page.next_cursor
    state.value = 'ready'
  } catch {
    state.value = 'error'
  }
}
async function more() {
  if (!cursor.value) return
  const page = await fetch(cursor.value)
  items.value = [...items.value, ...page.data]
  cursor.value = page.next_cursor
}
watch([orgId, filter], () => void load(), { immediate: true })

function canDiscard(e: EncounterSummary) {
  if (e.status !== 'draft') return false
  return (
    role.value === 'clinic_admin' ||
    (role.value === 'veterinarian' && e.attending_vet_id === session.me?.id)
  )
}

async function discard(e: EncounterSummary) {
  if (!reason.value.trim()) return
  error.value = null
  try {
    await discardEncounter(e.id, reason.value.trim())
    discarding.value = null
    reason.value = ''
    await load()
  } catch (err) {
    error.value = isApiError(err)
      ? t(`errors.${err.code}`, t('errors.GENERIC'))
      : t('errors.GENERIC')
  }
}

function typeLabel(k: string) {
  const key = `encounters.type.${k}`
  return te(key) ? t(key) : k
}
function setFilter(f: Filter) {
  void router.replace({ query: f ? { filter: f } : {} })
}
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-bold">{{ t('encounters.list.title') }}</h1>
    <p class="text-sm text-surface-500">{{ t('encounters.list.hint') }}</p>

    <div class="flex flex-wrap gap-2" role="group" :aria-label="t('encounters.list.filter')">
      <button
        v-for="f in ['', ...FILTERS] as Filter[]"
        :key="f || 'all'"
        type="button"
        class="rounded border px-3 py-1 text-sm"
        :class="
          filter === f
            ? 'border-blue-700 bg-blue-700 text-white'
            : 'border-surface-200 dark:border-surface-800'
        "
        :aria-pressed="filter === f"
        :data-testid="`filter-${f || 'all'}`"
        @click="setFilter(f)"
      >
        {{ t(`encounters.list.filters.${f || 'all'}`) }}
      </button>
    </div>

    <p
      v-if="error"
      class="rounded bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ error }}
    </p>
    <p v-if="state === 'error'" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <p
      v-else-if="state === 'ready' && items.length === 0"
      class="rounded-lg bg-surface-200/40 p-6 text-center text-sm"
    >
      {{ t('encounters.list.empty') }}
    </p>

    <table
      v-if="items.length"
      class="w-full text-left text-sm"
      :class="{ 'opacity-60': state === 'loading' }"
    >
      <thead>
        <tr class="text-surface-500">
          <th scope="col">{{ t('encounters.list.date') }}</th>
          <th scope="col">{{ t('encounters.list.patient') }}</th>
          <th scope="col">{{ t('encounters.fields.type') }}</th>
          <th scope="col">{{ t('encounters.fields.reason') }}</th>
          <th scope="col">{{ t('encounters.attendingVet') }}</th>
          <th scope="col">{{ t('encounters.list.status') }}</th>
          <th scope="col">
            <span class="sr-only">{{ t('encounters.list.actions') }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="e in items"
          :key="e.id"
          class="border-t border-surface-200 dark:border-surface-800"
          data-testid="encounter-row"
        >
          <td :data-label="t('encounters.list.date')">{{ d(e.started_at, 'long') }}</td>
          <td :data-label="t('encounters.list.patient')">{{ e.pet.name }}</td>
          <td :data-label="t('encounters.fields.type')">{{ typeLabel(e.type) }}</td>
          <td :data-label="t('encounters.fields.reason')">{{ e.reason }}</td>
          <td :data-label="t('encounters.attendingVet')">{{ e.attending_vet_name ?? '—' }}</td>
          <td :data-label="t('encounters.list.status')">
            {{ t(`encounters.status.${e.status}`) }}
            <span
              v-if="e.abandoned"
              class="ml-1 rounded bg-amber-100 px-2 text-amber-900 dark:bg-amber-900/40 dark:text-amber-100"
            >
              {{ t('encounters.abandoned') }}
            </span>
          </td>
          <td class="space-x-2 whitespace-nowrap">
            <RouterLink
              :to="{ name: 'org-encounter', params: { orgId, encounterId: e.id } }"
              class="underline"
            >
              {{ e.status === 'draft' ? t('encounters.list.resume') : t('encounters.list.view') }}
            </RouterLink>
            <button
              v-if="canDiscard(e)"
              type="button"
              class="underline"
              @click="discarding = discarding === e.id ? null : e.id"
            >
              {{ t('encounters.discard.button') }}
            </button>
            <div v-if="discarding === e.id" class="mt-2 flex flex-wrap items-end gap-2">
              <label>
                <span class="label">{{ t('encounters.discard.reason') }}</span>
                <input v-model="reason" class="input" maxlength="500" />
              </label>
              <button
                type="button"
                class="btn-primary"
                :disabled="!reason.trim()"
                @click="discard(e)"
              >
                {{ t('encounters.discard.confirm') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <button v-if="cursor" type="button" class="btn-secondary" @click="more">
      {{ t('orgs.patients.more') }}
    </button>
  </section>
</template>
