<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { suggestPatients } from './searchApi'
import type { SuggestPatientItem } from './searchTypes'
import { ApiError } from '@/shared/api/client'

// Modal de búsqueda global en cabecera clínica (PP-F3-FE-001).
// Atajo Ctrl+K / Cmd+K, debounce 150ms, cancelación con AbortController,
// máx 8 resultados con badge "acceso limitado", historial de búsquedas recientes.

const props = defineProps<{
  orgId: string
}>()

const { t, te } = useI18n()
const router = useRouter()

const isOpen = ref(false)
const query = ref('')
const results = ref<SuggestPatientItem[]>([])
const loading = ref(false)
const selectedIndex = ref(-1)
const retryAfter = ref<number | null>(null)
const inputRef = ref<HTMLInputElement | null>(null)

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
    // Ignore storage quota errors
  }
}

function openModal() {
  isOpen.value = true
  loadRecentSearches()
  nextTick(() => {
    inputRef.value?.focus()
  })
}

function closeModal() {
  isOpen.value = false
  query.value = ''
  results.value = []
  selectedIndex.value = -1
  if (abortController) {
    abortController.abort()
    abortController = null
  }
}

// Global shortcut listener (Ctrl+K or Cmd+K)
function handleGlobalKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault()
    if (isOpen.value) {
      closeModal()
    } else {
      openModal()
    }
  } else if (e.key === 'Escape' && isOpen.value) {
    e.preventDefault()
    closeModal()
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKeydown)
  loadRecentSearches()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  if (abortController) {
    abortController.abort()
  }
  if (debounceTimer) {
    clearTimeout(debounceTimer)
  }
})

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let abortController: AbortController | null = null

watch(query, (val) => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
  }
  selectedIndex.value = -1

  const trimmed = val.trim()
  if (!trimmed) {
    results.value = []
    loading.value = false
    if (abortController) {
      abortController.abort()
      abortController = null
    }
    return
  }

  debounceTimer = setTimeout(async () => {
    if (abortController) {
      abortController.abort()
    }
    abortController = new AbortController()
    loading.value = true
    retryAfter.value = null

    try {
      const resp = await suggestPatients(props.orgId, trimmed, abortController.signal)
      results.value = (resp.data || []).slice(0, 8)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      if (err instanceof ApiError && err.status === 429) {
        retryAfter.value = err.retryAfter ?? 30
      }
      results.value = []
    } finally {
      loading.value = false
    }
  }, 150)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (results.value.length > 0) {
      selectedIndex.value = (selectedIndex.value + 1) % results.value.length
    }
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (results.value.length > 0) {
      selectedIndex.value = (selectedIndex.value - 1 + results.value.length) % results.value.length
    }
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (selectedIndex.value >= 0 && selectedIndex.value < results.value.length) {
      selectItem(results.value[selectedIndex.value])
    } else if (query.value.trim()) {
      goToFullSearch(query.value.trim())
    }
  }
}

function selectItem(item: SuggestPatientItem) {
  saveRecentSearch(item.name)
  closeModal()
  router.push({
    name: 'org-patient',
    params: { orgId: props.orgId, petId: item.pet_id },
  })
}

function goToFullSearch(q: string) {
  saveRecentSearch(q)
  closeModal()
  router.push({
    name: 'org-search',
    params: { orgId: props.orgId },
    query: { q },
  })
}

function selectRecent(q: string) {
  query.value = q
}

function speciesLabel(s: string) {
  const key = `pets.species.${s}`
  return te(key) ? t(key) : s
}

defineExpose({
  openModal,
  closeModal,
  isOpen,
})
</script>

