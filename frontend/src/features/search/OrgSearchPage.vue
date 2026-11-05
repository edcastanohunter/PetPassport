<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { searchPatients, searchClinical } from './searchApi'
import type {
  ClinicalSearchResultItem,
  FacetBucket,
  PatientSearchResultItem,
  SearchMode,
} from './searchTypes'
import HighlightText from './HighlightText.vue'
import SearchDegradationBanner from './SearchDegradationBanner.vue'
import { ApiError } from '@/shared/api/client'

// Página de búsqueda global clínica (PP-F3-FE-001, FE-011).
// Pestañas Pacientes / Historial, filtros con facetas, agrupación por mascota,
// paginación por cursor, resaltado seguro sin v-html, degradación y frescura.

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

const orgId = computed(() => String(route.params.orgId ?? ''))
const activeTab = ref<'patients' | 'clinical'>('patients')

// Input de búsqueda y estado
const query = ref('')
const loading = ref(false)
const error = ref<string | null>(null)
const retryAfter = ref<number | null>(null)
const searchMode = ref<SearchMode>('full')
const lagSeconds = ref<number | null>(null)

// Filtros
const selectedSpecies = ref('')
const selectedType = ref('')
const selectedScope = ref<'all' | 'mine'>('all')
const confirmedOnly = ref(false)
const dateFrom = ref('')
const dateTo = ref('')

// Resultados
const patientRows = ref<PatientSearchResultItem[]>([])
const clinicalRows = ref<ClinicalSearchResultItem[]>([])
const patientFacets = ref<Record<string, FacetBucket[]>>({})
const clinicalFacets = ref<Record<string, FacetBucket[]>>({})
const nextCursor = ref<string | null>(null)

// Historial de búsquedas recientes
const RECENT_KEY = 'pp_recent_searches_clinic'
const recentSearches = ref<string[]>([])

function loadRecentSearches() {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        recentSearches.value = parsed.slice(0, 5)
      }
    }
  } catch {
    recentSearches.value = []
  }
}

function saveRecentSearch(q: string) {
  const trimmed = q.trim()
  if (!trimmed) return
  const current = recentSearches.value.filter(
    (item) => item.toLowerCase() !== trimmed.toLowerCase(),
  )
  current.unshift(trimmed)
  recentSearches.value = current.slice(0, 5)
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(recentSearches.value))
  } catch {
    // Ignore storage quota
  }
}

async function executeSearch(reset = true) {
  loading.value = true
  error.value = null
  retryAfter.value = null

  if (reset) {
    nextCursor.value = null
    patientRows.value = []
    clinicalRows.value = []
  }

  const cursorParam = reset ? undefined : (nextCursor.value ?? undefined)

  try {
    if (activeTab.value === 'patients') {
      const resp = await searchPatients(orgId.value, {
        q: query.value.trim() || undefined,
        species: selectedSpecies.value || undefined,
        cursor: cursorParam,
        size: 20,
      })
      searchMode.value = resp.mode
      lagSeconds.value = resp.freshness?.lag_seconds ?? null
      patientFacets.value = resp.facets || {}
      nextCursor.value = resp.next_cursor ?? null

      if (reset) {
        patientRows.value = resp.data || []
      } else {
        patientRows.value = [...patientRows.value, ...(resp.data || [])]
      }
    } else {
      const resp = await searchClinical(orgId.value, {
        q: query.value.trim() || undefined,
        species: selectedSpecies.value || undefined,
        type: selectedType.value || undefined,
        from: dateFrom.value ? `${dateFrom.value}T00:00:00Z` : undefined,
        to: dateTo.value ? `${dateTo.value}T23:59:59Z` : undefined,
        confirmed_only: confirmedOnly.value,
        organization_scope: selectedScope.value,
        cursor: cursorParam,
        size: 20,
      })
      searchMode.value = resp.mode
      lagSeconds.value = resp.freshness?.lag_seconds ?? null
      clinicalFacets.value = resp.facets || {}
      nextCursor.value = resp.next_cursor ?? null

      if (reset) {
        clinicalRows.value = resp.data || []
      } else {
        clinicalRows.value = [...clinicalRows.value, ...(resp.data || [])]
      }
    }

    if (query.value.trim()) {
      saveRecentSearch(query.value.trim())
    }
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.status === 429) {
        retryAfter.value = err.retryAfter ?? 30
      }
      error.value = t(`errors.${err.code}`, err.detail || t('errors.GENERIC'))
    } else {
      error.value = t('errors.GENERIC')
    }
  } finally {
    loading.value = false
  }
}

