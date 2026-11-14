<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type BillingAccount } from '@/shared/api/client'
import {
  requiresAcceptance,
  tokenize,
  WidgetError,
  type PaymentMethodSetup,
} from '../paymentWidget'

// Método de pago (FE-003, API-004, SEC-001): se tokeniza en el widget del
// proveedor; aquí solo se ven marca, últimos 4 y vencimiento. Retirar solo
// procede si ninguna suscripción vigente se cobra con él (409 si no).
const props = defineProps<{ account: BillingAccount }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()

const setup = ref<PaymentMethodSetup | null>(null)
const accepted = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

const pm = computed(() => props.account.payment_method)
// PSE solo existe en Colombia (PRD-004): no tokeniza; cada cargo se paga desde el banco.
const pseAvailable = computed(() => props.account.country === 'CO' && pm.value?.kind !== 'pse')

function message(e: unknown) {
  if (e instanceof WidgetError) return t(`billing.payment.${e.code}`)
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function start() {
  error.value = null
  notice.value = null
  busy.value = true
  try {
    const { data, error: err } = await api().POST('/billing-accounts/{id}/payment-methods/setup', {
      params: { path: { id: props.account.id } },
      body: { kind: 'card' },
    })
    if (err) throw err
    setup.value = data ?? null
    accepted.value = false
    if (setup.value && !requiresAcceptance(setup.value)) await openWidget()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

async function openWidget() {
  const cfg = setup.value
  if (!cfg) return
  busy.value = true
  error.value = null
  try {
    const result = await tokenize(cfg)
    if (!result) return
    const { error: err } = await api().POST('/billing-accounts/{id}/payment-methods', {
      params: { path: { id: props.account.id } },
      body: {
        kind: 'card',
        provider_token: result.providerToken,
        acceptance_token: cfg.client_config.acceptance_token,
        personal_auth_token: cfg.client_config.personal_auth_token,
      },
    })
    if (err) throw err
    setup.value = null
    notice.value = t('billing.payment.saved')
    emit('changed')
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

async function usePse() {
  error.value = null
  notice.value = null
  busy.value = true
  try {
    const { error: err } = await api().POST('/billing-accounts/{id}/payment-methods', {
      params: { path: { id: props.account.id } },
      body: { kind: 'pse' },
    })
    if (err) throw err
    setup.value = null
    notice.value = t('billing.pse.methodSaved')
    emit('changed')
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  error.value = null
  notice.value = null
  busy.value = true
  try {
    const { error: err } = await api().DELETE('/billing-accounts/{id}/payment-methods', {
      params: { path: { id: props.account.id } },
    })
    if (err) throw err
    notice.value = t('billing.payment.removed')
    emit('changed')
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card space-y-3" aria-labelledby="pm-title" data-testid="payment-method">
    <h2 id="pm-title" class="text-lg font-semibold">{{ t('billing.payment.title') }}</h2>
    <p v-if="pm" data-testid="pm-summary">
      {{ pm.brand ?? t(`billing.payment.kind.${pm.kind}`, pm.kind) }}
      <template v-if="pm.last4"> •••• {{ pm.last4 }}</template>
      <span v-if="pm.expires_at" class="text-sm text-surface-500">
        · {{ t('billing.payment.expires', { date: pm.expires_at.slice(0, 7) }) }}
      </span>
    </p>
    <p v-else class="text-surface-500">{{ t('billing.payment.none') }}</p>
    <p v-if="pm?.kind === 'pse'" class="text-sm text-surface-600 dark:text-surface-300">
      {{ t('billing.pse.methodHint') }}
    </p>

    <div
      v-if="setup && requiresAcceptance(setup)"
      class="space-y-2 rounded-lg border border-surface-200 p-3 dark:border-surface-700"
      data-testid="pm-acceptance"
    >
      <label class="flex items-start gap-2 text-sm">
        <input v-model="accepted" type="checkbox" class="mt-1" data-testid="pm-accept" />
        <span>
          {{ t('billing.payment.acceptPrefix') }}
          <a
            :href="setup.client_config.acceptance_permalink"
            target="_blank"
            rel="noopener noreferrer"
            class="underline"
            >{{ t('billing.payment.acceptTerms') }}</a
          >
          <template v-if="setup.client_config.personal_auth_permalink">
            {{ t('billing.payment.acceptAnd') }}
            <a
              :href="setup.client_config.personal_auth_permalink"
              target="_blank"
              rel="noopener noreferrer"
              class="underline"
              >{{ t('billing.payment.acceptData') }}</a
            >
          </template>
        </span>
      </label>
      <button
        type="button"
        class="btn-primary"
        :disabled="!accepted || busy"
        data-testid="pm-open-widget"
        @click="openWidget"
      >
        {{ t('billing.payment.continue') }}
      </button>
    </div>

    <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <p v-if="notice" class="text-sm text-green-700 dark:text-green-300" role="status">
      {{ notice }}
    </p>

    <div class="flex flex-wrap gap-2">
      <button
        type="button"
        class="btn-secondary"
        :disabled="busy"
        data-testid="pm-change"
        @click="start"
      >
        {{ pm ? t('billing.payment.replace') : t('billing.payment.add') }}
      </button>
      <button
        v-if="pseAvailable"
        type="button"
        class="btn-secondary"
        :disabled="busy"
        data-testid="pm-use-pse"
        @click="usePse"
      >
        {{ t('billing.pse.useMethod') }}
      </button>
      <button
        v-if="pm"
        type="button"
        class="btn-secondary"
        :disabled="busy"
        data-testid="pm-remove"
        @click="remove"
      >
        {{ t('billing.payment.remove') }}
      </button>
    </div>
    <p class="text-xs text-surface-500">{{ t('billing.payment.secureNote') }}</p>
  </section>
</template>
