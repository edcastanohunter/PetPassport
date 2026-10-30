<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Agenda, Appointment } from '@/shared/api/client'
import AppointmentDetail from './AppointmentDetail.vue'
import NewAppointmentForm from './NewAppointmentForm.vue'
import { listAgendas, listOrgAppointments } from './scheduleApi'
import { addDays, dayKey, fmtDay, fmtTime, scheduleErrorKey, startOfWeek } from './scheduleFormat'

// Agenda de la clínica (FE-001) y gestión de solicitudes (FE-002): vista de
// día o semana por profesional, bandeja de solicitudes pendientes y el panel
// de acciones de la cita seleccionada. La lista de agenda viene en orden de
// franja; la bandeja, de más reciente a más antigua (API-007).
const { t, te, locale } = useI18n()
const route = useRoute()
const orgId = computed(() => String(route.params.orgId))

const view = ref<'day' | 'week'>('week')
const anchor = ref(new Date())
const professionalId = ref('')
const agendas = ref<Agenda[]>([])
const items = ref<Appointment[]>([])
const inbox = ref<Appointment[]>([])
const selected = ref<Appointment | null>(null)
const creating = ref(false)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)

const professionals = computed(() =>
  Object.fromEntries(agendas.value.map((a) => [a.professional_id, a.professional_name ?? ''])),
)
const tz = computed(() => agendas.value.find((a) => a.time_zone)?.time_zone)

const range = computed(() => {
  const start = view.value === 'week' ? startOfWeek(anchor.value) : new Date(anchor.value)
  start.setHours(0, 0, 0, 0)
  return { from: start, to: addDays(start, view.value === 'week' ? 7 : 1) }
})

const HIDDEN = new Set(['cancelled', 'rejected', 'requested'])
const days = computed(() => {
  const groups = new Map<string, Appointment[]>()
  for (let i = 0; i < (view.value === 'week' ? 7 : 1); i++) {
    groups.set(dayKey(addDays(range.value.from, i).toISOString(), undefined), [])
  }
  for (const a of items.value) {
    if (!a.start || HIDDEN.has(a.status)) continue
    const k = dayKey(a.start, tz.value)
    groups.get(k)?.push(a)
  }
  return [...groups.entries()]
})

