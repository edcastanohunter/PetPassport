<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  api,
  type Appointment,
  type AppointmentType,
  type AvailabilitySlot,
  type PatientRow,
} from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import SlotPicker from './SlotPicker.vue'
import { createOrgAppointment } from './scheduleApi'
import { isConflict, scheduleErrorKey } from './scheduleFormat'

// Crear una cita confirmada desde la clínica (PRD-017): paciente vinculado,
// tipo y una franja libre de un veterinario con agenda activa.
const props = defineProps<{
  orgId: string
  professionals: Record<string, string>
  tz?: string
  petId?: string
}>()
const emit = defineEmits<{ created: [a: Appointment]; cancel: [] }>()
const { t, te } = useI18n()
const cmd = useCommand()

const types: AppointmentType[] = [
  'consultation',
  'follow_up',
  'vaccination',
  'surgery',
  'procedure',
  'other',
]
const patients = ref<PatientRow[]>([])
const petId = ref(props.petId ?? '')
const type = ref<AppointmentType>('consultation')
const reason = ref('')
const picked = ref<AvailabilitySlot[]>([])
const busy = ref(false)
const error = ref<string | null>(null)
const conflict = ref<string | null>(null)
const pickerKey = ref(0)

onMounted(async () => {
  if (props.petId) return
  const { data } = await api().GET('/organizations/{orgId}/patients', {
    params: { path: { orgId: props.orgId }, query: { limit: 100 } },
  })
  patients.value = data?.data ?? []
})

async function submit() {
  const slot = picked.value[0]
  if (!slot || !petId.value || busy.value) return
  busy.value = true
  error.value = conflict.value = null
  try {
    const a = await createOrgAppointment(props.orgId, cmd.key.value, {
      pet_id: petId.value,
      professional_id: slot.professional_id,
      type: type.value,
      ...(reason.value ? { reason: reason.value } : {}),
      start: slot.start,
      end: slot.end,
    })
    cmd.renew()
    emit('created', a)
  } catch (e) {
    if (isConflict(e)) {
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
</script>

<template>
  <form class="card space-y-3" data-testid="new-appointment" @submit.prevent="submit">
    <h2 class="text-lg font-semibold">{{ t('scheduling.new.title') }}</h2>
    <p v-if="error" class="rounded bg-red-50 p-2 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>

    <label v-if="!props.petId" class="block text-sm">
      {{ t('scheduling.new.patient') }}
      <select v-model="petId" class="input mt-1" required data-testid="new-pet">
        <option value="" disabled>{{ t('common.select') }}</option>
        <option v-for="p in patients" :key="p.pet.id" :value="p.pet.id">
          {{ p.pet.name }} · {{ p.guardian.display_name }}
        </option>
      </select>
    </label>

    <label class="block text-sm">
      {{ t('scheduling.new.type') }}
      <select v-model="type" class="input mt-1" @change="picked = []">
        <option v-for="ty in types" :key="ty" :value="ty">{{ t(`scheduling.type.${ty}`) }}</option>
      </select>
    </label>

    <label class="block text-sm">
      {{ t('scheduling.new.reason') }}
      <input v-model="reason" class="input mt-1" maxlength="300" />
    </label>

    <SlotPicker
      :key="pickerKey + type"
      v-model="picked"
      :org-id="orgId"
      :type="type"
      :tz="tz"
      :professionals="professionals"
      :conflict="conflict"
    />

    <div class="flex gap-2">
      <button type="submit" class="btn-primary" :disabled="busy || !picked.length || !petId">
        {{ busy ? t('common.saving') : t('scheduling.new.submit') }}
      </button>
      <button type="button" class="btn-secondary" @click="emit('cancel')">
        {{ t('common.cancel') }}
      </button>
    </div>
  </form>
</template>
