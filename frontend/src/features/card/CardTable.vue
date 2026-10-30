<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Card, CardEntry } from '@/shared/api/client'

// Lista de entradas del carnet y próximas dosis (FE-006/010/013): se usa en la
// ficha de la clínica, la vista del tutor y el enlace público. Las acciones
// por fila las aporta el padre en el slot `actions`.
defineProps<{ card: Card }>()
const { t, d } = useI18n()

function badge(e: CardEntry) {
  if (e.status === 'voided') return 'voided'
  return e.verification
}
</script>

<template>
  <div class="space-y-4">
    <section v-if="card.next_due.length" :aria-label="t('card.nextDue.title')">
      <h2 class="text-lg font-semibold">{{ t('card.nextDue.title') }}</h2>
      <ul class="mt-2 space-y-1" data-testid="card-next-due">
        <li
          v-for="n in card.next_due"
          :key="n.entry_id"
          class="flex justify-between rounded border px-3 py-2 text-sm"
          :class="n.status === 'overdue' ? 'border-red-300 bg-red-50 dark:bg-red-950' : ''"
        >
          <span>{{ n.product_name }}</span>
          <span>
            {{ d(n.due_date, 'short') }}
            <span class="text-xs text-surface-500"
              >· {{ t(`card.nextDue.status.${n.status}`) }}</span
            >
          </span>
        </li>
      </ul>
    </section>

    <p
      v-if="card.entries.length === 0"
      class="rounded-lg bg-surface-200/40 p-6 text-center text-sm"
    >
      {{ t('card.empty') }}
    </p>
    <ul v-else class="space-y-2" data-testid="card-entries">
      <li
        v-for="e in card.entries"
        :key="e.id"
        class="card space-y-1"
        :class="e.status === 'voided' ? 'opacity-60' : ''"
        data-testid="card-entry"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p class="font-semibold" :class="e.status === 'voided' ? 'line-through' : ''">
            {{ e.product.name }}
            <span class="text-xs font-normal text-surface-500"
              >· {{ t(`card.kind.${e.kind}`) }}</span
            >
          </p>
          <span
            class="rounded-full px-2 py-0.5 text-xs"
            :class="{
              'bg-emerald-100 text-emerald-800': badge(e) === 'verified',
              'bg-amber-100 text-amber-800': badge(e) === 'unverified',
              'bg-red-100 text-red-800': badge(e) === 'disputed',
              'bg-surface-200 text-surface-700': badge(e) === 'voided',
            }"
          >
            {{ t(`card.badge.${badge(e)}`) }}
          </span>
        </div>
        <p class="text-sm">
          {{ d(e.administered_at, 'short') }}
          <template v-if="e.lot"> · {{ t('card.lot') }} {{ e.lot }}</template>
          <template v-if="e.dose_label"> · {{ e.dose_label }}</template>
          <template v-if="e.next_due">
            · {{ t('card.nextDue.next') }} {{ d(e.next_due.date, 'short') }}
          </template>
        </p>
        <p class="text-xs text-surface-500">{{ t(`card.source.${e.source}`) }}</p>
        <p v-if="e.void_reason" class="text-xs">{{ e.void_reason }}</p>
        <slot name="actions" :entry="e" />
      </li>
    </ul>
  </div>
</template>
