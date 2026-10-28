<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, type AdminUserRow } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import ReasonDialog from './ReasonDialog.vue'
import { useAdminStore, type UserAction } from './adminStore'

// Usuarios (FE-009, PRD-018): búsqueda por email/nombre, tabla con roles
// globales, membresías y mascotas; suspender/reactivar con motivo. La propia
// cuenta no se puede suspender (el backend también lo impide).
const { t, d } = useI18n()
const store = useAdminStore()
const session = useSessionStore()

const q = ref('')
const status = ref<'' | AdminUserRow['status']>('')
const pending = ref<{ user: AdminUserRow; action: UserAction } | null>(null)
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function load(more = false) {
  error.value = null
  try {
    await store.loadUsers(q.value, status.value || undefined, more)
  } catch (e) {
    error.value = message(e)
  }
}

onMounted(() => load())

const isSelf = (u: AdminUserRow) => u.id === session.me?.id

async function confirm(reason: string) {
  if (!pending.value) return
  const { user, action } = pending.value
  busy.value = true
  error.value = null
  try {
    await store.userAction(user.id, action, reason)
    notice.value = t(`admin.users.done.${action}`, { email: user.email })
    pending.value = null
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('admin.nav.users') }}</h1>

    <form class="flex flex-wrap gap-2" role="search" @submit.prevent="load()">
      <label class="sr-only" for="user-search">{{ t('admin.users.search') }}</label>
      <input
        id="user-search"
        v-model="q"
        type="search"
        class="input flex-1"
        :placeholder="t('admin.users.search')"
      />
      <label class="sr-only" for="user-status">{{ t('admin.users.status') }}</label>
      <select id="user-status" v-model="status" class="input w-auto">
        <option value="">{{ t('admin.users.anyStatus') }}</option>
        <option value="active">{{ t('admin.users.statusValue.active') }}</option>
        <option value="suspended">{{ t('admin.users.statusValue.suspended') }}</option>
      </select>
      <button type="submit" class="btn-secondary" data-testid="search-users">
        {{ t('admin.search') }}
      </button>
    </form>

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

    <p v-if="!store.users.length" class="text-surface-500">{{ t('admin.users.empty') }}</p>
    <div v-else class="overflow-x-auto">
      <table class="table-cards w-full text-left text-sm">
        <thead class="text-surface-500">
          <tr>
            <th class="py-2">{{ t('admin.users.user') }}</th>
            <th>{{ t('admin.users.roles') }}</th>
            <th>{{ t('admin.users.memberships') }}</th>
            <th>{{ t('admin.users.pets') }}</th>
            <th>{{ t('admin.users.lastLogin') }}</th>
            <th>{{ t('admin.users.status') }}</th>
            <th>
              <span class="sr-only">{{ t('orgs.members.actions') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="u in store.users"
            :key="u.id"
            class="border-t border-surface-200 align-top dark:border-surface-700"
            data-testid="user-row"
          >
            <td class="py-2" :data-label="t('admin.users.user')">
              <span class="block font-medium">{{ u.display_name || '—' }}</span>
              <span class="block text-surface-500">{{ u.email }}</span>
            </td>
            <td :data-label="t('admin.users.roles')">
              <span
                v-for="r in u.global_roles"
                :key="r"
                class="mr-1 inline-block rounded bg-surface-100 px-1.5 py-0.5 text-xs dark:bg-surface-800"
                >{{ r }}</span
              >
            </td>
            <td :data-label="t('admin.users.memberships')">
              <span v-if="!u.memberships.length">—</span>
              <span v-for="m in u.memberships" :key="m.organization_id" class="block">
                {{ m.display_name }} · {{ t(`orgs.role.${m.role}`) }}
              </span>
            </td>
            <td :data-label="t('admin.users.pets')">{{ u.pet_count }}</td>
            <td :data-label="t('admin.users.lastLogin')">
              {{ u.last_login_at ? d(new Date(u.last_login_at), 'short') : '—' }}
            </td>
            <td :data-label="t('admin.users.status')">
              {{ t(`admin.users.statusValue.${u.status}`) }}
              <span v-if="u.status_reason" class="block text-xs text-surface-500">{{
                u.status_reason
              }}</span>
            </td>
            <td class="text-right">
              <button
                v-if="u.status === 'active'"
                type="button"
                class="text-red-600 underline disabled:opacity-40 dark:text-red-400"
                :disabled="busy || isSelf(u)"
                :title="isSelf(u) ? t('admin.users.self') : undefined"
                data-testid="suspend-user"
                @click="pending = { user: u, action: 'suspend' }"
              >
                {{ t('admin.users.action.suspend') }}
              </button>
              <button
                v-else
                type="button"
                class="underline"
                :disabled="busy"
                data-testid="reactivate-user"
                @click="pending = { user: u, action: 'reactivate' }"
              >
                {{ t('admin.users.action.reactivate') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="store.usersCursor" type="button" class="btn-secondary" @click="load(true)">
      {{ t('admin.loadMore') }}
    </button>

    <ReasonDialog
      v-if="pending"
      :title="t(`admin.users.action.${pending.action}`)"
      :target="pending.user.email"
      :busy="busy"
      @confirm="confirm"
      @cancel="pending = null"
    />
  </section>
</template>
