<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type CardVerification } from '@/shared/api/client'

// Verificación pública del carnet (QR del PDF, PP-F2-API-013 / FE-013): solo
// indica si la huella `h` coincide con el contenido vigente, sin datos clínicos.
const { t, d } = useI18n()
const route = useRoute()
const petId = computed(() => String(route.params.petId))
const hash = computed(() => (typeof route.query.h === 'string' ? route.query.h : ''))
const result = shallowRef<CardVerification | null>(null)
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')

onMounted(async () => {
  try {
    const { data, error } = await api().GET('/verify/cards/{petId}', {
      params: { path: { petId: petId.value }, query: { h: hash.value } },
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
  <section class="mx-auto max-w-lg space-y-4 py-8" data-testid="verify-card">
    <h1 class="text-2xl font-bold">{{ t('card.verify.title') }}</h1>
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-else-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('card.verify.notFound') }}
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
      <p class="text-lg font-semibold" data-testid="verify-card-result">
        {{ result.valid ? t('card.verify.valid') : t('card.verify.invalid') }}
      </p>
      <p>{{ t('card.verify.entries', { n: result.entries }) }}</p>
      <p v-if="result.verified_at" class="text-sm">{{ d(result.verified_at, 'long') }}</p>
    </div>
  </section>
</template>
