<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type BillingAccount, type Subscription } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'
import { useSessionStore } from '@/features/auth/sessionStore'
import BillingStatusBanner from '../components/BillingStatusBanner.vue'
import PaymentMethodPanel from '../components/PaymentMethodPanel.vue'
import { formatMoney } from '../money'

type Invoice = components['schemas']['Invoice']
type BillingPayment = components['schemas']['BillingPayment']
type BillingPlan = components['schemas']['BillingPlan']
type Preview = components['schemas']['SubscriptionPreviewResponse']
type LegalEntity = components['schemas']['LegalEntity']
type Entitlements = components['schemas']['EntitlementSet']

// Facturación de la organización (PP-F5-FE-003): resumen, cambio de plan con
// vista previa, método de pago (widget del proveedor), datos fiscales,
// facturas, pagos, cancelar y reactivar; banner persistente en grace/restricted.
// Los importes los calcula el backend (B6); aquí solo se muestran.
const route = useRoute()
const session = useSessionStore()
const { t, d, locale } = useI18n()

const orgId = computed(() => String(route.params.orgId ?? ''))
const kind = computed<'clinic' | 'pharmacy'>(() =>
  session.membershipFor(orgId.value)?.type === 'pharmacy' ? 'pharmacy' : 'clinic',
)

const loading = ref(true)
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const account = ref<BillingAccount | null>(null)
const sub = ref<Subscription | null>(null)
const entitlements = ref<Entitlements | null>(null)
const invoices = ref<Invoice[]>([])
const payments = ref<BillingPayment[]>([])
const plans = ref<BillingPlan[]>([])

function money(amount: number, currency?: string) {
  return formatMoney(amount, currency ?? account.value?.currency ?? 'COP', locale.value)
}
function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const { data, error: err } = await api().GET('/organizations/{orgId}/billing', {
      params: { path: { orgId: orgId.value } },
    })
    if (err) throw err
    account.value = data?.account ?? null
    sub.value = data?.subscription ?? null
    entitlements.value = data?.entitlements ?? null
    if (account.value) {
      const id = account.value.id
      const [inv, pay] = await Promise.all([
        api().GET('/billing-accounts/{id}/invoices', {
          params: { path: { id }, query: { limit: 50 } },
        }),
        api().GET('/billing-accounts/{id}/payments', {
          params: { path: { id }, query: { limit: 50 } },
        }),
      ])
      invoices.value = inv.data?.data ?? []
      payments.value = pay.data?.data ?? []
      resetFiscal()
    }
    if (sub.value) resetChange()
  } catch (e) {
    error.value = message(e)
  } finally {
    loading.value = false
  }
}

async function loadPlans() {
  const { data } = await api().GET('/plans', { params: { query: { kind: kind.value } } })
  plans.value = data?.data ?? []
}

onMounted(() => {
  void load()
  void loadPlans().catch(() => undefined)
})

