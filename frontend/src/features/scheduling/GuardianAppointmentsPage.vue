<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  Appointment,
  AppointmentType,
  AvailabilitySlot,
  CareLink,
  SlotPreference,
} from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import { usePetStore } from '@/features/pets/petStore'
import SlotPicker from './SlotPicker.vue'
import {
  acceptProposal,
  cancelMyAppointment,
  listMyAppointments,
  requestAppointment,
} from './scheduleApi'
import { fmtDateTime, scheduleErrorKey, wouldBeLate } from './scheduleFormat'

// Citas del tutor (FE-011, PRD-016/018): solicitar con hasta 3 preferencias
// (franjas publicadas o rangos de fechas), aceptar la alternativa propuesta
// antes de que venza (48 h) y cancelar avisando si es tardía (sin penalización).
const { t, te, locale } = useI18n()
const pets = usePetStore()
const cmd = useCommand()

const MAX_PREFS = 3
const types: AppointmentType[] = [
  'consultation',
  'follow_up',
  'vaccination',
  'surgery',
  'procedure',
  'other',
]

const items = ref<Appointment[]>([])
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const busy = ref(false)
const now = ref(Date.now())
const confirmCancel = ref<string | null>(null)
let timer: ReturnType<typeof setInterval> | undefined

// ---- formulario de solicitud
const showForm = ref(false)
const form = ref({ petId: '', orgId: '', type: 'consultation' as AppointmentType, reason: '' })
const links = ref<CareLink[]>([])
const picked = ref<AvailabilitySlot[]>([])
const ranges = ref<{ date_from: string; date_to: string }[]>([])
const rangeDraft = ref({ date_from: '', date_to: '' })

const slotsLeft = computed(() => MAX_PREFS - ranges.value.length)
const total = computed(() => picked.value.length + ranges.value.length)

