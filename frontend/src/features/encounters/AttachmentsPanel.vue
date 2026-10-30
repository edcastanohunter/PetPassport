<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { isApiError, type Attachment } from '@/shared/api/client'
import {
  attachmentLink,
  deleteAttachment,
  setAttachmentVisibility,
  uploadAttachment,
} from './encounterApi'
import { openUrl } from './openUrl'

// Adjuntos de una consulta (PP-F2-PRD-005, ARQ-011, FE-003/004): subida
// (imagen o PDF ≤ 10 MB; en móvil `capture` abre la cámara), descarga por URL
// prefirmada auditada, visibilidad para el tutor (solo veterinario; ocultar
// exige motivo) y borrado en borrador. El servidor valida tipo y contenido.
const props = withDefaults(
  defineProps<{
    encounterId: string
    attachments: Attachment[]
    canUpload?: boolean
    canChangeVisibility?: boolean
    canDelete?: boolean
    max?: number
  }>(),
  { canUpload: false, canChangeVisibility: false, canDelete: false, max: 10 },
)
const emit = defineEmits<{ uploaded: [Attachment]; changed: [] }>()
const { t, d } = useI18n()

const busy = ref(false)
const error = ref<string | null>(null)
const description = ref('')
const visible = ref(true)
const hiding = ref<string | null>(null)
const hideReason = ref('')
const input = ref<HTMLInputElement | null>(null)
const camera = ref<HTMLInputElement | null>(null)

const MAX_BYTES = 10 * 1024 * 1024

function fail(e: unknown) {
  error.value = isApiError(e) ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function onFile(ev: Event) {
  const el = ev.target as HTMLInputElement
  const file = el.files?.[0]
  el.value = ''
  if (!file) return
  error.value = null
  if (file.size > MAX_BYTES) {
    error.value = t('errors.PAYLOAD_TOO_LARGE')
    return
  }
  busy.value = true
  try {
    const att = await uploadAttachment(props.encounterId, file, {
      description: description.value.trim() || undefined,
      visibleToGuardian: visible.value,
    })
    description.value = ''
    emit('uploaded', att)
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function download(a: Attachment) {
  error.value = null
  try {
    openUrl((await attachmentLink(props.encounterId, a.id)).url)
  } catch (e) {
    fail(e)
  }
}

async function toggle(a: Attachment) {
  if (a.visible_to_guardian && hiding.value !== a.id) {
    hiding.value = a.id
    hideReason.value = ''
    return
  }
  busy.value = true
  error.value = null
  try {
    await setAttachmentVisibility(
      props.encounterId,
      a.id,
      !a.visible_to_guardian,
      a.visible_to_guardian ? hideReason.value.trim() : undefined,
    )
    hiding.value = null
    emit('changed')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function remove(a: Attachment) {
  busy.value = true
  error.value = null
  try {
    await deleteAttachment(props.encounterId, a.id)
    emit('changed')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

function kindLabel(a: Attachment) {
  return a.content_type === 'pdf' ? 'PDF' : t('encounters.attachments.image')
}
function sizeLabel(n: number) {
  return n >= 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)} MB` : `${Math.ceil(n / 1024)} KB`
}
</script>

<template>
  <div class="space-y-3">
    <p
      v-if="error"
      class="rounded bg-red-50 p-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
      role="alert"
    >
      {{ error }}
    </p>
    <p v-if="attachments.length === 0" class="text-sm text-surface-500">
      {{ t('encounters.attachments.none') }}
    </p>
    <ul
      v-else
      class="divide-y divide-surface-200 dark:divide-surface-800"
      data-testid="attachment-list"
    >
      <li
        v-for="a in attachments"
        :key="a.id"
        class="flex flex-wrap items-center gap-3 py-2 text-sm"
      >
        <span
          :class="a.content_type === 'pdf' ? 'pi pi-file-pdf' : 'pi pi-image'"
          aria-hidden="true"
        />
        <button type="button" class="underline" @click="download(a)">
          {{ a.description || kindLabel(a) }}
        </button>
        <span class="text-surface-500"
          >{{ kindLabel(a) }} · {{ sizeLabel(a.size) }} · {{ d(a.created_at, 'short') }}</span
        >
        <span
          v-if="a.pending_addendum"
          class="rounded bg-amber-100 px-2 text-amber-900 dark:bg-amber-900/40 dark:text-amber-100"
          >{{ t('encounters.attachments.pending') }}</span
        >
        <span class="ml-auto text-surface-500">
          <span
            :class="a.visible_to_guardian ? 'pi pi-eye' : 'pi pi-eye-slash'"
            aria-hidden="true"
          />
          {{
            a.visible_to_guardian
              ? t('encounters.attachments.visible')
              : t('encounters.attachments.hidden')
          }}
        </span>
        <template v-if="canChangeVisibility">
          <div v-if="hiding === a.id" class="flex w-full flex-wrap items-end gap-2">
            <label class="flex-1">
              <span class="label">{{ t('encounters.attachments.hideReason') }}</span>
              <input v-model="hideReason" class="input" maxlength="500" />
            </label>
            <button
              type="button"
              class="btn-primary"
              :disabled="busy || !hideReason.trim()"
              @click="toggle(a)"
            >
              {{ t('encounters.attachments.hide') }}
            </button>
            <button type="button" class="btn-secondary" @click="hiding = null">
              {{ t('common.cancel') }}
            </button>
          </div>
          <button v-else type="button" class="underline" :disabled="busy" @click="toggle(a)">
            {{
              a.visible_to_guardian
                ? t('encounters.attachments.hide')
                : t('encounters.attachments.show')
            }}
          </button>
        </template>
        <button
          v-if="canDelete && (a.pending_addendum || a.addendum_seq === undefined)"
          type="button"
          class="text-red-700 underline dark:text-red-300"
          :disabled="busy"
          @click="remove(a)"
        >
          {{ t('common.delete') }}
        </button>
      </li>
    </ul>

    <div v-if="canUpload && attachments.length < max" class="flex flex-wrap items-end gap-3">
      <label class="min-w-48 flex-1">
        <span class="label">{{ t('encounters.attachments.description') }}</span>
        <input v-model="description" class="input" maxlength="200" />
      </label>
      <label class="flex items-center gap-2 text-sm">
        <input v-model="visible" type="checkbox" />
        {{ t('encounters.attachments.visibleToGuardian') }}
      </label>
      <input
        ref="input"
        type="file"
        class="sr-only"
        accept="image/jpeg,image/png,image/webp,application/pdf"
        data-testid="attachment-input"
        @change="onFile"
      />
      <input
        ref="camera"
        type="file"
        class="sr-only"
        accept="image/*"
        capture="environment"
        @change="onFile"
      />
      <button type="button" class="btn-secondary" :disabled="busy" @click="input?.click()">
        <span class="pi pi-paperclip mr-2" aria-hidden="true" />
        {{ busy ? t('common.loading') : t('encounters.attachments.add') }}
      </button>
      <button
        type="button"
        class="btn-secondary sm:hidden"
        :disabled="busy"
        @click="camera?.click()"
      >
        <span class="pi pi-camera mr-2" aria-hidden="true" />
        {{ t('encounters.attachments.photo') }}
      </button>
    </div>
  </div>
</template>
