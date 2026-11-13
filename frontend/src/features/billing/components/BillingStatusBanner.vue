<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// Banner persistente de impago (PP-F5-PRD-006, FE-003): `grace` avisa con la
// fecha límite; `restricted` explica que la lectura sigue disponible (RN-32) y
// la escritura vuelve al pagar. En la página de facturación ofrece "Reintentar
// cobro"; en el resto del área de la organización enlaza a facturación.
const props = withDefaults(
  defineProps<{
    status: string
    orgId: string
    graceEndsAt?: string
    retry?: boolean
    busy?: boolean
  }>(),
  { graceEndsAt: undefined, retry: false, busy: false },
)
const emit = defineEmits<{ retry: [] }>()
const { t, d } = useI18n()

const visible = computed(() => props.status === 'grace' || props.status === 'restricted')
const restricted = computed(() => props.status === 'restricted')
</script>

<template>
  <div
    v-if="visible"
    class="rounded-lg border p-4"
    :class="
      restricted
        ? 'border-red-300 bg-red-50 text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-100'
        : 'border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100'
    "
    role="alert"
    data-testid="billing-status-banner"
  >
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div class="space-y-1">
        <p class="font-semibold">
          {{ restricted ? t('billing.banner.restrictedTitle') : t('billing.banner.graceTitle') }}
        </p>
        <p class="text-sm">
          <template v-if="restricted">{{ t('billing.banner.restrictedDesc') }}</template>
          <template v-else-if="graceEndsAt">
            {{ t('billing.banner.graceDesc', { date: d(new Date(graceEndsAt), 'short') }) }}
          </template>
          <template v-else>{{ t('billing.banner.graceDescNoDate') }}</template>
        </p>
      </div>
      <button
        v-if="retry"
        type="button"
        class="btn-primary shrink-0"
        :disabled="busy"
        data-testid="retry-charge"
        @click="emit('retry')"
      >
        {{ t('billing.banner.retry') }}
      </button>
      <a
        v-else
        :href="`/org/${orgId}/settings/billing`"
        class="btn-primary shrink-0"
        data-testid="banner-billing-link"
      >
        {{ t('billing.banner.goToBilling') }}
      </a>
    </div>
  </div>
</template>
