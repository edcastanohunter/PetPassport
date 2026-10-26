<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Pet } from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import { emailSchema } from '@/shared/validation/schemas'

// Transferencia de tutela en la ficha (FE-006/PRD-010): modal con email;
// estado pendiente con email enmascarado, caducidad y cancelar.
const props = defineProps<{ pet: Pet }>()
const emit = defineEmits<{ changed: [] }>()
const { t, d } = useI18n()
const { key, renew } = useCommand()

const open = ref(false)
const email = ref('')
const error = ref<string | null>(null)
const busy = ref(false)

async function send() {
  error.value = null
  if (!emailSchema().safeParse(email.value).success) {
    error.value = t('fieldErrors.INVALID')
    return
  }
  busy.value = true
  try {
    const { error: apiError } = await api().POST('/pets/{petId}/transfer', {
      params: { path: { petId: props.pet.id }, header: { 'Idempotency-Key': key.value } },
      body: { to_email: email.value.trim() },
    })
    if (apiError) throw apiError
    open.value = false
    email.value = ''
    emit('changed')
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    renew()
    busy.value = false
  }
}

async function cancel() {
  busy.value = true
  error.value = null
  try {
    const { error: apiError } = await api().DELETE('/pets/{petId}/transfer', {
      params: { path: { petId: props.pet.id } },
    })
    if (apiError) throw apiError
    emit('changed')
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    class="rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
    data-testid="transfer"
  >
    <h2 class="font-semibold">{{ t('pets.transfer.title') }}</h2>

    <div v-if="pet.pending_transfer" class="mt-2 space-y-2" data-testid="transfer-pending">
      <p class="text-sm">
        {{
          t('pets.transfer.pending', {
            email: pet.pending_transfer.to_email_masked,
            date: d(new Date(pet.pending_transfer.expires_at), 'short'),
          })
        }}
      </p>
      <button type="button" class="btn-secondary" :disabled="busy" @click="cancel">
        {{ t('pets.transfer.cancel') }}
      </button>
    </div>

    <template v-else-if="pet.status === 'active'">
      <p v-if="!open" class="mt-1 text-sm text-surface-500">
        {{ t('pets.transfer.info') }}
        <button
          type="button"
          class="ml-1 underline"
          data-testid="start-transfer"
          @click="open = true"
        >
          {{ t('pets.transfer.action') }}
        </button>
      </p>
      <form
        v-else
        class="mt-2 space-y-2"
        role="dialog"
        :aria-label="t('pets.transfer.title')"
        novalidate
        @submit.prevent="send"
      >
        <label class="label" for="transfer-email">{{ t('pets.transfer.email') }}</label>
        <input id="transfer-email" v-model="email" type="email" class="input" />
        <p class="text-xs text-surface-500">{{ t('pets.transfer.hint') }}</p>
        <div class="flex gap-2">
          <button type="submit" class="btn-primary" :disabled="busy" data-testid="send-transfer">
            {{ t('pets.transfer.send') }}
          </button>
          <button type="button" class="btn-secondary" @click="open = false">
            {{ t('common.cancel') }}
          </button>
        </div>
      </form>
    </template>

    <p v-if="error" class="mt-2 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
  </div>
</template>
