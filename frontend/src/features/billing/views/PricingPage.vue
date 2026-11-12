<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type BillingPlan } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { useOrgStore } from '@/features/orgs/orgStore'
import CheckoutModal from '../components/CheckoutModal.vue'
import { formatMoney as fmt } from '../money'

const { t, locale } = useI18n()
const router = useRouter()
const session = useSessionStore()
const orgStore = useOrgStore()

const loading = ref(false)
const error = ref<string | null>(null)
const plans = ref<BillingPlan[]>([])

const activeKind = ref<'clinic' | 'pharmacy'>('clinic')
const activeCycle = ref<'monthly' | 'annual'>('monthly')

// Modal state
const checkoutOpen = ref(false)
const selectedPlan = ref<BillingPlan | null>(null)

onMounted(async () => {
  await fetchPlans()
})

async function fetchPlans() {
  loading.value = true
  error.value = null
  try {
    const { data, error: apiErr } = await api().GET('/plans', {
      params: { query: { kind: activeKind.value } },
    })
    if (apiErr) throw apiErr
    plans.value = data?.data ?? []
  } catch {
    error.value = t('errors.GENERIC')
  } finally {
    loading.value = false
  }
}

function handleKindChange(kind: 'clinic' | 'pharmacy') {
  activeKind.value = kind
  fetchPlans()
}

// Determinación del CTA según estado de auth y rol
const userOrg = computed(() => orgStore.current)
const userMembership = computed(() => {
  if (!userOrg.value) return session.memberships[0] ?? null
  return session.membershipFor(userOrg.value.id) ?? null
})

const isOrgAdmin = computed(() => {
  if (!userMembership.value) return false
  const role = userMembership.value.role
  return (
    role === 'clinic_admin' || role === 'pharmacy_admin' || session.hasGlobalRole('super_admin')
  )
})

function onCtaClick(plan: BillingPlan) {
  if (!session.isAuthenticated) {
    router.push({ path: '/onboarding', query: { returnTo: '/pricing' } })
    return
  }
  if (!userOrg.value && session.memberships.length === 0) {
    router.push('/orgs/new')
    return
  }
  if (isOrgAdmin.value) {
    selectedPlan.value = plan
    checkoutOpen.value = true
  }
}

function getPlanPriceCents(plan: BillingPlan) {
  if (activeCycle.value === 'annual' && plan.pricing.annual != null) {
    return plan.pricing.annual
  }
  return plan.pricing.monthly ?? 0
}

// Los precios de la API vienen en unidades de la moneda, no en centavos.
function formatMoney(amount: number, currency = 'COP') {
  if (amount === 0) return t('billing.free')
  return fmt(amount, currency, locale.value)
}

function onSubscribed() {
  // Redirigir al resumen de la organización tras contratar
  if (userOrg.value) {
    router.push(`/org/${userOrg.value.id}/settings/billing`)
  }
}
</script>

