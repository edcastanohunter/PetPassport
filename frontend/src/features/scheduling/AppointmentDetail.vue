<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Appointment, AvailabilitySlot } from '@/shared/api/client'
import SlotPicker from './SlotPicker.vue'
import { orgAction, reschedule } from './scheduleApi'
import { fmtDateTime, isConflict, scheduleErrorKey } from './scheduleFormat'

// Gestión de una cita desde la clínica (FE-002, PRD-016/017/018/019): confirmar
// o proponer alternativa sobre una solicitud, reprogramar (If-Match y franja
// ocupada resaltada), cancelar, check-in, no-show, completar e historial.
const props = defineProps<{
  appointment: Appointment
  orgId: string
  professionals: Record<string, string>
  tz?: string
}>()
const emit = defineEmits<{ changed: [a: Appointment] }>()
const { t, te, locale } = useI18n()

type Mode = 'confirm' | 'propose' | 'reschedule' | 'reject' | 'cancel' | null
const mode = ref<Mode>(null)
const picked = ref<AvailabilitySlot[]>([])
const reason = ref('')
const busy = ref(false)
const error = ref<string | null>(null)
const conflict = ref<string | null>(null)
const pickerKey = ref(0)

const a = computed(() => props.appointment)
const needsSlot = computed(() => ['confirm', 'propose', 'reschedule'].includes(mode.value ?? ''))
const tzName = computed(() => a.value.time_zone || props.tz)

function open(m: Mode) {
  mode.value = m
  picked.value = []
  reason.value = ''
  error.value = null
  conflict.value = null
}

async function run(fn: () => Promise<Appointment>) {
  if (busy.value) return
  busy.value = true
  error.value = null
  conflict.value = null
  try {
    emit('changed', await fn())
    mode.value = null
  } catch (e) {
    if (isConflict(e)) {
      // Franja ocupada o cita modificada: se resalta y se vuelve a pedir la disponibilidad.
      conflict.value = t(scheduleErrorKey(e, te))
      picked.value = []
      pickerKey.value++
    } else {
      error.value = scheduleErrorKey(e, te)
    }
  } finally {
    busy.value = false
  }
}

function submit() {
  const m = mode.value
  const slot = picked.value[0]
  if (m === 'confirm' && slot) {
    return run(() =>
      orgAction(props.orgId, a.value.id, 'confirm', {
        professional_id: slot.professional_id,
        start: slot.start,
        end: slot.end,
      }),
    )
  }
  if (m === 'propose' && slot) {
    return run(() =>
      orgAction(props.orgId, a.value.id, 'propose', {
        professional_id: slot.professional_id,
        start: slot.start,
        end: slot.end,
      }),
    )
  }
  if (m === 'reschedule' && slot) {
    return run(() =>
      reschedule(props.orgId, a.value.id, a.value.version, {
        professional_id: slot.professional_id,
        start: slot.start,
        end: slot.end,
      }),
    )
  }
  if (m === 'reject') {
    return run(() =>
      orgAction(props.orgId, a.value.id, 'reject', { reason: reason.value || undefined }),
    )
  }
  if (m === 'cancel') {
    return run(() =>
      orgAction(props.orgId, a.value.id, 'cancel', { reason: reason.value || undefined }),
    )
  }
}

const simple = (action: 'check-in' | 'no-show' | 'complete') =>
  run(() => orgAction(props.orgId, a.value.id, action))
</script>

