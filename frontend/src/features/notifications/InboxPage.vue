<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type InboxItem } from '@/shared/api/client'
import { acceptProposal } from '@/features/scheduling/scheduleApi'
import { scheduleErrorKey } from '@/features/scheduling/scheduleFormat'

// Bandeja del tutor (FE-012): lo que espera una respuesta suya, de más
// reciente a más antiguo. Solicitudes de vínculo de una clínica (aprobar o
// rechazar, PRD-020), alternativas de cita propuestas (aceptar) y controles
// pendientes (agendar).
const { t, te, d } = useI18n()

const items = ref<InboxItem[]>([])
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const busy = ref(false)
const done = ref<string | null>(null)

function failure(e: unknown): string {
  if (e instanceof ApiError) return te(`errors.${e.code}`) ? `errors.${e.code}` : 'errors.GENERIC'
  return scheduleErrorKey(e, te)
}

async function load() {
  try {
    const { data, error: apiError } = await api().GET('/me/inbox')
    if (apiError) throw apiError
    items.value = data?.data ?? []
    state.value = 'ready'
  } catch (e) {
    error.value = failure(e)
    state.value = 'error'
  }
}
onMounted(load)

async function run(action: () => Promise<unknown>, doneKey: string) {
  if (busy.value) return
  busy.value = true
  error.value = null
  done.value = null
  try {
    await action()
    done.value = doneKey
    await load()
  } catch (e) {
    error.value = failure(e)
    await load() // la solicitud pudo caducar o resolverse en otro sitio
  } finally {
    busy.value = false
  }
}

function decideRequest(item: InboxItem, decision: 'approve' | 'decline') {
  const path = {
    petId: item.pet_id as string,
    requestId: item.ref_id,
  }
  return run(
    async () => {
      const res =
        decision === 'approve'
          ? await api().POST('/pets/{petId}/care-link-requests/{requestId}/approve', {
              params: { path },
            })
          : await api().POST('/pets/{petId}/care-link-requests/{requestId}/decline', {
              params: { path },
            })
      if (res.error) throw res.error
    },
    decision === 'approve' ? 'notifications.inbox.approved' : 'notifications.inbox.declined',
  )
}

function accept(item: InboxItem) {
  return run(() => acceptProposal(item.ref_id), 'notifications.inbox.accepted')
}
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-semibold">{{ t('notifications.inbox.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-700">{{ t(error) }}</p>
    <p v-if="done" role="status" class="text-sm text-green-700" data-testid="inbox-done">
      {{ t(done) }}
    </p>
    <p
      v-if="state === 'ready' && items.length === 0"
      class="text-sm text-surface-500"
      data-testid="inbox-empty"
    >
      {{ t('notifications.inbox.empty') }}
    </p>
    <ul v-if="items.length" class="space-y-2">
      <li
        v-for="it in items"
        :key="`${it.kind}:${it.ref_id}`"
        class="card space-y-2"
        data-testid="inbox-item"
      >
        <template v-if="it.kind === 'care_link_request'">
          <p class="font-medium" data-testid="inbox-carelink">
            {{ t('notifications.inbox.careLink', { clinic: it.title, pet: it.pet_name ?? '' }) }}
          </p>
          <p v-if="it.body" class="text-sm text-surface-500">«{{ it.body }}»</p>
          <p v-if="it.expires_at" class="text-xs text-surface-500">
            {{ t('notifications.inbox.expires', { date: d(new Date(it.expires_at), 'short') }) }}
          </p>
          <div class="flex gap-2">
            <button
              type="button"
              class="btn-primary"
              :disabled="busy"
              data-testid="inbox-approve"
              @click="decideRequest(it, 'approve')"
            >
              {{ t('notifications.inbox.approve') }}
            </button>
            <button
              type="button"
              class="btn-secondary"
              :disabled="busy"
              data-testid="inbox-decline"
              @click="decideRequest(it, 'decline')"
            >
              {{ t('notifications.inbox.decline') }}
            </button>
          </div>
        </template>

        <template v-else-if="it.kind === 'appointment_proposal'">
          <p class="font-medium">
            {{ t('notifications.inbox.proposal', { clinic: it.title, pet: it.pet_name ?? '' }) }}
          </p>
          <p v-if="it.body" class="text-sm">{{ it.body }}</p>
          <button type="button" class="btn-primary" :disabled="busy" @click="accept(it)">
            {{ t('scheduling.guardian.accept') }}
          </button>
        </template>

        <template v-else>
          <p class="font-medium">
            {{ t('notifications.inbox.followUp', { clinic: it.title, pet: it.pet_name ?? '' }) }}
          </p>
          <p v-if="it.body" class="text-sm">{{ it.body }}</p>
          <p v-if="it.expires_at" class="text-xs text-surface-500">
            {{ t('notifications.inbox.dueOn', { date: d(new Date(it.expires_at), 'short') }) }}
          </p>
          <RouterLink to="/app/appointments" class="underline text-sm">{{
            t('notifications.inbox.book')
          }}</RouterLink>
        </template>
      </li>
    </ul>
    <RouterLink to="/app/appointments" class="underline text-sm">{{
      t('notifications.inbox.allAppointments')
    }}</RouterLink>
  </section>
</template>
