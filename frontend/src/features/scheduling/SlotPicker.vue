<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AppointmentType, AvailabilitySlot } from '@/shared/api/client'
import { getAvailability } from './scheduleApi'
import { dayKey, fmtDay, fmtTime, scheduleErrorKey } from './scheduleFormat'

// Selector de franjas libres (FE-001/002/011): consulta la disponibilidad
// publicada y deja elegir 1 franja (clínica) o hasta `max` (tutor, A1). La
// disponibilidad es informativa; la autoridad es la reserva al confirmar.
const props = withDefaults(
  defineProps<{
    orgId: string
    type: AppointmentType
    professionalId?: string
    tz?: string
    max?: number
    days?: number
    professionals?: Record<string, string>
    conflict?: string | null
  }>(),
  {
    max: 1,
    days: 14,
    professionalId: undefined,
    tz: undefined,
    professionals: () => ({}),
    conflict: null,
  },
)
const model = defineModel<AvailabilitySlot[]>({ default: () => [] })
const { t, te, locale } = useI18n()

const slots = ref<AvailabilitySlot[]>([])
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)

async function load() {
  state.value = 'loading'
  const from = new Date()
  try {
    slots.value = await getAvailability(props.orgId, {
      from: from.toISOString(),
      to: new Date(from.getTime() + props.days * 86400_000).toISOString(),
      type: props.type,
      ...(props.professionalId ? { professional_id: props.professionalId } : {}),
    })
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
}
watch(() => [props.orgId, props.type, props.professionalId, props.days], load, { immediate: true })

const byDay = computed(() => {
  const groups = new Map<string, AvailabilitySlot[]>()
  for (const s of slots.value) {
    const k = dayKey(s.start, props.tz)
    groups.set(k, [...(groups.get(k) ?? []), s])
  }
  return [...groups.entries()]
})

const same = (a: AvailabilitySlot, b: AvailabilitySlot) =>
  a.start === b.start && a.professional_id === b.professional_id
const isSelected = (s: AvailabilitySlot) => model.value.some((m) => same(m, s))

function toggle(s: AvailabilitySlot) {
  if (isSelected(s)) model.value = model.value.filter((m) => !same(m, s))
  else if (props.max === 1) model.value = [s]
  else if (model.value.length < props.max) model.value = [...model.value, s]
}
</script>

<template>
  <div class="space-y-3" data-testid="slot-picker">
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-else-if="state === 'error' && error" class="text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>
    <p
      v-else-if="byDay.length === 0"
      class="text-sm text-surface-500"
      data-testid="slot-picker-empty"
    >
      {{ t('scheduling.slots.none') }}
    </p>
    <p v-if="conflict" class="rounded bg-amber-50 p-2 text-sm text-amber-800" role="alert">
      {{ conflict }}
    </p>
    <div v-for="[day, list] in byDay" :key="day">
      <h4 class="text-sm font-medium">{{ fmtDay(list[0].start, tz, locale) }}</h4>
      <ul class="mt-1 flex flex-wrap gap-2">
        <li v-for="s in list" :key="s.professional_id + s.start">
          <button
            type="button"
            class="rounded border px-2 py-1 text-sm"
            :class="
              isSelected(s)
                ? 'border-blue-600 bg-blue-50 font-semibold dark:bg-blue-950'
                : 'border-surface-300'
            "
            :aria-pressed="isSelected(s)"
            :title="professionals[s.professional_id]"
            @click="toggle(s)"
          >
            {{ fmtTime(s.start, tz, locale) }}
            <span v-if="!professionalId && professionals[s.professional_id]" class="text-xs">
              · {{ professionals[s.professional_id] }}
            </span>
          </button>
        </li>
      </ul>
    </div>
    <p v-if="max > 1" class="text-xs text-surface-500">
      {{ t('scheduling.slots.pickUpTo', { n: max }) }} ({{ model.length }}/{{ max }})
    </p>
  </div>
</template>
