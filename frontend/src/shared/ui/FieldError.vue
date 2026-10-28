<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'

// Mensaje de error por campo a partir del código de `errors[]` (FE-003).
// `field` es el id del control (como `<label for>`): el mensaje se asocia con
// aria-describedby y el control se marca aria-invalid (FE-010, WCAG 3.3.1).
// `message` sustituye al texto por código cuando la pantalla tiene uno propio.
const props = defineProps<{ code?: string; message?: string; field?: string }>()
const { t } = useI18n()

const errorId = computed(() => (props.field ? `${props.field}-error` : undefined))

function control(): HTMLElement | null {
  if (!props.field || typeof document === 'undefined') return null
  return document.getElementById(props.field)
}

function describedBy(el: HTMLElement, id: string, on: boolean) {
  const ids = new Set((el.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean))
  if (on) ids.add(id)
  else ids.delete(id)
  if (ids.size) el.setAttribute('aria-describedby', [...ids].join(' '))
  else el.removeAttribute('aria-describedby')
}

function sync(active = !!(props.code || props.message)) {
  const el = control()
  if (!el || !errorId.value) return
  describedBy(el, errorId.value, active)
  if (active) el.setAttribute('aria-invalid', 'true')
  else el.removeAttribute('aria-invalid')
}

onMounted(() => sync())
watch(
  () => [props.code, props.message],
  () => sync(),
  { flush: 'post' },
)
onBeforeUnmount(() => sync(false))
</script>

<template>
  <p
    v-if="code || message"
    :id="errorId"
    class="mt-1 text-sm text-red-700 dark:text-red-400"
    role="alert"
  >
    {{ message ?? t(`fieldErrors.${code}`, t('fieldErrors.INVALID')) }}
  </p>
</template>
