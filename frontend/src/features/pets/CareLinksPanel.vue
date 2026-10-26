<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type OrgSummary, type Pet } from '@/shared/api/client'
import { publicCodeSchema } from '@/shared/validation/schemas'

// Vínculos de atención en la ficha (FE-008, PRD-015): lista con estado y
// revocar; "Vincular con clínica" → código → previsualización OrgSummary →
// confirmar. Solo clínicas activas; máx. 20; vincular dos veces es idempotente.
const props = defineProps<{ pet: Pet }>()
const emit = defineEmits<{ changed: [] }>()
const { t, d } = useI18n()

const open = ref(false)
const code = ref('')
const preview = ref<OrgSummary | null>(null)
const error = ref<string | null>(null)
const busy = ref(false)
const confirmRevoke = ref<string | null>(null)

const activeLinks = computed(() => props.pet.care_links.filter((l) => l.status === 'active'))
const canLink = computed(() => props.pet.status === 'active')

function reset() {
  open.value = false
  code.value = ''
  preview.value = null
  error.value = null
}

function apiMessage(e: unknown) {
  return e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function lookup() {
  error.value = null
  preview.value = null
  const normalized = code.value.trim().toUpperCase()
  if (!publicCodeSchema().safeParse(normalized).success) {
    error.value = t('pets.links.codeInvalid')
    return
  }
  busy.value = true
  try {
    const { data, error: apiError } = await api().GET('/organizations/lookup', {
      params: { query: { code: normalized } },
    })
    if (apiError) throw apiError
    preview.value = data ?? null
  } catch (e) {
    error.value =
      e instanceof ApiError && e.status === 404 ? t('pets.links.notFound') : apiMessage(e)
  } finally {
    busy.value = false
  }
}

async function confirmLink() {
  if (!preview.value) return
  busy.value = true
  error.value = null
  try {
    const { error: apiError } = await api().POST('/pets/{petId}/care-links', {
      params: { path: { petId: props.pet.id } },
      body: { organization_id: preview.value.id },
    })
    if (apiError) throw apiError
    reset()
    emit('changed')
  } catch (e) {
    error.value = apiMessage(e)
  } finally {
    busy.value = false
  }
}

async function revoke(orgId: string) {
  busy.value = true
  error.value = null
  try {
    const { error: apiError } = await api().DELETE('/pets/{petId}/care-links/{organizationId}', {
      params: { path: { petId: props.pet.id, organizationId: orgId } },
    })
    if (apiError) throw apiError
    confirmRevoke.value = null
    emit('changed')
  } catch (e) {
    error.value = apiMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    class="rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
    data-testid="care-links"
  >
    <div class="flex items-center justify-between">
      <h2 class="font-semibold">{{ t('pets.links.title') }}</h2>
      <button
        v-if="canLink && !open"
        type="button"
        class="text-sm text-green-700 underline dark:text-green-400"
        data-testid="link-clinic"
        @click="open = true"
      >
        {{ t('pets.links.add') }}
      </button>
    </div>

    <p v-if="!activeLinks.length && !open" class="mt-1 text-sm text-surface-500">
      {{ t('pets.links.empty') }}
    </p>
    <ul v-else class="mt-2 divide-y divide-surface-200 dark:divide-surface-700">
      <li
        v-for="l in activeLinks"
        :key="l.organization_id"
        class="flex flex-wrap items-center justify-between gap-2 py-2"
      >
        <span>
          <span class="block font-medium">{{ l.display_name || t('pets.links.unnamed') }}</span>
          <span class="block text-xs text-surface-500">
            <template v-if="l.city">{{ l.city }} · </template
            >{{ t('pets.links.since', { date: d(new Date(l.granted_at), 'short') }) }}
          </span>
          <span
            v-if="l.org_suspended"
            class="mt-1 inline-block rounded-full bg-red-100 px-2 py-0.5 text-xs text-red-800 dark:bg-red-900 dark:text-red-200"
          >
            {{ t('pets.links.suspended') }}
          </span>
        </span>
        <span
          v-if="confirmRevoke === l.organization_id"
          class="flex items-center gap-2 text-sm"
          role="alert"
        >
          {{ t('pets.links.confirmRevoke') }}
          <button
            type="button"
            class="text-red-600 underline"
            :disabled="busy"
            data-testid="confirm-revoke"
            @click="revoke(l.organization_id)"
          >
            {{ t('pets.links.revoke') }}
          </button>
          <button type="button" class="text-surface-500" @click="confirmRevoke = null">
            {{ t('common.cancel') }}
          </button>
        </span>
        <button
          v-else
          type="button"
          class="text-sm text-red-600 underline dark:text-red-400"
          :disabled="busy"
          @click="confirmRevoke = l.organization_id"
        >
          {{ t('pets.links.revoke') }}
        </button>
      </li>
    </ul>

    <div
      v-if="open"
      class="mt-3 space-y-3 rounded-lg bg-surface-200/30 p-3"
      role="dialog"
      :aria-label="t('pets.links.add')"
    >
      <form class="flex flex-wrap items-end gap-2" @submit.prevent="lookup">
        <div>
          <label class="label" for="link-code">{{ t('pets.links.code') }}</label>
          <input
            id="link-code"
            v-model="code"
            maxlength="6"
            class="input w-32 font-mono uppercase tracking-widest"
            autocomplete="off"
          />
        </div>
        <button type="submit" class="btn-secondary" :disabled="busy">
          {{ t('pets.links.search') }}
        </button>
        <button type="button" class="text-sm text-surface-500" @click="reset">
          {{ t('common.cancel') }}
        </button>
      </form>
      <div v-if="preview" class="card" data-testid="link-preview">
        <p class="font-semibold">{{ preview.display_name }}</p>
        <p class="text-sm text-surface-500">{{ preview.city }} ({{ preview.country }})</p>
        <p class="mt-2 text-sm">{{ t('pets.links.confirmInfo', { name: pet.name }) }}</p>
        <button
          type="button"
          class="btn-primary mt-2"
          :disabled="busy"
          data-testid="confirm-link"
          @click="confirmLink"
        >
          {{ t('pets.links.confirm') }}
        </button>
      </div>
    </div>

    <p v-if="error" class="mt-2 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
  </div>
</template>
