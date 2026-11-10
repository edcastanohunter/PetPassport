<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    label: string
    used: number
    limit: number
    unit?: string
    softLimitPercent?: number
  }>(),
  {
    unit: '',
    softLimitPercent: 90,
  },
)

const { t } = useI18n()

const percentage = computed(() => {
  if (props.limit <= 0) return 0
  return Math.min(100, Math.round((props.used / props.limit) * 100))
})

const isWarning = computed(() => percentage.value >= props.softLimitPercent)
const isFull = computed(() => props.used >= props.limit)
</script>

<template>
  <div
    class="rounded-lg border border-surface-200 bg-surface-0 p-4 shadow-sm"
    data-testid="usage-indicator"
  >
    <div class="flex items-center justify-between text-sm font-medium">
      <span class="text-surface-700">{{ label }}</span>
      <span
        class="font-semibold"
        :class="isFull ? 'text-red-600' : isWarning ? 'text-amber-600' : 'text-surface-900'"
      >
        {{ used }} / {{ limit }} {{ unit }}
      </span>
    </div>

    <!-- Barra de progreso accesible -->
    <div
      class="mt-2 h-2.5 w-full overflow-hidden rounded-full bg-surface-100"
      role="progressbar"
      :aria-valuenow="used"
      :aria-valuemin="0"
      :aria-valuemax="limit"
      :aria-label="label"
    >
      <div
        class="h-full transition-all duration-300"
        :class="isFull ? 'bg-red-500' : isWarning ? 'bg-amber-500' : 'bg-primary-600'"
        :style="{ width: `${percentage}%` }"
      />
    </div>

    <div
      v-if="isWarning"
      class="mt-2 flex items-center gap-1.5 text-xs text-amber-700 font-medium"
      role="alert"
    >
      <svg class="h-4 w-4 shrink-0 text-amber-500" fill="currentColor" viewBox="0 0 20 20">
        <path
          fill-rule="evenodd"
          d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z"
          clip-rule="evenodd"
        />
      </svg>
      <span>{{ t('billing.softLimitWarning') }}</span>
    </div>
  </div>
</template>
