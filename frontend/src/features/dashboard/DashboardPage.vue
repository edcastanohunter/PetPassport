<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSessionStore } from '@/features/auth/sessionStore'
import { usePetStore } from '@/features/pets/petStore'
import PetCard from '@/features/pets/PetCard.vue'

// Dashboard del tutor (/app): resumen de mascotas con acceso a la lista (FE-006).
const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()
const pets = usePetStore()

onMounted(() => pets.load('active').catch(() => undefined))
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-bold">
      {{ t('dashboard.title', { name: session.me?.display_name ?? '' }) }}
    </h1>
    <p class="text-surface-500">{{ t('dashboard.petsIntro') }}</p>

    <div v-if="pets.items.length" class="space-y-3">
      <PetCard
        v-for="p in pets.items.slice(0, 3)"
        :key="p.id"
        :pet="p"
        @open="router.push({ name: 'pet-detail', params: { petId: p.id } })"
      />
    </div>

    <div class="flex gap-3">
      <button
        type="button"
        class="rounded-lg border border-surface-300 px-4 py-2 font-medium dark:border-surface-600"
        data-testid="go-pets"
        @click="router.push({ name: 'pets' })"
      >
        {{ t('pets.listTitle') }}
      </button>
      <button
        type="button"
        class="rounded-lg bg-green-600 px-4 py-2 font-medium text-white"
        @click="router.push({ name: 'pet-new' })"
      >
        + {{ t('pets.addPet') }}
      </button>
    </div>
  </section>
</template>
