<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, isApiError, type SharedRecord } from '@/shared/api/client'
import CardTable from './CardTable.vue'

// Enlace compartido de solo lectura (PP-F2-FE-013, PRD-014): sin sesión. Si el
// enlace tiene PIN se pide, y el token devuelto viaja en `X-PP-Share-Unlock`
// (en memoria; no se persiste). Mensajes distintos para caducado/revocado.
const { t, d } = useI18n()
const route = useRoute()
const token = computed(() => String(route.params.token))

type State = 'loading' | 'ready' | 'pin' | 'expired' | 'locked' | 'notFound' | 'error'
const state = ref<State>('loading')
const record = shallowRef<SharedRecord | null>(null)
const unlock = ref<string | undefined>()
const pin = ref('')
const pinError = ref(false)

async function load() {
  state.value = 'loading'
  const { data, error } = await api().GET('/share/{token}', {
    params: {
      path: { token: token.value },
      header: unlock.value ? { 'X-PP-Share-Unlock': unlock.value } : {},
    },
  })
  if (!error) {
    record.value = data ?? null
    state.value = 'ready'
    return
  }
  const code = isApiError(error) ? error.code : ''
  const status = isApiError(error) ? error.status : 0
  if (code === 'PIN_REQUIRED') state.value = 'pin'
  else if (code === 'SHARE_EXPIRED' || status === 410) state.value = 'expired'
  else if (code === 'SHARE_LOCKED') state.value = 'locked'
  else if (status === 404) state.value = 'notFound'
  else state.value = 'error'
}

async function submitPin() {
  pinError.value = false
  const { data, error } = await api().POST('/share/{token}/unlock', {
    params: { path: { token: token.value } },
    body: { pin: pin.value },
  })
  if (error) {
    const code = isApiError(error) ? error.code : ''
    if (code === 'SHARE_LOCKED') state.value = 'locked'
    else pinError.value = true
    return
  }
  unlock.value = data?.token
  pin.value = ''
  await load()
}
onMounted(load)
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-4 py-8" data-testid="shared-record">
    <h1 class="text-2xl font-bold">{{ t('card.shared.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>

    <form v-else-if="state === 'pin'" class="card space-y-3" @submit.prevent="submitPin">
      <label class="block text-sm">
        {{ t('card.shared.pinPrompt') }}
        <input
          v-model="pin"
          class="input mt-1 w-full"
          inputmode="numeric"
          autocomplete="off"
          maxlength="6"
          required
        />
      </label>
      <p v-if="pinError" class="text-sm text-red-700" role="alert">{{ t('errors.PIN_INVALID') }}</p>
      <button type="submit" class="btn-primary">{{ t('card.shared.unlock') }}</button>
    </form>

    <p
      v-else-if="state === 'expired'"
      class="rounded-lg bg-surface-200/40 p-6 text-center"
      role="alert"
    >
      {{ t('errors.SHARE_EXPIRED') }}
    </p>
    <p
      v-else-if="state === 'locked'"
      class="rounded-lg bg-surface-200/40 p-6 text-center"
      role="alert"
    >
      {{ t('errors.SHARE_LOCKED') }}
    </p>
    <p v-else-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('card.shared.notFound') }}
    </p>
    <p
      v-else-if="state === 'error'"
      class="rounded bg-red-50 p-3 text-sm text-red-700"
      role="alert"
    >
      {{ t('errors.GENERIC') }}
    </p>

    <template v-else-if="record">
      <div class="card">
        <p class="text-lg font-semibold">{{ record.pet.name }}</p>
        <p class="text-sm text-surface-500">
          {{ record.pet.species
          }}<template v-if="record.pet.breed"> · {{ record.pet.breed }}</template>
          <template v-if="record.pet.birth_date">
            · {{ d(record.pet.birth_date, 'short') }}</template
          >
          <template v-if="record.pet.microchip_masked">
            · {{ record.pet.microchip_masked }}</template
          >
        </p>
        <p class="text-xs text-surface-500">
          {{ t('card.shared.verifiedAt', { when: d(record.verified_at, 'long') }) }}
        </p>
      </div>
      <CardTable :card="record.card" />
    </template>
  </section>
</template>
