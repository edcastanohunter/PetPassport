<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  api,
  ApiError,
  type BillingPlan,
  type Subscription,
  type OpenBillingAccountRequest,
} from '@/shared/api/client'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    plan: BillingPlan | null
    initialCycle?: 'monthly' | 'annual'
    orgId?: string
    accountId?: string
  }>(),
  {
    initialCycle: 'monthly',
    orgId: '',
    accountId: '',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'subscribed', sub: Subscription): void
}>()

const { t } = useI18n()

const cycle = ref<'monthly' | 'annual'>(props.initialCycle)
const seats = ref<number>(1)
const coupon = ref<string>('')
const busy = ref(false)
const error = ref<string | null>(null)

// Datos fiscales para crear cuenta si no existe
const legalName = ref('')
const taxIdValue = ref('')
const taxIdKind = ref('nit')
const taxRegime = ref('common')
const billingEmail = ref('')
const address = ref('')
const country = ref('CO')

watch(
  () => props.plan,
  (newPlan) => {
    if (newPlan) {
      seats.value = newPlan.seats?.included ?? newPlan.seats?.min ?? 1
    }
  },
  { immediate: true },
)

watch(
  () => props.initialCycle,
  (newCycle) => {
    cycle.value = newCycle
  },
)

const basePriceCents = computed(() => {
  if (!props.plan?.pricing) return 0
  if (cycle.value === 'annual' && props.plan.pricing.annual != null) {
    return props.plan.pricing.annual
  }
  return props.plan.pricing.monthly ?? 0
})

const annualDiscountPercent = computed(() => {
  if (!props.plan?.pricing?.monthly || !props.plan?.pricing?.annual) return 0
  const fullYearMonthly = props.plan.pricing.monthly * 12
  const annual = props.plan.pricing.annual
  if (fullYearMonthly <= annual) return 0
  return Math.round(((fullYearMonthly - annual) / fullYearMonthly) * 100)
})

const subtotalCents = computed(() => {
  let sub = basePriceCents.value
  const inc = props.plan?.seats?.included ?? 1
  if (seats.value > inc && props.plan?.pricing?.tiers && props.plan.pricing.tiers.length > 0) {
    const extraSeats = seats.value - inc
    const tierUnitPrice = props.plan.pricing.tiers[0].monthly_price
    const seatMultiplier = cycle.value === 'annual' ? 12 : 1
    sub += extraSeats * tierUnitPrice * seatMultiplier
  }
  return sub
})

const taxCents = computed(() => {
  return Math.round(subtotalCents.value * 0.19)
})

const totalCents = computed(() => {
  return subtotalCents.value + taxCents.value
})

function formatMoney(cents: number, currency = 'COP') {
  return new Intl.NumberFormat('es-CO', {
    style: 'currency',
    currency: currency,
    maximumFractionDigits: 0,
  }).format(cents / 100)
}

function close() {
  emit('update:modelValue', false)
}