function onSearchSubmit() {
  router.replace({
    query: {
      ...route.query,
      q: query.value.trim() || undefined,
      tab: activeTab.value,
    },
  })
  executeSearch(true)
}

function selectTab(tab: 'patients' | 'clinical') {
  if (activeTab.value === tab) return
  activeTab.value = tab
  router.replace({
    query: {
      ...route.query,
      tab,
    },
  })
  executeSearch(true)
}

function resetFilters() {
  selectedSpecies.value = ''
  selectedType.value = ''
  selectedScope.value = 'all'
  confirmedOnly.value = false
  dateFrom.value = ''
  dateTo.value = ''
  executeSearch(true)
}

function openPatient(petId: string) {
  router.push({
    name: 'org-patient',
    params: { orgId: orgId.value, petId },
  })
}

function openEncounter(encounterId: string) {
  router.push({
    name: 'org-encounter',
    params: { orgId: orgId.value, encounterId },
  })
}

function speciesLabel(s: string) {
  const key = `pets.species.${s}`
  return te(key) ? t(key) : s
}

function encounterTypeLabel(typ: string) {
  const key = `encounters.types.${typ}`
  return te(key) ? t(key) : typ
}

onMounted(() => {
  loadRecentSearches()
  if (typeof route.query.q === 'string') {
    query.value = route.query.q
  }
  if (route.query.tab === 'clinical') {
    activeTab.value = 'clinical'
  }
  executeSearch(true)
})

watch(
  () => route.query.q,
  (newQ) => {
    if (typeof newQ === 'string' && newQ !== query.value) {
      query.value = newQ
      executeSearch(true)
    }
  },
)
</script>

