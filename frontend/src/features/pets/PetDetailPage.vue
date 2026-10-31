<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Pet } from '@/shared/api/client'
import { microchipSchema } from '@/shared/validation/schemas'
import CareLinksPanel from './CareLinksPanel.vue'
import PetAlertsPanel from './PetAlertsPanel.vue'
import TransferPanel from './TransferPanel.vue'

// Ficha de mascota (FE-006/FE-008, PRD-006..010, PRD-015): datos, foto,
// microchip (una corrección), vínculos con clínicas, transferencia de tutela,
// fallecida + deshacer 24 h. ETag de cada lectura = If-Match de los comandos.
const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

const petId = String(route.params.petId)
const pet = ref<Pet | null>(null)
const error = ref<string | null>(null)
const notFound = ref(false)
const busy = ref(false)

// Microchip
const editingMicrochip = ref(false)
const microchipDraft = ref('')
const microchipError = ref<string | null>(null)

// Fallecida
const confirmDeceased = ref(false)
const deceasedDate = ref('')

const fileInput = ref<HTMLInputElement | null>(null)

async function load() {
  error.value = null
  notFound.value = false
  try {
    const { data, error: apiError } = await api().GET('/pets/{petId}', {
      params: { path: { petId } },
    })
    if (apiError) throw apiError
    pet.value = data ?? null
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else error.value = t('common.errorGeneric')
  }
}

onMounted(load)

const speciesLabel = computed(() => {
  if (!pet.value) return ''
  const key = `pets.species.${pet.value.species}`
  return te(key) ? t(key) : pet.value.species
})

const sexLabel = computed(() => (pet.value ? t(`pets.sex.${pet.value.sex}`) : ''))
const neuteredLabel = computed(() => (pet.value ? t(`pets.neutered.${pet.value.neutered}`) : ''))

const ageLabel = computed(() => {
  if (!pet.value?.age) return null
  return t('pets.ageShort', { years: pet.value.age.years, months: pet.value.age.months })
})

const undoAvailable = computed(() => {
  if (!pet.value?.status || pet.value.status !== 'deceased' || !pet.value.deceased_at) return false
  return true // la ventana la decide el servidor (409 UNDO_WINDOW_EXPIRED)
})

async function withBusy(fn: () => Promise<unknown>) {
  busy.value = true
  error.value = null
  try {
    await fn()
  } catch (e) {
    if (e instanceof ApiError) {
      error.value = t(`errors.${e.code}`, e.code)
      if (e.code === 'CONFLICT_VERSION') await load()
    } else {
      error.value = t('common.errorGeneric')
    }
  } finally {
    busy.value = false
  }
}

function ifMatch() {
  return { 'If-Match': pet.value?.version ?? 0 }
}

function startMicrochip() {
  microchipDraft.value = pet.value?.microchip ?? ''
  microchipError.value = null
  editingMicrochip.value = true
}

function saveMicrochip() {
  const parsed = microchipSchema().safeParse(microchipDraft.value)
  if (!parsed.success) {
    microchipError.value = t('pets.form.microchipError')
    return
  }
  void withBusy(async () => {
    const { error: apiError } = await api().PUT('/pets/{petId}/microchip', {
      params: { path: { petId }, header: ifMatch() },
      body: { microchip: parsed.data },
    })
    if (apiError) throw apiError
    editingMicrochip.value = false
    await load()
  })
}

function pickPhoto() {
  fileInput.value?.click()
}

async function onPhotoChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  await withBusy(async () => {
    const form = new FormData()
    form.append('file', file)
    const { error: apiError } = await api().PUT('/pets/{petId}/photo', {
      params: { path: { petId } },
      // openapi-fetch envía FormData tal cual (multipart); el tipo generado
      // declara string por binario: elenco controlado.
      body: form as unknown as { file: string },
    })
    if (apiError) throw apiError
    await load()
  })
}

function removePhoto() {
  void withBusy(async () => {
    const { error: apiError } = await api().DELETE('/pets/{petId}/photo', {
      params: { path: { petId } },
    })
    if (apiError) throw apiError
    await load()
  })
}

function markDeceased() {
  void withBusy(async () => {
    const { error: apiError } = await api().POST('/pets/{petId}/deceased', {
      params: { path: { petId }, header: ifMatch() },
      body: { deceased_at: deceasedDate.value || undefined },
    })
    if (apiError) throw apiError
    confirmDeceased.value = false
    await load()
  })
}

