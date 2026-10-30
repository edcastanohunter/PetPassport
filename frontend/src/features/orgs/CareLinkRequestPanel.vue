<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, type CareLinkRequestItem, type PetLookupResult } from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import { listCareLinkRequests, lookupPets, requestCareLink } from './careLinkApi'

// Solicitar acceso a una mascota (FE-008, PRD-020): la clínica busca por
// microchip o email del tutor, ve un resultado mínimo (tutor enmascarado),
// escribe un mensaje opcional y envía la solicitud. Debajo, sus solicitudes
// con estado y caducidad. El tutor decide desde su bandeja.
const props = defineProps<{ orgId: string }>()
const { t, d } = useI18n()
const { key, renew } = useCommand()

const MESSAGE_MAX = 300
const mode = ref<'microchip' | 'email'>('microchip')
const term = ref('')
const results = ref<PetLookupResult[] | null>(null)
const selected = ref<PetLookupResult | null>(null)
const message = ref('')
const requests = ref<CareLinkRequestItem[]>([])
const busy = ref(false)
const error = ref<string | null>(null)
const sent = ref(false)

function apiMessage(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function loadRequests() {
  try {
    requests.value = await listCareLinkRequests(props.orgId)
  } catch {
    requests.value = []
  }
}
onMounted(loadRequests)

async function search() {
  const value = term.value.trim()
  if (!value || busy.value) return
  busy.value = true
  error.value = null
  sent.value = false
  selected.value = null
  try {
    results.value = await lookupPets(
      props.orgId,
      mode.value === 'microchip' ? { microchip: value } : { guardian_email: value },
    )
  } catch (e) {
    results.value = null
    error.value = apiMessage(e)
  } finally {
    busy.value = false
  }
}

function pick(r: PetLookupResult) {
  selected.value = r
  renew() // otro intento del usuario → otra Idempotency-Key
}

async function send() {
  if (!selected.value || busy.value) return
  busy.value = true
  error.value = null
  try {
    await requestCareLink(props.orgId, key.value, {
      pet_id: selected.value.pet_id,
      message: message.value.trim() || undefined,
    })
    sent.value = true
    results.value = null
    selected.value = null
    message.value = ''
    term.value = ''
    renew()
    await loadRequests()
  } catch (e) {
    error.value = apiMessage(e)
  } finally {
    busy.value = false
  }
}

function speciesLabel(s: string) {
  const k = `pets.species.${s}`
  return t(k) === k ? s : t(k)
}
</script>

<template>
  <section class="space-y-4" data-testid="carelink-request-panel">
    <h2 class="text-lg font-semibold">{{ t('orgs.requests.title') }}</h2>
    <p class="text-sm text-surface-500">{{ t('orgs.requests.hint') }}</p>

    <form class="space-y-2" role="search" @submit.prevent="search">
      <fieldset class="flex gap-4">
        <legend class="sr-only">{{ t('orgs.requests.searchBy') }}</legend>
        <label class="flex items-center gap-1 text-sm">
          <input v-model="mode" type="radio" value="microchip" />
          {{ t('orgs.requests.byMicrochip') }}
        </label>
        <label class="flex items-center gap-1 text-sm">
          <input v-model="mode" type="radio" value="email" />
          {{ t('orgs.requests.byEmail') }}
        </label>
      </fieldset>
      <div class="flex gap-2">
        <label class="sr-only" for="lookup-term">{{ t('orgs.requests.searchBy') }}</label>
        <input
          id="lookup-term"
          v-model="term"
          :type="mode === 'email' ? 'email' : 'text'"
          class="input"
          maxlength="254"
          :placeholder="
            mode === 'email'
              ? t('orgs.requests.emailPlaceholder')
              : t('orgs.requests.chipPlaceholder')
          "
        />
        <button type="submit" class="btn-secondary" :disabled="busy || !term.trim()">
          {{ t('orgs.patients.search') }}
        </button>
      </div>
    </form>

    <p v-if="error" role="alert" class="text-sm text-red-700" data-testid="request-error">
      {{ error }}
    </p>
    <p v-if="sent" role="status" class="text-sm text-green-700" data-testid="request-sent">
      {{ t('orgs.requests.sent') }}
    </p>

    <p
      v-if="results && results.length === 0"
      class="text-sm text-surface-500"
      data-testid="lookup-empty"
    >
      {{ t('orgs.requests.noResults') }}
    </p>
    <ul v-if="results && results.length" class="space-y-2">
      <li v-for="r in results" :key="r.pet_id">
        <button
          type="button"
          class="card w-full text-left"
          :aria-pressed="selected?.pet_id === r.pet_id"
          data-testid="lookup-result"
          @click="pick(r)"
        >
          <span class="block font-semibold">{{ r.name }}</span>
          <span class="block text-sm text-surface-500">
            {{ speciesLabel(r.species) }} · {{ t('orgs.requests.guardian') }}:
            {{ r.guardian_masked }}
          </span>
        </button>
      </li>
    </ul>

    <form v-if="selected" class="space-y-2" @submit.prevent="send">
      <label class="block text-sm font-medium" for="request-message">
        {{ t('orgs.requests.message') }} ({{ t('common.optional') }})
      </label>
      <textarea
        id="request-message"
        v-model="message"
        class="input"
        rows="3"
        :maxlength="MESSAGE_MAX"
      />
      <p class="text-xs text-surface-500">{{ message.length }}/{{ MESSAGE_MAX }}</p>
      <button type="submit" class="btn-primary" :disabled="busy" data-testid="request-send">
        {{ t('orgs.requests.send', { name: selected.name }) }}
      </button>
    </form>

    <div v-if="requests.length">
      <h3 class="font-medium">{{ t('orgs.requests.mine') }}</h3>
      <ul class="mt-2 space-y-2">
        <li v-for="r in requests" :key="r.id" class="card" data-testid="request-row">
          <span class="font-semibold">{{ r.pet_name }}</span>
          <span class="ml-2 rounded bg-surface-200/60 px-2 py-0.5 text-xs">
            {{ t(`orgs.requests.status.${r.status}`) }}
          </span>
          <span v-if="r.status === 'pending'" class="block text-sm text-surface-500">
            {{ t('orgs.requests.expires', { date: d(new Date(r.expires_at), 'short') }) }}
          </span>
        </li>
      </ul>
    </div>
  </section>
</template>
