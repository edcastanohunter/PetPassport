<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type EncounterVerification } from '@/shared/api/client'

// Verificación pública de una consulta firmada (PP-F2-API-013; enlazada desde
// el QR del PDF y el sello, FE-004): sin autenticación ni datos clínicos;
// solo validez, fecha de firma, versión y clínica.
const { t, d } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.encounterId))
const hash = computed(() => (typeof route.query.hash === 'string' ? route.query.hash : undefined))
const result = shallowRef<EncounterVerification | null>(null)
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')

onMounted(async () => {
  try {
    const { data, error } = await api().GET('/verify/encounters/{encounterId}', {
      params: { path: { encounterId: id.value }, query: hash.value ? { hash: hash.value } : {} },
    })
    if (error) throw error
    result.value = data ?? null
    state.value = 'ready'
  } catch (e) {
    state.value = (e as { status?: number }).status === 404 ? 'notFound' : 'error'
  }
})
</script>

<template>
  <section class="mx-auto max-w-lg space-y-4 py-8" data-testid="verify">
    <h1 class="text-2xl font-bold">{{ t('records.verify.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-else-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('records.verify.notFound') }}
    </p>
    <p
      v-else-if="state === 'error'"
      class="rounded bg-red-50 p-3 text-sm text-red-700"
      role="alert"
    >
      {{ t('errors.GENERIC') }}
    </p>
    <div
      v-else-if="result"
      class="space-y-2 rounded-xl border p-6"
      :class="
        result.valid
          ? 'border-emerald-300 bg-emerald-50 dark:border-emerald-800 dark:bg-emerald-950'
          : 'border-red-300 bg-red-50 dark:border-red-800 dark:bg-red-950'
      "
      role="status"
    >
      <p class="text-lg font-semibold" data-testid="verify-result">
        <span
          :class="result.valid ? 'pi pi-verified' : 'pi pi-times-circle'"
          class="mr-2"
          aria-hidden="true"
        />
        {{ result.valid ? t('records.verify.valid') : t('records.verify.invalid') }}
      </p>
      <p v-if="result.organization_name">
        {{ t('records.verify.clinic') }}: {{ result.organization_name }}
      </p>
      <p>{{ t('records.verify.signedAt') }}: {{ d(result.signed_at, 'long') }}</p>
      <p>{{ t('records.verify.version') }}: {{ result.stream_version }}</p>
      <p v-if="result.content_hash" class="break-all font-mono text-xs">
        {{ result.content_hash }}
      </p>
    </div>
  </section>
</template>
