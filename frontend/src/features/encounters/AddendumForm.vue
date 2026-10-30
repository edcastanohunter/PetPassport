<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { isApiError, type AddendumReason, type Encounter } from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import AttachmentsPanel from './AttachmentsPanel.vue'
import { addAddendum } from './encounterApi'

// Adenda (PP-F2-PRD-004, FE-004): texto, motivo y hasta 5 adjuntos. Los
// adjuntos se suben antes a la consulta firmada y quedan preparados
// (`pending_addendum`) hasta que la adenda los incorpora.
const props = defineProps<{ encounter: Encounter }>()
const emit = defineEmits<{ added: [Encounter]; changed: [] }>()
const { t } = useI18n()
const { key, renew } = useCommand()

const REASONS: AddendumReason[] = ['correction', 'additional_findings', 'lab_results', 'other']
const MAX_ATTACHMENTS = 5
const open = ref(false)
const text = ref('')
const reason = ref<AddendumReason>('additional_findings')
const busy = ref(false)
const error = ref<string | null>(null)

const pending = computed(() => props.encounter.attachments.filter((a) => a.pending_addendum))

async function submit() {
  if (!text.value.trim()) return
  busy.value = true
  error.value = null
  try {
    const enc = await addAddendum(props.encounter.id, key.value, {
      text: text.value.trim(),
      reason: reason.value,
      ...(pending.value.length ? { attachment_ids: pending.value.map((a) => a.id) } : {}),
    })
    text.value = ''
    open.value = false
    renew()
    emit('added', enc)
  } catch (e) {
    error.value = isApiError(e) ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-3" aria-labelledby="addendum-title">
    <button
      v-if="!open"
      type="button"
      class="btn-secondary"
      data-testid="addendum-open"
      @click="open = true"
    >
      <span class="pi pi-plus mr-2" aria-hidden="true" />{{ t('encounters.addenda.add') }}
    </button>
    <form
      v-else
      class="space-y-3 rounded-xl border border-surface-200 p-4 dark:border-surface-800"
      @submit.prevent="submit"
    >
      <h2 id="addendum-title" class="font-semibold">{{ t('encounters.addenda.new') }}</h2>
      <p
        v-if="error"
        class="rounded bg-red-50 p-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
        role="alert"
      >
        {{ error }}
      </p>
      <label class="block">
        <span class="label">{{ t('encounters.addenda.reason') }}</span>
        <select v-model="reason" class="input" @change="renew()">
          <option v-for="r in REASONS" :key="r" :value="r">
            {{ t(`encounters.addendumReason.${r}`) }}
          </option>
        </select>
      </label>
      <label class="block">
        <span class="label">{{ t('encounters.addenda.text') }}</span>
        <textarea
          v-model="text"
          class="input min-h-28"
          maxlength="5000"
          required
          data-testid="addendum-text"
          @input="renew()"
        />
      </label>
      <div>
        <p class="label">{{ t('encounters.addenda.attachments', { max: MAX_ATTACHMENTS }) }}</p>
        <AttachmentsPanel
          :encounter-id="encounter.id"
          :attachments="pending"
          :max="MAX_ATTACHMENTS"
          can-upload
          can-delete
          @uploaded="emit('changed')"
          @changed="emit('changed')"
        />
      </div>
      <div class="flex gap-2">
        <button
          type="submit"
          class="btn-primary"
          :disabled="busy || !text.trim()"
          data-testid="addendum-submit"
        >
          {{ t('encounters.addenda.sign') }}
        </button>
        <button type="button" class="btn-secondary" @click="open = false">
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>
  </section>
</template>
