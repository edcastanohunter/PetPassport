<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SearchMode } from './searchTypes'

// Banner de degradación y frescura en búsqueda (PP-F3-FE-011):
// - Modo degraded: "Búsqueda limitada temporalmente: solo nombre y microchip" + reintento automático cada 30s.
// - Frescura con lag > 10s: "Los resultados pueden no incluir cambios de los últimos N s".
// - Error 429: mensaje con cuenta atrás de Retry-After.

const props = defineProps<{
  mode?: SearchMode
  lagSeconds?: number | null
  retryAfter?: number | null
}>()

const emit = defineEmits<{
  retry: []
  reload: []
}>()

const { t } = useI18n()

// Temporizador de reintento automático en modo degraded (30 s)
const degradedCountdown = ref(30)
let degradedTimer: ReturnType<typeof setInterval> | null = null

function startDegradedTimer() {
  stopDegradedTimer()
  degradedCountdown.value = 30
  degradedTimer = setInterval(() => {
    if (degradedCountdown.value > 1) {
      degradedCountdown.value -= 1
    } else {
      degradedCountdown.value = 30
      emit('retry')
    }
  }, 1000)
}

function stopDegradedTimer() {
  if (degradedTimer) {
    clearInterval(degradedTimer)
    degradedTimer = null
  }
}

watch(
  () => props.mode,
  (newMode) => {
    if (newMode === 'degraded') {
      startDegradedTimer()
    } else {
      stopDegradedTimer()
    }
  },
  { immediate: true },
)

// Temporizador para cuenta atrás de 429 (Retry-After)
const rateLimitRemaining = ref<number | null>(null)
let rateLimitTimer: ReturnType<typeof setInterval> | null = null

watch(
  () => props.retryAfter,
  (val) => {
    if (val && val > 0) {
      rateLimitRemaining.value = val
      if (rateLimitTimer) clearInterval(rateLimitTimer)
      rateLimitTimer = setInterval(() => {
        if (rateLimitRemaining.value && rateLimitRemaining.value > 1) {
          rateLimitRemaining.value -= 1
        } else {
          rateLimitRemaining.value = null
          if (rateLimitTimer) clearInterval(rateLimitTimer)
        }
      }, 1000)
    } else {
      rateLimitRemaining.value = null
      if (rateLimitTimer) clearInterval(rateLimitTimer)
    }
  },
  { immediate: true },
)

onUnmounted(() => {
  stopDegradedTimer()
  if (rateLimitTimer) clearInterval(rateLimitTimer)
})

const showDegraded = computed(() => props.mode === 'degraded')
const showLag = computed(
  () => props.lagSeconds !== null && props.lagSeconds !== undefined && props.lagSeconds > 10,
)
const roundedLag = computed(() => Math.round(props.lagSeconds ?? 0))
</script>

<template>
  <div class="space-y-2">
    <!-- Aviso de límite de tasa 429 -->
    <div
      v-if="rateLimitRemaining !== null && rateLimitRemaining > 0"
      class="flex items-center justify-between rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
      role="alert"
      data-testid="search-ratelimit-banner"
    >
      <div class="flex items-center gap-2">
        <i class="pi pi-exclamation-circle text-red-600 dark:text-red-400" />
        <span>{{ t('search.rateLimited', { seconds: rateLimitRemaining }) }}</span>
      </div>
    </div>

    <!-- Banner de modo degradado (PostgreSQL fallback) -->
    <div
      v-if="showDegraded"
      class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-100"
      role="status"
      data-testid="search-degraded-banner"
    >
      <div class="flex items-center gap-2">
        <i class="pi pi-exclamation-triangle text-amber-600 dark:text-amber-400" />
        <span>
          {{ t('search.degradedNotice') }}
          <span class="text-xs text-amber-700 dark:text-amber-300">
            ({{ t('search.degradedRetryIn', { seconds: degradedCountdown }) }})
          </span>
        </span>
      </div>
      <button
        type="button"
        class="rounded bg-amber-200 px-2.5 py-1 text-xs font-semibold hover:bg-amber-300 dark:bg-amber-800 dark:hover:bg-amber-700"
        data-testid="search-retry-btn"
        @click="emit('retry')"
      >
        {{ t('search.retryNow') }}
      </button>
    </div>

    <!-- Banner de frescura (lag > 10s) -->
    <div
      v-if="showLag"
      class="flex items-center justify-between rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100"
      role="status"
      data-testid="search-lag-banner"
    >
      <div class="flex items-center gap-2">
        <i class="pi pi-info-circle text-blue-600 dark:text-blue-400" />
        <span>{{ t('search.freshnessLagNotice', { seconds: roundedLag }) }}</span>
      </div>
      <button
        type="button"
        class="underline hover:no-underline"
        data-testid="search-reload-freshness-btn"
        @click="emit('reload')"
      >
        {{ t('search.reload') }}
      </button>
    </div>
  </div>
</template>
