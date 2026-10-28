<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { FRESHNESS_VISIBLE_FROM_SECONDS } from '@/shared/api/freshness'

// Indicador de frescura de vistas derivadas (PP-F2-FE-014): oculto si el
// retraso de la proyección es < 3 s; si no, "Actualizando… (hace N s)" con
// botón Recargar. Se usa junto a useDerivedView.
const props = defineProps<{ lagSeconds: number | null; loading?: boolean }>()
const emit = defineEmits<{ reload: [] }>()
const { t } = useI18n()

const visible = computed(
  () => props.lagSeconds !== null && props.lagSeconds >= FRESHNESS_VISIBLE_FROM_SECONDS,
)
const seconds = computed(() => Math.round(props.lagSeconds ?? 0))
</script>

<template>
  <div
    v-if="visible"
    class="flex flex-wrap items-center gap-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-900/40 dark:text-amber-100"
    role="status"
    aria-live="polite"
    data-testid="freshness"
  >
    <span>{{ t('freshness.updating', { seconds }) }}</span>
    <button
      type="button"
      class="underline"
      :disabled="loading"
      data-testid="freshness-reload"
      @click="emit('reload')"
    >
      {{ t('freshness.reload') }}
    </button>
  </div>
</template>
