<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Subscription } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'
import { formatMoney } from '@/features/billing/money'
import BillingActionDialog, { type BillingField } from './BillingActionDialog.vue'

type AccountRow = components['schemas']['AdminBillingAccountSummary']
type Detail = components['schemas']['AdminBillingAccountDetail']
type Invoice = components['schemas']['Invoice']
type Discrepancy = components['schemas']['BillingDiscrepancy']
type Coupon = components['schemas']['Coupon']
type BillingPlan = components['schemas']['BillingPlan']
type PlanInput = components['schemas']['PlanInput']
type Charge = components['schemas']['SubscriptionCharge']

// Admin de facturación (PP-F5-FE-005, API-007): cuentas con filtros y detalle
// (suscripciones, cargos, pagos, facturas, discrepancias), ajustes con motivo,
// cola de facturas con reintento, discrepancias, cupones (PRD-022) y planes.
const { t, d, locale } = useI18n()

type Tab = 'accounts' | 'invoices' | 'discrepancies' | 'coupons' | 'plans'
const TABS: readonly Tab[] = ['accounts', 'invoices', 'discrepancies', 'coupons', 'plans']
const tab = ref<Tab>('accounts')
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}
function money(amount: number, currency = 'COP') {
  return formatMoney(amount, currency, locale.value)
}
async function run<T>(fn: () => Promise<{ data?: T; error?: unknown }>): Promise<T | undefined> {
  error.value = null
  const { data, error: err } = await fn()
  if (err) throw err
  return data
}

// ---------------------------------------------------------------- cuentas
type AccountStatus = '' | 'trialing' | 'active' | 'grace' | 'restricted' | 'cancelled'
const filters = reactive({ q: '', status: '' as AccountStatus, country: '' })
const accounts = ref<AccountRow[]>([])
const detail = ref<Detail | null>(null)

async function loadAccounts() {
  try {
    const data = await run(() =>
      api().GET('/admin/billing/accounts', {
        params: {
          query: {
            q: filters.q.trim() || undefined,
            status: filters.status || undefined,
            country: filters.country.trim().toUpperCase() || undefined,
            limit: 100,
          },
        },
      }),
    )
    accounts.value = data?.data ?? []
  } catch (e) {
    error.value = message(e)
  }
}

async function openAccount(id: string) {
  try {
    detail.value =
      (await run(() => api().GET('/admin/billing/accounts/{id}', { params: { path: { id } } }))) ??
      null
  } catch (e) {
    error.value = message(e)
  }
}

// ---------------------------------------------------------------- facturas y discrepancias
const invoiceStatus = ref<'' | Invoice['status']>('rejected')
const invoices = ref<Invoice[]>([])
async function loadInvoices() {
  try {
    const data = await run(() =>
      api().GET('/admin/billing/invoices', {
        params: { query: { status: invoiceStatus.value || undefined, limit: 100 } },
      }),
    )
    invoices.value = data?.data ?? []
  } catch (e) {
    error.value = message(e)
  }
}

const discrepancyStatus = ref<'open' | 'resolved'>('open')
const discrepancies = ref<Discrepancy[]>([])
async function loadDiscrepancies() {
  try {
    const data = await run(() =>
      api().GET('/admin/billing/discrepancies', {
        params: { query: { status: discrepancyStatus.value, limit: 100 } },
      }),
    )
    discrepancies.value = data?.data ?? []
  } catch (e) {
    error.value = message(e)
  }
}