<template>
  <div>
    <!-- Botón disparador visible en la cabecera -->
    <button
      type="button"
      class="flex items-center gap-2 rounded-lg border border-surface-300 bg-surface-50 px-3 py-1.5 text-xs text-surface-600 transition-colors hover:border-surface-400 hover:text-surface-900 dark:border-surface-700 dark:bg-surface-900 dark:text-surface-400 dark:hover:border-surface-600 dark:hover:text-surface-100"
      data-testid="quick-search-trigger"
      @click="openModal"
    >
      <i class="pi pi-search text-xs" />
      <span>{{ t('search.quickSearchPlaceholder') }}</span>
      <kbd
        class="hidden rounded bg-surface-200 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-surface-700 sm:inline-block dark:bg-surface-800 dark:text-surface-300"
      >
        Ctrl+K
      </kbd>
    </button>

    <!-- Modal Backdrop -->
    <div
      v-if="isOpen"
      class="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-4 pt-16 backdrop-blur-xs"
      data-testid="quick-search-backdrop"
      @click.self="closeModal"
    >
      <!-- Modal Container -->
      <div
        class="w-full max-w-xl overflow-hidden rounded-xl border border-surface-200 bg-white shadow-2xl dark:border-surface-800 dark:bg-surface-900"
        role="dialog"
        aria-modal="true"
        aria-labelledby="quick-search-title"
      >
        <!-- Barra de búsqueda principal -->
        <div
          class="flex items-center border-b border-surface-200 px-4 py-3 dark:border-surface-800"
        >
          <i class="pi pi-search mr-3 text-surface-400" />
          <input
            ref="inputRef"
            v-model="query"
            type="text"
            :disabled="!!retryAfter && retryAfter > 0"
            :placeholder="t('search.quickSearchInputPlaceholder')"
            class="w-full bg-transparent text-sm text-surface-900 outline-none placeholder:text-surface-400 dark:text-surface-0"
            data-testid="quick-search-input"
            @keydown="onKeydown"
          />
          <button
            v-if="query"
            type="button"
            class="text-surface-400 hover:text-surface-600 dark:hover:text-surface-200"
            aria-label="Limpiar búsqueda"
            @click="query = ''"
          >
            <i class="pi pi-times text-xs" />
          </button>
          <kbd
            class="ml-2 rounded border border-surface-300 px-1.5 py-0.5 text-[10px] text-surface-500 dark:border-surface-700"
            @click="closeModal"
          >
            ESC
          </kbd>
        </div>

        <!-- Banner de 429 si aplica -->
        <div
          v-if="retryAfter"
          class="border-b border-red-100 bg-red-50 p-3 text-xs text-red-800 dark:border-red-950 dark:bg-red-950 dark:text-red-200"
        >
          <i class="pi pi-exclamation-circle mr-1" />
          {{ t('search.rateLimited', { seconds: retryAfter }) }}
        </div>

        <!-- Resultados del autocompletado -->
        <div class="max-h-96 overflow-y-auto p-2" data-testid="quick-search-results">
          <!-- Spinner si está cargando -->
          <div
            v-if="loading && results.length === 0"
            class="flex items-center justify-center py-8 text-sm text-surface-400"
          >
            <i class="pi pi-spin pi-spinner mr-2 text-primary-500" />
            <span>{{ t('search.loading') }}</span>
          </div>

          <!-- Lista de sugerencias (máx 8) -->
          <ul v-else-if="results.length > 0" class="space-y-1" role="listbox">
            <li
              v-for="(item, idx) in results"
              :key="item.pet_id"
              role="option"
              :aria-selected="selectedIndex === idx"
              :class="[
                'flex cursor-pointer items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors',
                selectedIndex === idx
                  ? 'bg-primary-50 text-primary-900 dark:bg-primary-950 dark:text-primary-100'
                  : 'text-surface-700 hover:bg-surface-100 dark:text-surface-200 dark:hover:bg-surface-800',
              ]"
              data-testid="suggest-item"
              @click="selectItem(item)"
              @mouseenter="selectedIndex = idx"
            >
              <div class="flex items-center gap-3">
                <div
                  class="flex h-8 w-8 items-center justify-center rounded-full bg-surface-200 text-xs font-semibold text-surface-700 dark:bg-surface-800 dark:text-surface-300"
                >
                  <i v-if="item.species === 'dog'" class="pi pi-heart" />
                  <i v-else-if="item.species === 'cat'" class="pi pi-star" />
                  <i v-else class="pi pi-tag" />
                </div>
                <div>
                  <div class="flex items-center gap-2">
                    <span class="font-semibold">{{ item.name }}</span>
                    <span class="text-xs text-surface-500">({{ speciesLabel(item.species) }})</span>
                    <span
                      v-if="item.access_level === 'custody'"
                      class="rounded bg-amber-100 px-1.5 py-0.2 text-[10px] font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
                      data-testid="limited-access-badge"
                    >
                      {{ t('search.limitedAccessBadge') }}
                    </span>
                  </div>
                  <div class="text-xs text-surface-500">
                    <span v-if="item.guardian_display_name"
                      >{{ t('search.guardian') }}: {{ item.guardian_display_name }} ·
                    </span>
                    <span v-if="item.microchip_suffix">
                      {{ t('search.chipSuffix') }} ···{{ item.microchip_suffix }}
                    </span>
                  </div>
                </div>
              </div>
              <i class="pi pi-chevron-right text-xs text-surface-400" />
            </li>
          </ul>

          <!-- Opción de presionar Enter para ver todos los resultados -->
          <div
            v-if="query.trim() && !loading"
            class="mt-2 border-t border-surface-100 pt-2 text-center dark:border-surface-800"
          >
            <button
              type="button"
              class="w-full rounded-lg px-3 py-2 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-950"
              data-testid="view-all-results-btn"
              @click="goToFullSearch(query.trim())"
            >
              {{ t('search.pressEnterToViewAll', { query: query.trim() }) }}
            </button>
          </div>

          <!-- Búsquedas recientes si no hay query -->
          <div v-if="!query.trim() && recentSearches.length > 0" class="py-2">
            <p class="px-3 text-xs font-semibold text-surface-400">
              {{ t('search.recentSearches') }}
            </p>
            <ul class="mt-1 space-y-0.5">
              <li
                v-for="recent in recentSearches"
                :key="recent"
                class="flex cursor-pointer items-center justify-between rounded-lg px-3 py-1.5 text-xs text-surface-600 hover:bg-surface-100 dark:text-surface-300 dark:hover:bg-surface-800"
                data-testid="recent-search-item"
                @click="selectRecent(recent)"
              >
                <div class="flex items-center gap-2">
                  <i class="pi pi-history text-surface-400" />
                  <span>{{ recent }}</span>
                </div>
                <i class="pi pi-arrow-up-left text-[10px] text-surface-400" />
              </li>
            </ul>
          </div>

          <!-- Estado vacío cuando no hay resultados para la query -->
          <div
            v-if="!loading && query.trim() && results.length === 0"
            class="py-8 text-center text-sm text-surface-500"
            data-testid="no-quick-results"
          >
            <p>{{ t('search.noMatchesFor', { query: query.trim() }) }}</p>
            <button
              type="button"
              class="mt-2 text-xs text-primary-600 underline hover:no-underline dark:text-primary-400"
              @click="goToFullSearch(query.trim())"
            >
              {{ t('search.searchFullHistory') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