<template>
  <div class="space-y-6" data-testid="org-search-page">
    <!-- Cabecera de búsqueda -->
    <div>
      <h1 class="text-2xl font-bold tracking-tight text-surface-900 dark:text-surface-0">
        {{ t('search.clinicalSearchTitle') }}
      </h1>
      <p class="mt-1 text-sm text-surface-500">
        {{ t('search.clinicalSearchSubtitle') }}
      </p>
    </div>

    <!-- Banner de degradación, frescura y 429 (PP-F3-FE-011) -->
    <SearchDegradationBanner
      :mode="searchMode"
      :lag-seconds="lagSeconds"
      :retry-after="retryAfter"
      @retry="executeSearch(true)"
      @reload="executeSearch(true)"
    />

    <!-- Barra de búsqueda principal -->
    <form class="flex gap-2" @submit.prevent="onSearchSubmit">
      <div class="relative flex-1">
        <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
        <input
          v-model="query"
          type="search"
          :disabled="!!retryAfter && retryAfter > 0"
          :placeholder="t('search.mainInputPlaceholder')"
          class="w-full rounded-lg border border-surface-300 bg-surface-0 py-2.5 pl-10 pr-4 text-sm text-surface-900 placeholder:text-surface-400 focus:border-primary-500 focus:outline-none dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
          data-testid="search-main-input"
        />
      </div>
      <button
        type="submit"
        :disabled="loading || (!!retryAfter && retryAfter > 0)"
        class="rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-semibold text-white shadow-xs hover:bg-primary-700 disabled:opacity-50 dark:bg-primary-500 dark:hover:bg-primary-600"
        data-testid="search-submit-btn"
      >
        <i v-if="loading" class="pi pi-spin pi-spinner mr-1" />
        {{ t('search.searchAction') }}
      </button>
    </form>

    <!-- Selector de pestañas: Pacientes vs Historial clínico -->
    <div class="flex border-b border-surface-200 dark:border-surface-800">
      <button
        type="button"
        :class="[
          'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
          activeTab === 'patients'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300',
        ]"
        data-testid="tab-patients"
        @click="selectTab('patients')"
      >
        <i class="pi pi-users" />
        {{ t('search.tabPatients') }}
        <span
          v-if="patientRows.length > 0"
          class="ml-1 rounded-full bg-surface-200 px-2 py-0.5 text-xs text-surface-700 dark:bg-surface-800 dark:text-surface-300"
        >
          {{ patientRows.length }}
        </span>
      </button>

      <button
        type="button"
        :class="[
          'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
          activeTab === 'clinical'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300',
        ]"
        data-testid="tab-clinical"
        @click="selectTab('clinical')"
      >
        <i class="pi pi-file-edit" />
        {{ t('search.tabClinical') }}
        <span
          v-if="clinicalRows.length > 0"
          class="ml-1 rounded-full bg-surface-200 px-2 py-0.5 text-xs text-surface-700 dark:bg-surface-800 dark:text-surface-300"
        >
          {{ clinicalRows.length }}
        </span>
      </button>
    </div>

    <!-- Contenido en dos columnas: Filtros laterales y Lista de resultados -->
    <div class="grid grid-cols-1 gap-6 md:grid-cols-4">
      <!-- Filtros laterales -->
      <aside class="space-y-4 rounded-xl border border-surface-200 p-4 dark:border-surface-800">
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold text-surface-900 dark:text-surface-0">
            {{ t('search.filtersTitle') }}
          </h2>
          <button
            type="button"
            class="text-xs text-primary-600 hover:underline dark:text-primary-400"
            data-testid="clear-filters-btn"
            @click="resetFilters"
          >
            {{ t('search.clearFilters') }}
          </button>
        </div>

        <!-- Filtro por especie con conteo de facetas -->
        <div class="space-y-1">
          <label class="text-xs font-medium text-surface-600 dark:text-surface-300">
            {{ t('search.filterSpecies') }}
          </label>
          <select
            v-model="selectedSpecies"
            class="w-full rounded-lg border border-surface-300 bg-surface-0 p-2 text-xs text-surface-900 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
            data-testid="filter-species-select"
            @change="executeSearch(true)"
          >
            <option value="">{{ t('search.allSpecies') }}</option>
            <option
              v-for="bucket in activeTab === 'patients'
                ? patientFacets.species
                : clinicalFacets.species"
              :key="bucket.key"
              :value="bucket.key"
            >
              {{ speciesLabel(bucket.key) }} ({{ bucket.count }})
            </option>
          </select>
        </div>

        <!-- Filtros específicos para Historial Clínico -->
        <template v-if="activeTab === 'clinical'">
          <!-- Tipo de encuentro -->
          <div class="space-y-1">
            <label class="text-xs font-medium text-surface-600 dark:text-surface-300">
              {{ t('search.filterEncounterType') }}
            </label>
            <select
              v-model="selectedType"
              class="w-full rounded-lg border border-surface-300 bg-surface-0 p-2 text-xs text-surface-900 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
              data-testid="filter-type-select"
              @change="executeSearch(true)"
            >
              <option value="">{{ t('search.allTypes') }}</option>
              <option
                v-for="bucket in clinicalFacets.encounter_type"
                :key="bucket.key"
                :value="bucket.key"
              >
                {{ encounterTypeLabel(bucket.key) }} ({{ bucket.count }})
              </option>
            </select>
          </div>

          <!-- Ámbito: Solo mi clínica vs Todos -->
          <div class="space-y-1">
            <label class="text-xs font-medium text-surface-600 dark:text-surface-300">
              {{ t('search.filterScope') }}
            </label>
            <div class="flex flex-col gap-1 text-xs">
              <label class="flex items-center gap-2">
                <input
                  v-model="selectedScope"
                  type="radio"
                  value="all"
                  @change="executeSearch(true)"
                />
                {{ t('search.scopeAll') }}
              </label>
              <label class="flex items-center gap-2">
                <input
                  v-model="selectedScope"
                  type="radio"
                  value="mine"
                  @change="executeSearch(true)"
                />
                {{ t('search.scopeMineOnly') }}
              </label>
            </div>
          </div>

          <!-- Solo confirmados -->
          <div class="pt-1">
            <label class="flex items-center gap-2 text-xs text-surface-700 dark:text-surface-300">
              <input
                v-model="confirmedOnly"
                type="checkbox"
                class="rounded border-surface-300 text-primary-600"
                data-testid="filter-confirmed-only"
                @change="executeSearch(true)"
              />
              {{ t('search.filterConfirmedOnly') }}
            </label>
          </div>

          <!-- Rango de fechas -->
          <div class="space-y-2 pt-2">
            <label class="text-xs font-medium text-surface-600 dark:text-surface-300">
              {{ t('search.dateRange') }}
            </label>
            <input
              v-model="dateFrom"
              type="date"
              class="w-full rounded-lg border border-surface-300 bg-surface-0 p-1.5 text-xs text-surface-900 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
              @change="executeSearch(true)"
            />
            <input
              v-model="dateTo"
              type="date"
              class="w-full rounded-lg border border-surface-300 bg-surface-0 p-1.5 text-xs text-surface-900 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
              @change="executeSearch(true)"
            />
          </div>
        </template>
      </aside>

      <!-- Panel principal de resultados -->
      <div class="md:col-span-3">
        <!-- Error -->
        <div
          v-if="error"
          class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
          role="alert"
        >
          {{ error }}
        </div>

        <!-- Cargando inicial -->
        <div
          v-else-if="loading && patientRows.length === 0 && clinicalRows.length === 0"
          class="flex items-center justify-center py-12 text-sm text-surface-400"
        >
          <i class="pi pi-spin pi-spinner mr-2 text-primary-500" />
          {{ t('search.loading') }}
        </div>

        <!-- Pestaña Pacientes -->
        <div v-else-if="activeTab === 'patients'" class="space-y-3">
          <div
            v-if="patientRows.length === 0"
            class="rounded-xl border border-dashed border-surface-300 p-8 text-center dark:border-surface-700"
            data-testid="empty-patients-state"
          >
            <i class="pi pi-search mb-2 text-3xl text-surface-400" />
            <h3 class="font-semibold text-surface-900 dark:text-surface-0">
              {{ t('search.noPatientsFound') }}
            </h3>
            <p class="mt-1 text-xs text-surface-500">
              {{ t('search.emptyStateSuggestions') }}
            </p>
          </div>

          <div
            v-for="item in patientRows"
            :key="item.pet.id"
            class="flex cursor-pointer items-center justify-between rounded-xl border border-surface-200 bg-surface-0 p-4 transition-all hover:border-primary-400 hover:shadow-xs dark:border-surface-800 dark:bg-surface-900"
            data-testid="patient-card"
            @click="openPatient(item.pet.id)"
          >
            <div class="flex items-center gap-4">
              <div
                class="flex h-12 w-12 items-center justify-center rounded-full bg-surface-100 text-lg font-bold text-surface-600 dark:bg-surface-800 dark:text-surface-300"
              >
                <img
                  v-if="item.pet.photo_thumb_url"
                  :src="item.pet.photo_thumb_url"
                  :alt="item.pet.name"
                  width="48"
                  height="48"
                  loading="lazy"
                  decoding="async"
                  class="h-12 w-12 rounded-full object-cover"
                />
                <span v-else>{{ item.pet.name.charAt(0) }}</span>
              </div>
              <div>
                <div class="flex items-center gap-2">
                  <span class="font-semibold text-surface-900 dark:text-surface-0">
                    <HighlightText :text="item.highlight?.name || item.pet.name" />
                  </span>
                  <span class="text-xs text-surface-500">
                    · {{ speciesLabel(item.pet.species) }}
                    <span v-if="item.pet.breed">· {{ item.pet.breed }}</span>
                  </span>
                  <span
                    v-if="item.access_level === 'custody'"
                    class="rounded bg-amber-100 px-2 py-0.5 text-[10px] font-semibold text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                  >
                    {{ t('search.limitedAccessBadge') }}
                  </span>
                </div>
                <div class="mt-1 text-xs text-surface-500">
                  <span v-if="item.guardian_display_name">
                    {{ t('search.guardian') }}:
                    <HighlightText
                      :text="item.highlight?.guardian_display_name || item.guardian_display_name"
                    />
                  </span>
                  <span v-if="item.pet.microchip_suffix">
                    · {{ t('search.chipSuffix') }} ···{{ item.pet.microchip_suffix }}
                  </span>
                </div>
              </div>
            </div>
            <i class="pi pi-chevron-right text-surface-400" />
          </div>
        </div>

        <!-- Pestaña Historial Clínico (agrupado por mascota con fragmentos resaltados) -->
        <div v-else-if="activeTab === 'clinical'" class="space-y-4">
          <div
            v-if="clinicalRows.length === 0"
            class="rounded-xl border border-dashed border-surface-300 p-8 text-center dark:border-surface-700"
            data-testid="empty-clinical-state"
          >
            <i class="pi pi-file mb-2 text-3xl text-surface-400" />
            <h3 class="font-semibold text-surface-900 dark:text-surface-0">
              {{ t('search.noClinicalFound') }}
            </h3>
            <p class="mt-1 text-xs text-surface-500">
              {{ t('search.emptyStateClinicalSuggestions') }}
            </p>
          </div>

          <!-- Grupo por mascota -->
          <div
            v-for="group in clinicalRows"
            :key="group.pet.id"
            class="rounded-xl border border-surface-200 bg-surface-0 p-4 dark:border-surface-800 dark:bg-surface-900"
            data-testid="clinical-group"
          >
            <!-- Cabecera del paciente -->
            <div
              class="flex cursor-pointer items-center justify-between border-b border-surface-100 pb-3 dark:border-surface-800"
              @click="openPatient(group.pet.id)"
            >
              <div class="flex items-center gap-2">
                <span class="font-bold text-surface-900 dark:text-surface-0">
                  {{ group.pet.name }}
                </span>
                <span class="text-xs text-surface-500">
                  ({{ speciesLabel(group.pet.species) }})
                </span>
              </div>
              <span class="text-xs text-primary-600 hover:underline dark:text-primary-400">
                {{ t('search.viewPatientProfile') }} →
              </span>
            </div>

            <!-- Coincidencias en consultas del paciente -->
            <div class="mt-3 space-y-3">
              <div
                v-for="hit in group.hits"
                :key="hit.ref_id"
                class="cursor-pointer rounded-lg bg-surface-50 p-3 transition-colors hover:bg-surface-100 dark:bg-surface-800/50 dark:hover:bg-surface-800"
                data-testid="clinical-hit"
                @click="openEncounter(hit.ref_id)"
              >
                <div class="flex items-center justify-between text-xs text-surface-500">
                  <div class="flex items-center gap-2">
                    <span
                      class="font-semibold uppercase tracking-wider text-surface-700 dark:text-surface-300"
                    >
                      {{ encounterTypeLabel(hit.encounter_type) }}
                    </span>
                    <span v-if="hit.organization?.name">· {{ hit.organization.name }}</span>
                  </div>
                  <span>{{ new Date(hit.occurred_at).toLocaleDateString() }}</span>
                </div>

                <!-- Diagnósticos si hay -->
                <div
                  v-if="hit.diagnoses && hit.diagnoses.length > 0"
                  class="mt-2 flex flex-wrap gap-1.5"
                >
                  <span
                    v-for="(diag, dIdx) in hit.diagnoses"
                    :key="dIdx"
                    class="rounded-full bg-primary-100 px-2 py-0.5 text-xs font-medium text-primary-800 dark:bg-primary-950 dark:text-primary-200"
                  >
                    {{ diag.text }}
                  </span>
                </div>

                <!-- Fragmentos de texto con resaltado seguro -->
                <div
                  v-if="hit.highlight?.text && hit.highlight.text.length > 0"
                  class="mt-2 text-xs leading-relaxed text-surface-700 dark:text-surface-300"
                  data-testid="highlighted-snippet"
                >
                  <p v-for="(snip, sIdx) in hit.highlight.text" :key="sIdx">
                    … <HighlightText :text="snip" /> …
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Botón Cargar más (paginación por cursor) -->
        <div v-if="nextCursor" class="mt-6 text-center">
          <button
            type="button"
            :disabled="loading"
            class="rounded-lg border border-surface-300 bg-surface-0 px-4 py-2 text-xs font-semibold text-surface-700 hover:bg-surface-50 disabled:opacity-50 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-300"
            data-testid="load-more-btn"
            @click="executeSearch(false)"
          >
            <i v-if="loading" class="pi pi-spin pi-spinner mr-1" />
            {{ t('search.loadMore') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