async function handleConfirm() {
  if (!props.plan || busy.value) return
  busy.value = true
  error.value = null

  try {
    let targetAccountId = props.accountId

    // Si no tenemos cuenta de facturación previa, crearla primero
    if (!targetAccountId && props.orgId) {
      const openReq: OpenBillingAccountRequest = {
        organization_id: props.orgId,
        legal_entity: {
          kind: 'company',
          legal_name: legalName.value || 'Organización',
          tax_id: {
            country: country.value,
            value: taxIdValue.value || '900123456-1',
            kind: taxIdKind.value,
          },
          tax_regime: taxRegime.value,
          email: billingEmail.value || 'facturacion@example.com',
          address: address.value || 'Calle Principal 123',
          phone: '+573001234567',
        },
      }

      const { data: accData, error: accError } = await api().POST('/billing-accounts', {
        body: openReq,
      })
      if (accError) throw accError
      if (accData) targetAccountId = accData.id
    }

    if (!targetAccountId) {
      throw new Error(t('billing.accountRequired'))
    }

    // Contratar la suscripción
    const { data: subData, error: subError } = await api().POST(
      '/billing-accounts/{id}/subscriptions',
      {
        params: { path: { id: targetAccountId } },
        body: {
          kind: props.plan.kind,
          plan_code: props.plan.code,
          seats: seats.value,
          cycle: cycle.value,
          coupon: coupon.value.trim() ? coupon.value.trim() : undefined,
        },
      },
    )

    if (subError) throw subError
    if (subData) {
      emit('subscribed', subData)
      close()
    }
  } catch (err: unknown) {
    if (err instanceof ApiError) {
      error.value = t(`errors.${err.code}`, t('errors.GENERIC'))
    } else if (err instanceof Error) {
      error.value = err.message
    } else {
      error.value = t('errors.GENERIC')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    v-if="modelValue && plan"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 overflow-y-auto"
    role="dialog"
    aria-modal="true"
    aria-labelledby="checkout-title"
    data-testid="checkout-modal"
  >
    <div
      class="my-8 w-full max-w-lg rounded-xl bg-surface-0 p-6 text-surface-900 shadow-2xl space-y-5"
    >
      <div class="flex items-start justify-between border-b border-surface-200 pb-3">
        <div>
          <h2 id="checkout-title" class="text-xl font-bold">
            {{ t('billing.checkoutTitle') }}
          </h2>
          <p class="text-sm text-surface-500">
            {{ plan.name || plan.code }}
          </p>
        </div>
        <button
          type="button"
          class="rounded p-1 text-surface-400 hover:text-surface-700"
          :aria-label="t('billing.close')"
          @click="close"
        >
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M6 18L18 6M6 6l12 12"
            />
          </svg>
        </button>
      </div>

      <p v-if="error" role="alert" class="rounded bg-red-50 p-3 text-sm text-red-600 font-medium">
        {{ error }}
      </p>

      <!-- Toggle de Ciclo de Facturación -->
      <div class="space-y-2">
        <label class="text-sm font-semibold text-surface-700">{{
          t('billing.billingCycle')
        }}</label>
        <div class="grid grid-cols-2 gap-3">
          <button
            type="button"
            class="flex flex-col items-center justify-center rounded-lg border p-3 text-sm font-medium transition"
            :class="
              cycle === 'monthly'
                ? 'border-primary-600 bg-primary-50 text-primary-900 ring-2 ring-primary-600'
                : 'border-surface-200 hover:border-surface-300 text-surface-700'
            "
            @click="cycle = 'monthly'"
          >
            <span>{{ t('billing.monthly') }}</span>
          </button>
          <button
            type="button"
            class="relative flex flex-col items-center justify-center rounded-lg border p-3 text-sm font-medium transition"
            :class="
              cycle === 'annual'
                ? 'border-primary-600 bg-primary-50 text-primary-900 ring-2 ring-primary-600'
                : 'border-surface-200 hover:border-surface-300 text-surface-700'
            "
            @click="cycle = 'annual'"
          >
            <span
              v-if="annualDiscountPercent > 0"
              class="absolute -top-2.5 right-2 rounded-full bg-emerald-600 px-2 py-0.5 text-[10px] font-bold text-white uppercase tracking-wider"
            >
              {{ t('billing.savePercent', { percent: annualDiscountPercent }) }}
            </span>
            <span>{{ t('billing.annual') }}</span>
          </button>
        </div>
      </div>

      <!-- Selector de Asientos si aplica -->
      <div v-if="plan.seats" class="space-y-2">
        <div class="flex justify-between items-center text-sm font-semibold text-surface-700">
          <label for="seats-input">{{ t('billing.seats') }}</label>
          <span class="text-xs text-surface-500 font-normal">
            {{ t('billing.includedSeats', { count: plan.seats.included }) }}
          </span>
        </div>
        <div class="flex items-center gap-3">
          <input
            id="seats-input"
            v-model.number="seats"
            type="number"
            :min="plan.seats.min"
            :max="plan.seats.max ?? 100"
            class="w-full rounded-md border border-surface-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
          />
        </div>
      </div>

      <!-- Formulario fiscal si no hay cuenta previa -->
      <div
        v-if="!accountId"
        class="space-y-3 rounded-lg border border-surface-200 bg-surface-50 p-4"
      >
        <h3 class="text-xs font-bold uppercase tracking-wider text-surface-600">
          {{ t('billing.legalEntity') }}
        </h3>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
          <div>
            <label class="block text-xs font-medium text-surface-700 mb-1">{{
              t('billing.legalName')
            }}</label>
            <input
              v-model="legalName"
              type="text"
              required
              class="w-full rounded border border-surface-300 px-2.5 py-1.5 text-sm bg-surface-0"
              placeholder="Clínica Veterinaria SAS"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-surface-700 mb-1">{{
              t('billing.taxId')
            }}</label>
            <input
              v-model="taxIdValue"
              type="text"
              required
              class="w-full rounded border border-surface-300 px-2.5 py-1.5 text-sm bg-surface-0"
              placeholder="900123456-1"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-surface-700 mb-1">{{
              t('billing.email')
            }}</label>
            <input
              v-model="billingEmail"
              type="email"
              required
              class="w-full rounded border border-surface-300 px-2.5 py-1.5 text-sm bg-surface-0"
              placeholder="facturacion@clinica.com"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-surface-700 mb-1">{{
              t('billing.address')
            }}</label>
            <input
              v-model="address"
              type="text"
              required
              class="w-full rounded border border-surface-300 px-2.5 py-1.5 text-sm bg-surface-0"
              placeholder="Calle 100 # 15-20"
            />
          </div>
        </div>
      </div>

      <!-- Cupón -->
      <div>
        <label class="block text-xs font-medium text-surface-700 mb-1">{{
          t('billing.coupon')
        }}</label>
        <div class="flex gap-2">
          <input
            v-model="coupon"
            type="text"
            class="w-full rounded-md border border-surface-300 px-3 py-1.5 text-sm uppercase placeholder:normal-case"
            placeholder="PROMO2026"
          />
        </div>
      </div>

      <!-- Resumen Económico -->
      <div class="rounded-lg bg-surface-100 p-4 space-y-2 text-sm">
        <h4 class="font-semibold text-surface-800">{{ t('billing.summary') }}</h4>
        <div class="flex justify-between text-surface-600">
          <span>{{ t('billing.subtotal') }}</span>
          <span>{{ formatMoney(subtotalCents, plan.pricing.currency) }}</span>
        </div>
        <div class="flex justify-between text-surface-600">
          <span>{{ t('billing.tax') }}</span>
          <span>{{ formatMoney(taxCents, plan.pricing.currency) }}</span>
        </div>
        <div
          class="border-t border-surface-200 pt-2 flex justify-between font-bold text-base text-surface-900"
        >
          <span>{{ t('billing.total') }}</span>
          <span class="text-primary-600">{{ formatMoney(totalCents, plan.pricing.currency) }}</span>
        </div>
      </div>

      <!-- Acciones -->
      <div class="flex justify-end gap-3 pt-2">
        <button
          type="button"
          class="rounded px-4 py-2 text-sm font-medium text-surface-700 hover:bg-surface-100 transition"
          @click="close"
        >
          {{ t('billing.cancel') }}
        </button>
        <button
          type="button"
          :disabled="busy"
          class="rounded bg-primary-600 px-5 py-2 text-sm font-semibold text-white shadow-sm hover:bg-primary-700 disabled:opacity-50 transition"
          @click="handleConfirm"
        >
          {{ busy ? '...' : t('billing.confirmSubscribe') }}
        </button>
      </div>
    </div>
  </div>
</template>
