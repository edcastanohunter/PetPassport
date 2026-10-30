<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Card, CardEntryKind, Vaccine } from '@/shared/api/client'
import { useCommand } from '@/shared/composables/useCommand'
import CardTable from './CardTable.vue'
import { cardErrorKey } from './cardErrors'
import {
  getCard,
  listVaccines,
  proposeNextDue,
  recordEntry,
  reviewEntry,
  voidEntry,
} from './cardApi'

// Carnet en la ficha clínica (PP-F2-FE-006): registrar vacunas/preventivos con
// próxima dosis propuesta (editable), verificar, disputar y anular. Solo
// escribe quien tiene vínculo activo; el servidor decide con authz.
const props = defineProps<{ petId: string; species: string; canWrite: boolean }>()
const { t, te } = useI18n()
const { key, renew } = useCommand()

const card = shallowRef<Card | null>(null)
const vaccines = ref<Vaccine[]>([])
const state = ref<'loading' | 'ready' | 'error'>('loading')
const error = ref<string | null>(null)
const busy = ref(false)
const form = ref({
  kind: 'vaccine' as CardEntryKind,
  catalog_id: '',
  name: '',
  administered_at: new Date().toISOString().slice(0, 10),
  lot: '',
  dose_label: '',
  next_due_date: '',
})
const proposal = ref<string | null>(null)
const voiding = ref<string | null>(null)
const voidReason = ref('')
const valid = computed(() => form.value.name.trim() !== '' && form.value.administered_at !== '')

async function load() {
  try {
    card.value = await getCard(props.petId)
    state.value = 'ready'
  } catch {
    state.value = 'error'
  }
}
onMounted(async () => {
  await load()
  try {
    vaccines.value = await listVaccines(props.species)
  } catch {
    vaccines.value = []
  }
})

async function propose() {
  proposal.value = null
  if (form.value.kind !== 'vaccine' || !form.value.catalog_id || !form.value.administered_at) return
  try {
    const p = await proposeNextDue(props.petId, {
      kind: form.value.kind,
      product_catalog_id: form.value.catalog_id,
      administered_at: form.value.administered_at,
    })
    proposal.value = p.explanation
    if (p.date && !form.value.next_due_date) form.value.next_due_date = p.date
  } catch {
    proposal.value = null
  }
}
function pickVaccine() {
  const v = vaccines.value.find((x) => x.id === form.value.catalog_id)
  if (v) form.value.name = v.name
  void propose()
}

async function submit() {
  if (!valid.value || busy.value) return
  busy.value = true
  error.value = null
  const f = form.value
  try {
    await recordEntry(props.petId, key.value, {
      kind: f.kind,
      product: { name: f.name.trim(), ...(f.catalog_id ? { catalog_id: f.catalog_id } : {}) },
      administered_at: f.administered_at,
      ...(f.lot ? { lot: f.lot } : {}),
      ...(f.dose_label ? { dose_label: f.dose_label } : {}),
      ...(f.next_due_date ? { next_due_date: f.next_due_date } : {}),
    })
    renew()
    form.value = { ...f, catalog_id: '', name: '', lot: '', dose_label: '', next_due_date: '' }
    proposal.value = null
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  } finally {
    busy.value = false
  }
}

async function review(id: string, action: 'verify' | 'dispute') {
  error.value = null
  try {
    await reviewEntry(props.petId, id, action)
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  }
}
async function confirmVoid(id: string) {
  if (voidReason.value.trim().length < 3) return
  error.value = null
  try {
    await voidEntry(props.petId, id, voidReason.value.trim())
    voiding.value = null
    voidReason.value = ''
    await load()
  } catch (e) {
    error.value = cardErrorKey(e, te)
  }
}
</script>

<template>
  <section class="space-y-4" data-testid="clinic-card">
    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-if="state === 'error'" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>
    <p v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700" role="alert">
      {{ t(error) }}
    </p>

    <form
      v-if="canWrite"
      class="card space-y-3"
      data-testid="card-form"
      @submit.prevent="submit"
      @input="renew"
    >
      <h2 class="text-lg font-semibold">{{ t('card.record.title') }}</h2>
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block text-sm">
          {{ t('card.record.kind') }}
          <select v-model="form.kind" class="input mt-1 w-full" @change="propose">
            <option v-for="k in ['vaccine', 'deworming', 'ectoparasite']" :key="k" :value="k">
              {{ t(`card.kind.${k}`) }}
            </option>
          </select>
        </label>
        <label v-if="form.kind === 'vaccine' && vaccines.length" class="block text-sm">
          {{ t('card.record.catalog') }}
          <select v-model="form.catalog_id" class="input mt-1 w-full" @change="pickVaccine">
            <option value="">{{ t('card.record.other') }}</option>
            <option v-for="v in vaccines" :key="v.id" :value="v.id">{{ v.name }}</option>
          </select>
        </label>
        <label class="block text-sm">
          {{ t('card.record.product') }}
          <input v-model="form.name" class="input mt-1 w-full" maxlength="120" required />
        </label>
        <label class="block text-sm">
          {{ t('card.record.date') }}
          <input
            v-model="form.administered_at"
            type="date"
            class="input mt-1 w-full"
            required
            @change="propose"
          />
        </label>
        <label class="block text-sm">
          {{ t('card.lot') }}
          <input v-model="form.lot" class="input mt-1 w-full" maxlength="60" />
        </label>
        <label class="block text-sm">
          {{ t('card.record.dose') }}
          <input v-model="form.dose_label" class="input mt-1 w-full" maxlength="60" />
        </label>
        <label class="block text-sm">
          {{ t('card.record.nextDue') }}
          <input v-model="form.next_due_date" type="date" class="input mt-1 w-full" />
        </label>
      </div>
      <p v-if="proposal" class="text-xs text-surface-500" data-testid="card-proposal">
        {{ proposal }}
      </p>
      <button type="submit" class="btn-primary" :disabled="!valid || busy">
        {{ t('card.record.submit') }}
      </button>
    </form>

    <CardTable v-if="card" :card="card">
      <template #actions="{ entry }">
        <div v-if="canWrite && entry.status === 'active'" class="flex flex-wrap gap-2 pt-1">
          <button
            v-if="entry.verification !== 'verified'"
            type="button"
            class="btn-secondary text-sm"
            @click="review(entry.id, 'verify')"
          >
            {{ t('card.action.verify') }}
          </button>
          <button
            v-if="entry.verification !== 'disputed'"
            type="button"
            class="btn-secondary text-sm"
            @click="review(entry.id, 'dispute')"
          >
            {{ t('card.action.dispute') }}
          </button>
          <button type="button" class="btn-secondary text-sm" @click="voiding = entry.id">
            {{ t('card.action.void') }}
          </button>
        </div>
        <form
          v-if="voiding === entry.id"
          class="flex flex-wrap items-end gap-2 pt-1"
          @submit.prevent="confirmVoid(entry.id)"
        >
          <label class="text-sm">
            {{ t('card.void.reason') }}
            <input v-model="voidReason" class="input mt-1" minlength="3" maxlength="500" required />
          </label>
          <button type="submit" class="btn-primary text-sm">{{ t('card.void.confirm') }}</button>
          <button type="button" class="btn-secondary text-sm" @click="voiding = null">
            {{ t('common.cancel') }}
          </button>
        </form>
      </template>
    </CardTable>
  </section>
</template>
