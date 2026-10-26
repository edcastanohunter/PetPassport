<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type TransferPreview } from '@/shared/api/client'

// Aceptar transferencia (FE-006, PRD-010): previsualización y aceptar; 404
// como "enlace inválido o caducado"; email distinto → mensaje claro.
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()

const token = computed(() => String(route.params.token))
const preview = ref<TransferPreview | null>(null)
const state = ref<'loading' | 'ready' | 'invalid' | 'mismatch' | 'error'>('loading')
const busy = ref(false)

onMounted(async () => {
  try {
    const { data, error } = await api().GET('/pet-transfers/{token}', {
      params: { path: { token: token.value } },
    })
    if (error) throw error
    preview.value = data ?? null
    state.value = 'ready'
  } catch (e) {
    state.value = e instanceof ApiError && e.status === 404 ? 'invalid' : 'error'
  }
})

const species = computed(() => {
  if (!preview.value) return ''
  const key = `pets.species.${preview.value.species}`
  return te(key) ? t(key) : preview.value.species
})

async function accept() {
  busy.value = true
  try {
    const { data, error } = await api().POST('/pet-transfers/{token}/accept', {
      params: { path: { token: token.value } },
    })
    if (error) throw error
    if (data) await router.replace({ name: 'pet-detail', params: { petId: data.id } })
  } catch (e) {
    if (e instanceof ApiError && e.code === 'TRANSFER_EMAIL_MISMATCH') state.value = 'mismatch'
    else if (e instanceof ApiError && e.status === 404) state.value = 'invalid'
    else state.value = 'error'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="mx-auto max-w-md space-y-4 py-8">
    <h1 class="text-2xl font-bold">{{ t('pets.transfer.acceptTitle') }}</h1>
    <p v-if="state === 'loading'" class="text-surface-500">{{ t('common.loading') }}</p>
    <p
      v-else-if="state === 'invalid'"
      class="rounded-lg bg-surface-200/40 p-4"
      data-testid="transfer-invalid"
    >
      {{ t('pets.transfer.invalid') }}
    </p>
    <p v-else-if="state === 'error'" class="rounded-lg bg-red-50 p-4 text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <template v-if="preview && (state === 'ready' || state === 'mismatch')">
      <div class="card">
        <p class="text-lg font-semibold">{{ preview.pet_name }}</p>
        <p class="text-sm text-surface-500">{{ species }}</p>
        <p class="mt-2">
          {{ t('pets.transfer.from', { name: preview.from_guardian_display_name }) }}
        </p>
        <p class="text-sm text-surface-500">
          {{ t('orgs.invitations.expires', { date: d(new Date(preview.expires_at), 'short') }) }}
        </p>
      </div>
      <p
        v-if="state === 'mismatch'"
        class="rounded-lg bg-amber-50 p-4 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
        role="alert"
      >
        {{ t('errors.TRANSFER_EMAIL_MISMATCH') }}
      </p>
      <button
        v-else
        type="button"
        class="btn-primary"
        :disabled="busy"
        data-testid="accept-transfer"
        @click="accept"
      >
        {{ t('pets.transfer.accept') }}
      </button>
    </template>
  </section>
</template>
