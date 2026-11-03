<script setup lang="ts">
import { computed } from 'vue'

// Convierte texto plano con marcadores [[mark]]...[[/mark]] en fragmentos seguros
// sin recurrir a v-html ni arriesgar inyecciones HTML (PP-F3-FE-001).

const props = defineProps<{
  text?: string
}>()

interface Segment {
  text: string
  mark: boolean
}

const segments = computed<Segment[]>(() => {
  const raw = props.text ?? ''
  if (!raw) return []

  const result: Segment[] = []
  const parts = raw.split(/\[\[(\/?)mark\]\]/)

  let isMark = false
  for (const part of parts) {
    if (part === '') {
      isMark = true
      continue
    }
    if (part === '/') {
      isMark = false
      continue
    }
    result.push({ text: part, mark: isMark })
  }

  return result
})
</script>

<template>
  <span class="inline">
    <template v-for="(seg, idx) in segments" :key="idx">
      <mark
        v-if="seg.mark"
        class="rounded bg-amber-200 px-0.5 font-medium text-surface-900 dark:bg-amber-800 dark:text-surface-0"
        >{{ seg.text }}</mark
      >
      <span v-else>{{ seg.text }}</span>
    </template>
  </span>
</template>
