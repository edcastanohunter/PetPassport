<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'

type AccountOrg = components['schemas']['BillingAccountOrganization']
type Invitation = components['schemas']['BillingOrgInvitation']

// Organizaciones que facturan con la cuenta (PP-F5-PRD-008): invitar otra
// organización por su código público, revocar invitaciones pendientes y
// retirar organizaciones (nunca la última).
const props = defineProps<{ accountId: string; orgId: string }>()
const { t, d } = useI18n()

const orgs = ref<AccountOrg[]>([])
const pending = ref<Invitation[]>([])
const code = ref('')
const seatPool = ref<'own' | 'shared'>('own')
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

async function load() {
  const res = await api().GET('/billing-accounts/{id}/organizations', {
    params: { path: { id: props.accountId } },
  })
  orgs.value = res?.data?.data ?? []
  pending.value = res?.data?.invitations ?? []
}

onMounted(() => void load().catch(() => undefined))
watch(
  () => props.accountId,
  () => void load().catch(() => undefined),
)

async function run(fn: () => Promise<{ error?: unknown }>, done: string) {
  busy.value = true
  error.value = null
  notice.value = null
  try {
    const { error: err } = await fn()
    if (err) throw err
    notice.value = done
    await load()
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}

function invite() {
  const publicCode = code.value.trim().toUpperCase()
  if (publicCode.length < 3) return
  void run(
    () =>
      api().POST('/billing-accounts/{id}/organizations', {
        params: { path: { id: props.accountId } },
        body: { public_code: publicCode, seat_pool: seatPool.value },
      }),
    t('billing.orgs.invited'),
  ).then(() => {
    if (!error.value) code.value = ''
  })
}

function revoke(inv: Invitation) {
  void run(
    () =>
      api().POST('/billing-invitations/{invitationId}/decline', {
        params: { path: { invitationId: inv.id } },
      }),
    t('billing.orgs.revoked'),
  )
}

function detach(org: AccountOrg) {
  void run(
    () =>
      api().DELETE('/billing-accounts/{id}/organizations/{orgId}', {
        params: { path: { id: props.accountId, orgId: org.organization_id } },
      }),
    t('billing.orgs.detached'),
  )
}
</script>

<template>
  <section class="card space-y-3" aria-labelledby="account-orgs-title" data-testid="account-orgs">
    <h2 id="account-orgs-title" class="text-lg font-semibold">{{ t('billing.orgs.title') }}</h2>
    <p class="text-sm text-surface-500">{{ t('billing.orgs.intro') }}</p>
    <p v-if="error" class="text-sm text-red-700 dark:text-red-300" role="alert">{{ error }}</p>
    <p v-if="notice" class="text-sm text-green-800 dark:text-green-200" role="status">
      {{ notice }}
    </p>

    <ul class="divide-y divide-surface-200 text-sm dark:divide-surface-700">
      <li v-for="o in orgs" :key="o.organization_id" class="flex flex-wrap items-center gap-3 py-2">
        <span class="flex-1">
          {{ o.name || o.organization_id }}
          <span class="text-surface-500">
            · {{ t(`billing.orgs.pool.${o.seat_pool}`) }} ·
            {{ t('billing.orgs.joined', { date: d(new Date(o.joined_at), 'short') }) }}
          </span>
        </span>
        <button
          v-if="orgs.length > 1 && o.organization_id !== orgId"
          type="button"
          class="btn-secondary"
          :disabled="busy"
          data-testid="detach-org"
          @click="detach(o)"
        >
          {{ t('billing.orgs.detach') }}
        </button>
      </li>
      <li
        v-for="inv in pending"
        :key="inv.id"
        class="flex flex-wrap items-center gap-3 py-2"
        data-testid="pending-invitation"
      >
        <span class="flex-1">
          {{ inv.organization_name }}
          <span class="text-surface-500">· {{ t('billing.orgs.pending') }}</span>
        </span>
        <button
          type="button"
          class="btn-secondary"
          :disabled="busy"
          data-testid="revoke-invitation"
          @click="revoke(inv)"
        >
          {{ t('billing.orgs.revoke') }}
        </button>
      </li>
    </ul>

    <form class="flex flex-wrap items-end gap-2" data-testid="invite-org" @submit.prevent="invite">
      <div>
        <label class="label" for="invite-code">{{ t('billing.orgs.code') }}</label>
        <input
          id="invite-code"
          v-model="code"
          class="input uppercase"
          maxlength="32"
          autocomplete="off"
        />
      </div>
      <div>
        <label class="label" for="invite-pool">{{ t('billing.orgs.seatPool') }}</label>
        <select id="invite-pool" v-model="seatPool" class="input">
          <option value="own">{{ t('billing.orgs.pool.own') }}</option>
          <option value="shared">{{ t('billing.orgs.pool.shared') }}</option>
        </select>
      </div>
      <button
        type="submit"
        class="btn-primary"
        :disabled="busy || code.trim().length < 3"
        data-testid="send-invite"
      >
        {{ t('billing.orgs.invite') }}
      </button>
    </form>
  </section>
</template>