async function load() {
  state.value = 'loading'
  try {
    const [ag, list, pending] = await Promise.all([
      agendas.value.length ? Promise.resolve(agendas.value) : listAgendas(orgId.value),
      listOrgAppointments(orgId.value, {
        from: range.value.from.toISOString(),
        to: range.value.to.toISOString(),
        ...(professionalId.value ? { professional_id: professionalId.value } : {}),
      }),
      listOrgAppointments(orgId.value, { status: 'requested' }),
    ])
    agendas.value = ag
    items.value = list.data
    inbox.value = pending.data
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
}
onMounted(load)
watch([view, anchor, professionalId], load)

function step(dir: -1 | 1) {
  anchor.value = addDays(anchor.value, dir * (view.value === 'week' ? 7 : 1))
}

// Tras un comando se reemplaza la cita y se recarga la agenda y la bandeja.
async function changed(a: Appointment) {
  selected.value = a
  creating.value = false
  await load()
}

function startCreate() {
  creating.value = true
  selected.value = null
}
function select(a: Appointment) {
  selected.value = a
  creating.value = false
}
</script>

<template>
  <section class="mx-auto max-w-6xl space-y-6" data-testid="org-agenda">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('scheduling.agenda.title') }}</h1>
      <div class="flex flex-wrap items-center gap-2">
        <label class="sr-only" for="agenda-prof">{{ t('scheduling.agenda.professional') }}</label>
        <select id="agenda-prof" v-model="professionalId" class="input w-auto">
          <option value="">{{ t('scheduling.agenda.allProfessionals') }}</option>
          <option
            v-for="a in agendas.filter((x) => x.defined)"
            :key="a.professional_id"
            :value="a.professional_id"
          >
            {{ a.professional_name }}
          </option>
        </select>
        <div role="group" :aria-label="t('scheduling.agenda.view')" class="flex gap-1">
          <button
            type="button"
            class="btn-secondary"
            :aria-pressed="view === 'day'"
            @click="view = 'day'"
          >
            {{ t('scheduling.agenda.day') }}
          </button>
          <button
            type="button"
            class="btn-secondary"
            :aria-pressed="view === 'week'"
            @click="view = 'week'"
          >
            {{ t('scheduling.agenda.week') }}
          </button>
        </div>
        <button
          type="button"
          class="btn-secondary"
          :aria-label="t('scheduling.agenda.prev')"
          @click="step(-1)"
        >
          ‹
        </button>
        <button type="button" class="btn-secondary" @click="anchor = new Date()">
          {{ t('scheduling.agenda.today') }}
        </button>
        <button
          type="button"
          class="btn-secondary"
          :aria-label="t('scheduling.agenda.next')"
          @click="step(1)"
        >
          ›
        </button>
        <button
          type="button"
          class="btn-primary"
          data-testid="new-appointment-btn"
          @click="startCreate"
        >
          {{ t('scheduling.new.title') }}
        </button>
      </div>
    </header>

    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p
      v-if="state === 'error' && error"
      class="rounded bg-red-50 p-3 text-sm text-red-700"
      role="alert"
    >
      {{ t(error) }}
    </p>

    <p
      v-if="state === 'ready' && agendas.every((a) => !a.defined)"
      class="rounded border border-dashed p-4 text-sm"
      data-testid="no-agendas"
    >
      {{ t('scheduling.agenda.noAgendas') }}
      <RouterLink :to="{ name: 'org-agendas', params: { orgId } }" class="underline">{{
        t('scheduling.agenda.configure')
      }}</RouterLink>
    </p>

    <div class="grid gap-6 lg:grid-cols-[1fr_22rem]">
      <div class="space-y-4">
        <section v-for="[key, list] in days" :key="key" :aria-label="key" data-testid="agenda-day">
          <h2 class="text-sm font-semibold">{{ fmtDay(key + 'T12:00:00', undefined, locale) }}</h2>
          <p v-if="list.length === 0" class="text-xs text-surface-500">
            {{ t('scheduling.agenda.free') }}
          </p>
          <ul class="mt-1 space-y-1">
            <li v-for="a in list" :key="a.id">
              <button
                type="button"
                class="flex w-full items-center justify-between rounded border px-3 py-2 text-left text-sm"
                :class="
                  selected?.id === a.id
                    ? 'border-blue-600'
                    : 'border-surface-200 dark:border-surface-800'
                "
                @click="select(a)"
              >
                <span>
                  <strong>{{ fmtTime(a.start!, tz, locale) }}</strong>
                  {{ a.pet_name }} · {{ t(`scheduling.type.${a.type}`) }}
                  <span
                    v-if="!professionalId && a.professional_name"
                    class="text-xs text-surface-500"
                    >· {{ a.professional_name }}</span
                  >
                </span>
                <span class="text-xs">{{ t(`scheduling.status.${a.status}`) }}</span>
              </button>
            </li>
          </ul>
        </section>
      </div>

      <aside class="space-y-4">
        <section v-if="inbox.length" :aria-label="t('scheduling.inbox.title')" data-testid="inbox">
          <h2 class="text-lg font-semibold">
            {{ t('scheduling.inbox.title') }} ({{ inbox.length }})
          </h2>
          <ul class="mt-2 space-y-1">
            <li v-for="a in inbox" :key="a.id">
              <button
                type="button"
                class="w-full rounded border border-amber-300 px-3 py-2 text-left text-sm"
                @click="select(a)"
              >
                {{ a.pet_name }} · {{ t(`scheduling.type.${a.type}`) }}
              </button>
            </li>
          </ul>
        </section>

        <NewAppointmentForm
          v-if="creating"
          :org-id="orgId"
          :professionals="professionals"
          :tz="tz"
          @created="changed"
          @cancel="creating = false"
        />
        <AppointmentDetail
          v-else-if="selected"
          :key="selected.id + selected.version"
          :appointment="selected"
          :org-id="orgId"
          :professionals="professionals"
          :tz="tz"
          @changed="changed"
        />
      </aside>
    </div>
  </section>
</template>