// Ejecuta una acción sobre la suscripción y recarga el resumen.
async function act(fn: () => Promise<{ error?: unknown }>, done: string) {
  busy.value = true
  error.value = null
  notice.value = null
  try {
    const { error: err } = await fn()
    if (err) throw err
    notice.value = done
    await load()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

// ---------------------------------------------------------------- resumen
const statusClass: Record<string, string> = {
  trialing: 'bg-primary-100 text-primary-900',
  active: 'bg-green-100 text-green-900',
  grace: 'bg-amber-100 text-amber-900',
  restricted: 'bg-red-100 text-red-900',
  cancelled: 'bg-surface-200 text-surface-800',
  expired: 'bg-surface-200 text-surface-800',
}
const seatPct = computed(() => {
  const s = sub.value?.seats
  if (!s || !s.purchased) return 0
  return Math.min(100, Math.round((s.used / s.purchased) * 100))
})
const nextCharge = computed(() => {
  const s = sub.value
  if (!s || s.status === 'cancelled' || s.status === 'expired' || s.cancel_at_period_end)
    return null
  return s.status === 'trialing' && s.trial_ends_at ? s.trial_ends_at : s.current_period.end
})
const canRetry = computed(() => sub.value?.status === 'grace' || sub.value?.status === 'restricted')

function retry() {
  const s = sub.value
  if (!s) return
  void act(
    () => api().POST('/subscriptions/{id}/retry-charge', { params: { path: { id: s.id } } }),
    t('billing.page.retried'),
  )
}

// ---------------------------------------------------------------- cambio de plan
const planCode = ref('')
const cycle = ref<'monthly' | 'annual'>('monthly')
const seats = ref(1)
const preview = ref<Preview | null>(null)
const selectedPlan = computed(() => plans.value.find((p) => p.code === planCode.value))
const perSeat = computed(() => selectedPlan.value?.pricing.model !== 'flat')

function resetChange() {
  const s = sub.value
  if (!s) return
  planCode.value = s.plan.code
  cycle.value = s.cycle
  seats.value = s.seats?.purchased ?? 1
  preview.value = null
}
watch([planCode, cycle, seats], () => (preview.value = null))

async function runPreview() {
  const a = account.value
  if (!a || !planCode.value) return
  busy.value = true
  error.value = null
  try {
    const { data, error: err } = await api().POST('/billing-accounts/{id}/subscriptions/preview', {
      params: { path: { id: a.id } },
      body: {
        kind: kind.value,
        plan_code: planCode.value,
        cycle: cycle.value,
        seats: perSeat.value ? seats.value : undefined,
      },
    })
    if (err) throw err
    preview.value = data ?? null
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

function confirmChange() {
  const s = sub.value
  if (!s) return
  void act(
    () =>
      api().POST('/subscriptions/{id}/change', {
        params: { path: { id: s.id } },
        body: {
          plan_code: planCode.value,
          cycle: cycle.value,
          seats: perSeat.value ? seats.value : undefined,
        },
      }),
    t('billing.page.changed'),
  )
}

// ---------------------------------------------------------------- datos fiscales
const fiscal = ref<LegalEntity | null>(null)
function resetFiscal() {
  const le = account.value?.legal_entity
  fiscal.value = le ? { ...le, tax_id: { ...le.tax_id } } : null
}
const fiscalValid = computed(() => {
  const f = fiscal.value
  return Boolean(
    f && f.legal_name.trim() && f.tax_id.value.trim() && f.address.trim() && f.email.trim(),
  )
})
function saveFiscal() {
  const a = account.value
  const f = fiscal.value
  if (!a || !f || !fiscalValid.value) return
  void act(
    () =>
      api().PATCH('/billing-accounts/{id}', {
        params: { path: { id: a.id } },
        body: { legal_entity: f },
      }),
    t('billing.page.fiscalSaved'),
  )
}

// ---------------------------------------------------------------- descargas
async function download(inv: Invoice, kind: 'pdf' | 'xml') {
  error.value = null
  try {
    const opts = {
      params: { path: { id: inv.id }, header: { Accept: 'application/json' } },
    }
    const { data, error: err } =
      kind === 'pdf'
        ? await api().GET('/invoices/{id}/pdf', opts)
        : await api().GET('/invoices/{id}/xml', opts)
    if (err) throw err
    if (data?.url) window.open(data.url, '_blank', 'noopener')
  } catch (e) {
    error.value = message(e)
  }
}

// ---------------------------------------------------------------- cancelar / reactivar
const cancelOpen = ref(false)
const cancelReason = ref('')
const cancelAtEnd = ref(true)
function cancel() {
  const s = sub.value
  if (!s) return
  void act(
    () =>
      api().POST('/subscriptions/{id}/cancel', {
        params: { path: { id: s.id } },
        body: { at_period_end: cancelAtEnd.value, reason: cancelReason.value.trim() || undefined },
      }),
    t('billing.page.cancelled'),
  ).then(() => {
    cancelOpen.value = false
    cancelReason.value = ''
  })
}
function resume() {
  const s = sub.value
  if (!s) return
  void act(
    () => api().POST('/subscriptions/{id}/resume', { params: { path: { id: s.id } } }),
    t('billing.page.resumed'),
  )
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('billing.page.title') }}</h1>

    <BillingStatusBanner
      v-if="sub"
      :status="sub.status"
      :org-id="orgId"
      :grace-ends-at="sub.dunning?.grace_ends_at"
      :retry="canRetry"
      :busy="busy"
      @retry="retry"
    />

    <p
      v-if="error"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-if="notice"
      class="rounded-lg bg-green-50 p-4 text-green-800 dark:bg-green-950 dark:text-green-200"
      role="status"
    >
      {{ notice }}
    </p>

    <p v-if="loading" class="text-surface-500">{{ t('common.loading') }}</p>

    <div v-else-if="!account || !sub" class="card space-y-3" data-testid="billing-empty">
      <p>{{ t('billing.page.noSubscription') }}</p>
      <RouterLink to="/pricing" class="btn-primary inline-block">{{
        t('billing.page.seePlans')
      }}</RouterLink>
    </div>

    <template v-else>
      <!-- Resumen -->
      <section class="card space-y-3" aria-labelledby="summary-title" data-testid="billing-summary">
        <div class="flex flex-wrap items-center gap-3">
          <h2 id="summary-title" class="text-lg font-semibold">{{ sub.plan.name }}</h2>
          <span
            class="rounded-full px-2.5 py-0.5 text-xs font-medium"
            :class="statusClass[sub.status] ?? statusClass.cancelled"
            data-testid="sub-status"
          >
            {{ t(`billing.status.${sub.status}`) }}
          </span>
          <span class="text-sm text-surface-500">{{ t(`billing.cycle.${sub.cycle}`) }}</span>
        </div>
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <template v-if="nextCharge">
            <dt class="text-surface-500">{{ t('billing.page.nextCharge') }}</dt>
            <dd data-testid="next-charge">{{ d(new Date(nextCharge), 'short') }}</dd>
          </template>
          <template v-if="sub.cancel_at_period_end">
            <dt class="text-surface-500">{{ t('billing.page.endsAt') }}</dt>
            <dd>{{ d(new Date(sub.current_period.end), 'short') }}</dd>
          </template>
          <template v-if="sub.pending_change">
            <dt class="text-surface-500">{{ t('billing.page.pendingChange') }}</dt>
            <dd>
              {{ sub.pending_change.plan_code ?? sub.plan.code }} ·
              {{ d(new Date(sub.pending_change.effective_at), 'short') }}
            </dd>
          </template>
        </dl>
        <div v-if="sub.seats" class="space-y-1">
          <p class="text-sm">
            {{ t('billing.page.seats', { used: sub.seats.used, purchased: sub.seats.purchased }) }}
          </p>
          <div
            class="h-2 w-full rounded-full bg-surface-200 dark:bg-surface-700"
            role="progressbar"
            :aria-valuenow="sub.seats.used"
            :aria-valuemin="0"
            :aria-valuemax="sub.seats.purchased"
            :aria-label="t('billing.page.seatsLabel')"
          >
            <div
              class="h-2 rounded-full"
              :class="
                seatPct >= 100 ? 'bg-red-500' : seatPct >= 80 ? 'bg-amber-500' : 'bg-primary-600'
              "
              :style="{ width: `${seatPct}%` }"
            />
          </div>
        </div>
      </section>

      <!-- Cambio de plan -->
      <section
        v-if="sub.status !== 'cancelled' && sub.status !== 'expired'"
        class="card space-y-3"
        aria-labelledby="change-title"
        data-testid="change-plan"
      >
        <h2 id="change-title" class="text-lg font-semibold">{{ t('billing.page.changeTitle') }}</h2>
        <div class="grid gap-3 sm:grid-cols-3">
          <div>
            <label class="label" for="plan-select">{{ t('billing.page.plan') }}</label>
            <select id="plan-select" v-model="planCode" class="input">
              <option v-for="p in plans" :key="p.code" :value="p.code">{{ p.name }}</option>
            </select>
          </div>
          <div>
            <label class="label" for="cycle-select">{{ t('billing.billingCycle') }}</label>
            <select id="cycle-select" v-model="cycle" class="input">
              <option value="monthly">{{ t('billing.cycle.monthly') }}</option>
              <option value="annual">{{ t('billing.cycle.annual') }}</option>
            </select>
          </div>
          <div v-if="perSeat">
            <label class="label" for="seats-change">{{ t('billing.seats') }}</label>
            <input
              id="seats-change"
              v-model.number="seats"
              type="number"
              class="input"
              :min="Math.max(1, sub.seats?.used ?? 1)"
            />
          </div>
        </div>
        <div
          v-if="preview"
          class="rounded-lg bg-surface-50 p-3 text-sm dark:bg-surface-800"
          data-testid="preview"
        >
          <ul class="space-y-1">
            <li v-for="(l, i) in preview.lines" :key="i" class="flex justify-between">
              <span>{{ l.description }}</span
              ><span>{{ money(l.amount, preview.currency) }}</span>
            </li>
            <li v-if="preview.proration_credit" class="flex justify-between">
              <span>{{ t('billing.page.proration') }}</span>
              <span>−{{ money(preview.proration_credit, preview.currency) }}</span>
            </li>
            <li class="flex justify-between">
              <span>{{ t('billing.tax') }}</span
              ><span>{{ money(preview.tax, preview.currency) }}</span>
            </li>
            <li class="flex justify-between font-semibold">
              <span>{{ t('billing.total') }}</span
              ><span>{{ money(preview.total, preview.currency) }}</span>
            </li>
          </ul>
          <p class="mt-2 text-surface-500">
            {{
              t('billing.page.previewNext', { date: d(new Date(preview.next_charge_at), 'short') })
            }}
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn-secondary"
            :disabled="busy"
            data-testid="run-preview"
            @click="runPreview"
          >
            {{ t('billing.page.preview') }}
          </button>
          <button
            type="button"
            class="btn-primary"
            :disabled="busy || !preview"
            data-testid="confirm-change"
            @click="confirmChange"
          >
            {{ t('billing.page.confirmChange') }}
          </button>
        </div>
      </section>

      <PaymentMethodPanel :account="account" @changed="load" />

      <!-- Datos fiscales -->
      <form
        v-if="fiscal"
        class="card space-y-3"
        aria-labelledby="fiscal-title"
        data-testid="fiscal-form"
        novalidate
        @submit.prevent="saveFiscal"
      >
        <h2 id="fiscal-title" class="text-lg font-semibold">{{ t('billing.legalEntity') }}</h2>
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label class="label" for="f-name">{{ t('billing.legalName') }}</label>
            <input id="f-name" v-model="fiscal.legal_name" class="input" required maxlength="200" />
          </div>
          <div>
            <label class="label" for="f-tax">{{ t('billing.taxId') }}</label>
            <input id="f-tax" v-model="fiscal.tax_id.value" class="input" required maxlength="32" />
          </div>
          <div>
            <label class="label" for="f-email">{{ t('billing.email') }}</label>
            <input id="f-email" v-model="fiscal.email" type="email" class="input" required />
          </div>
          <div>
            <label class="label" for="f-address">{{ t('billing.address') }}</label>
            <input id="f-address" v-model="fiscal.address" class="input" required maxlength="300" />
          </div>
        </div>
        <button type="submit" class="btn-primary" :disabled="busy || !fiscalValid">
          {{ t('common.save') }}
        </button>
      </form>

      <!-- Facturas -->
      <section class="card space-y-3" aria-labelledby="inv-title">
        <h2 id="inv-title" class="text-lg font-semibold">{{ t('billing.page.invoices') }}</h2>
        <p v-if="!invoices.length" class="text-surface-500">{{ t('billing.page.noInvoices') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-left text-sm" data-testid="invoices">
            <thead class="text-surface-500">
              <tr>
                <th scope="col" class="py-1 pr-3">{{ t('billing.page.number') }}</th>
                <th scope="col" class="py-1 pr-3">{{ t('billing.page.date') }}</th>
                <th scope="col" class="py-1 pr-3">{{ t('billing.total') }}</th>
                <th scope="col" class="py-1 pr-3">{{ t('billing.page.dianStatus') }}</th>
                <th scope="col" class="py-1">
                  <span class="sr-only">{{ t('billing.page.files') }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="inv in invoices"
                :key="inv.id"
                class="border-t border-surface-200 dark:border-surface-700"
              >
                <td class="py-1 pr-3">{{ inv.number ?? '—' }}</td>
                <td class="py-1 pr-3">
                  {{ d(new Date(inv.issued_at ?? inv.created_at), 'short') }}
                </td>
                <td class="py-1 pr-3">{{ money(inv.total, inv.currency) }}</td>
                <td class="py-1 pr-3">{{ t(`billing.invoiceStatus.${inv.status}`) }}</td>
                <td class="flex gap-2 py-1">
                  <button
                    v-if="inv.has_pdf"
                    type="button"
                    class="underline"
                    @click="download(inv, 'pdf')"
                  >
                    PDF
                  </button>
                  <button
                    v-if="inv.has_xml"
                    type="button"
                    class="underline"
                    @click="download(inv, 'xml')"
                  >
                    XML
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Pagos -->
      <section class="card space-y-3" aria-labelledby="pay-title">
        <h2 id="pay-title" class="text-lg font-semibold">{{ t('billing.page.payments') }}</h2>
        <p v-if="!payments.length" class="text-surface-500">{{ t('billing.page.noPayments') }}</p>
        <ul
          v-else
          class="divide-y divide-surface-200 text-sm dark:divide-surface-700"
          data-testid="payments"
        >
          <li v-for="p in payments" :key="p.id" class="flex justify-between py-1">
            <span>{{ d(new Date(p.at), 'short') }} · {{ t(`billing.paymentKind.${p.kind}`) }}</span>
            <span>{{ p.kind === 'refund' ? '−' : '' }}{{ money(p.amount, p.currency) }}</span>
          </li>
        </ul>
      </section>

      <!-- Cancelar / reactivar -->
      <section class="card space-y-3" aria-labelledby="cancel-title">
        <h2 id="cancel-title" class="text-lg font-semibold">{{ t('billing.page.cancelTitle') }}</h2>
        <template v-if="sub.cancel_at_period_end">
          <p>
            {{
              t('billing.page.cancelScheduled', {
                date: d(new Date(sub.current_period.end), 'short'),
              })
            }}
          </p>
          <button
            type="button"
            class="btn-primary"
            :disabled="busy"
            data-testid="resume"
            @click="resume"
          >
            {{ t('billing.page.resume') }}
          </button>
        </template>
        <template v-else-if="sub.status !== 'cancelled' && sub.status !== 'expired'">
          <button
            v-if="!cancelOpen"
            type="button"
            class="btn-secondary"
            data-testid="open-cancel"
            @click="cancelOpen = true"
          >
            {{ t('billing.page.cancel') }}
          </button>
          <form v-else class="space-y-2" data-testid="cancel-form" @submit.prevent="cancel">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="cancelAtEnd" type="checkbox" />
              {{
                t('billing.page.cancelAtEnd', {
                  date: d(new Date(sub.current_period.end), 'short'),
                })
              }}
            </label>
            <p class="text-sm text-surface-500">
              {{
                cancelAtEnd
                  ? t('billing.page.cancelEffectiveEnd', {
                      date: d(new Date(sub.current_period.end), 'short'),
                    })
                  : t('billing.page.cancelEffectiveNow')
              }}
            </p>
            <label class="label" for="cancel-reason">{{ t('billing.page.cancelReason') }}</label>
            <textarea id="cancel-reason" v-model="cancelReason" class="input" maxlength="500" />
            <div class="flex gap-2">
              <button type="button" class="btn-secondary" @click="cancelOpen = false">
                {{ t('common.cancel') }}
              </button>
              <button
                type="submit"
                class="btn-primary"
                :disabled="busy"
                data-testid="confirm-cancel"
              >
                {{ t('billing.page.confirmCancel') }}
              </button>
            </div>
          </form>
        </template>
        <p v-else class="text-surface-500">{{ t('billing.page.cancelledInfo') }}</p>
      </section>
    </template>
  </section>
</template>
