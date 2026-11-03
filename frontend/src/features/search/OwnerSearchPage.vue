<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { searchMe } from './searchApi'
import type { ClinicalSearchResultItem, SearchMode } from './searchTypes'
import HighlightText from './HighlightText.vue'
import SearchDegradationBanner from './SearchDegradationBanner.vue'
import { ApiError } from '@/shared/api/client'

// Búsqueda del tutor en sus mascotas e historiales clínicos (PP-F3-FE-002, FE-011).
// Atajos sugeridos ("última vacuna de rabia", "medicamentos recetados"),
// enlaces a historial e información de carnet, modo degradado y frescura.

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

const query = ref('')
const loading = ref(false)
const error = ref<string | null>(null)
const retryAfter = ref<number | null>(null)
const searchMode = ref<SearchMode>('full')
const lagSeconds = ref<number | null>(null)

const results = ref<ClinicalSearchResultItem[]>([])
const nextCursor = ref<string | null>(null)

// Atajos sugeridos (FE-002)
const shortcuts = [
  { labelKey: 'search.shortcuts.rabiesVaccine', queryText: 'última vacuna de rabia' },
  { labelKey: 'search.shortcuts.prescriptions', queryText: 'medicamentos recetados' },
  { labelKey: 'search.shortcuts.deworming', queryText: 'desparasitación' },
  { labelKey: 'search.shortcuts.allergies', queryText: 'alergias' },
]

async function executeSearch(reset = true) {
  loading.value = true
  error.value = null
  retryAfter.value = null

  if (reset) {
    nextCursor.value = null
    results.value = []
  }

  const cursorParam = reset ? undefined : (nextCursor.value ?? undefined)

  try {
    const resp = await searchMe({
      q: query.value.trim() || undefined,
      cursor: cursorParam,
      size: 20,
    })

    searchMode.value = resp.mode
    lagSeconds.value = resp.freshness?.lag_seconds ?? null
    nextCursor.value = resp.next_cursor ?? null

    if (reset) {
      results.value = resp.data || []
    } else {
      results.value = [...results.value, ...(resp.data || [])]
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
    },
  })
  executeSearch(true)
}

function applyShortcut(text: string) {
  query.value = text
  onSearchSubmit()
}

function openHistory(petId: string) {
  router.push({
    name: 'pet-history',
    params: { petId },
  })
}

