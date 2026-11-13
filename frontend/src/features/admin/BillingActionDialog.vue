<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MIN_REASON } from './adminStore'

// Acción de SuperAdmin sobre facturación (PP-F5-FE-005, PRD-011): campos propios
// de la acción (fecha, importe, plan…) y motivo obligatorio de ≥ 10 caracteres.
// El backend repite las validaciones y deja la entrada en el audit log.
export type BillingField =
  | 'effective_from'
  | 'until'
  | 'amount'
  | 'plan_code'
  | 'seats'
  | 'cycle'
  | 'reference'
  | 'resolution'

const props = defineProps<{
  title: string
  target: string
  fields: readonly BillingField[]
  plans?: readonly { code: string; name: string }[]
  busy?: boolean
}>()
const emit = defineEmits<{
  confirm: [values: Record<string, string>, reason: string]
  cancel: []
}>()
const { t } = useI18n()

// Los inputs numéricos entregan number: se normaliza todo a texto.
const values = reactive<Record<string, string | number>>({ cycle: 'monthly' })
const text = (f: string) => String(values[f] ?? '').trim()
const reason = ref('')
const touched = ref(false)
const dialog = ref<HTMLElement | null>(null)

const reasonOk = computed(() => reason.value.trim().length >= MIN_REASON)
const fieldsOk = computed(() =>
  props.fields.every((f) => {
    const v = text(f)
    if (f === 'seats') return v === '' || Number(v) >= 1
    if (f === 'amount') return Number(v) > 0
    return v !== ''
  }),
)

onMounted(async () => {
  await nextTick()
  dialog.value?.querySelector<HTMLElement>('input, select, textarea')?.focus()
})

function submit() {
  touched.value = true
  if (!reasonOk.value || !fieldsOk.value || props.busy) return
  const out: Record<string, string> = {}
  for (const f of props.fields) if (text(f) !== '') out[f] = text(f)
  emit('confirm', out, reason.value.trim())
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
    <form
      ref="dialog"
      class="card w-full max-w-lg space-y-3"
      role="dialog"
      aria-modal="true"
      aria-labelledby="billing-action-title"
      novalidate
      data-testid="billing-action-dialog"
      @submit.prevent="submit"
      @keydown.esc="emit('cancel')"
    >
      <h2 id="billing-action-title" class="text-lg font-semibold">{{ title }}</h2>
      <p class="text-sm text-surface-500">{{ target }}</p>

      <div v-for="f in fields" :key="f">
        <label class="label" :for="`ba-${f}`">{{ t(`admin.billing.field.${f}`) }}</label>
        <select v-if="f === 'plan_code'" :id="`ba-${f}`" v-model="values[f]" class="input">
          <option v-for="p in plans ?? []" :key="p.code" :value="p.code">{{ p.name }}</option>
        </select>
        <select v-else-if="f === 'cycle'" :id="`ba-${f}`" v-model="values[f]" class="input">
          <option value="monthly">{{ t('billing.cycle.monthly') }}</option>
          <option value="annual">{{ t('billing.cycle.annual') }}</option>
        </select>
        <input
          v-else
          :id="`ba-${f}`"
          v-model="values[f]"
          class="input"
          :type="
            f === 'until' || f === 'effective_from'
              ? 'date'
              : f === 'amount' || f === 'seats'
                ? 'number'
                : 'text'
          "
          :min="f === 'amount' ? '0' : f === 'seats' ? '1' : undefined"
          :step="f === 'amount' ? 'any' : undefined"
          maxlength="120"
          :data-testid="`ba-${f}`"
        />
      </div>

      <label class="label" for="ba-reason">{{ t('admin.reason.label') }}</label>
      <textarea
        id="ba-reason"
        v-model="reason"
        class="input min-h-20"
        maxlength="500"
        :aria-invalid="touched && !reasonOk"
        data-testid="ba-reason"
      />
      <p
        class="text-sm"
        :class="touched && !reasonOk ? 'text-red-600 dark:text-red-400' : 'text-surface-500'"
      >
        {{ t('admin.reason.hint', { min: MIN_REASON, n: reason.trim().length }) }}
      </p>
      <p v-if="touched && !fieldsOk" class="text-sm text-red-600 dark:text-red-400" role="alert">
        {{ t('admin.billing.fieldsRequired') }}
      </p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" class="btn-primary" :disabled="busy" data-testid="ba-confirm">
          {{ t('admin.reason.confirm') }}
        </button>
      </div>
    </form>
  </div>
</template>
