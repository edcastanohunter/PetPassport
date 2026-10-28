<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Member, type OrgRole } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { useCommand } from '@/shared/composables/useCommand'
import { ADMIN_ROLES, emailSchema, ROLES_BY_TYPE, type OrgType } from '@/shared/validation/schemas'
import FieldError from '@/shared/ui/FieldError.vue'
import { useOrgStore } from './orgStore'

// Miembros e invitaciones (FE-007, PRD-013/014): tabla de miembros con
// cambio de rol (roles válidos por tipo, O1) y retiro con confirmación
// (deshabilitado para el último admin, O2); pestaña de invitaciones con
// crear, reenviar (máx 3/24 h) y revocar.
const { t, d } = useI18n()
const route = useRoute()
const session = useSessionStore()
const store = useOrgStore()
const { key: inviteKey, renew } = useCommand()

const orgId = computed(() => String(route.params.orgId))
const tab = ref<'members' | 'invitations'>('members')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const busy = ref(false)
const confirmRemove = ref<Member | null>(null)

const orgType = computed<OrgType>(
  () => (store.current?.type ?? session.membershipFor(orgId.value)?.type ?? 'clinic') as OrgType,
)
const roles = computed(() => ROLES_BY_TYPE[orgType.value] as readonly OrgRole[])
const isLastAdmin = (m: Member) => ADMIN_ROLES.includes(m.role) && store.adminCount() <= 1

const invite = ref({ email: '', role: '' as OrgRole | '' })
const inviteErrors = ref<Record<string, string>>({})

async function load() {
  error.value = null
  try {
    if (!store.current || store.current.id !== orgId.value) await store.load(orgId.value)
    await Promise.all([store.loadMembers(orgId.value), store.loadInvitations(orgId.value)])
  } catch (e) {
    error.value = message(e)
  }
}

onMounted(load)