function openCard(petId: string) {
  router.push({
    name: 'pet-card',
    params: { petId },
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
  if (typeof route.query.q === 'string') {
    query.value = route.query.q
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
  <div class="mx-auto max-w-4xl space-y-6" data-testid="owner-search-page">
    <div>
      <h1 class="text-2xl font-bold tracking-tight text-surface-900 dark:text-surface-0">
        {{ t('search.ownerSearchTitle') }}
      </h1>
      <p class="mt-1 text-sm text-surface-500">
        {{ t('search.ownerSearchSubtitle') }}
      </p>
    </div>

    <!-- Banner de degradación y frescura (FE-011) -->
    <SearchDegradationBanner
      :mode="searchMode"
      :lag-seconds="lagSeconds"
      :retry-after="retryAfter"
      @retry="executeSearch(true)"
      @reload="executeSearch(true)"
    />

    <!-- Input de búsqueda del tutor -->
    <form class="flex gap-2" @submit.prevent="onSearchSubmit">
      <div class="relative flex-1">
        <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
        <input
          v-model="query"
          type="search"
          :disabled="!!retryAfter && retryAfter > 0"
          :placeholder="t('search.ownerInputPlaceholder')"
          class="w-full rounded-lg border border-surface-300 bg-surface-0 py-2.5 pl-10 pr-4 text-sm text-surface-900 placeholder:text-surface-400 focus:border-primary-500 focus:outline-none dark:border-surface-700 dark:bg-surface-900 dark:text-surface-0"
          data-testid="owner-search-input"
        />
      </div>
      <button
        type="submit"
        :disabled="loading || (!!retryAfter && retryAfter > 0)"
        class="rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-semibold text-white shadow-xs hover:bg-primary-700 disabled:opacity-50 dark:bg-primary-500 dark:hover:bg-primary-600"
        data-testid="owner-search-submit"
      >
        <i v-if="loading" class="pi pi-spin pi-spinner mr-1" />
        {{ t('search.searchAction') }}
      </button>
    </form>

    <!-- Atajos de consultas sugeridas (PP-F3-FE-002) -->
    <div class="flex flex-wrap items-center gap-2">
      <span class="text-xs text-surface-500">{{ t('search.suggestedQueries') }}:</span>
      <button
        v-for="sc in shortcuts"
        :key="sc.queryText"
        type="button"
        class="rounded-full border border-surface-200 bg-surface-50 px-3 py-1 text-xs text-surface-700 transition-colors hover:border-primary-400 hover:bg-primary-50 hover:text-primary-800 dark:border-surface-700 dark:bg-surface-800 dark:text-surface-300 dark:hover:border-primary-600 dark:hover:bg-primary-950 dark:hover:text-primary-200"
        data-testid="shortcut-btn"
        @click="applyShortcut(sc.queryText)"
      >
        {{ t(sc.labelKey) }}
      </button>
    </div>

    <!-- Resultados -->
    <div class="space-y-4">
      <div
        v-if="error"
        class="rounded-lg bg-red-50 p-4 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        role="alert"
      >
        {{ error }}
      </div>

      <div
        v-else-if="loading && results.length === 0"
        class="flex items-center justify-center py-12 text-sm text-surface-400"
      >
        <i class="pi pi-spin pi-spinner mr-2 text-primary-500" />
        {{ t('search.loading') }}
      </div>

      <div
        v-else-if="results.length === 0"
        class="rounded-xl border border-dashed border-surface-300 p-8 text-center dark:border-surface-700"
        data-testid="owner-empty-state"
      >
        <i class="pi pi-search mb-2 text-3xl text-surface-400" />
        <h3 class="font-semibold text-surface-900 dark:text-surface-0">
          {{ t('search.noOwnerResults') }}
        </h3>
        <p class="mt-1 text-xs text-surface-500">
          {{ t('search.ownerEmptySuggestions') }}
        </p>
      </div>

      <div
        v-for="group in results"
        :key="group.pet.id"
        class="rounded-xl border border-surface-200 bg-surface-0 p-5 dark:border-surface-800 dark:bg-surface-900"
        data-testid="owner-pet-group"
      >
        <!-- Ficha de la mascota con enlaces a historial y carnet -->
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-surface-100 pb-4 dark:border-surface-800">
          <div class="flex items-center gap-3">
            <div
              class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 font-bold text-primary-700 dark:bg-primary-950 dark:text-primary-300"
            >
              {{ group.pet.name.charAt(0) }}
            </div>
            <div>
              <h2 class="font-bold text-surface-900 dark:text-surface-0">
                {{ group.pet.name }}
              </h2>
              <span class="text-xs text-surface-500">
                {{ speciesLabel(group.pet.species) }}
              </span>
            </div>
          </div>
          <div class="flex gap-2 text-xs">
            <button
              type="button"
              class="rounded-lg border border-surface-300 px-3 py-1.5 font-medium text-surface-700 hover:bg-surface-50 dark:border-surface-700 dark:text-surface-300 dark:hover:bg-surface-800"
              data-testid="open-card-btn"
              @click="openCard(group.pet.id)"
            >
              <i class="pi pi-id-card mr-1" />
              {{ t('search.viewCard') }}
            </button>
            <button
              type="button"
              class="rounded-lg bg-primary-600 px-3 py-1.5 font-medium text-white hover:bg-primary-700 dark:bg-primary-500 dark:hover:bg-primary-600"
              data-testid="open-history-btn"
              @click="openHistory(group.pet.id)"
            >
              <i class="pi pi-clock mr-1" />
              {{ t('search.viewHistory') }}
            </button>
          </div>
        </div>

        <!-- Coincidencias en consultas clínicas -->
        <div class="mt-4 space-y-3">
          <div
            v-for="hit in group.hits"
            :key="hit.ref_id"
            class="rounded-lg bg-surface-50 p-3.5 dark:bg-surface-800/40"
            data-testid="owner-hit"
          >
            <div class="flex items-center justify-between text-xs text-surface-500">
              <span class="font-semibold text-surface-800 dark:text-surface-200">
                {{ encounterTypeLabel(hit.encounter_type) }}
                <span v-if="hit.organization?.name">· {{ hit.organization.name }}</span>
              </span>
              <span>{{ new Date(hit.occurred_at).toLocaleDateString() }}</span>
            </div>

            <!-- Diagnósticos si hay -->
            <div v-if="hit.diagnoses && hit.diagnoses.length > 0" class="mt-2 flex flex-wrap gap-1">
              <span
                v-for="(diag, dIdx) in hit.diagnoses"
                :key="dIdx"
                class="rounded-md bg-surface-200 px-2 py-0.5 text-xs text-surface-800 dark:bg-surface-700 dark:text-surface-200"
              >
                {{ diag.text }}
              </span>
            </div>

            <!-- Texto resaltado -->
            <div
              v-if="hit.highlight?.text && hit.highlight.text.length > 0"
              class="mt-2 text-xs leading-relaxed text-surface-700 dark:text-surface-300"
            >
              <p v-for="(snip, sIdx) in hit.highlight.text" :key="sIdx">
                … <HighlightText :text="snip" /> …
              </p>
            </div>
          </div>
        </div>
      </div>

      <!-- Cargar más si hay cursor -->
      <div v-if="nextCursor" class="pt-4 text-center">
        <button
          type="button"
          :disabled="loading"
          class="rounded-lg border border-surface-300 bg-surface-0 px-4 py-2 text-xs font-semibold text-surface-700 hover:bg-surface-50 disabled:opacity-50 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-300"
          @click="executeSearch(false)"
        >
          <i v-if="loading" class="pi pi-spin pi-spinner mr-1" />
          {{ t('search.loadMore') }}
        </button>
      </div>
    </div>
  </div>
</template>