<template>
  <article class="card space-y-4" data-testid="appointment-detail">
    <header class="flex flex-wrap items-baseline justify-between gap-2">
      <div>
        <h2 class="text-lg font-semibold">
          {{ a.pet_name }}
          <span class="text-sm font-normal text-surface-500"
            >· {{ t(`scheduling.type.${a.type}`) }}</span
          >
        </h2>
        <p v-if="a.start" class="text-sm" data-testid="appointment-when">
          {{ fmtDateTime(a.start, tzName, locale) }}
          <span v-if="a.professional_name"> · {{ a.professional_name }}</span>
        </p>
      </div>
      <span class="rounded bg-surface-100 px-2 py-0.5 text-xs dark:bg-surface-800">
        {{ t(`scheduling.status.${a.status}`) }}
      </span>
    </header>

    <p v-if="a.reason" class="text-sm">{{ a.reason }}</p>

    <section v-if="a.status === 'requested' && a.preferences.length" data-testid="preferences">
      <h3 class="text-sm font-medium">{{ t('scheduling.detail.preferences') }}</h3>
      <ul class="mt-1 list-disc pl-5 text-sm">
        <li v-for="(p, i) in a.preferences" :key="i">
          <template v-if="p.start">{{ fmtDateTime(p.start, tzName, locale) }}</template>
          <template v-else>{{ p.date_from }} → {{ p.date_to }}</template>
        </li>
      </ul>
    </section>

    <p v-if="a.status === 'proposed' && a.proposal_expires_at" class="text-sm text-amber-700">
      {{
        t('scheduling.detail.proposalExpires', {
          when: fmtDateTime(a.proposal_expires_at, tzName, locale),
        })
      }}
    </p>
    <p v-if="a.cancellation" class="text-sm">
      {{ t(`scheduling.detail.cancelledBy.${a.cancellation.by}`) }}
      <span v-if="a.cancellation.late" class="text-amber-700"
        >· {{ t('scheduling.detail.late') }}</span
      >
      <span v-if="a.cancellation.reason"> · {{ a.cancellation.reason }}</span>
    </p>

    <p v-if="error" class="rounded bg-red-50 p-2 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>

    <div v-if="mode === null" class="flex flex-wrap gap-2" data-testid="appointment-actions">
      <template v-if="a.status === 'requested'">
        <button type="button" class="btn-primary" @click="open('confirm')">
          {{ t('scheduling.action.confirm') }}
        </button>
        <button type="button" class="btn-secondary" @click="open('propose')">
          {{ t('scheduling.action.propose') }}
        </button>
        <button type="button" class="btn-secondary" @click="open('reject')">
          {{ t('scheduling.action.reject') }}
        </button>
      </template>
      <template v-if="a.status === 'proposed'">
        <button type="button" class="btn-secondary" @click="open('reject')">
          {{ t('scheduling.action.reject') }}
        </button>
      </template>
      <template v-if="a.status === 'confirmed'">
        <button type="button" class="btn-primary" :disabled="busy" @click="simple('check-in')">
          {{ t('scheduling.action.checkIn') }}
        </button>
        <button type="button" class="btn-secondary" @click="open('reschedule')">
          {{ t('scheduling.action.reschedule') }}
        </button>
        <button type="button" class="btn-secondary" :disabled="busy" @click="simple('no-show')">
          {{ t('scheduling.action.noShow') }}
        </button>
        <button type="button" class="btn-secondary" @click="open('cancel')">
          {{ t('scheduling.action.cancel') }}
        </button>
      </template>
      <template v-if="a.status === 'checked_in'">
        <RouterLink
          class="btn-primary"
          :to="{ name: 'org-patient', params: { orgId, petId: a.pet_id } }"
        >
          {{ t('scheduling.action.openEncounter') }}
        </RouterLink>
        <button type="button" class="btn-secondary" :disabled="busy" @click="simple('complete')">
          {{ t('scheduling.action.complete') }}
        </button>
      </template>
      <template v-if="a.status === 'proposed' || a.status === 'checked_in'">
        <button type="button" class="btn-secondary" @click="open('cancel')">
          {{ t('scheduling.action.cancel') }}
        </button>
      </template>
    </div>

    <form v-else class="space-y-3" data-testid="appointment-form" @submit.prevent="submit">
      <h3 class="font-medium">{{ t(`scheduling.action.${mode}`) }}</h3>
      <SlotPicker
        v-if="needsSlot"
        :key="pickerKey"
        v-model="picked"
        :org-id="orgId"
        :type="a.type"
        :tz="tzName"
        :professionals="professionals"
        :conflict="conflict"
      />
      <label v-else class="block text-sm">
        {{ t('scheduling.detail.reason') }}
        <textarea v-model="reason" maxlength="300" rows="2" class="input mt-1 w-full" />
      </label>
      <div class="flex gap-2">
        <button
          type="submit"
          class="btn-primary"
          :disabled="busy || (needsSlot && picked.length === 0)"
        >
          {{ busy ? t('common.saving') : t('scheduling.detail.submit') }}
        </button>
        <button type="button" class="btn-secondary" @click="mode = null">
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>

    <section v-if="a.history.length" :aria-label="t('scheduling.detail.history')">
      <h3 class="text-sm font-medium">{{ t('scheduling.detail.history') }}</h3>
      <ol class="mt-1 space-y-0.5 text-xs text-surface-500" data-testid="appointment-history">
        <li v-for="(h, i) in a.history" :key="i">
          {{ fmtDateTime(h.at, tzName, locale) }} · {{ t(`scheduling.status.${h.status}`) }}
          <span v-if="h.note">· {{ h.note }}</span>
        </li>
      </ol>
    </section>
  </article>
</template>
