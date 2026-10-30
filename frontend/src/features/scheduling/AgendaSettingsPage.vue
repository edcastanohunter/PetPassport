<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Agenda, AgendaTimeRange, AppointmentType } from '@/shared/api/client'
import { addException, listAgendas, putAgenda, removeException } from './scheduleApi'
import { scheduleErrorKey } from './scheduleFormat'

// Configuración de agendas (FE-007, PRD-019): plantilla semanal por
// profesional, duración de franja por tipo, antelaciones, política de
// cancelación, activación y excepciones por día. Solo clinic_admin escribe;
// la zona horaria se fija al crear la agenda y no cambia después (A6).
const { t, te } = useI18n()
const route = useRoute()
const orgId = computed(() => String(route.params.orgId))

const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const
const TYPES: AppointmentType[] = [
  'consultation',
  'follow_up',
  'vaccination',
  'surgery',
  'procedure',
  'other',
]

interface Draft {
  time_zone: string
  weekly_template: Record<string, AgendaTimeRange[]>
  slot_minutes: Record<string, number>
  lead_time_min_hours: number
  lead_time_max_days: number
  cancellation_policy_hours: number
  active: boolean
}

const agendas = ref<Agenda[]>([])
const current = ref<Agenda | null>(null)
const draft = ref<Draft | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const saved = ref(false)
const busy = ref(false)
const exc = ref({ date: '', closed: true, start: '09:00', end: '12:00' })

function blank(tz?: string): Draft {
  return {
    time_zone: tz ?? '',
    weekly_template: Object.fromEntries(
      DAYS.map((d) => [d, d === 'sat' || d === 'sun' ? [] : [{ start: '08:00', end: '17:00' }]]),
    ),
    slot_minutes: Object.fromEntries(TYPES.map((ty) => [ty, ty === 'surgery' ? 60 : 30])),
    lead_time_min_hours: 2,
    lead_time_max_days: 60,
    cancellation_policy_hours: 24,
    active: true,
  }
}

function edit(a: Agenda) {
  current.value = a
  saved.value = false
  error.value = null
  if (!a.defined) {
    draft.value = blank()
    return
  }
  const base = blank(a.time_zone ?? '')
  draft.value = {
    time_zone: a.time_zone ?? '',
    weekly_template: Object.fromEntries(
      DAYS.map((d) => [d, structuredClone(a.weekly_template?.[d] ?? [])]),
    ),
    slot_minutes: { ...base.slot_minutes, ...(a.slot_minutes ?? {}) },
    lead_time_min_hours: a.lead_time_min_hours ?? base.lead_time_min_hours,
    lead_time_max_days: a.lead_time_max_days ?? base.lead_time_max_days,
    cancellation_policy_hours: a.cancellation_policy_hours ?? base.cancellation_policy_hours,
    active: a.active,
  }
}

