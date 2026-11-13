<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'

type Invitation = components['schemas']['BillingOrgInvitation']

// Invitaciones que recibió la organización para facturar con la cuenta de una
// cadena (PP-F5-PRD-008). Aceptar vincula la organización a esa cuenta.
const props = defineProps<{ orgId: string }>()
const emit = defineEmits<{ accepted: [] }>()
const { t } = useI18n()

const invitations = ref<Invitation[]>([])
const busy = ref(false)
const error = ref<string | null>(null)

async function load() {
  const res = await api().GET('/organizations/{orgId}/billing-invitations', {
    params: { path: { orgId: props.orgId } },
  })
  invitations.value = res?.data?.data ?? []
}

onMounted(() => void load().catch(() => undefined))
watch(
  () => props.orgId,
  () => void load().catch(() => undefined),
)

async function resolve(inv: Invitation, action: 'accept' | 'decline') {
  busy.value = true
  error.value = null
  try {
    const path =
      action === 'accept'
        ? '/billing-invitations/{invitationId}/accept'
        : '/billing-invitations/{invitationId}/decline'
    const { error: err } = await api().POST(path, {
      params: { path: { invitationId: inv.id } },
    })
    if (err) throw err
    invitations.value = invitations.value.filter((i) => i.id !== inv.id)
    if (action === 'accept') emit('accepted')
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section
    v-if="invitations.length"
    class="card space-y-3"
    aria-labelledby="billing-invitations-title"
    data-testid="billing-invitations"
  >
    <h2 id="billing-invitations-title" class="text-lg font-semibold">
      {{ t('billing.orgs.invitationsTitle') }}
    </h2>
    <p v-if="error" class="text-sm text-red-700 dark:text-red-300" role="alert">{{ error }}</p>
    <ul class="divide-y divide-surface-200 dark:divide-surface-700">
      <li v-for="inv in invitations" :key="inv.id" class="flex flex-wrap items-center gap-3 py-2">
        <span class="flex-1">{{ t('billing.orgs.invitedBy', { account: inv.account_name }) }}</span>
        <button
          type="button"
          class="btn-secondary"
          :disabled="busy"
          data-testid="decline-invitation"
          @click="resolve(inv, 'decline')"
        >
          {{ t('billing.orgs.decline') }}
        </button>
        <button
          type="button"
          class="btn-primary"
          :disabled="busy"
          data-testid="accept-invitation"
          @click="resolve(inv, 'accept')"
        >
          {{ t('billing.orgs.accept') }}
        </button>
      </li>
    </ul>
    <p class="text-sm text-surface-500">{{ t('billing.orgs.acceptHint') }}</p>
  </section>
</template>
