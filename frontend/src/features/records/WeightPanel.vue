<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { api, type WeightHistory } from '@/shared/api/client'
import { useDerivedView } from '@/shared/composables/useDerivedView'
import FreshnessIndicator from '@/shared/ui/FreshnessIndicator.vue'
import WeightChart from './WeightChart.vue'

// Pestaña Peso (PP-F2-PRD-025, FE-005): vista derivada con frescura (FE-014).
// El servidor ya limita los puntos a lo visible (custodia, ADR-010).
const props = defineProps<{ petId: string }>()
const { t } = useI18n()

const view = useDerivedView<WeightHistory>(async () => {
  const res = await api().GET('/pets/{petId}/weight-history', {
    params: { path: { petId: props.petId } },
  })
  if (res.error) throw res.error
  return res
})
void view.reload()
</script>

<template>
  <section class="space-y-3" :aria-label="t('records.weight.title')">
    <FreshnessIndicator
      :lag-seconds="view.lagSeconds.value"
      :loading="view.loading.value"
      @reload="view.reload()"
    />
    <p v-if="view.error.value" class="text-sm text-red-700 dark:text-red-300" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <div :class="{ 'opacity-60': view.loading.value && view.data.value }">
      <WeightChart v-if="view.data.value" :history="view.data.value" />
    </div>
  </section>
</template>
