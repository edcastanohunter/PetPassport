<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type InvitationPreview } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'

// Aceptar invitación (FE-007, PRD-013): previsualización (organización, rol,
// email enmascarado). Si el email de la sesión no coincide, mensaje claro y
// "Cambiar de cuenta" (logout + login). 404 = enlace inválido o caducado.
const { t, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const token = computed(() => String(route.params.token))
const preview = ref<InvitationPreview | null>(null)
const state = ref<'loading' | 'ready' | 'invalid' | 'mismatch' | 'unverified' | 'error'>('loading')
const busy = ref(false)

onMounted(async () => {
  try {
    const { data, error } = await api().GET('/invitations/{token}', {
      params: { path: { token: token.value } },
    })
    if (error) throw error
    preview.value = data ?? null
    state.value = 'ready'
  } catch (e) {
    state.value = e instanceof ApiError && e.status === 404 ? 'invalid' : 'error'
  }
})

async function accept() {
  busy.value = true
  try {
    const { data, error } = await api().POST('/invitations/{token}/accept', {
      params: { path: { token: token.value } },
    })
    if (error) throw error
    if (data) session.setMe(data)
    const orgId = preview.value?.organization.id
    await router.replace(orgId ? { name: 'org-summary', params: { orgId } } : { name: 'app' })
  } catch (e) {
    if (e instanceof ApiError && e.code === 'INVITATION_EMAIL_MISMATCH') state.value = 'mismatch'
    else if (e instanceof ApiError && e.code === 'EMAIL_NOT_VERIFIED') state.value = 'unverified'
    else if (e instanceof ApiError && e.status === 404) state.value = 'invalid'
    else state.value = 'error'
  } finally {
    busy.value = false
  }
}

async function switchAccount() {
  // Al volver tras el login, la ruta de la invitación se recupera de returnTo.
  sessionStorage.setItem('pp.returnTo', route.fullPath)
  await session.logout()
}
</script>

<template>
  <section class="mx-auto max-w-md space-y-4 py-8">
    <h1 class="text-2xl font-bold">{{ t('orgs.invite.title') }}</h1>

    <p v-if="state === 'loading'" class="text-surface-500">{{ t('common.loading') }}</p>

    <p
      v-else-if="state === 'invalid'"
      class="rounded-lg bg-surface-200/40 p-4"
      data-testid="invitation-invalid"
    >
      {{ t('orgs.invite.invalid') }}
    </p>

    <p
      v-else-if="state === 'error'"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ t('errors.GENERIC') }}
    </p>

    <template
      v-if="preview && (state === 'ready' || state === 'mismatch' || state === 'unverified')"
    >
      <div class="card space-y-1">
        <p class="text-lg font-semibold">{{ preview.organization.display_name }}</p>
        <p class="text-sm text-surface-500">
          {{ t(`orgs.type.${preview.organization.type}`) }} · {{ preview.organization.city }}
        </p>
        <p>{{ t('orgs.invite.role', { role: t(`orgs.role.${preview.role}`) }) }}</p>
        <p class="text-sm text-surface-500">
          {{ t('orgs.invite.for', { email: preview.email_masked }) }} ·
          {{ t('orgs.invitations.expires', { date: d(new Date(preview.expires_at), 'short') }) }}
        </p>
      </div>

      <div
        v-if="state === 'mismatch'"
        class="space-y-2 rounded-lg bg-amber-50 p-4 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
        role="alert"
        data-testid="invitation-mismatch"
      >
        <p>{{ t('orgs.invite.mismatch', { email: session.me?.email ?? '' }) }}</p>
        <button type="button" class="btn-secondary" @click="switchAccount">
          {{ t('orgs.invite.switchAccount') }}
        </button>
      </div>
      <p
        v-else-if="state === 'unverified'"
        class="rounded-lg bg-amber-50 p-4 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
        role="alert"
      >
        {{ t('errors.EMAIL_NOT_VERIFIED') }}
      </p>
      <button
        v-else
        type="button"
        class="btn-primary"
        :disabled="busy"
        data-testid="accept-invitation"
        @click="accept"
      >
        {{ t('orgs.invite.accept') }}
      </button>
    </template>
  </section>
</template>
