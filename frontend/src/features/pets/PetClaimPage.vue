<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '@/features/auth/sessionStore'

interface ClaimPreview {
  pet: {
    name: string
    species: string
    breed?: string
    sex?: string
    photo_url?: string
  }
  organization: {
    display_name: string
  }
  contact_masked: string
  existing_match?: {
    pet_id: string
    name: string
  }
}

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const token = computed(() => String(route.params.token))
const preview = ref<ClaimPreview | null>(null)
const state = ref<'loading' | 'ready' | 'invalid' | 'error' | 'success'>('loading')
const busy = ref(false)
const errorMessage = ref<string | null>(null)

const isAuthenticated = computed(() => session.isAuthenticated)

const speciesLabel = computed(() => {
  if (!preview.value?.pet?.species) return ''
  const key = `pets.species.${preview.value.pet.species}`
  return te(key) ? t(key) : preview.value.pet.species
})

async function fetchPreview() {
  state.value = 'loading'
  errorMessage.value = null
  try {
    const headers: Record<string, string> = {}
    if (session.accessToken) {
      headers['Authorization'] = `Bearer ${session.accessToken}`
    }
    const res = await fetch(`/api/v1/pet-claims/${token.value}`, { headers })
    if (res.status === 404 || res.status === 410) {
      state.value = 'invalid'
      return
    }
    if (!res.ok) {
      state.value = 'error'
      return
    }
    preview.value = await res.json()
    state.value = 'ready'
  } catch {
    state.value = 'error'
  }
}

onMounted(() => {
  void fetchPreview()
})