async function load(keep?: string) {
  try {
    agendas.value = await listAgendas(orgId.value)
    state.value = 'ready'
    const again = agendas.value.find((a) => a.professional_id === keep)
    if (again) edit(again)
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
}
onMounted(() => load())

const exceptions = computed(() =>
  Object.entries(current.value?.exceptions ?? {}).sort(([a], [b]) => a.localeCompare(b)),
)

function addRange(day: string) {
  draft.value?.weekly_template[day].push({ start: '08:00', end: '12:00' })
}
function dropRange(day: string, i: number) {
  draft.value?.weekly_template[day].splice(i, 1)
}

async function run(fn: () => Promise<Agenda>) {
  if (busy.value || !current.value) return
  busy.value = true
  error.value = null
  saved.value = false
  try {
    const a = await fn()
    saved.value = true
    await load(a.professional_id)
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
  } finally {
    busy.value = false
  }
}

function save() {
  const d = draft.value
  const a = current.value
  if (!d || !a) return
  return run(() =>
    putAgenda(
      orgId.value,
      a.professional_id,
      {
        ...(a.defined ? {} : d.time_zone ? { time_zone: d.time_zone } : {}),
        weekly_template: d.weekly_template,
        slot_minutes: d.slot_minutes,
        lead_time_min_hours: d.lead_time_min_hours,
        lead_time_max_days: d.lead_time_max_days,
        cancellation_policy_hours: d.cancellation_policy_hours,
        active: d.active,
      },
      a.defined ? a.version : undefined,
    ),
  )
}

function addExc() {
  const a = current.value
  if (!a || !exc.value.date) return
  return run(() =>
    addException(
      orgId.value,
      a.professional_id,
      exc.value.date,
      exc.value.closed
        ? { closed: true }
        : { closed: false, ranges: [{ start: exc.value.start, end: exc.value.end }] },
    ),
  )
}
function dropExc(date: string) {
  const a = current.value
  if (!a) return
  return run(() => removeException(orgId.value, a.professional_id, date))
}
</script>

<template>
  <section class="mx-auto max-w-5xl space-y-6" data-testid="agenda-settings">
    <h1 class="text-2xl font-bold">{{ t('scheduling.settings.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>
    <p v-if="state === 'ready' && agendas.length === 0" class="text-sm text-surface-500">
      {{ t('scheduling.settings.noVets') }}
    </p>

    <div class="grid gap-6 md:grid-cols-[14rem_1fr]">
      <ul class="space-y-1" data-testid="agenda-list">
        <li v-for="a in agendas" :key="a.professional_id">
          <button
            type="button"
            class="w-full rounded border px-3 py-2 text-left text-sm"
            :class="
              current?.professional_id === a.professional_id
                ? 'border-blue-600'
                : 'border-surface-200 dark:border-surface-800'
            "
            @click="edit(a)"
          >
            {{ a.professional_name }}
            <span class="block text-xs text-surface-500">
              {{
                !a.defined
                  ? t('scheduling.settings.undefined')
                  : a.active
                    ? t('scheduling.settings.active')
                    : t('scheduling.settings.inactive')
              }}
            </span>
          </button>
        </li>
      </ul>

      <form
        v-if="draft && current"
        class="space-y-6"
        data-testid="agenda-form"
        @submit.prevent="save"
      >
        <h2 class="text-lg font-semibold">{{ current.professional_name }}</h2>

        <label class="block text-sm">
          {{ t('scheduling.settings.timeZone') }}
          <input
            v-model="draft.time_zone"
            class="input mt-1"
            :disabled="current.defined"
            :placeholder="t('scheduling.settings.timeZoneAuto')"
          />
          <span class="text-xs text-surface-500">{{ t('scheduling.settings.timeZoneHint') }}</span>
        </label>

        <fieldset class="space-y-2">
          <legend class="font-medium">{{ t('scheduling.settings.weekly') }}</legend>
          <div
            v-for="d in DAYS"
            :key="d"
            class="flex flex-wrap items-center gap-2 text-sm"
            :data-testid="`day-${d}`"
          >
            <span class="w-24">{{ t(`scheduling.days.${d}`) }}</span>
            <span v-if="draft.weekly_template[d].length === 0" class="text-xs text-surface-500">{{
              t('scheduling.settings.closed')
            }}</span>
            <span
              v-for="(r, i) in draft.weekly_template[d]"
              :key="i"
              class="flex items-center gap-1"
            >
              <input
                v-model="r.start"
                type="time"
                class="input w-28"
                :aria-label="t('scheduling.settings.from')"
              />
              –
              <input
                v-model="r.end"
                type="time"
                class="input w-28"
                :aria-label="t('scheduling.settings.to')"
              />
              <button
                type="button"
                class="btn-secondary"
                :aria-label="t('common.delete')"
                @click="dropRange(d, i)"
              >
                ✕
              </button>
            </span>
            <button type="button" class="btn-secondary" @click="addRange(d)">+</button>
          </div>
        </fieldset>

        <fieldset class="grid gap-2 sm:grid-cols-3">
          <legend class="font-medium">{{ t('scheduling.settings.durations') }}</legend>
          <label v-for="ty in TYPES" :key="ty" class="block text-sm">
            {{ t(`scheduling.type.${ty}`) }}
            <input
              v-model.number="draft.slot_minutes[ty]"
              type="number"
              min="5"
              max="480"
              step="5"
              class="input mt-1"
            />
          </label>
        </fieldset>

        <fieldset class="grid gap-2 sm:grid-cols-3">
          <legend class="font-medium">{{ t('scheduling.settings.policies') }}</legend>
          <label class="block text-sm">
            {{ t('scheduling.settings.leadMin') }}
            <input
              v-model.number="draft.lead_time_min_hours"
              type="number"
              min="0"
              class="input mt-1"
            />
          </label>
          <label class="block text-sm">
            {{ t('scheduling.settings.leadMax') }}
            <input
              v-model.number="draft.lead_time_max_days"
              type="number"
              min="1"
              class="input mt-1"
            />
          </label>
          <label class="block text-sm">
            {{ t('scheduling.settings.cancelHours') }}
            <input
              v-model.number="draft.cancellation_policy_hours"
              type="number"
              min="0"
              class="input mt-1"
            />
          </label>
        </fieldset>

        <label class="flex items-center gap-2 text-sm">
          <input v-model="draft.active" type="checkbox" />
          {{ t('scheduling.settings.activeLabel') }}
        </label>

        <div class="flex items-center gap-3">
          <button type="submit" class="btn-primary" :disabled="busy">
            {{ busy ? t('common.saving') : t('common.save') }}
          </button>
          <span v-if="saved" class="text-sm text-green-700" role="status">{{
            t('scheduling.settings.saved')
          }}</span>
        </div>

        <section v-if="current.defined" class="space-y-2" data-testid="exceptions">
          <h3 class="font-medium">{{ t('scheduling.settings.exceptions') }}</h3>
          <ul class="space-y-1 text-sm">
            <li
              v-for="[date, e] in exceptions"
              :key="date"
              class="flex items-center justify-between rounded border px-3 py-1"
            >
              <span>
                {{ date }} ·
                {{
                  e.closed
                    ? t('scheduling.settings.closed')
                    : (e.ranges ?? []).map((r) => `${r.start}–${r.end}`).join(', ')
                }}
              </span>
              <button type="button" class="btn-secondary" @click="dropExc(date)">
                {{ t('common.delete') }}
              </button>
            </li>
          </ul>
          <div class="flex flex-wrap items-end gap-2 text-sm">
            <label
              >{{ t('scheduling.settings.date')
              }}<input v-model="exc.date" type="date" class="input mt-1"
            /></label>
            <label class="flex items-center gap-1"
              ><input v-model="exc.closed" type="checkbox" />{{
                t('scheduling.settings.closedDay')
              }}</label
            >
            <template v-if="!exc.closed">
              <input
                v-model="exc.start"
                type="time"
                class="input w-28"
                :aria-label="t('scheduling.settings.from')"
              />
              <input
                v-model="exc.end"
                type="time"
                class="input w-28"
                :aria-label="t('scheduling.settings.to')"
              />
            </template>
            <button
              type="button"
              class="btn-secondary"
              :disabled="busy || !exc.date"
              @click="addExc"
            >
              {{ t('scheduling.settings.addException') }}
            </button>
          </div>
        </section>
      </form>
    </div>
  </section>
</template>
