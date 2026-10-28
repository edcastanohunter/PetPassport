<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, type Organization } from '@/shared/api/client'
import ReasonDialog from './ReasonDialog.vue'
import { ORG_ACTIONS, useAdminStore, type OrgAction, type OrgStatus } from './adminStore'

// Organizaciones (FE-009, PRD-017): pestañas por estado (la cola de
// verificación primero, FIFO), detalle lateral con datos legales, fundador y
// miembros; verificar/rechazar/suspender/reactivar con modal de motivo.
const { t, d } = useI18n()
const store = useAdminStore()

const STATUSES: readonly OrgStatus[] = ['pending_verification', 'active', 'suspended', 'rejected']
const status = ref<OrgStatus>('pending_verification')
const q = ref('')
const selected = ref<Organization | null>(null)
const pending = ref<OrgAction | null>(null)
const busy = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

const actions = computed(() => (selected.value ? ORG_ACTIONS[selected.value.status] : []))

function message(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function load(more = false) {
  error.value = null
  try {
    await store.loadOrgs(status.value, q.value, more)
    if (!more) selected.value = null
  } catch (e) {
    error.value = message(e)
  }
}

function selectTab(s: OrgStatus) {
  status.value = s
  notice.value = null
  void load()
}

onMounted(() => load())

async function confirm(reason: string) {
  const org = selected.value
  const action = pending.value
  if (!org || !action) return
  busy.value = true
  error.value = null
  try {
    await store.orgAction(org.id, action, reason)
    notice.value = t(`admin.orgs.done.${action}`, { name: org.display_name })
    selected.value = null
    pending.value = null
  } catch (e) {
    // Otro admin la movió: se recarga la pestaña para no actuar sobre datos viejos.
    if (e instanceof ApiError && e.code === 'INVALID_STATUS_TRANSITION') {
      pending.value = null
      await load()
    }
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('admin.nav.organizations') }}</h1>

    <div class="flex flex-wrap gap-2" role="tablist">
      <button
        v-for="s in STATUSES"
        :key="s"
        type="button"
        role="tab"
        :aria-selected="status === s"
        class="rounded-full px-3 py-1.5 text-sm font-medium"
        :class="
          status === s
            ? 'bg-surface-900 text-white dark:bg-surface-100 dark:text-surface-900'
            : 'bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300'
        "
        :data-testid="`tab-${s}`"
        @click="selectTab(s)"
      >
        {{ t(`orgs.status.${s}`) }}
      </button>
    </div>

    <form class="flex gap-2" role="search" @submit.prevent="load()">
      <label class="sr-only" for="org-search">{{ t('admin.orgs.search') }}</label>
      <input
        id="org-search"
        v-model="q"
        type="search"
        class="input"
        :placeholder="t('admin.orgs.search')"
      />
      <button type="submit" class="btn-secondary">{{ t('admin.search') }}</button>
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

    <div class="grid gap-6 lg:grid-cols-[1fr_22rem]">
      <div>
        <p v-if="!store.orgs.length" class="text-surface-500">{{ t('admin.orgs.empty') }}</p>
        <ul class="space-y-2">
          <li v-for="o in store.orgs" :key="o.id">
            <button
              type="button"
              class="card w-full text-left hover:border-primary-400"
              :class="selected?.id === o.id ? 'border-primary-500' : ''"
              :aria-pressed="selected?.id === o.id"
              data-testid="org-row"
              @click="selected = o"
            >
              <span class="block font-semibold">{{ o.display_name }}</span>
              <span class="block text-sm text-surface-500">
                {{ t(`orgs.type.${o.type}`) }} · {{ o.address.city }} ·
                {{ t('admin.orgs.createdAt', { date: d(new Date(o.created_at), 'short') }) }}
              </span>
            </button>
          </li>
        </ul>
        <button
          v-if="store.orgsCursor"
          type="button"
          class="btn-secondary mt-3"
          @click="load(true)"
        >
          {{ t('admin.loadMore') }}
        </button>
      </div>

      <aside
        v-if="selected"
        class="card space-y-3 self-start"
        :aria-label="t('admin.orgs.detail')"
        data-testid="org-detail"
      >
        <h2 class="text-lg font-semibold">{{ selected.display_name }}</h2>
        <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <dt class="text-surface-500">{{ t('admin.orgs.legalName') }}</dt>
          <dd>{{ selected.legal_name }}</dd>
          <dt class="text-surface-500">{{ t('admin.orgs.taxId') }}</dt>
          <dd>{{ selected.tax_id }}</dd>
          <dt class="text-surface-500">{{ t('admin.orgs.address') }}</dt>
          <dd>
            {{ selected.address.line1
            }}<template v-if="selected.address.line2">, {{ selected.address.line2 }}</template
            ><br />{{ selected.address.city }}, {{ selected.address.region }} ·
            {{ selected.address.country }}
          </dd>
          <dt class="text-surface-500">{{ t('admin.orgs.contact') }}</dt>
          <dd>{{ selected.contact.email }} · {{ selected.contact.phone }}</dd>
          <dt class="text-surface-500">{{ t('admin.orgs.founder') }}</dt>
          <dd data-testid="org-founder">
            <template v-if="selected.founder">
              {{ selected.founder.display_name || '—' }} · {{ selected.founder.email }}
            </template>
            <template v-else>—</template>
          </dd>
          <dt class="text-surface-500">{{ t('admin.orgs.members') }}</dt>
          <dd>{{ selected.member_count ?? '—' }}</dd>
          <template v-if="selected.status_reason">
            <dt class="text-surface-500">{{ t('orgs.statusReason') }}</dt>
            <dd>{{ selected.status_reason }}</dd>
          </template>
        </dl>
        <div v-if="actions.length" class="flex flex-wrap gap-2 pt-2">
          <button
            v-for="a in actions"
            :key="a"
            type="button"
            :class="a === 'verify' || a === 'reactivate' ? 'btn-primary' : 'btn-secondary'"
            :disabled="busy"
            :data-testid="`action-${a}`"
            @click="pending = a"
          >
            {{ t(`admin.orgs.action.${a}`) }}
          </button>
        </div>
      </aside>
    </div>

    <ReasonDialog
      v-if="pending && selected"
      :title="t(`admin.orgs.action.${pending}`)"
      :target="selected.display_name"
      :optional="pending === 'verify'"
      :busy="busy"
      @confirm="confirm"
      @cancel="pending = null"
    />
  </section>
</template>
