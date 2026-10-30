<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Card, CardEntryKind, CreatedShare, Share } from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import CardTable from './CardTable.vue'
import PdfButton from '@/features/encounters/PdfButton.vue'
import { cardErrorKey } from './cardErrors'
import { createShare, declareEntry, getCard, listShares, revokeShare, voidEntry } from './cardApi'

// Carnet del tutor (PP-F2-FE-010): ver, declarar vacunas propias (quedan
// «no verificadas»), anular lo declarado y gestionar enlaces compartidos
// (PRD-014): alcance, caducidad y PIN opcional. La URL solo se muestra al crear.
const { t, te, d } = useI18n()
const route = useRoute()
const petId = computed(() => String(route.params.petId))
const decl = useCommand()
const shr = useCommand()

const card = shallowRef<Card | null>(null)
const shares = ref<Share[]>([])
const created = ref<CreatedShare | null>(null)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const busy = ref(false)
const form = ref({
  kind: 'vaccine' as CardEntryKind,
  name: '',
  administered_at: new Date().toISOString().slice(0, 10),
  lot: '',
})
const share = ref({
  scope: 'vaccination_card' as 'vaccination_card' | 'full_record',
  expires_in_days: 7,
  pin: '',
})
const pinOk = computed(() => share.value.pin === '' || /^\d{4,6}$/.test(share.value.pin))

async function load() {
  try {
    const [c, s] = await Promise.all([getCard(petId.value), listShares(petId.value)])
    card.value = c
    shares.value = s
    state.value = 'ready'
  } catch {
    state.value = 'error'
  }
}
onMounted(load)

async function declare() {
  if (!form.value.name.trim() || busy.value) return
  busy.value = true
  error.value = null
  try {
    await declareEntry(petId.value, decl.key.value, {
      kind: form.value.kind,
      product: { name: form.value.name.trim() },
      administered_at: form.value.administered_at,
      ...(form.value.lot ? { lot: form.value.lot } : {}),
    })
    decl.renew()
    form.value.name = ''
    form.value.lot = ''
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  } finally {
    busy.value = false
  }
}
async function voidOwn(id: string) {
  error.value = null
  try {
    await voidEntry(petId.value, id, t('card.void.byGuardian'))
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  }
}

async function newShare() {
  if (!pinOk.value || busy.value) return
  busy.value = true
  error.value = null
  try {
    created.value = await createShare(petId.value, shr.key.value, {
      scope: share.value.scope,
      expires_in_days: share.value.expires_in_days,
      ...(share.value.pin ? { pin: share.value.pin } : {}),
    })
    shr.renew()
    share.value.pin = ''
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  } finally {
    busy.value = false
  }
}
async function revoke(id: string) {
  error.value = null
  try {
    await revokeShare(petId.value, id)
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  }
}
</script>

<template>
  <section class="mx-auto max-w-3xl space-y-6" data-testid="guardian-card">
    <RouterLink :to="{ name: 'pet-detail', params: { petId } }" class="text-sm underline">
      ← {{ t('card.back') }}
    </RouterLink>
    <h1 class="text-2xl font-bold">{{ t('card.title') }}</h1>

    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="state === 'error'" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <p v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>

    <PdfButton :pet-id="petId" />

    <CardTable v-if="card" :card="card">
      <template #actions="{ entry }">
        <button
          v-if="
            entry.source === 'declared' &&
            entry.status === 'active' &&
            entry.verification === 'unverified'
          "
          type="button"
          class="btn-secondary text-sm"
          @click="voidOwn(entry.id)"
        >
          {{ t('card.action.void') }}
        </button>
      </template>
    </CardTable>

    <form
      class="card space-y-3"
      data-testid="declare-form"
      @submit.prevent="declare"
      @input="decl.renew"
    >
      <h2 class="text-lg font-semibold">{{ t('card.declare.title') }}</h2>
      <p class="text-sm text-surface-500">{{ t('card.declare.hint') }}</p>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block text-sm">
          {{ t('card.record.kind') }}
          <select v-model="form.kind" class="input mt-1 w-full">
            <option v-for="k in ['vaccine', 'deworming', 'ectoparasite']" :key="k" :value="k">
              {{ t(`card.kind.${k}`) }}
            </option>
          </select>
        </label>
        <label class="block text-sm">
          {{ t('card.record.product') }}
          <input v-model="form.name" class="input mt-1 w-full" maxlength="120" required />
        </label>
        <label class="block text-sm">
          {{ t('card.record.date') }}
          <input v-model="form.administered_at" type="date" class="input mt-1 w-full" required />
        </label>
        <label class="block text-sm">
          {{ t('card.lot') }}
          <input v-model="form.lot" class="input mt-1 w-full" maxlength="60" />
        </label>
      </div>
      <button type="submit" class="btn-primary" :disabled="busy || !form.name.trim()">
        {{ t('card.declare.submit') }}
      </button>
    </form>

    <section class="card space-y-3" data-testid="shares">
      <h2 class="text-lg font-semibold">{{ t('card.share.title') }}</h2>
      <form class="grid gap-3 sm:grid-cols-3" @submit.prevent="newShare" @input="shr.renew">
        <label class="block text-sm">
          {{ t('card.share.scope') }}
          <select v-model="share.scope" class="input mt-1 w-full">
            <option value="vaccination_card">{{ t('card.share.scopes.vaccination_card') }}</option>
            <option value="full_record">{{ t('card.share.scopes.full_record') }}</option>
          </select>
        </label>
        <label class="block text-sm">
          {{ t('card.share.expires') }}
          <select v-model.number="share.expires_in_days" class="input mt-1 w-full">
            <option v-for="n in [1, 7, 30, 90]" :key="n" :value="n">
              {{ t('card.share.days', { n }) }}
            </option>
          </select>
        </label>
        <label class="block text-sm">
          {{ t('card.share.pin') }}
          <input
            v-model="share.pin"
            class="input mt-1 w-full"
            inputmode="numeric"
            autocomplete="off"
            maxlength="6"
            :aria-invalid="!pinOk"
          />
        </label>
        <button type="submit" class="btn-primary sm:col-span-3" :disabled="busy || !pinOk">
          {{ t('card.share.create') }}
        </button>
      </form>

      <div
        v-if="created"
        class="rounded border border-emerald-300 bg-emerald-50 p-3 text-sm dark:bg-emerald-950"
        data-testid="share-created"
      >
        <p class="font-semibold">{{ t('card.share.createdHint') }}</p>
        <p class="break-all font-mono text-xs">{{ created.url }}</p>
      </div>

      <ul class="space-y-2">
        <li
          v-for="s in shares"
          :key="s.id"
          class="flex flex-wrap items-center justify-between gap-2 text-sm"
        >
          <span>
            {{ t(`card.share.scopes.${s.scope}`) }}
            <template v-if="s.has_pin"> · {{ t('card.share.withPin') }}</template>
            · {{ t('card.share.until') }} {{ d(s.expires_at, 'short') }} ·
            {{ t('card.share.accesses', { n: s.access_count }) }}
          </span>
          <button type="button" class="btn-secondary text-sm" @click="revoke(s.id)">
            {{ t('card.share.revoke') }}
          </button>
        </li>
      </ul>
    </section>
  </section>
</template>
