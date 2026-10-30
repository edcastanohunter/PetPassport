<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Appointment } from '@/shared/api/client'
import AppointmentDetail from './AppointmentDetail.vue'
import NewAppointmentForm from './NewAppointmentForm.vue'
import { listOrgAppointments } from './scheduleApi'
import { fmtDateTime, scheduleErrorKey } from './scheduleFormat'

// Pestaña «Citas» de la ficha del paciente (FE-002): citas de la mascota en
// esta clínica con sus acciones, y alta de una cita nueva.
const props = defineProps<{ orgId: string; petId: string; canManage: boolean }>()
const { t, te, locale } = useI18n()

const items = ref<Appointment[]>([])
const selected = ref<Appointment | null>(null)
const creating = ref(false)
const error = ref<string | null>(null)

async function load() {
  try {
    items.value = (await listOrgAppointments(props.orgId, { pet_id: props.petId })).data
  } catch (e) {
    error.value = scheduleErrorKey(e, te)
  }
}
onMounted(load)

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
  <div class="space-y-4" data-testid="patient-appointments">
    <p v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>
    <button v-if="canManage && !creating" type="button" class="btn-primary" @click="startCreate">
      {{ t('scheduling.new.title') }}
    </button>
    <NewAppointmentForm
      v-if="creating"
      :org-id="orgId"
      :pet-id="petId"
      :professionals="{}"
      @created="changed"
      @cancel="creating = false"
    />
    <p v-if="items.length === 0 && !creating" class="text-sm text-surface-500">
      {{ t('scheduling.patient.empty') }}
    </p>
    <ul class="space-y-1">
      <li v-for="a in items" :key="a.id">
        <button
          type="button"
          class="flex w-full justify-between rounded border px-3 py-2 text-left text-sm"
          @click="select(a)"
        >
          <span
            >{{
              a.start ? fmtDateTime(a.start, a.time_zone, locale) : t('scheduling.patient.noSlot')
            }}
            · {{ t(`scheduling.type.${a.type}`) }}</span
          >
          <span class="text-xs">{{ t(`scheduling.status.${a.status}`) }}</span>
        </button>
      </li>
    </ul>
    <AppointmentDetail
      v-if="selected && canManage"
      :key="selected.id + selected.version"
      :appointment="selected"
      :org-id="orgId"
      :professionals="{}"
      @changed="changed"
    />
  </div>
</template>
