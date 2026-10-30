<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Appointment } from '@/shared/api/client'
import { acceptProposal, listMyAppointments } from '@/features/scheduling/scheduleApi'
import { fmtDateTime, scheduleErrorKey } from '@/features/scheduling/scheduleFormat'

// Bandeja del tutor (FE-012): lo que espera una respuesta suya. Hoy, las
// alternativas de cita propuestas por la clínica; las solicitudes de vínculo
// de una clínica llegan con M2.5 (PRD-020).
const { t, te, locale } = useI18n()

const proposals = ref<Appointment[]>([])
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const busy = ref(false)

async function load() {
  try {
    proposals.value = (await listMyAppointments({ status: 'proposed' })).data
    state.value = 'ready'
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
    state.value = 'error'
  }
}
onMounted(load)

async function accept(id: string) {
  if (busy.value) return
  busy.value = true
  error.value = null
  try {
    await acceptProposal(id)
    await load()
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-semibold">{{ t('notifications.inbox.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-700">{{ t(error) }}</p>
    <p
      v-if="state === 'ready' && proposals.length === 0"
      class="text-sm text-surface-500"
      data-testid="inbox-empty"
    >
      {{ t('notifications.inbox.empty') }}
    </p>
    <ul v-if="proposals.length" class="space-y-2">
      <li v-for="a in proposals" :key="a.id" class="card space-y-2" data-testid="inbox-item">
        <p class="font-medium">
          {{ t('notifications.inbox.proposal', { clinic: a.organization_name, pet: a.pet_name }) }}
        </p>
        <p v-if="a.start" class="text-sm">{{ fmtDateTime(a.start, a.time_zone, locale) }}</p>
        <button type="button" class="btn-primary" :disabled="busy" @click="accept(a.id)">
          {{ t('scheduling.guardian.accept') }}
        </button>
      </li>
    </ul>
    <RouterLink to="/app/appointments" class="underline text-sm">{{
      t('notifications.inbox.allAppointments')
    }}</RouterLink>
  </section>
</template>
