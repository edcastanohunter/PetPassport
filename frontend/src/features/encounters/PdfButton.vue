<script setup lang="ts">
import { onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { cardPdf, documentJob, encounterPdf } from './encounterApi'
import { openUrl } from './openUrl'

// PDF firmado (PP-F2-FE-004, INF-004): si está listo se abre la URL
// prefirmada; si se está generando muestra "Generando…" y consulta
// /documents/{job} cada 2 s hasta que esté listo, y entonces ofrece el enlace
// (el navegador puede bloquear la apertura automática fuera del clic).
const props = defineProps<{ encounterId?: string; petId?: string }>()
const { t } = useI18n()

const state = ref<'idle' | 'loading' | 'generating' | 'ready' | 'failed'>('idle')
const url = ref<string | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
onScopeDispose(() => {
  disposed = true
  clearTimeout(timer)
})

const POLL_MS = 2000
const MAX_POLLS = 45

async function poll(jobId: string, n = 0) {
  if (disposed) return
  if (n >= MAX_POLLS) {
    state.value = 'failed'
    return
  }
  try {
    const job = await documentJob(jobId)
    if (job.status === 'ready' && job.url) {
      url.value = job.url
      state.value = 'ready'
      return
    }
    if (job.status === 'failed') {
      state.value = 'failed'
      return
    }
  } catch {
    state.value = 'failed'
    return
  }
  timer = setTimeout(() => void poll(jobId, n + 1), POLL_MS)
}

async function request() {
  state.value = 'loading'
  url.value = null
  try {
    const res = props.petId
      ? await cardPdf(props.petId)
      : await encounterPdf(props.encounterId ?? '')
    if (res.ready) {
      url.value = res.link.url
      state.value = 'ready'
      openUrl(res.link.url)
      return
    }
    if (res.job.status === 'failed') {
      state.value = 'failed'
      return
    }
    state.value = 'generating'
    timer = setTimeout(() => void poll(res.job.id), POLL_MS)
  } catch {
    state.value = 'failed'
  }
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2" aria-live="polite">
    <button
      type="button"
      class="btn-secondary"
      :disabled="state === 'loading' || state === 'generating'"
      data-testid="pdf-button"
      @click="request"
    >
      <span class="pi pi-file-pdf mr-2" aria-hidden="true" />
      {{ state === 'generating' ? t('encounters.pdf.generating') : t('encounters.pdf.download') }}
    </button>
    <a
      v-if="state === 'ready' && url"
      :href="url"
      target="_blank"
      rel="noopener noreferrer"
      class="text-sm underline"
      data-testid="pdf-link"
      >{{ t('encounters.pdf.open') }}</a
    >
    <span v-if="state === 'failed'" class="text-sm text-red-700 dark:text-red-300" role="alert">{{
      t('encounters.pdf.failed')
    }}</span>
  </div>
</template>
