<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Pet } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

// Ficha de paciente (FE-008, PRD-016): solo lectura, con contacto del tutor
// (pet:guardian_contact:read vía vínculo activo). Historia clínica en F2.
const { t, te } = useI18n()
const route = useRoute()
const session = useSessionStore()

const orgId = computed(() => String(route.params.orgId))
const petId = computed(() => String(route.params.petId))
const suspended = computed(() => session.membershipFor(orgId.value)?.org_status === 'suspended')
const pet = ref<Pet | null>(null)
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')

onMounted(async () => {
  if (suspended.value) return
  try {
    const { data, error } = await api().GET('/organizations/{orgId}/patients/{petId}', {
      params: { path: { orgId: orgId.value, petId: petId.value } },
    })
    if (error) throw error
    pet.value = data ?? null
    state.value = 'ready'
  } catch (e) {
    state.value = e instanceof ApiError && e.status === 404 ? 'notFound' : 'error'
  }
})

const speciesLabel = computed(() => {
  if (!pet.value) return ''
  const key = `pets.species.${pet.value.species}`
  return te(key) ? t(key) : pet.value.species
})
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-6">
    <RouterLink :to="{ name: 'org-patients', params: { orgId } }" class="text-sm underline">
      ← {{ t('orgs.nav.patients') }}
    </RouterLink>

    <p
      v-if="suspended"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ t('errors.ORGANIZATION_SUSPENDED') }}
    </p>
    <p v-else-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('pets.notFound') }}
    </p>
    <p v-else-if="state === 'error'" class="rounded-lg bg-red-50 p-4 text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>

    <template v-if="pet">
      <header class="flex items-center gap-4">
        <img
          v-if="pet.photo"
          :src="pet.photo.url"
          :alt="t('pets.photoAlt', { name: pet.name })"
          class="h-20 w-20 rounded-full object-cover"
        />
        <div>
          <h1 class="text-2xl font-bold">{{ pet.name }}</h1>
          <p class="text-sm text-surface-500">
            {{ speciesLabel }}<template v-if="pet.breed"> · {{ pet.breed }}</template>
            <template v-if="pet.status === 'deceased'"> · {{ t('pets.inMemoriam') }}</template>
          </p>
        </div>
      </header>

      <dl class="card grid grid-cols-2 gap-4">
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.form.sex') }}</dt>
          <dd>{{ t(`pets.sex.${pet.sex}`) }}</dd>
        </div>
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.form.neutered') }}</dt>
          <dd>{{ t(`pets.neutered.${pet.neutered}`) }}</dd>
        </div>
        <div v-if="pet.birth_date">
          <dt class="text-sm text-surface-500">{{ t('pets.form.birthDate') }}</dt>
          <dd>
            {{ pet.birth_date.date }}
            <span v-if="pet.birth_date.approximate">({{ t('pets.approximate') }})</span>
            <span v-if="pet.age"> · {{ t('pets.ageShort', pet.age) }}</span>
          </dd>
        </div>
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.microchip') }}</dt>
          <dd class="font-mono">{{ pet.microchip || t('pets.noMicrochip') }}</dd>
        </div>
        <div v-if="pet.markings" class="col-span-2">
          <dt class="text-sm text-surface-500">{{ t('pets.form.markings') }}</dt>
          <dd>{{ pet.markings }}</dd>
        </div>
      </dl>

      <div v-if="pet.guardian_contact" class="card space-y-1" data-testid="guardian-contact">
        <h2 class="font-semibold">{{ t('orgs.patients.guardianContact') }}</h2>
        <p>{{ pet.guardian_contact.display_name }}</p>
        <p>
          <a :href="`mailto:${pet.guardian_contact.email}`" class="underline">{{
            pet.guardian_contact.email
          }}</a>
        </p>
        <p v-if="pet.guardian_contact.phone">
          <a :href="`tel:${pet.guardian_contact.phone}`" class="underline">{{
            pet.guardian_contact.phone
          }}</a>
        </p>
      </div>

      <div
        class="rounded-xl border border-dashed border-surface-200 p-6 text-center text-surface-500 dark:border-surface-800"
      >
        <h2 class="font-semibold">{{ t('orgs.patients.history') }}</h2>
        <p class="text-sm">{{ t('orgs.patients.historySoon') }}</p>
      </div>
    </template>
  </section>
</template>
