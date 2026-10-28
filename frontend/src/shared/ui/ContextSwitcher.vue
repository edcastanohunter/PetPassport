<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

// Selector de contexto (FE-004) usable desde 360 px (FE-010): en línea desde
// `sm`; por debajo, botón de menú con aria-expanded que despliega la lista y se
// cierra con Escape o al navegar.
defineProps<{ items: { to: string; label: string }[] }>()
const { t } = useI18n()
const route = useRoute()
const open = ref(false)
const toggle = ref<HTMLButtonElement | null>(null)

watch(
  () => route.fullPath,
  () => (open.value = false),
)

function close() {
  if (!open.value) return
  open.value = false
  toggle.value?.focus()
}
</script>

<template>
  <div class="relative" @keydown.esc="close">
    <button
      ref="toggle"
      type="button"
      class="tap inline-flex items-center gap-1 rounded px-2 text-sm sm:hidden"
      :aria-expanded="open"
      aria-controls="context-switcher"
      data-testid="context-menu-toggle"
      @click="open = !open"
    >
      <span class="pi pi-bars" aria-hidden="true" />
      {{ t('nav.menu') }}
    </button>
    <ul
      id="context-switcher"
      :class="[
        open ? 'flex' : 'hidden',
        'absolute left-0 top-full z-20 mt-2 min-w-48 flex-col gap-1 rounded border border-surface-200 bg-surface-0 p-2 text-sm shadow dark:border-surface-800 dark:bg-surface-900',
        'sm:static sm:mt-0 sm:flex sm:min-w-0 sm:flex-row sm:gap-3 sm:border-0 sm:bg-transparent sm:p-0 sm:shadow-none dark:sm:bg-transparent',
      ]"
      data-testid="context-switcher"
    >
      <li v-for="c in items" :key="c.to">
        <RouterLink
          :to="c.to"
          class="tap flex items-center rounded px-2 hover:underline sm:px-0"
          active-class="font-semibold"
          >{{ c.label }}</RouterLink
        >
      </li>
    </ul>
  </div>
</template>
