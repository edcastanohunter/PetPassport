<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePetStore } from './petStore'
import PetCard from './PetCard.vue'

// Lista de mascotas (FE-006, PRD-005/006): filtro activas/en memoria, CTA
// añadir. La proyección síncrona hace visible una mascota recién creada.
const { t } = useI18n()
const router = useRouter()
const store = usePetStore()

const error = ref(false)
const filter = computed({
  get: () => store.status,
  set: (s: 'active' | 'deceased' | 'all') => store.load(s).catch(() => (error.value = true)),
})

onMounted(() => store.load().catch(() => (error.value = true)))

const deceased = computed(() => store.items.filter((p) => p.status === 'deceased'))
const active = computed(() => store.items.filter((p) => p.status !== 'deceased'))

function open(id: string) {
  router.push({ name: 'pet-detail', params: { petId: id } })
}
</script>

<template>
  <section class="space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('pets.listTitle') }}</h1>
      <button
        type="button"
        class="rounded-lg bg-green-600 px-4 py-2 font-medium text-white shadow-sm transition hover:bg-green-700"
        data-testid="new-pet"
        @click="router.push({ name: 'pet-new' })"
      >
        + {{ t('pets.addPet') }}
      </button>
    </header>

    <p v-if="error" class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300">
      {{ t('common.errorGeneric') }}
    </p>

    <div class="flex gap-2" role="tablist" :aria-label="t('pets.listTitle')">
      <button
        v-for="s in ['all', 'active', 'deceased'] as const"
        :key="s"
        type="button"
        role="tab"
        :aria-selected="filter === s"
        class="rounded-full px-3 py-1.5 text-sm font-medium transition"
        :class="
          filter === s
            ? 'bg-surface-900 text-white dark:bg-surface-100 dark:text-surface-900'
            : 'bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300'
        "
        @click="filter = s"
      >
        {{ t(`pets.filter.${s}`) }}
      </button>
    </div>

    <p v-if="store.loading" class="text-surface-500">{{ t('common.loading') }}</p>

    <template v-else>
      <div v-if="active.length" class="space-y-3">
        <PetCard v-for="p in active" :key="p.id" :pet="p" @open="open(p.id)" />
      </div>
      <p
        v-else-if="filter !== 'deceased'"
        class="rounded-xl border border-dashed border-surface-300 p-8 text-center text-surface-500 dark:border-surface-600"
      >
        {{ t('pets.empty') }}
      </p>

      <div v-if="deceased.length && filter !== 'active'" class="space-y-3">
        <h2 class="text-lg font-semibold text-surface-600 dark:text-surface-300">
          {{ t('pets.inMemoriamTitle') }}
        </h2>
        <PetCard v-for="p in deceased" :key="p.id" :pet="p" @open="open(p.id)" />
      </div>
    </template>
  </section>
</template>