function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function run(fn: () => Promise<void>, ok?: string) {
  busy.value = true
  error.value = null
  notice.value = null
  try {
    await fn()
    if (ok) notice.value = ok
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}

function changeRole(m: Member, role: OrgRole) {
  if (role === m.role) return
  void run(async () => {
    const { error: apiError } = await api().PATCH('/organizations/{orgId}/members/{userId}', {
      params: { path: { orgId: orgId.value, userId: m.user_id } },
      body: { role },
    })
    if (apiError) throw apiError
    await store.loadMembers(orgId.value)
    if (m.user_id === session.me?.id) await session.loadMe(true)
  }, t('orgs.members.roleChanged'))
}

function remove(m: Member) {
  void run(async () => {
    const { error: apiError } = await api().DELETE('/organizations/{orgId}/members/{userId}', {
      params: { path: { orgId: orgId.value, userId: m.user_id } },
    })
    if (apiError) throw apiError
    confirmRemove.value = null
    await store.loadMembers(orgId.value)
  }, t('orgs.members.removed'))
}

function sendInvitation() {
  const errs: Record<string, string> = {}
  if (!emailSchema().safeParse(invite.value.email).success) errs.email = 'INVALID'
  if (!invite.value.role) errs.role = 'REQUIRED'
  inviteErrors.value = errs
  if (Object.keys(errs).length) return
  void run(async () => {
    try {
      const { error: apiError } = await api().POST('/organizations/{orgId}/invitations', {
        params: { path: { orgId: orgId.value }, header: { 'Idempotency-Key': inviteKey.value } },
        body: { email: invite.value.email.trim(), role: invite.value.role as OrgRole },
      })
      if (apiError) throw apiError
    } finally {
      renew()
    }
    invite.value = { email: '', role: '' }
    await store.loadInvitations(orgId.value)
  }, t('orgs.invitations.sent'))
}

function resend(id: string) {
  void run(async () => {
    const { error: apiError } = await api().POST(
      '/organizations/{orgId}/invitations/{invitationId}/resend',
      { params: { path: { orgId: orgId.value, invitationId: id } } },
    )
    if (apiError) throw apiError
  }, t('orgs.invitations.resent'))
}

function revoke(id: string) {
  void run(async () => {
    const { error: apiError } = await api().DELETE(
      '/organizations/{orgId}/invitations/{invitationId}',
      { params: { path: { orgId: orgId.value, invitationId: id } } },
    )
    if (apiError) throw apiError
    await store.loadInvitations(orgId.value)
  }, t('orgs.invitations.revoked'))
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('orgs.nav.members') }}</h1>

    <div class="flex gap-2" role="tablist">
      <button
        v-for="tb in ['members', 'invitations'] as const"
        :key="tb"
        type="button"
        role="tab"
        :aria-selected="tab === tb"
        class="rounded-full px-3 py-1.5 text-sm font-medium"
        :class="
          tab === tb
            ? 'bg-surface-900 text-white dark:bg-surface-100 dark:text-surface-900'
            : 'bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300'
        "
        :data-testid="`tab-${tb}`"
        @click="tab = tb"
      >
        {{ t(`orgs.${tb}.tab`) }}
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

    <!-- Miembros: tabla en escritorio, tarjetas < 768 px (FE-010). -->
    <div v-if="tab === 'members'" class="overflow-x-auto">
      <table class="hidden w-full text-left text-sm md:table">
        <thead class="text-surface-500">
          <tr>
            <th class="py-2">{{ t('orgs.members.name') }}</th>
            <th>{{ t('orgs.members.email') }}</th>
            <th>{{ t('orgs.members.role') }}</th>
            <th>{{ t('orgs.members.since') }}</th>
            <th>
              <span class="sr-only">{{ t('orgs.members.actions') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="m in store.members"
            :key="m.user_id"
            class="border-t border-surface-200 dark:border-surface-700"
            data-testid="member-row"
          >
            <td class="py-2">{{ m.display_name || '—' }}</td>
            <td>{{ m.email }}</td>
            <td>
              <select
                :value="m.role"
                class="input w-auto"
                :aria-label="t('orgs.members.roleOf', { name: m.display_name || m.email })"
                :disabled="busy || isLastAdmin(m)"
                @change="changeRole(m, ($event.target as HTMLSelectElement).value as OrgRole)"
              >
                <option v-for="r in roles" :key="r" :value="r">{{ t(`orgs.role.${r}`) }}</option>
              </select>
            </td>
            <td>{{ d(new Date(m.joined_at), 'short') }}</td>
            <td class="text-right">
              <button
                type="button"
                class="text-red-600 underline disabled:opacity-40 dark:text-red-400"
                :disabled="busy || isLastAdmin(m)"
                :title="isLastAdmin(m) ? t('orgs.members.lastAdmin') : undefined"
                @click="confirmRemove = m"
              >
                {{ t('orgs.members.remove') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <ul class="space-y-3 md:hidden">
        <li v-for="m in store.members" :key="m.user_id" class="card">
          <p class="font-semibold">{{ m.display_name || m.email }}</p>
          <p class="text-sm text-surface-500">{{ m.email }} · {{ t(`orgs.role.${m.role}`) }}</p>
          <div class="mt-2 flex items-center gap-2">
            <select
              :value="m.role"
              class="input w-auto"
              :aria-label="t('orgs.members.roleOf', { name: m.display_name || m.email })"
              :disabled="busy || isLastAdmin(m)"
              @change="changeRole(m, ($event.target as HTMLSelectElement).value as OrgRole)"
            >
              <option v-for="r in roles" :key="r" :value="r">{{ t(`orgs.role.${r}`) }}</option>
            </select>
            <button
              type="button"
              class="text-sm text-red-600 underline disabled:opacity-40"
              :disabled="busy || isLastAdmin(m)"
              @click="confirmRemove = m"
            >
              {{ t('orgs.members.remove') }}
            </button>
          </div>
        </li>
      </ul>
      <div
        v-if="confirmRemove"
        class="mt-4 rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950"
        role="alertdialog"
        aria-modal="true"
      >
        <p>
          {{
            t('orgs.members.confirmRemove', {
              name: confirmRemove.display_name || confirmRemove.email,
            })
          }}
        </p>
        <div class="mt-2 flex gap-2">
          <button
            type="button"
            class="btn-primary bg-red-600 hover:bg-red-700"
            :disabled="busy"
            data-testid="confirm-remove"
            @click="remove(confirmRemove)"
          >
            {{ t('orgs.members.remove') }}
          </button>
          <button type="button" class="btn-secondary" @click="confirmRemove = null">
            {{ t('common.cancel') }}
          </button>
        </div>
      </div>
    </div>

    <div v-else class="space-y-6">
      <form
        class="card grid gap-3 sm:grid-cols-[1fr_auto_auto] sm:items-end"
        novalidate
        @submit.prevent="sendInvitation"
      >
        <div>
          <label class="label" for="invite-email">{{ t('orgs.members.email') }}</label>
          <input id="invite-email" v-model="invite.email" type="email" class="input" />
          <FieldError field="invite-email" :code="inviteErrors.email" />
        </div>
        <div>
          <label class="label" for="invite-role">{{ t('orgs.members.role') }}</label>
          <select id="invite-role" v-model="invite.role" class="input">
            <option value="" disabled>{{ t('common.select') }}</option>
            <option v-for="r in roles" :key="r" :value="r">{{ t(`orgs.role.${r}`) }}</option>
          </select>
          <FieldError field="invite-role" :code="inviteErrors.role" />
        </div>
        <button type="submit" class="btn-primary" :disabled="busy" data-testid="send-invitation">
          {{ t('orgs.invitations.send') }}
        </button>
      </form>

      <p v-if="!store.invitations.length" class="text-surface-500">
        {{ t('orgs.invitations.empty') }}
      </p>
      <ul class="space-y-3">
        <li
          v-for="inv in store.invitations"
          :key="inv.id"
          class="card flex flex-wrap items-center justify-between gap-2"
          data-testid="invitation-row"
        >
          <span>
            <span class="block font-medium">{{ inv.email }}</span>
            <span class="block text-sm text-surface-500">
              {{ t(`orgs.role.${inv.role}`) }} · {{ t(`orgs.invitations.status.${inv.status}`) }}
              <template v-if="inv.status === 'pending'">
                ·
                {{ t('orgs.invitations.expires', { date: d(new Date(inv.expires_at), 'short') }) }}
              </template>
            </span>
          </span>
          <span v-if="inv.status === 'pending'" class="flex gap-3 text-sm">
            <button type="button" class="underline" :disabled="busy" @click="resend(inv.id)">
              {{ t('orgs.invitations.resend') }}
            </button>
            <button
              type="button"
              class="text-red-600 underline dark:text-red-400"
              :disabled="busy"
              @click="revoke(inv.id)"
            >
              {{ t('orgs.invitations.revoke') }}
            </button>
          </span>
        </li>
      </ul>
    </div>
  </section>
</template>
