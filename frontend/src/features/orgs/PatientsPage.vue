<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type PatientRow } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import CareLinkRequestPanel from './CareLinkRequestPanel.vue'
import { useOrgStore } from './orgStore'

// Pacientes de la clínica (FE-008, PRD-016): búsqueda simple (q), paginación
// por cursor, columnas foto/mascota/especie-raza/edad/tutor (tel:)/vinculado
// desde; estado vacío con el código público. Org suspendida → aviso y no se
// consulta la API.
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const store = useOrgStore()

const orgId = computed(() => String(route.params.orgId))
const suspended = computed(() => session.membershipFor(orgId.value)?.org_status === 'suspended')
const rows = ref<PatientRow[]>([])
const nextCursor = ref<string | null>(null)
const q = ref('')
const loading = ref(false)
const error = ref<string | null>(null)

async function fetchPage(reset: boolean) {
  if (suspended.value) return
  loading.value = true
  error.value = null
  try {
    const { data, error: apiError } = await api().GET('/organizations/{orgId}/patients', {
      params: {
        path: { orgId: orgId.value },
        query: {
          q: q.value.trim() || undefined,
          cursor: reset ? undefined : (nextCursor.value ?? undefined),
          limit: 20,
        },
      },
    })
    if (apiError) throw apiError
    rows.value = reset ? (data?.data ?? []) : [...rows.value, ...(data?.data ?? [])]
    nextCursor.value = data?.next_cursor ?? null
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  if (!suspended.value && (!store.current || store.current.id !== orgId.value)) {
    await store.load(orgId.value).catch(() => undefined)
  }
  await fetchPage(true)
})

function speciesLabel(s: string) {
  const key = `pets.species.${s}`
  return te(key) ? t(key) : s
}

function age(row: PatientRow) {
  const bd = row.pet.birth_date
  if (!bd) return null
  const b = new Date(`${bd.date}T00:00:00Z`)
  const now = new Date()
  let months =
    (now.getUTCFullYear() - b.getUTCFullYear()) * 12 + (now.getUTCMonth() - b.getUTCMonth())
  if (now.getUTCDate() < b.getUTCDate()) months--
  months = Math.max(months, 0)
  return t('pets.ageShort', { years: Math.floor(months / 12), months: months % 12 })
}

function open(row: PatientRow) {
  router.push({ name: 'org-patient', params: { orgId: orgId.value, petId: row.pet.id } })
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('orgs.nav.patients') }}</h1>

    <p
      v-if="suspended"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
      role="alert"
      data-testid="org-suspended"
    >
      {{ t('errors.ORGANIZATION_SUSPENDED') }}
    </p>

    <template v-else>
      <form class="flex gap-2" role="search" @submit.prevent="fetchPage(true)">
        <label class="sr-only" for="patients-q">{{ t('orgs.patients.search') }}</label>
        <input
          id="patients-q"
          v-model="q"
          type="search"
          maxlength="80"
          class="input"
          :placeholder="t('orgs.patients.searchPlaceholder')"
        />
        <button type="submit" class="btn-secondary">{{ t('orgs.patients.search') }}</button>
      </form>

      <p
        v-if="error"
        class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
        role="alert"
      >
        {{ error }}
      </p>

      <div
        v-if="!loading && !rows.length && !error"
        class="rounded-xl border border-dashed border-surface-200 p-8 text-center text-surface-500 dark:border-surface-800"
        data-testid="patients-empty"
      >
        <template v-if="q">{{ t('orgs.patients.noResults') }}</template>
        <template v-else>
          {{ t('orgs.patients.empty', { code: store.current?.public_code ?? '' }) }}
        </template>
      </div>

      <table v-if="rows.length" class="hidden w-full text-left text-sm md:table">
        <thead class="text-surface-500">
          <tr>
            <th class="py-2">
              <span class="sr-only">{{ t('orgs.patients.photo') }}</span>
            </th>
            <th>{{ t('orgs.patients.pet') }}</th>
            <th>{{ t('orgs.patients.speciesBreed') }}</th>
            <th>{{ t('orgs.patients.age') }}</th>
            <th>{{ t('orgs.patients.guardian') }}</th>
            <th>{{ t('orgs.patients.linkedAt') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.pet.id"
            class="cursor-pointer border-t border-surface-200 hover:bg-surface-200/30 dark:border-surface-800"
            data-testid="patient-row"
            @click="open(row)"
          >
            <td class="py-2">
              <img
                v-if="row.pet.photo"
                :src="row.pet.photo.thumb_url"
                :alt="t('pets.photoAlt', { name: row.pet.name })"
                class="h-10 w-10 rounded-full object-cover"
              />
              <span
                v-else
                class="flex h-10 w-10 items-center justify-center rounded-full bg-surface-200/60 font-bold"
                aria-hidden="true"
              >
                {{ row.pet.name.charAt(0).toUpperCase() }}
              </span>
            </td>
            <td>
              <RouterLink
                :to="{ name: 'org-patient', params: { orgId, petId: row.pet.id } }"
                class="font-medium underline"
                @click.stop
              >
                {{ row.pet.name }}
              </RouterLink>
              <span v-if="row.pet.status === 'deceased'" class="ml-1 text-xs text-surface-500"
                >({{ t('pets.inMemoriam') }})</span
              >
            </td>
            <td>
              {{ speciesLabel(row.pet.species)
              }}<template v-if="row.pet.breed"> · {{ row.pet.breed }}</template>
            </td>
            <td>{{ age(row) ?? '—' }}</td>
            <td>
              {{ row.guardian.display_name }}
              <a
                v-if="row.guardian.phone"
                :href="`tel:${row.guardian.phone}`"
                class="block text-xs underline"
                @click.stop
              >
                {{ row.guardian.phone }}
              </a>
            </td>
            <td>{{ d(new Date(row.linked_at), 'short') }}</td>
          </tr>
        </tbody>
      </table>

      <ul v-if="rows.length" class="space-y-3 md:hidden">
        <li v-for="row in rows" :key="row.pet.id">
          <button type="button" class="card w-full" @click="open(row)">
            <span class="block font-semibold">{{ row.pet.name }}</span>
            <span class="block text-sm text-surface-500">
              {{ speciesLabel(row.pet.species) }} · {{ row.guardian.display_name }}
            </span>
          </button>
        </li>
      </ul>

      <button
        v-if="nextCursor"
        type="button"
        class="btn-secondary"
        :disabled="loading"
        data-testid="patients-more"
        @click="fetchPage(false)"
      >
        {{ t('orgs.patients.more') }}
      </button>
      <p v-if="loading" class="text-surface-500">{{ t('common.loading') }}</p>

      <CareLinkRequestPanel :org-id="orgId" />
    </template>
  </section>
</template>
