<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    status: 'trialing' | 'grace' | string
    daysRemaining?: number
    planName?: string
  }>(),
  {
    daysRemaining: 0,
    planName: '',
  },
)

const emit = defineEmits<{
  (e: 'upgrade'): void
}>()

const { t } = useI18n()

const isTrial = computed(() => props.status === 'trialing')
const isGrace = computed(() => props.status === 'grace')
const visible = computed(() => isTrial.value || isGrace.value)
</script>

<template>
  <div
    v-if="visible"
    class="mb-6 rounded-lg p-4 shadow-sm"
    :class="
      isGrace
        ? 'border border-red-200 bg-red-50 text-red-900'
        : 'border border-primary-200 bg-primary-50 text-primary-900'
    "
    role="region"
    :aria-label="isGrace ? t('billing.graceBannerTitle') : t('billing.trialBannerTitle')"
    data-testid="trial-banner"
  >
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
      <div class="space-y-1">
        <h3 class="font-semibold text-sm sm:text-base flex items-center gap-2">
          <span v-if="isGrace" class="inline-block h-2 w-2 rounded-full bg-red-600 animate-pulse" />
          <span v-else class="inline-block h-2 w-2 rounded-full bg-primary-600" />
          {{ isGrace ? t('billing.graceBannerTitle') : t('billing.trialBannerTitle') }}
        </h3>
        <p class="text-xs sm:text-sm text-surface-600">
          {{
            isGrace
              ? t('billing.graceBannerDesc', { days: daysRemaining })
              : t('billing.trialBannerDesc', { days: daysRemaining, plan: planName })
          }}
        </p>
      </div>

      <div class="shrink-0">
        <button
          type="button"
          class="rounded px-4 py-2 text-xs sm:text-sm font-medium text-white shadow-sm transition hover:opacity-95"
          :class="isGrace ? 'bg-red-600 hover:bg-red-700' : 'bg-primary-600 hover:bg-primary-700'"
          @click="emit('upgrade')"
        >
          {{ isGrace ? t('billing.updatePayment') : t('billing.subscribeNow') }}
        </button>
      </div>
    </div>
  </div>
</template>
