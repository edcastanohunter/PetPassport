<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Pet, type PetAlert } from '@/shared/api/client'

// Alertas del perfil (FE-005, DOM-013): alergia, condición crónica, conducta u
// otra, hasta 20 con 200 caracteres. Las edita el tutor y la clínica con
// vínculo; el guardado usa If-Match con la versión de la ficha.
const props = defineProps<{ pet: Pet; editable: boolean }>()
const emit = defineEmits<{ changed: [pet: Pet] }>()
const { t } = useI18n()

const KINDS = ['allergy', 'chronic_condition', 'behavior', 'other'] as const
const MAX_ALERTS = 20
const MAX_TEXT = 200

const draft = ref<PetAlert[]>([])
const editing = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)

watch(
  () => props.pet.alerts,
  (alerts) => {
    draft.value = (alerts ?? []).map((a) => ({ ...a }))
  },
  { immediate: true },
)

function add() {
  if (draft.value.length < MAX_ALERTS) draft.value.push({ kind: 'allergy', text: '' })
}

function remove(i: number) {
  draft.value.splice(i, 1)
}

function cancel() {
  draft.value = (props.pet.alerts ?? []).map((a) => ({ ...a }))
  editing.value = false
  error.value = null
}

async function save() {
  if (busy.value) return
  const cleaned = draft.value
    .map((a) => ({ kind: a.kind, text: a.text.trim() }))
    .filter((a) => a.text !== '')
  busy.value = true
  error.value = null
  try {
    const { data, error: apiError } = await api().PUT('/pets/{petId}/alerts', {
      params: { path: { petId: props.pet.id }, header: { 'If-Match': props.pet.version } },
      body: { alerts: cleaned },
    })
    if (apiError) throw apiError
    editing.value = false
    if (data) emit('changed', data)
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-2" data-testid="pet-alerts">
    <div class="flex items-center justify-between">
      <h2 class="text-lg font-semibold">{{ t('pets.alerts.title') }}</h2>
      <button
        v-if="editable && !editing"
        type="button"
        class="btn-secondary"
        data-testid="alerts-edit"
        @click="editing = true"
      >
        {{ t('pets.alerts.edit') }}
      </button>
    </div>

    <template v-if="!editing">
      <p v-if="!(pet.alerts ?? []).length" class="text-sm text-surface-500">
        {{ t('pets.alerts.none') }}
      </p>
      <ul v-else class="space-y-1">
        <li v-for="(a, i) in pet.alerts" :key="i" class="text-sm" data-testid="alert-item">
          <span class="rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-900">
            {{ t(`pets.alerts.kind.${a.kind}`) }}
          </span>
          {{ a.text }}
        </li>
      </ul>
    </template>

    <form v-else class="space-y-2" @submit.prevent="save">
      <div v-for="(a, i) in draft" :key="i" class="flex gap-2" data-testid="alert-row">
        <label class="sr-only" :for="`alert-kind-${i}`">{{ t('pets.alerts.kindLabel') }}</label>
        <select :id="`alert-kind-${i}`" v-model="a.kind" class="input w-48">
          <option v-for="k in KINDS" :key="k" :value="k">{{ t(`pets.alerts.kind.${k}`) }}</option>
        </select>
        <label class="sr-only" :for="`alert-text-${i}`">{{ t('pets.alerts.textLabel') }}</label>
        <input
          :id="`alert-text-${i}`"
          v-model="a.text"
          class="input"
          :maxlength="MAX_TEXT"
          required
        />
        <button type="button" class="btn-secondary" @click="remove(i)">
          {{ t('common.delete') }}
        </button>
      </div>
      <button
        v-if="draft.length < MAX_ALERTS"
        type="button"
        class="btn-secondary"
        data-testid="alerts-add"
        @click="add"
      >
        {{ t('pets.alerts.add') }}
      </button>
      <p v-if="error" role="alert" class="text-sm text-red-700">{{ error }}</p>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary" :disabled="busy" data-testid="alerts-save">
          {{ t('common.save') }}
        </button>
        <button type="button" class="btn-secondary" @click="cancel">
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>
  </section>
</template>
