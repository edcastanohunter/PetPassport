<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PetListItem } from '@/shared/api/client'

// Tarjeta de mascota (FE-006 lista): foto/miniatura, nombre, especie, edad y
// badges. Fallecida se muestra en la sección "En memoria".
const props = defineProps<{ pet: PetListItem }>()
const { t, te } = useI18n()
const emit = defineEmits<{ open: [] }>()

const speciesLabel = computed(() => {
  const key = `pets.species.${props.pet.species}`
  return te(key) ? t(key) : props.pet.species
})

const age = computed(() => {
  if (!props.pet.birth_date) return null
  const b = new Date(`${props.pet.birth_date.date}T00:00:00Z`)
  const now = new Date()
  let months =
    (now.getUTCFullYear() - b.getUTCFullYear()) * 12 + (now.getUTCMonth() - b.getUTCMonth())
  if (now.getUTCDate() < b.getUTCDate()) months--
  if (months < 0) months = 0
  const years = Math.floor(months / 12)
  months -= years * 12
  return { years, months }
})
</script>

<template>
  <button
    type="button"
    class="flex w-full items-center gap-4 rounded-xl border border-surface-200 bg-white p-4 text-left shadow-sm transition hover:shadow-md dark:border-surface-700 dark:bg-surface-800"
    data-testid="pet-card"
    @click="emit('open')"
  >
    <span
      class="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-100 text-xl font-bold text-surface-500 dark:bg-surface-700"
      aria-hidden="true"
    >
      {{ pet.name.charAt(0).toUpperCase() }}
    </span>
    <span class="min-w-0 flex-1">
      <span class="flex items-center gap-2">
        <span class="truncate font-semibold">{{ pet.name }}</span>
        <span
          v-if="pet.status === 'deceased'"
          class="rounded-full bg-surface-200 px-2 py-0.5 text-xs text-surface-600 dark:bg-surface-700 dark:text-surface-300"
        >
          {{ t('pets.inMemoriam') }}
        </span>
      </span>
      <span class="block truncate text-sm text-surface-500">
        {{ speciesLabel }}<template v-if="pet.breed"> · {{ pet.breed }}</template>
        <template v-if="age"> · {{ t('pets.ageShort', age) }}</template>
      </span>
    </span>
    <span
      v-if="pet.active_links > 0"
      class="shrink-0 rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-800 dark:bg-green-900 dark:text-green-200"
    >
      {{ t('pets.linksBadge', { n: pet.active_links }) }}
    </span>
  </button>
</template>