// ---------------------------------------------------------------- cupones
const coupons = ref<Coupon[]>([])
const couponForm = reactive({
  code: '',
  kind: 'pct' as 'pct' | 'amount',
  value: '',
  duration_months: '',
  max_uses: '',
  valid_until: '',
  plans: '',
  reason: '',
})
async function loadCoupons() {
  try {
    coupons.value = (await run(() => api().GET('/admin/coupons', {})))?.data ?? []
  } catch (e) {
    error.value = message(e)
  }
}
async function createCoupon() {
  busy.value = true
  notice.value = null
  try {
    const f = couponForm
    const plans = f.plans
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    await run(() =>
      api().POST('/admin/coupons', {
        body: {
          code: f.code.trim().toUpperCase(),
          kind: f.kind,
          value: Number(f.value),
          duration_months: f.duration_months ? Number(f.duration_months) : undefined,
          max_uses: f.max_uses ? Number(f.max_uses) : undefined,
          valid_until: f.valid_until || undefined,
          plans: plans.length ? plans : undefined,
          reason: f.reason.trim(),
        },
      }),
    )
    notice.value = t('admin.billing.couponCreated', { code: f.code.trim().toUpperCase() })
    Object.assign(couponForm, {
      code: '',
      value: '',
      duration_months: '',
      max_uses: '',
      valid_until: '',
      plans: '',
      reason: '',
    })
    await loadCoupons()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

// ---------------------------------------------------------------- planes
const plans = ref<BillingPlan[]>([])
const planJson = ref('')
const planCode = ref<string | null>(null) // null = plan nuevo
async function loadPlans() {
  try {
    // Todas las versiones (draft, published, retired): la consola las gestiona.
    plans.value = (await run(() => api().GET('/admin/plans', {})))?.data ?? []
  } catch (e) {
    error.value = message(e)
  }
}
function toInput(p: BillingPlan): PlanInput {
  return {
    code: p.code,
    kind: p.kind,
    name: p.name,
    description: p.description,
    pricing: p.pricing,
    trial_days: p.trial_days,
    seats: p.seats,
    quotas: p.quotas,
    features: p.features,
    effective_from: new Date(Date.now() + 30 * 86_400_000).toISOString(),
  }
}
function editPlan(p: BillingPlan | null) {
  planCode.value = p?.code ?? null
  planJson.value = JSON.stringify(
    p
      ? toInput(p)
      : {
          code: '',
          kind: 'clinic',
          name: '',
          pricing: { model: 'per_seat', currency: 'COP', monthly: 0, annual: 0, tiers: [] },
          trial_days: 0,
          seats: { min: 1, max: 0, included: 1 },
          quotas: {},
          features: [],
          effective_from: new Date().toISOString(),
        },
    null,
    2,
  )
}
async function savePlan() {
  let body: PlanInput
  try {
    body = JSON.parse(planJson.value) as PlanInput
  } catch {
    error.value = t('admin.billing.planJsonInvalid')
    return
  }
  busy.value = true
  notice.value = null
  try {
    const code = planCode.value
    if (code) {
      await run(() => api().PUT('/admin/plans/{code}', { params: { path: { code } }, body }))
    } else {
      await run(() => api().POST('/admin/plans', { body }))
    }
    notice.value = t('admin.billing.planSaved', { code: body.code })
    planJson.value = ''
    await loadPlans()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

function selectTab(next: Tab) {
  tab.value = next
  notice.value = null
  error.value = null
  const loaders: Record<Tab, () => Promise<void>> = {
    accounts: loadAccounts,
    invoices: loadInvoices,
    discrepancies: loadDiscrepancies,
    coupons: loadCoupons,
    plans: loadPlans,
  }
  void loaders[next]()
}

onMounted(() => {
  void loadAccounts()
  void loadPlans()
})

// ---------------------------------------------------------------- acciones con motivo
type Action =
  | 'extend_trial'
  | 'credit'
  | 'comp_plan'
  | 'change_plan'
  | 'cancel'
  | 'manual_payment'
  | 'refund'
  | 'resolve'
  | 'retry_invoice'
  | 'publish_plan'
  | 'retire_plan'
const FIELDS: Record<Action, readonly BillingField[]> = {
  extend_trial: ['until'],
  credit: ['amount'],
  comp_plan: ['until'],
  change_plan: ['plan_code', 'seats', 'cycle'],
  cancel: [],
  manual_payment: ['amount', 'reference'],
  refund: ['amount'],
  resolve: ['resolution'],
  retry_invoice: [],
  publish_plan: ['effective_from'],
  retire_plan: [],
}
const SUB_ACTIONS: readonly Action[] = [
  'extend_trial',
  'credit',
  'comp_plan',
  'change_plan',
  'cancel',
]
const pending = ref<{ action: Action; id: string; label: string } | null>(null)

function ask(action: Action, id: string, label: string) {
  notice.value = null
  pending.value = { action, id, label }
  // La reemisión de una factura no lleva motivo en la API: se ejecuta directa.
  if (action === 'retry_invoice') void confirm({}, '')
}

function endOfDay(date: string) {
  return new Date(`${date}T23:59:59Z`).toISOString()
}

async function confirm(values: Record<string, string>, reason: string) {
  const p = pending.value
  if (!p) return
  busy.value = true
  try {
    const id = p.id
    const amount = Number(values.amount)
    switch (p.action) {
      case 'extend_trial':
      case 'credit':
      case 'comp_plan':
      case 'change_plan':
        await run(() =>
          api().POST('/admin/subscriptions/{id}/adjust', {
            params: { path: { id } },
            body: {
              kind: p.action as 'extend_trial',
              reason,
              until: values.until ? endOfDay(values.until) : undefined,
              amount: values.amount ? amount : undefined,
              plan_code: values.plan_code,
              seats: values.seats ? Number(values.seats) : undefined,
              cycle: values.cycle as 'monthly' | 'annual' | undefined,
            },
          }),
        )
        break
      case 'cancel':
        await run(() =>
          api().POST('/admin/subscriptions/{id}/cancel', {
            params: { path: { id } },
            body: { reason },
          }),
        )
        break
      case 'manual_payment':
        await run(() =>
          api().POST('/admin/charges/{id}/manual-payment', {
            params: { path: { id } },
            body: { amount, reference: values.reference ?? '', reason },
          }),
        )
        break
      case 'refund':
        await run(() =>
          api().POST('/admin/charges/{id}/refund', {
            params: { path: { id } },
            body: { amount, reason },
          }),
        )
        break
      case 'resolve':
        await run(() =>
          api().POST('/admin/billing/discrepancies/{id}/resolve', {
            params: { path: { id } },
            body: { resolution: values.resolution ?? '', reason },
          }),
        )
        break
      case 'retry_invoice':
        await run(() => api().POST('/admin/invoices/{id}/retry', { params: { path: { id } } }))
        break
      case 'publish_plan':
      case 'retire_plan': {
        const [code, version] = planRef(id)
        const path = { params: { path: { code } } }
        if (p.action === 'publish_plan') {
          await run(() =>
            api().POST('/admin/plans/{code}/publish', {
              ...path,
              body: { version, effective_from: values.effective_from ?? '', reason },
            }),
          )
        } else {
          await run(() =>
            api().POST('/admin/plans/{code}/retire', { ...path, body: { version, reason } }),
          )
        }
        await loadPlans()
        break
      }
    }
    notice.value = t(`admin.billing.done.${p.action}`)
    pending.value = null
    if (detail.value) await openAccount(detail.value.account.id)
    if (tab.value === 'invoices') await loadInvoices()
    if (tab.value === 'discrepancies') await loadDiscrepancies()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

// Planes contratables: una entrada por código con versión publicada.
const planOptions = computed(() => {
  const seen = new Set<string>()
  return plans.value
    .filter((p) => p.status === 'published' && !seen.has(p.code) && seen.add(p.code))
    .map((p) => ({ code: p.code, name: `${p.name} (${p.kind})` }))
})
// Las acciones de plan identifican la versión como "código@versión".
function planKey(p: BillingPlan) {
  return `${p.code}@${p.version}`
}
function planRef(key: string): [string, number] {
  const at = key.lastIndexOf('@')
  return [key.slice(0, at), Number(key.slice(at + 1))]
}
function charges(s: Subscription): Charge[] {
  return s.charges ?? []
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('admin.nav.billing') }}</h1>

    <div class="flex flex-wrap gap-2" role="tablist">
      <button
        v-for="s in TABS"
        :key="s"
        type="button"
        role="tab"
        :aria-selected="tab === s"
        class="rounded-full px-3 py-1.5 text-sm font-medium"
        :class="
          tab === s
            ? 'bg-surface-900 text-white dark:bg-surface-100 dark:text-surface-900'
            : 'bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300'
        "
        :data-testid="`tab-${s}`"
        @click="selectTab(s)"
      >
        {{ t(`admin.billing.tab.${s}`) }}
      </button>
    </div>

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

    <!-- Cuentas -->
    <template v-if="tab === 'accounts'">
      <form class="flex flex-wrap gap-2" role="search" @submit.prevent="loadAccounts">
        <label class="sr-only" for="ba-q">{{ t('admin.billing.search') }}</label>
        <input
          id="ba-q"
          v-model="filters.q"
          type="search"
          class="input max-w-xs"
          :placeholder="t('admin.billing.search')"
        />
        <label class="sr-only" for="ba-status">{{ t('admin.billing.status') }}</label>
        <select id="ba-status" v-model="filters.status" class="input max-w-[12rem]">
          <option value="">{{ t('admin.billing.allStatuses') }}</option>
          <option
            v-for="s in ['trialing', 'active', 'grace', 'restricted', 'cancelled']"
            :key="s"
            :value="s"
          >
            {{ t(`billing.status.${s}`) }}
          </option>
        </select>
        <label class="sr-only" for="ba-country">{{ t('admin.billing.country') }}</label>
        <input
          id="ba-country"
          v-model="filters.country"
          class="input w-20"
          maxlength="2"
          :placeholder="t('admin.billing.country')"
        />
        <button type="submit" class="btn-secondary">{{ t('admin.search') }}</button>
      </form>

      <div class="grid gap-6 lg:grid-cols-[1fr_28rem]">
        <div>
          <p v-if="!accounts.length" class="text-surface-500">
            {{ t('admin.billing.noAccounts') }}
          </p>
          <ul class="space-y-2">
            <li v-for="a in accounts" :key="a.id">
              <button
                type="button"
                class="card w-full text-left hover:border-primary-400"
                :class="detail?.account.id === a.id ? 'border-primary-500' : ''"
                data-testid="account-row"
                @click="openAccount(a.id)"
              >
                <span class="block font-semibold">{{ a.legal_name }}</span>
                <span class="block text-sm text-surface-500">
                  {{ a.country }} · {{ a.currency }} ·
                  <template v-for="(s, i) in a.subscriptions" :key="s.id">
                    {{ i ? ', ' : '' }}{{ s.plan_code }} ({{
                      t(`billing.status.${s.status}`, s.status)
                    }})
                  </template>
                </span>
              </button>
            </li>
          </ul>
        </div>

        <aside v-if="detail" class="card space-y-4 self-start" data-testid="account-detail">
          <div>
            <h2 class="text-lg font-semibold">{{ detail.account.legal_entity.legal_name }}</h2>
            <p class="text-sm text-surface-500">
              {{ detail.account.legal_entity.tax_id.kind }}
              {{ detail.account.legal_entity.tax_id.value }} ·
              {{ detail.account.country }}
            </p>
            <p v-if="detail.account.payment_method" class="text-sm">
              {{ detail.account.payment_method.brand ?? detail.account.payment_method.kind }}
              <template v-if="detail.account.payment_method.last4">
                •••• {{ detail.account.payment_method.last4 }}</template
              >
            </p>
          </div>

          <div
            v-for="s in detail.subscriptions"
            :key="s.id"
            class="space-y-2 border-t border-surface-200 pt-3 dark:border-surface-700"
          >
            <p class="font-medium">
              {{ s.plan.name }} · {{ t(`billing.status.${s.status}`) }} ·
              {{ t(`billing.cycle.${s.cycle}`) }}
            </p>
            <div class="flex flex-wrap gap-1">
              <button
                v-for="a in SUB_ACTIONS"
                :key="a"
                type="button"
                class="btn-secondary px-2 py-1 text-xs"
                :data-testid="`sub-action-${a}`"
                @click="ask(a, s.id, s.plan.name)"
              >
                {{ t(`admin.billing.action.${a}`) }}
              </button>
            </div>
            <ul class="text-sm">
              <li
                v-for="c in charges(s)"
                :key="c.id"
                class="flex flex-wrap items-center justify-between gap-1 py-0.5"
              >
                <span>#{{ c.number }} · {{ money(c.amount, c.currency) }} · {{ c.status }}</span>
                <span class="flex gap-1">
                  <button
                    v-if="c.status === 'pending' || c.status === 'failed'"
                    type="button"
                    class="underline"
                    @click="ask('manual_payment', c.id, `#${c.number}`)"
                  >
                    {{ t('admin.billing.action.manual_payment') }}
                  </button>
                  <button
                    v-if="c.status === 'paid' || c.status === 'partially_refunded'"
                    type="button"
                    class="underline"
                    @click="ask('refund', c.id, `#${c.number}`)"
                  >
                    {{ t('admin.billing.action.refund') }}
                  </button>
                </span>
              </li>
            </ul>
          </div>

          <div class="border-t border-surface-200 pt-3 dark:border-surface-700">
            <h3 class="font-medium">{{ t('billing.page.payments') }}</h3>
            <ul class="text-sm">
              <li v-for="p in detail.payments" :key="p.id">
                {{ d(new Date(p.at), 'short') }} · {{ t(`billing.paymentKind.${p.kind}`) }} ·
                {{ money(p.amount, p.currency) }} · {{ p.status }}
              </li>
            </ul>
          </div>

          <div class="border-t border-surface-200 pt-3 dark:border-surface-700">
            <h3 class="font-medium">{{ t('billing.page.invoices') }}</h3>
            <ul class="text-sm">
              <li v-for="i in detail.invoices" :key="i.id" class="flex justify-between gap-2">
                <span
                  >{{ i.number ?? '—' }} · {{ money(i.total, i.currency) }} ·
                  {{ t(`billing.invoiceStatus.${i.status}`) }}</span
                >
                <button
                  v-if="i.status === 'rejected' || i.status === 'contingency'"
                  type="button"
                  class="underline"
                  @click="ask('retry_invoice', i.id, i.number ?? i.id)"
                >
                  {{ t('admin.billing.action.retry_invoice') }}
                </button>
              </li>
            </ul>
          </div>

          <div
            v-if="detail.discrepancies.length"
            class="border-t border-surface-200 pt-3 dark:border-surface-700"
          >
            <h3 class="font-medium">{{ t('admin.billing.tab.discrepancies') }}</h3>
            <ul class="text-sm">
              <li v-for="x in detail.discrepancies" :key="x.id" class="flex justify-between gap-2">
                <span>{{ x.kind }} · {{ x.status }}</span>
                <button
                  v-if="x.status === 'open'"
                  type="button"
                  class="underline"
                  @click="ask('resolve', x.id, x.kind)"
                >
                  {{ t('admin.billing.action.resolve') }}
                </button>
              </li>
            </ul>
          </div>

          <div class="border-t border-surface-200 pt-3 dark:border-surface-700">
            <h3 class="font-medium">{{ t('admin.billing.audit') }}</h3>
            <p v-if="!detail.audit.length" class="text-sm text-surface-500">
              {{ t('admin.billing.noAudit') }}
            </p>
            <ul class="text-sm" data-testid="account-audit">
              <li v-for="a in detail.audit" :key="a.id">
                {{ d(new Date(a.occurred_at), 'short') }} ·
                {{ a.action.replace('admin.billing.', '') }}
                <span v-if="a.reason" class="block text-surface-500">{{ a.reason }}</span>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </template>

    <!-- Facturas -->
    <template v-else-if="tab === 'invoices'">
      <form class="flex gap-2" @submit.prevent="loadInvoices">
        <label class="sr-only" for="inv-status">{{ t('admin.billing.status') }}</label>
        <select id="inv-status" v-model="invoiceStatus" class="input max-w-[14rem]">
          <option value="">{{ t('admin.billing.allStatuses') }}</option>
          <option
            v-for="s in ['issued', 'accepted', 'rejected', 'contingency', 'credited']"
            :key="s"
            :value="s"
          >
            {{ t(`billing.invoiceStatus.${s}`) }}
          </option>
        </select>
        <button type="submit" class="btn-secondary">{{ t('admin.search') }}</button>
      </form>
      <p v-if="!invoices.length" class="text-surface-500">{{ t('billing.page.noInvoices') }}</p>
      <ul class="space-y-2" data-testid="admin-invoices">
        <li
          v-for="i in invoices"
          :key="i.id"
          class="card flex flex-wrap items-center justify-between gap-2"
        >
          <span>
            {{ i.number ?? '—' }} · {{ money(i.total, i.currency) }} ·
            {{ t(`billing.invoiceStatus.${i.status}`) }}
            <span v-if="i.rejection_reason" class="block text-sm text-red-600">{{
              i.rejection_reason
            }}</span>
          </span>
          <button
            v-if="i.status === 'rejected' || i.status === 'contingency'"
            type="button"
            class="btn-secondary"
            @click="ask('retry_invoice', i.id, i.number ?? i.id)"
          >
            {{ t('admin.billing.action.retry_invoice') }}
          </button>
        </li>
      </ul>
    </template>

    <!-- Discrepancias -->
    <template v-else-if="tab === 'discrepancies'">
      <div class="flex gap-2">
        <select
          v-model="discrepancyStatus"
          class="input max-w-[12rem]"
          :aria-label="t('admin.billing.status')"
          @change="loadDiscrepancies"
        >
          <option value="open">{{ t('admin.billing.open') }}</option>
          <option value="resolved">{{ t('admin.billing.resolved') }}</option>
        </select>
      </div>
      <p v-if="!discrepancies.length" class="text-surface-500">
        {{ t('admin.billing.noDiscrepancies') }}
      </p>
      <ul class="space-y-2" data-testid="admin-discrepancies">
        <li
          v-for="x in discrepancies"
          :key="x.id"
          class="card flex flex-wrap items-center justify-between gap-2"
        >
          <span>
            {{ x.kind }} · {{ d(new Date(x.detected_at), 'short') }}
            <span v-if="x.resolution" class="block text-sm text-surface-500">{{
              x.resolution
            }}</span>
          </span>
          <span class="flex gap-2">
            <button
              type="button"
              class="btn-secondary"
              @click="openAccount(x.account_id).then(() => (tab = 'accounts'))"
            >
              {{ t('admin.billing.viewAccount') }}
            </button>
            <button
              v-if="x.status === 'open'"
              type="button"
              class="btn-primary"
              @click="ask('resolve', x.id, x.kind)"
            >
              {{ t('admin.billing.action.resolve') }}
            </button>
          </span>
        </li>
      </ul>
    </template>

    <!-- Cupones -->
    <template v-else-if="tab === 'coupons'">
      <form
        class="card grid gap-3 sm:grid-cols-4"
        data-testid="coupon-form"
        @submit.prevent="createCoupon"
      >
        <div>
          <label class="label" for="c-code">{{ t('admin.billing.coupon.code') }}</label>
          <input
            id="c-code"
            v-model="couponForm.code"
            class="input"
            required
            maxlength="32"
            pattern="[A-Za-z0-9_-]{3,32}"
          />
        </div>
        <div>
          <label class="label" for="c-kind">{{ t('admin.billing.coupon.kind') }}</label>
          <select id="c-kind" v-model="couponForm.kind" class="input">
            <option value="pct">{{ t('admin.billing.coupon.pct') }}</option>
            <option value="amount">{{ t('admin.billing.coupon.amount') }}</option>
          </select>
        </div>
        <div>
          <label class="label" for="c-value">{{ t('admin.billing.coupon.value') }}</label>
          <input
            id="c-value"
            v-model="couponForm.value"
            type="number"
            min="0"
            step="any"
            class="input"
            required
          />
        </div>
        <div>
          <label class="label" for="c-duration">{{ t('admin.billing.coupon.duration') }}</label>
          <input
            id="c-duration"
            v-model="couponForm.duration_months"
            type="number"
            min="1"
            max="36"
            class="input"
          />
        </div>
        <div>
          <label class="label" for="c-max">{{ t('admin.billing.coupon.maxUses') }}</label>
          <input id="c-max" v-model="couponForm.max_uses" type="number" min="1" class="input" />
        </div>
        <div>
          <label class="label" for="c-until">{{ t('admin.billing.coupon.validUntil') }}</label>
          <input id="c-until" v-model="couponForm.valid_until" type="date" class="input" />
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="c-plans">{{ t('admin.billing.coupon.plans') }}</label>
          <input id="c-plans" v-model="couponForm.plans" class="input" />
        </div>
        <div class="sm:col-span-4">
          <label class="label" for="c-reason">{{ t('admin.reason.label') }}</label>
          <textarea
            id="c-reason"
            v-model="couponForm.reason"
            class="input"
            minlength="10"
            maxlength="500"
            required
          />
        </div>
        <div class="sm:col-span-4">
          <button
            type="submit"
            class="btn-primary"
            :disabled="busy || couponForm.reason.trim().length < 10"
          >
            {{ t('admin.billing.coupon.create') }}
          </button>
        </div>
      </form>
      <p v-if="!coupons.length" class="text-surface-500">{{ t('admin.billing.coupon.empty') }}</p>
      <ul class="space-y-1 text-sm" data-testid="coupons">
        <li v-for="c in coupons" :key="c.code" class="card">
          <span class="font-semibold">{{ c.code }}</span> ·
          {{ c.kind === 'pct' ? `${c.value} %` : c.value }} ·
          {{ t('admin.billing.coupon.usesOf', { uses: c.uses, max: c.max_uses ?? '∞' }) }}
          <template v-if="c.duration_months">
            · {{ t('admin.billing.coupon.months', { n: c.duration_months }) }}</template
          >
          <template v-if="c.valid_until"> · {{ c.valid_until }}</template>
        </li>
      </ul>
    </template>

    <!-- Planes -->
    <template v-else>
      <p class="text-sm text-surface-500">{{ t('admin.billing.plansHelp') }}</p>
      <ul class="space-y-2" data-testid="plans">
        <li
          v-for="p in plans"
          :key="`${p.code}-${p.version}`"
          class="card flex flex-wrap items-center justify-between gap-2"
        >
          <span>
            <span class="font-semibold">{{ p.name }}</span> · {{ p.code }} v{{ p.version }} ·
            {{ p.kind }} · {{ t(`admin.billing.planStatus.${p.status}`) }}
            <template v-if="p.effective_from && p.status !== 'draft'">
              · {{ d(new Date(p.effective_from), 'short') }}
            </template>
          </span>
          <span class="flex flex-wrap gap-2">
            <button
              v-if="p.status === 'draft'"
              type="button"
              class="btn-primary"
              :data-testid="`publish-${planKey(p)}`"
              @click="ask('publish_plan', planKey(p), `${p.name} v${p.version}`)"
            >
              {{ t('admin.billing.action.publish_plan') }}
            </button>
            <button
              v-if="p.status === 'published'"
              type="button"
              class="btn-secondary"
              :data-testid="`retire-${planKey(p)}`"
              @click="ask('retire_plan', planKey(p), `${p.name} v${p.version}`)"
            >
              {{ t('admin.billing.action.retire_plan') }}
            </button>
            <button type="button" class="btn-secondary" @click="editPlan(p)">
              {{ t('admin.billing.newVersion') }}
            </button>
          </span>
        </li>
      </ul>
      <button type="button" class="btn-secondary" @click="editPlan(null)">
        {{ t('admin.billing.newPlan') }}
      </button>
      <form v-if="planJson" class="card space-y-2" @submit.prevent="savePlan">
        <label class="label" for="plan-json">
          {{
            planCode ? t('admin.billing.versionOf', { code: planCode }) : t('admin.billing.newPlan')
          }}
        </label>
        <textarea
          id="plan-json"
          v-model="planJson"
          class="input min-h-80 font-mono text-xs"
          spellcheck="false"
        />
        <div class="flex gap-2">
          <button type="button" class="btn-secondary" @click="planJson = ''">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn-primary" :disabled="busy">{{ t('common.save') }}</button>
        </div>
      </form>
    </template>

    <BillingActionDialog
      v-if="pending && pending.action !== 'retry_invoice'"
      :title="t(`admin.billing.action.${pending.action}`)"
      :target="pending.label"
      :fields="FIELDS[pending.action]"
      :plans="planOptions"
      :busy="busy"
      @confirm="confirm"
      @cancel="pending = null"
    />
  </section>
</template>