function undoDeceased() {
  void withBusy(async () => {
    const { error: apiError } = await api().POST('/pets/{petId}/deceased/undo', {
      params: { path: { petId } },
    })
    if (apiError) throw apiError
    await load()
  })
}
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-6">
    <p
      v-if="notFound"
      class="rounded-lg bg-surface-100 p-6 text-center text-surface-500 dark:bg-surface-800"
    >
      {{ t('pets.notFound') }}
    </p>

    <template v-else-if="pet">
      <header class="flex items-center justify-between">
        <h1 class="text-2xl font-bold">{{ pet.name }}</h1>
        <button
          type="button"
          class="rounded-lg px-3 py-1.5 text-sm font-medium text-green-700 underline dark:text-green-400"
          data-testid="edit-pet"
          @click="router.push({ name: 'pet-edit', params: { petId } })"
        >
          {{ t('pets.edit') }}
        </button>
      </header>

      <p
        v-if="error"
        class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
        role="alert"
      >
        {{ error }}
      </p>

      <div
        class="flex flex-wrap items-center gap-4 rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
      >
        <img
          v-if="pet.photo"
          :src="pet.photo.url"
          :alt="t('pets.photoAlt', { name: pet.name })"
          class="h-20 w-20 rounded-full object-cover"
        />
        <span
          v-else
          class="flex h-20 w-20 items-center justify-center rounded-full bg-surface-100 text-2xl font-bold text-surface-400 dark:bg-surface-700"
          aria-hidden="true"
        >
          {{ pet.name.charAt(0).toUpperCase() }}
        </span>
        <div class="flex gap-2">
          <button
            type="button"
            class="rounded-lg border border-surface-300 px-3 py-1.5 text-sm dark:border-surface-600"
            :disabled="busy"
            @click="pickPhoto"
          >
            {{ pet.photo ? t('pets.photoChange') : t('pets.photoAdd') }}
          </button>
          <button
            v-if="pet.photo"
            type="button"
            class="rounded-lg px-3 py-1.5 text-sm text-red-700 dark:text-red-400"
            :disabled="busy"
            @click="removePhoto"
          >
            {{ t('pets.photoRemove') }}
          </button>
          <input
            ref="fileInput"
            type="file"
            accept="image/jpeg,image/png,image/webp,image/heic"
            class="hidden"
            @change="onPhotoChange"
          />
        </div>
      </div>

      <dl
        class="grid grid-cols-2 gap-4 rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
      >
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.form.species') }}</dt>
          <dd>
            {{ speciesLabel
            }}<template v-if="pet.species_other"> ({{ pet.species_other }})</template>
          </dd>
        </div>
        <div v-if="pet.breed">
          <dt class="text-sm text-surface-500">{{ t('pets.form.breed') }}</dt>
          <dd>{{ pet.breed }}</dd>
        </div>
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.form.sex') }}</dt>
          <dd>{{ sexLabel }}</dd>
        </div>
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.form.neutered') }}</dt>
          <dd>{{ neuteredLabel }}</dd>
        </div>
        <div v-if="pet.birth_date">
          <dt class="text-sm text-surface-500">{{ t('pets.form.birthDate') }}</dt>
          <dd>
            {{ pet.birth_date.date }}
            <span v-if="pet.birth_date.approximate">({{ t('pets.approximate') }})</span>
            <span v-if="ageLabel"> · {{ ageLabel }}</span>
          </dd>
        </div>
        <div v-if="pet.markings">
          <dt class="text-sm text-surface-500">{{ t('pets.form.markings') }}</dt>
          <dd>{{ pet.markings }}</dd>
        </div>
        <div>
          <dt class="text-sm text-surface-500">{{ t('pets.version') }}</dt>
          <dd>{{ pet.version }}</dd>
        </div>
      </dl>

      <div
        class="rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
      >
        <div class="flex items-center justify-between">
          <h2 class="font-semibold">{{ t('pets.microchip') }}</h2>
          <button
            v-if="!editingMicrochip"
            type="button"
            class="text-sm text-green-700 underline dark:text-green-400"
            data-testid="edit-microchip"
            @click="startMicrochip"
          >
            {{ pet.microchip ? t('pets.correct') : t('pets.add') }}
          </button>
        </div>
        <p v-if="!editingMicrochip" class="mt-1 font-mono">
          {{ pet.microchip || t('pets.noMicrochip') }}
        </p>
        <p
          v-if="!editingMicrochip && pet.microchip && (pet.microchip_corrections ?? 0) >= 1"
          class="mt-1 text-xs text-surface-500"
        >
          {{ t('pets.microchipCorrected') }}
        </p>
        <form v-else class="mt-2 flex flex-wrap items-center gap-2" @submit.prevent="saveMicrochip">
          <input
            v-model="microchipDraft"
            type="text"
            maxlength="20"
            class="rounded-lg border border-surface-300 px-3 py-1.5 font-mono dark:border-surface-600 dark:bg-surface-800"
            aria-label="Microchip"
          />
          <button
            type="submit"
            class="rounded-lg bg-green-700 px-3 py-1.5 text-sm font-medium text-white"
            :disabled="busy"
          >
            {{ t('common.save') }}
          </button>
          <button type="button" class="text-sm text-surface-500" @click="editingMicrochip = false">
            {{ t('common.cancel') }}
          </button>
          <p v-if="microchipError" class="w-full text-sm text-red-700 dark:text-red-400">
            {{ microchipError }}
          </p>
          <p v-if="pet.microchip" class="w-full text-xs text-surface-500">
            {{ t('pets.microchipWarning') }}
          </p>
        </form>
      </div>

      <div
        v-if="pet.status === 'active'"
        class="rounded-xl border border-surface-200 bg-white p-4 shadow-sm dark:border-surface-700 dark:bg-surface-800"
      >
        <h2 class="font-semibold">{{ t('pets.deceased.title') }}</h2>
        <p v-if="!confirmDeceased" class="mt-1 text-sm text-surface-500">
          {{ t('pets.deceased.info') }}
          <button
            type="button"
            class="ml-1 underline"
            data-testid="mark-deceased"
            @click="confirmDeceased = true"
          >
            {{ t('pets.deceased.action') }}
          </button>
        </p>
        <div v-else class="mt-2 space-y-2" role="alert">
          <p class="text-sm">{{ t('pets.deceased.confirm') }}</p>
          <div class="flex flex-wrap items-center gap-2">
            <input
              v-model="deceasedDate"
              type="date"
              :max="new Date().toISOString().slice(0, 10)"
              class="rounded-lg border border-surface-300 px-3 py-1.5 dark:border-surface-600 dark:bg-surface-800"
            />
            <button
              type="button"
              class="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white"
              :disabled="busy"
              @click="markDeceased"
            >
              {{ t('pets.deceased.confirmAction') }}
            </button>
            <button type="button" class="text-sm text-surface-500" @click="confirmDeceased = false">
              {{ t('common.cancel') }}
            </button>
          </div>
        </div>
      </div>

      <div
        v-else
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950"
      >
        <h2 class="font-semibold text-amber-900 dark:text-amber-200">{{ t('pets.inMemoriam') }}</h2>
        <p v-if="undoAvailable" class="mt-1 text-sm text-amber-800 dark:text-amber-300">
          {{ t('pets.deceased.undoInfo') }}
          <button
            type="button"
            class="underline"
            data-testid="undo-deceased"
            :disabled="busy"
            @click="undoDeceased"
          >
            {{ t('pets.deceased.undo') }}
          </button>
        </p>
        <p v-else class="mt-1 text-sm text-amber-800 dark:text-amber-300">
          {{ t('pets.deceased.windowClosed') }}
        </p>
      </div>

      <RouterLink
        :to="{ name: 'pet-history', params: { petId } }"
        class="card flex items-center gap-3"
        data-testid="history-link"
      >
        <span class="pi pi-book" aria-hidden="true" />
        <span>
          <span class="block font-semibold">{{ t('records.history.link') }}</span>
          <span class="block text-sm text-surface-500">{{ t('records.history.linkHint') }}</span>
        </span>
      </RouterLink>

      <RouterLink
        :to="{ name: 'pet-card', params: { petId } }"
        class="card flex items-center gap-3"
        data-testid="card-link"
      >
        <span class="pi pi-shield" aria-hidden="true" />
        <span>
          <span class="block font-semibold">{{ t('card.link') }}</span>
          <span class="block text-sm text-surface-500">{{ t('card.linkHint') }}</span>
        </span>
      </RouterLink>

      <PetAlertsPanel class="card" :pet="pet" :editable="pet.status === 'active'" @changed="load" />
      <CareLinksPanel :pet="pet" @changed="load" />
      <TransferPanel :pet="pet" @changed="load" />
    </template>
  </section>
</template>