<template>
  <main class="mx-auto max-w-7xl px-4 py-12 sm:px-6 lg:px-8">
    <!-- Header -->
    <div class="text-center space-y-4">
      <h1 class="text-3xl font-extrabold tracking-tight text-surface-900 sm:text-4xl">
        {{ t('billing.pricingTitle') }}
      </h1>
      <p class="mx-auto max-w-2xl text-base text-surface-500 sm:text-lg">
        {{ t('billing.pricingSubtitle') }}
      </p>

      <!-- Selector de Tipo de Organización (Clínica vs Farmacia) -->
      <div class="mt-6 inline-flex rounded-lg bg-surface-100 p-1">
        <button
          type="button"
          class="rounded-md px-4 py-2 text-sm font-semibold transition"
          :class="
            activeKind === 'clinic'
              ? 'bg-surface-0 text-primary-600 shadow-sm'
              : 'text-surface-600 hover:text-surface-900'
          "
          @click="handleKindChange('clinic')"
        >
          {{ t('billing.clinicPlans') }}
        </button>
        <button
          type="button"
          class="rounded-md px-4 py-2 text-sm font-semibold transition"
          :class="
            activeKind === 'pharmacy'
              ? 'bg-surface-0 text-primary-600 shadow-sm'
              : 'text-surface-600 hover:text-surface-900'
          "
          @click="handleKindChange('pharmacy')"
        >
          {{ t('billing.pharmacyPlans') }}
        </button>
      </div>

      <!-- Toggle Mensual / Anual -->
      <div class="mt-4 flex items-center justify-center gap-3 text-sm font-medium">
        <span
          :class="activeCycle === 'monthly' ? 'text-surface-900 font-bold' : 'text-surface-500'"
        >
          {{ t('billing.monthly') }}
        </span>
        <button
          type="button"
          role="switch"
          :aria-checked="activeCycle === 'annual'"
          class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-600 focus:ring-offset-2"
          :class="activeCycle === 'annual' ? 'bg-primary-600' : 'bg-surface-300'"
          @click="activeCycle = activeCycle === 'monthly' ? 'annual' : 'monthly'"
        >
          <span
            class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-surface-0 shadow ring-0 transition duration-200 ease-in-out"
            :class="activeCycle === 'annual' ? 'translate-x-5' : 'translate-x-0'"
          />
        </button>
        <div class="flex items-center gap-1.5">
          <span
            :class="activeCycle === 'annual' ? 'text-surface-900 font-bold' : 'text-surface-500'"
          >
            {{ t('billing.annual') }}
          </span>
          <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-bold text-emerald-800">
            -15%
          </span>
        </div>
      </div>
    </div>

    <!-- Loading & Error States -->
    <div v-if="loading" class="mt-12 text-center text-surface-500">
      <div
        class="inline-block h-8 w-8 animate-spin rounded-full border-4 border-primary-600 border-r-transparent"
      />
    </div>

    <div v-else-if="error" class="mt-12 rounded-md bg-red-50 p-4 text-center text-red-600">
      {{ error }}
    </div>

    <!-- Plans Grid -->
    <div v-else class="mt-12 grid grid-cols-1 gap-8 md:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="plan in plans"
        :key="plan.code"
        class="relative flex flex-col justify-between rounded-2xl border bg-surface-0 p-8 shadow-sm transition hover:shadow-md"
        :class="
          plan.code.includes('standard') || plan.code.includes('pro')
            ? 'border-primary-500 ring-2 ring-primary-500'
            : 'border-surface-200'
        "
        data-testid="plan-card"
      >
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <h2 class="text-xl font-bold text-surface-900">
              {{ plan.name || plan.code }}
            </h2>
            <span
              v-if="plan.code.includes('standard') || plan.code.includes('pro')"
              class="rounded-full bg-primary-100 px-2.5 py-0.5 text-xs font-semibold text-primary-800"
            >
              {{ t('billing.popular') }}
            </span>
          </div>

          <p v-if="plan.description" class="text-sm text-surface-500">
            {{ plan.description }}
          </p>

          <div class="flex items-baseline gap-1">
            <span class="text-4xl font-extrabold tracking-tight text-surface-900">
              {{ formatMoney(getPlanPriceCents(plan), plan.pricing.currency) }}
            </span>
            <span class="text-sm font-medium text-surface-500">
              / {{ activeCycle === 'annual' ? t('billing.perYear') : t('billing.perMonth') }}
            </span>
          </div>

          <!-- Asientos -->
          <div v-if="plan.seats" class="text-xs text-surface-600 font-medium">
            <span v-if="plan.seats.max">
              {{ t('billing.maxSeats', { count: plan.seats.max }) }}
            </span>
            <span v-else>
              {{ t('billing.unlimitedSeats') }}
            </span>
          </div>

          <!-- Separador -->
          <div class="border-t border-surface-100 pt-4">
            <ul class="space-y-3 text-sm text-surface-600">
              <li v-for="feature in plan.features" :key="feature" class="flex items-center gap-2">
                <svg
                  class="h-4 w-4 shrink-0 text-emerald-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2.5"
                    d="M5 13l4 4L19 7"
                  />
                </svg>
                <span>{{ feature }}</span>
              </li>
            </ul>
          </div>
        </div>

        <!-- CTA Button -->
        <div class="mt-8 pt-4">
          <!-- Anonymous CTA -->
          <button
            v-if="!session.isAuthenticated"
            type="button"
            class="w-full rounded-lg bg-primary-600 px-4 py-2.5 text-center text-sm font-semibold text-white shadow-sm hover:bg-primary-700 transition"
            @click="onCtaClick(plan)"
          >
            {{ t('billing.loginToSubscribe') }}
          </button>

          <!-- Authenticated without Org -->
          <button
            v-else-if="!userOrg && session.memberships.length === 0"
            type="button"
            class="w-full rounded-lg bg-primary-600 px-4 py-2.5 text-center text-sm font-semibold text-white shadow-sm hover:bg-primary-700 transition"
            @click="onCtaClick(plan)"
          >
            {{ t('billing.createOrgToSubscribe') }}
          </button>

          <!-- Org Admin -->
          <button
            v-else-if="isOrgAdmin"
            type="button"
            class="w-full rounded-lg bg-primary-600 px-4 py-2.5 text-center text-sm font-semibold text-white shadow-sm hover:bg-primary-700 transition"
            @click="onCtaClick(plan)"
          >
            {{ t('billing.selectPlan') }}
          </button>

          <!-- Member without Admin permissions -->
          <button
            v-else
            type="button"
            disabled
            class="w-full cursor-not-allowed rounded-lg bg-surface-200 px-4 py-2.5 text-center text-sm font-medium text-surface-500 opacity-75"
          >
            {{ t('billing.contactAdmin') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Modal de Checkout -->
    <CheckoutModal
      v-model="checkoutOpen"
      :plan="selectedPlan"
      :initial-cycle="activeCycle"
      :org-id="userOrg?.id"
      @subscribed="onSubscribed"
    />
  </main>
</template>
