<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MIN_REASON } from './adminStore'

// Modal de motivo (FE-009, PRD-017/018): toda acción admin exige motivo de
// ≥ 10 caracteres, salvo verificar (opcional, pero ≥ 10 si se escribe). El
// backend repite la validación (REASON_REQUIRED). Escape cancela; el foco
// entra en el textarea y no sale del diálogo.
const props = defineProps<{ title: string; target: string; optional?: boolean; busy?: boolean }>()
const emit = defineEmits<{ confirm: [reason: string]; cancel: [] }>()
const { t } = useI18n()

const reason = ref('')
const touched = ref(false)
const field = ref<HTMLTextAreaElement | null>(null)
const dialog = ref<HTMLElement | null>(null)

const length = computed(() => reason.value.trim().length)
const valid = computed(() =>
  props.optional ? length.value === 0 || length.value >= MIN_REASON : length.value >= MIN_REASON,
)

onMounted(async () => {
  await nextTick()
  field.value?.focus()
})

function submit() {
  touched.value = true
  if (!valid.value || props.busy) return
  emit('confirm', reason.value.trim())
}

// Trampa de foco mínima: Tab en el último elemento vuelve al primero y viceversa.
function trap(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    emit('cancel')
    return
  }
  if (e.key !== 'Tab' || !dialog.value) return
  const focusable = dialog.value.querySelectorAll<HTMLElement>('textarea, button:not([disabled])')
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault()
    last?.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first?.focus()
  }
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
    <form
      ref="dialog"
      class="card w-full max-w-lg space-y-3"
      role="dialog"
      aria-modal="true"
      aria-labelledby="reason-title"
      novalidate
      data-testid="reason-dialog"
      @submit.prevent="submit"
      @keydown="trap"
    >
      <h2 id="reason-title" class="text-lg font-semibold">{{ title }}</h2>
      <p class="text-sm text-surface-500">{{ target }}</p>
      <label class="label" for="admin-reason">
        {{ optional ? t('admin.reason.labelOptional') : t('admin.reason.label') }}
      </label>
      <textarea
        id="admin-reason"
        ref="field"
        v-model="reason"
        class="input min-h-24"
        maxlength="500"
        :aria-invalid="touched && !valid"
        aria-describedby="admin-reason-hint"
      />
      <p
        id="admin-reason-hint"
        class="text-sm"
        :class="touched && !valid ? 'text-red-600 dark:text-red-400' : 'text-surface-500'"
      >
        {{ t('admin.reason.hint', { min: MIN_REASON, n: length }) }}
      </p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" class="btn-primary" :disabled="busy" data-testid="confirm-reason">
          {{ t('admin.reason.confirm') }}
        </button>
      </div>
    </form>
  </div>
</template>