async function acceptClaim(mergeIntoPetId?: string) {
  if (!isAuthenticated.value) return
  busy.value = true
  errorMessage.value = null
  try {
    const res = await fetch(`/api/v1/pet-claims/${token.value}/accept`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${session.accessToken}`,
      },
      body: JSON.stringify({
        merge_into_pet_id: mergeIntoPetId || undefined,
      }),
    })
    if (!res.ok) {
      const prob = await res.json().catch(() => null)
      throw new Error(prob?.detail || prob?.title || t('common.errorGeneric'))
    }
    const data = await res.json()
    state.value = 'success'
    const targetId = mergeIntoPetId || data.id || data.pet_id
    if (targetId) {
      await router.replace({ name: 'pet-detail', params: { petId: targetId } })
    } else {
      await router.replace({ name: 'pets' })
    }
  } catch (err: unknown) {
    errorMessage.value = err instanceof Error ? err.message : t('common.errorGeneric')
  } finally {
    busy.value = false
  }
}

function login() {
  session.login({ returnTo: route.fullPath })
}
</script>

<template>
  <div class="mx-auto max-w-xl space-y-6 py-8 px-4">
    <div v-if="state === 'loading'" class="card p-8 text-center text-surface-500">
      <span class="pi pi-spinner animate-spin text-2xl mr-2" />
      {{ t('common.loading') }}
    </div>

    <div v-else-if="state === 'invalid'" class="card p-8 text-center space-y-3" role="alert">
      <span class="pi pi-exclamation-triangle text-3xl text-amber-500" />
      <h1 class="text-xl font-bold">{{ t('pets.claim.invalidTitle') }}</h1>
      <p class="text-sm text-surface-600 dark:text-surface-400">
        {{ t('pets.claim.expired') }}
      </p>
      <RouterLink to="/app" class="btn-primary inline-block text-sm">
        {{ t('common.back') }}
      </RouterLink>
    </div>

    <div v-else-if="state === 'error'" class="card p-8 text-center space-y-3" role="alert">
      <span class="pi pi-times-circle text-3xl text-red-500" />
      <h1 class="text-xl font-bold">{{ t('common.errorGeneric') }}</h1>
      <button type="button" class="btn-secondary text-sm" @click="fetchPreview">
        {{ t('common.retry') }}
      </button>
    </div>

    <div v-else-if="state === 'ready' && preview" class="space-y-6">
      <header class="text-center space-y-1">
        <h1 class="text-2xl font-bold">{{ t('pets.claim.title') }}</h1>
        <p class="text-sm text-surface-500">{{ t('pets.claim.subtitle') }}</p>
      </header>

      <div
        v-if="errorMessage"
        class="rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        role="alert"
      >
        {{ errorMessage }}
      </div>

      <!-- Tarjeta de la mascota provisional -->
      <section class="card p-6 space-y-4">
        <div class="flex items-center gap-4">
          <div
            class="flex h-16 w-16 items-center justify-center rounded-full bg-primary-100 text-primary-700 text-2xl font-bold dark:bg-primary-950 dark:text-primary-300"
          >
            {{ preview.pet.name ? preview.pet.name.charAt(0).toUpperCase() : '🐾' }}
          </div>
          <div>
            <h2 class="text-xl font-bold" data-testid="claim-pet-name">{{ preview.pet.name }}</h2>
            <p class="text-sm text-surface-500">
              {{ speciesLabel }}
              <template v-if="preview.pet.breed"> · {{ preview.pet.breed }}</template>
            </p>
          </div>
        </div>

        <div class="border-t border-surface-200 pt-3 text-sm space-y-1 dark:border-surface-800">
          <div class="flex justify-between">
            <span class="text-surface-500">{{ t('pets.claim.clinicLabel') }}</span>
            <span class="font-medium">{{ preview.organization.display_name }}</span>
          </div>
          <div v-if="preview.contact_masked" class="flex justify-between">
            <span class="text-surface-500">{{ t('pets.claim.contactLabel') }}</span>
            <span class="font-mono text-xs">{{ preview.contact_masked }}</span>
          </div>
        </div>
      </section>

      <!-- Detección de duplicado por microchip -->
      <section
        v-if="preview.existing_match"
        class="rounded-xl border border-amber-300 bg-amber-50 p-4 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100 space-y-3"
        role="alert"
        data-testid="microchip-duplicate-alert"
      >
        <div class="flex items-start gap-2">
          <span class="pi pi-exclamation-circle text-xl flex-shrink-0 mt-0.5" />
          <div>
            <h3 class="font-semibold text-base">
              {{ t('pets.claim.duplicateQuestion', { name: preview.existing_match.name }) }}
            </h3>
            <p class="text-sm mt-1">
              {{ t('pets.claim.duplicateAlert') }}
            </p>
          </div>
        </div>

        <div v-if="isAuthenticated" class="flex flex-wrap gap-2 pt-2">
          <button
            type="button"
            class="btn-primary text-sm"
            :disabled="busy"
            data-testid="btn-merge-claim"
            @click="acceptClaim(preview.existing_match?.pet_id)"
          >
            {{ t('pets.claim.mergeButton', { name: preview.existing_match.name }) }}
          </button>
          <button
            type="button"
            class="btn-secondary text-sm"
            :disabled="busy"
            data-testid="btn-separate-claim"
            @click="acceptClaim()"
          >
            {{ t('pets.claim.separateButton') }}
          </button>
        </div>
      </section>

      <!-- Caso normal sin duplicado -->
      <div v-else-if="isAuthenticated" class="flex justify-end gap-3">
        <button
          type="button"
          class="btn-primary w-full text-base py-2.5"
          :disabled="busy"
          data-testid="btn-accept-claim"
          @click="acceptClaim()"
        >
          {{ t('pets.claim.acceptButton') }}
        </button>
      </div>

      <!-- Aviso para usuarios no autenticados -->
      <div v-if="!isAuthenticated" class="card p-6 text-center space-y-3">
        <p class="text-sm text-surface-600 dark:text-surface-400">
          {{ t('pets.claim.loginPrompt') }}
        </p>
        <button type="button" class="btn-primary text-sm" @click="login">
          {{ t('nav.login') }}
        </button>
      </div>
    </div>
  </div>
</template>