async function load() {
  try {
    items.value = (await listMyAppointments()).data
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
}

onMounted(async () => {
  timer = setInterval(() => (now.value = Date.now()), 1000)
  await Promise.all([load(), pets.load('active').catch(() => undefined)])
})
onBeforeUnmount(() => timer && clearInterval(timer))

watch(
  () => form.value.petId,
  async (id) => {
    form.value.orgId = ''
    links.value = []
    if (!id) return
    const pet = await pets.loadPet(id)
    links.value = (pet?.care_links ?? []).filter((l) => l.status === 'active' && !l.org_suspended)
    if (links.value.length === 1) form.value.orgId = links.value[0].organization_id
  },
)
watch(
  () => [form.value.orgId, form.value.type],
  () => (picked.value = []),
)

function addRange() {
  const r = rangeDraft.value
  if (
    !r.date_from ||
    !r.date_to ||
    r.date_to < r.date_from ||
    slotsLeft.value <= picked.value.length
  )
    return
  ranges.value.push({ ...r })
  rangeDraft.value = { date_from: '', date_to: '' }
}

async function submit() {
  const f = form.value
  if (!f.petId || !f.orgId || total.value === 0 || busy.value) return
  busy.value = true
  error.value = notice.value = null
  const preferences: SlotPreference[] = [
    ...picked.value.map((s) => ({ start: s.start, end: s.end })),
    ...ranges.value,
  ]
  try {
    await requestAppointment(cmd.key.value, {
      pet_id: f.petId,
      organization_id: f.orgId,
      type: f.type,
      ...(f.reason ? { reason: f.reason } : {}),
      preferences,
    })
    cmd.renew()
    notice.value = t('scheduling.guardian.requested')
    showForm.value = false
    picked.value = []
    ranges.value = []
    form.value.reason = ''
    await load()
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
  } finally {
    busy.value = false
  }
}

async function act(fn: () => Promise<Appointment>) {
  if (busy.value) return
  busy.value = true
  error.value = notice.value = null
  try {
    await fn()
    confirmCancel.value = null
    await load()
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    await load()
  } finally {
    busy.value = false
  }
}

function remaining(a: Appointment): string | null {
  if (!a.proposal_expires_at) return null
  const ms = new Date(a.proposal_expires_at).getTime() - now.value
  if (ms <= 0) return null
  const h = Math.floor(ms / 3600_000)
  const m = Math.floor((ms % 3600_000) / 60_000)
  return `${h} h ${String(m).padStart(2, '0')} min`
}

const OPEN = new Set(['requested', 'proposed', 'confirmed', 'checked_in'])
const upcoming = computed(() => items.value.filter((a) => OPEN.has(a.status)))
const past = computed(() => items.value.filter((a) => !OPEN.has(a.status)))
const canCancel = (a: Appointment) => ['requested', 'proposed', 'confirmed'].includes(a.status)
</script>

<template>
  <section class="mx-auto max-w-3xl space-y-6" data-testid="guardian-appointments">
    <header class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">{{ t('scheduling.guardian.title') }}</h1>
      <button
        type="button"
        class="btn-primary"
        data-testid="request-btn"
        @click="showForm = !showForm"
      >
        {{ t('scheduling.guardian.request') }}
      </button>
    </header>

    <p v-if="notice" class="rounded bg-green-50 p-3 text-sm text-green-800" role="status">
      {{ notice }}
    </p>
    <p v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>

    <form
      v-if="showForm"
      class="card space-y-3"
      data-testid="request-form"
      @submit.prevent="submit"
    >
      <label class="block text-sm">
        {{ t('scheduling.guardian.pet') }}
        <select v-model="form.petId" class="input mt-1" required data-testid="req-pet">
          <option value="" disabled>{{ t('common.select') }}</option>
          <option v-for="p in pets.items" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </label>
      <label v-if="form.petId" class="block text-sm">
        {{ t('scheduling.guardian.clinic') }}
        <select v-model="form.orgId" class="input mt-1" required data-testid="req-org">
          <option value="" disabled>{{ t('common.select') }}</option>
          <option v-for="l in links" :key="l.organization_id" :value="l.organization_id">
            {{ l.display_name }}
          </option>
        </select>
        <span v-if="links.length === 0" class="text-xs text-surface-500">{{
          t('scheduling.guardian.noLinks')
        }}</span>
      </label>
      <label class="block text-sm">
        {{ t('scheduling.new.type') }}
        <select v-model="form.type" class="input mt-1">
          <option v-for="ty in types" :key="ty" :value="ty">
            {{ t(`scheduling.type.${ty}`) }}
          </option>
        </select>
      </label>
      <label class="block text-sm">
        {{ t('scheduling.new.reason') }}
        <input v-model="form.reason" class="input mt-1" maxlength="300" />
      </label>

      <fieldset v-if="form.orgId" class="space-y-2">
        <legend class="font-medium text-sm">{{ t('scheduling.guardian.preferences') }}</legend>
        <SlotPicker v-model="picked" :org-id="form.orgId" :type="form.type" :max="slotsLeft" />
        <div class="flex flex-wrap items-end gap-2 text-sm">
          <label
            >{{ t('scheduling.guardian.dateFrom')
            }}<input v-model="rangeDraft.date_from" type="date" class="input mt-1"
          /></label>
          <label
            >{{ t('scheduling.guardian.dateTo')
            }}<input v-model="rangeDraft.date_to" type="date" class="input mt-1"
          /></label>
          <button
            type="button"
            class="btn-secondary"
            :disabled="total >= MAX_PREFS"
            @click="addRange"
          >
            {{ t('scheduling.guardian.addRange') }}
          </button>
        </div>
        <ul class="text-sm" data-testid="range-list">
          <li v-for="(r, i) in ranges" :key="i" class="flex items-center gap-2">
            {{ r.date_from }} → {{ r.date_to }}
            <button
              type="button"
              class="btn-secondary"
              :aria-label="t('common.delete')"
              @click="ranges.splice(i, 1)"
            >
              ✕
            </button>
          </li>
        </ul>
        <p class="text-xs text-surface-500">
          {{ t('scheduling.guardian.prefHint', { n: MAX_PREFS }) }} ({{ total }}/{{ MAX_PREFS }})
        </p>
      </fieldset>

      <div class="flex gap-2">
        <button type="submit" class="btn-primary" :disabled="busy || total === 0 || !form.orgId">
          {{ busy ? t('common.saving') : t('scheduling.guardian.send') }}
        </button>
        <button type="button" class="btn-secondary" @click="showForm = false">
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>

    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p
      v-if="state === 'ready' && items.length === 0"
      class="text-sm text-surface-500"
      data-testid="no-appointments"
    >
      {{ t('scheduling.guardian.empty') }}
    </p>

    <section v-if="upcoming.length" :aria-label="t('scheduling.guardian.upcoming')">
      <h2 class="text-lg font-semibold">{{ t('scheduling.guardian.upcoming') }}</h2>
      <ul class="mt-2 space-y-2">
        <li v-for="a in upcoming" :key="a.id" class="card space-y-2" data-testid="appointment-item">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <p class="font-medium">
              {{ a.pet_name }} · {{ t(`scheduling.type.${a.type}`) }}
              <span class="text-sm font-normal text-surface-500">· {{ a.organization_name }}</span>
            </p>
            <span class="rounded bg-surface-100 px-2 py-0.5 text-xs dark:bg-surface-800">{{
              t(`scheduling.status.${a.status}`)
            }}</span>
          </div>
          <p v-if="a.start" class="text-sm">{{ fmtDateTime(a.start, a.time_zone, locale) }}</p>

          <div
            v-if="a.status === 'proposed'"
            class="rounded bg-amber-50 p-2 text-sm text-amber-900"
            data-testid="proposal"
          >
            <template v-if="remaining(a)">
              {{ t('scheduling.guardian.proposal', { left: remaining(a) }) }}
              <button
                type="button"
                class="btn-primary ml-2"
                :disabled="busy"
                @click="act(() => acceptProposal(a.id))"
              >
                {{ t('scheduling.guardian.accept') }}
              </button>
            </template>
            <template v-else>{{ t('scheduling.guardian.proposalExpired') }}</template>
          </div>

          <div v-if="canCancel(a)">
            <button
              v-if="confirmCancel !== a.id"
              type="button"
              class="btn-secondary"
              @click="confirmCancel = a.id"
            >
              {{ t('scheduling.action.cancel') }}
            </button>
            <div v-else class="space-y-2 rounded border p-2 text-sm" data-testid="cancel-confirm">
              <p v-if="wouldBeLate(a)" class="text-amber-800">
                {{ t('scheduling.guardian.lateWarning', { hours: a.cancellation_policy_hours }) }}
              </p>
              <p v-else>{{ t('scheduling.guardian.cancelAsk') }}</p>
              <button
                type="button"
                class="btn-primary"
                :disabled="busy"
                @click="act(() => cancelMyAppointment(a.id))"
              >
                {{ t('scheduling.guardian.cancelYes') }}
              </button>
              <button type="button" class="btn-secondary ml-2" @click="confirmCancel = null">
                {{ t('common.back') }}
              </button>
            </div>
          </div>
        </li>
      </ul>
    </section>

    <section v-if="past.length" :aria-label="t('scheduling.guardian.past')">
      <h2 class="text-lg font-semibold">{{ t('scheduling.guardian.past') }}</h2>
      <ul class="mt-2 space-y-1 text-sm">
        <li v-for="a in past" :key="a.id" class="flex justify-between rounded border px-3 py-2">
          <span
            >{{ a.pet_name }} · {{ t(`scheduling.type.${a.type}`) }} ·
            {{ a.organization_name }}</span
          >
          <span class="text-xs"
            >{{ a.start ? fmtDateTime(a.start, a.time_zone, locale) + ' · ' : ''
            }}{{ t(`scheduling.status.${a.status}`) }}</span
          >
        </li>
      </ul>
    </section>
  </section>
</template>
