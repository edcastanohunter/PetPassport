<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type BillingAccount, type Subscription } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'
import { formatMoney } from '../money'

type PseBank = components['schemas']['PseBank']
type PsePaymentRequest = components['schemas']['PsePaymentRequest']

// Pago por PSE (PP-F5-PRD-004, FE-003): PSE no tokeniza, así que cada cargo se
// paga con su propio redirect al banco. El pagador se autentica en su banco;
// PetPassport solo envía banco, tipo de persona y documento (SEC-001). Se ofrece
// a las cuentas con PSE como método y para pagar un cargo de tarjeta rechazado.
const props = defineProps<{ account: BillingAccount; subscription: Subscription; orgId: string }>()
const emit = defineEmits<{ changed: [] }>()
const { t, locale } = useI18n()

const charge = computed(() => {
  const open = (props.subscription.charges ?? [])
    .filter((c) => (c.status === 'failed' || c.status === 'pending') && c.amount > 0)
    .sort((a, b) => a.created_at.localeCompare(b.created_at))[0]
  if (!open || props.account.country !== 'CO') return null
  // Un cargo de tarjeta pendiente aún se está cobrando: solo se ofrece PSE si falló.
  return props.account.payment_method?.kind === 'pse' || open.status === 'failed' ? open : null
})

const open = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const banks = ref<PseBank[]>([])
const config = ref<Record<string, string>>({})
const accepted = ref(false)

const tax = props.account.legal_entity.tax_id
const form = ref({
  bank_code: '',
  payer_type: (props.account.legal_entity.kind === 'person'
    ? 'person'
    : 'company') as PsePaymentRequest['payer_type'],
  legal_id_type: (tax.kind === 'NIT' || tax.kind === 'CC'
    ? tax.kind
    : 'NIT') as PsePaymentRequest['legal_id_type'],
  // El NIT va sin dígito de verificación.
  legal_id: tax.value.split('-')[0]?.replace(/\D/g, '') ?? '',
  phone: props.account.legal_entity.phone ?? '',
})
const legalIdTypes: PsePaymentRequest['legal_id_type'][] = ['NIT', 'CC', 'CE', 'PP', 'TI']

const ready = computed(
  () =>
    !!form.value.bank_code &&
    form.value.legal_id.trim().length >= 3 &&
    !!config.value.acceptance_token &&
    accepted.value,
)

function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function start() {
  error.value = null
  notice.value = null
  busy.value = true
  try {
    const id = props.account.id
    const [list, setup] = await Promise.all([
      api().GET('/billing-accounts/{id}/payment-methods/pse-banks', { params: { path: { id } } }),
      api().POST('/billing-accounts/{id}/payment-methods/setup', {
        params: { path: { id } },
        body: { kind: 'pse' },
      }),
    ])
    if (list.error) throw list.error
    if (setup.error) throw setup.error
    banks.value = list.data?.data ?? []
    config.value = setup.data?.client_config ?? {}
    accepted.value = false
    open.value = true
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

async function pay() {
  const ch = charge.value
  if (!ch || !ready.value) return
  error.value = null
  busy.value = true
  try {
    const { data, error: err } = await api().POST('/charges/{id}/pse-payment', {
      params: { path: { id: ch.id } },
      body: {
        bank_code: form.value.bank_code,
        payer_type: form.value.payer_type,
        legal_id_type: form.value.legal_id_type,
        legal_id: form.value.legal_id.trim(),
        phone: form.value.phone.trim() || undefined,
        acceptance_token: config.value.acceptance_token ?? '',
        personal_auth_token: config.value.personal_auth_token || undefined,
        return_path: `/org/${props.orgId}/settings/billing?pse=return`,
      },
    })
    if (err) throw err
    if (data?.status === 'pending' && data.redirect_url) {
      window.location.assign(data.redirect_url)
      return
    }
    if (data?.status === 'paid') {
      open.value = false
      notice.value = t('billing.pse.paid')
      emit('changed')
    } else {
      error.value = t('billing.pse.declined')
    }
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section
    v-if="charge"
    class="card space-y-3"
    aria-labelledby="pse-title"
    data-testid="pse-payment"
  >
    <h2 id="pse-title" class="text-lg font-semibold">{{ t('billing.pse.title') }}</h2>
    <p>
      {{
        t('billing.pse.due', {
          amount: formatMoney(charge.amount, charge.currency, locale),
        })
      }}
    </p>

    <button
      v-if="!open"
      type="button"
      class="btn-primary"
      :disabled="busy"
      data-testid="pse-start"
      @click="start"
    >
      {{ t('billing.pse.start') }}
    </button>

    <form v-else class="grid gap-3 sm:grid-cols-2" data-testid="pse-form" @submit.prevent="pay">
      <label class="flex flex-col gap-1 text-sm sm:col-span-2">
        {{ t('billing.pse.bank') }}
        <select v-model="form.bank_code" class="input" required data-testid="pse-bank">
          <option value="" disabled>{{ t('billing.pse.chooseBank') }}</option>
          <option v-for="b in banks" :key="b.code" :value="b.code">{{ b.name }}</option>
        </select>
      </label>
      <label class="flex flex-col gap-1 text-sm">
        {{ t('billing.pse.payerType') }}
        <select v-model="form.payer_type" class="input" data-testid="pse-payer-type">
          <option value="company">{{ t('billing.pse.company') }}</option>
          <option value="person">{{ t('billing.pse.person') }}</option>
        </select>
      </label>
      <div class="flex gap-2">
        <label class="flex flex-col gap-1 text-sm">
          {{ t('billing.pse.legalIdType') }}
          <select v-model="form.legal_id_type" class="input" data-testid="pse-id-type">
            <option v-for="k in legalIdTypes" :key="k" :value="k">{{ k }}</option>
          </select>
        </label>
        <label class="flex flex-1 flex-col gap-1 text-sm">
          {{ t('billing.pse.legalId') }}
          <input
            v-model="form.legal_id"
            class="input"
            inputmode="numeric"
            autocomplete="off"
            required
            data-testid="pse-id"
          />
        </label>
      </div>
      <label class="flex flex-col gap-1 text-sm sm:col-span-2">
        {{ t('billing.pse.phone') }}
        <input
          v-model="form.phone"
          class="input"
          type="tel"
          autocomplete="tel"
          data-testid="pse-phone"
        />
      </label>
      <label class="flex items-start gap-2 text-sm sm:col-span-2">
        <input v-model="accepted" type="checkbox" class="mt-1" data-testid="pse-accept" />
        <span>
          {{ t('billing.payment.acceptPrefix') }}
          <a
            :href="config.acceptance_permalink"
            target="_blank"
            rel="noopener noreferrer"
            class="underline"
            >{{ t('billing.payment.acceptTerms') }}</a
          >
          <template v-if="config.personal_auth_permalink">
            {{ t('billing.payment.acceptAnd') }}
            <a
              :href="config.personal_auth_permalink"
              target="_blank"
              rel="noopener noreferrer"
              class="underline"
              >{{ t('billing.payment.acceptData') }}</a
            >
          </template>
        </span>
      </label>
      <div class="sm:col-span-2">
        <button type="submit" class="btn-primary" :disabled="!ready || busy" data-testid="pse-pay">
          {{ t('billing.pse.pay') }}
        </button>
      </div>
    </form>

    <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <p v-if="notice" class="text-sm text-green-700 dark:text-green-300" role="status">
      {{ notice }}
    </p>
    <p class="text-xs text-surface-500">{{ t('billing.pse.secureNote') }}</p>
  </section>
</template>
