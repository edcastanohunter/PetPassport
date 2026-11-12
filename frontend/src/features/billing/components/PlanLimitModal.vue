<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  modelValue: boolean
  resourceName: string
  currentUsage: number
  limit: number
  planName?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'upgrade'): void
}>()

const { t } = useI18n()

function close() {
  emit('update:modelValue', false)
}

function handleUpgrade() {
  close()
  emit('upgrade')
}
</script>

<template>
  <div
    v-if="modelValue"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="plan-limit-title"
    data-testid="plan-limit-modal"
  >
    <div class="w-full max-w-md space-y-4 rounded-lg bg-surface-0 p-6 text-surface-900 shadow-xl">
      <div class="flex items-center gap-3">
        <div
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-100 text-amber-600"
        >
          <svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
            />
          </svg>
        </div>
        <div>
          <h2 id="plan-limit-title" class="text-lg font-semibold">
            {{ t('billing.limitReachedTitle') }}
          </h2>
        </div>
      </div>

      <p class="text-sm text-surface-600">
        {{
          t('billing.limitReachedDesc', {
            resource: resourceName,
            used: currentUsage,
            limit: limit,
          })
        }}
      </p>

      <div class="rounded-md bg-surface-50 p-3 text-sm">
        <div class="flex justify-between font-medium">
          <span class="text-surface-500">{{ resourceName }}:</span>
          <span class="text-red-600">{{ currentUsage }} / {{ limit }}</span>
        </div>
      </div>

      <div class="flex items-center justify-end gap-3 pt-2">
        <button
          type="button"
          class="rounded px-4 py-2 text-sm font-medium text-surface-700 hover:bg-surface-100 transition"
          @click="close"
        >
          {{ t('billing.close') }}
        </button>
        <button
          type="button"
          class="rounded bg-primary-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-primary-700 transition"
          @click="handleUpgrade"
        >
          {{ t('billing.upgradeCTA') }}
        </button>
      </div>
    </div>
  </div>
</template>
