<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Pet } from '@/shared/api/client'
import {
  petFormSchema,
  civilToday,
  SPECIES,
  SEXES,
  NEUTERED,
  type PetForm,
} from '@/shared/validation/schemas'
import { useCommand } from '@/shared/composables/useCommand'
import FieldError from '@/shared/ui/FieldError.vue'

// Formulario crear/editar (FE-006, PRD-005/006): validación zod espejo de los
// VOs (FE-011); en edición envía If-Match con la versión conocida y 409
// CONFLICT_VERSION muestra el diálogo de recargar.
const { t, te } = useI18n()
const router = useRouter()
const route = useRoute()
const { key: idempotencyKey } = useCommand()

const petId = computed(() => (route.name === 'pet-edit' ? String(route.params.petId) : null))
const isEdit = computed(() => petId.value !== null)

const form = reactive<PetForm>({
  name: '',
  species: 'dog',
  species_other: '',
  breed: '',
  sex: 'unknown',
  neutered: 'unknown',
  birth_date: undefined,
  markings: '',
})
const approximate = ref(false)
const birthDate = ref('')
const version = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const fieldErrors = ref<Record<string, string>>({})
const generalError = ref<string | null>(null)
const conflict = ref(false)

onMounted(async () => {
  if (!isEdit.value) return
  loading.value = true
  try {
    const { data, error } = await api().GET('/pets/{petId}', {
      params: { path: { petId: petId.value as string } },
    })
    if (error) throw error
    const pet = data as Pet
    version.value = pet.version
    fill(pet)
  } catch {
    generalError.value = t('common.errorGeneric')
  } finally {
    loading.value = false
  }
})

function fill(pet: Pet) {
  form.name = pet.name
  form.species = pet.species
  form.species_other = pet.species_other ?? ''
  form.breed = pet.breed ?? ''
  form.sex = pet.sex
  form.neutered = pet.neutered
  form.markings = pet.markings ?? ''
  if (pet.birth_date) {
    birthDate.value = pet.birth_date.date
    approximate.value = pet.birth_date.approximate
  }
}

function speciesLabel(s: string) {
  const key = `pets.species.${s}`
  return te(key) ? t(key) : s
}

function validate(): string | null {
  const candidate: PetForm = {
    ...form,
    birth_date: birthDate.value
      ? { date: birthDate.value, approximate: approximate.value }
      : undefined,
  }
  const parsed = petFormSchema(civilToday()).safeParse(candidate)
  if (parsed.success) {
    fieldErrors.value = {}
    return null
  }
  const errs: Record<string, string> = {}
  for (const issue of parsed.error.issues) {
    const field = issue.path.join('.') || 'form'
    if (!errs[field]) errs[field] = issue.message
  }
  fieldErrors.value = errs
  return 'invalid'
}

async function submit() {
  generalError.value = null
  conflict.value = false
  if (validate() !== null) return
  saving.value = true
  try {
    const candidate = {
      ...form,
      birth_date: birthDate.value
        ? { date: birthDate.value, approximate: approximate.value }
        : undefined,
    }
    if (isEdit.value) {
      const { data, error } = await api().PATCH('/pets/{petId}', {
        params: {
          path: { petId: petId.value as string },
          header: { 'If-Match': version.value ?? 0 },
        },
        body: {
          name: candidate.name,
          species: candidate.species,
          species_other: candidate.species_other || '',
          breed: candidate.breed ?? '',
          sex: candidate.sex,
          neutered: candidate.neutered,
          birth_date: candidate.birth_date,
          markings: candidate.markings ?? '',
        },
      })
      if (error) throw error
      await router.push({ name: 'pet-detail', params: { petId: (data as Pet).id } })
    } else {
      const { data, error } = await api().POST('/pets', {
        params: { header: { 'Idempotency-Key': idempotencyKey.value } },
        body: {
          name: candidate.name,
          species: candidate.species,
          species_other: candidate.species_other || undefined,
          breed: candidate.breed || undefined,
          sex: candidate.sex,
          neutered: candidate.neutered,
          birth_date: candidate.birth_date,
          markings: candidate.markings || undefined,
        },
      })
      if (error) throw error
      await router.push({ name: 'pet-detail', params: { petId: (data as Pet).id } })
    }
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'CONFLICT_VERSION') {
        conflict.value = true
      } else {
        generalError.value = t(`errors.${e.code}`, e.code)
        Object.assign(fieldErrors.value, e.fieldErrors())
      }
    } else {
      generalError.value = t('common.errorGeneric')
    }
  } finally {
    saving.value = false
  }
}

async function reload() {
  if (!petId.value) return
  conflict.value = false
  loading.value = true
  try {
    const { data } = await api().GET('/pets/{petId}', { params: { path: { petId: petId.value } } })
    if (data) {
      version.value = data.version
      fill(data)
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <section class="mx-auto max-w-xl space-y-6">
    <h1 class="text-2xl font-bold">{{ isEdit ? t('pets.editTitle') : t('pets.newTitle') }}</h1>

    <p
      v-if="conflict"
      class="rounded-lg bg-amber-50 p-4 text-amber-800 dark:bg-amber-950 dark:text-amber-200"
      role="alert"
    >
      {{ t('pets.conflict') }}
      <button type="button" class="ml-2 underline" data-testid="reload-pet" @click="reload">
        {{ t('pets.reload') }}
      </button>
    </p>
    <p
      v-if="generalError"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ generalError }}
    </p>

    <form class="space-y-5" novalidate @submit.prevent="submit">
      <div>
        <label class="block text-sm font-medium" for="pet-name">{{ t('pets.form.name') }}</label>
        <input
          id="pet-name"
          v-model="form.name"
          type="text"
          maxlength="60"
          required
          class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          :aria-describedby="fieldErrors['name'] ? 'pet-name-error' : undefined"
        />
        <FieldError
          v-if="fieldErrors['name']"
          id="pet-name-error"
          :message="t(`pets.form.nameError`)"
        />
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-sm font-medium" for="pet-species">{{
            t('pets.form.species')
          }}</label>
          <select
            id="pet-species"
            v-model="form.species"
            class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          >
            <option v-for="s in SPECIES" :key="s" :value="s">{{ speciesLabel(s) }}</option>
          </select>
        </div>
        <div v-if="form.species === 'other'">
          <label class="block text-sm font-medium" for="pet-species-other">{{
            t('pets.form.speciesOther')
          }}</label>
          <input
            id="pet-species-other"
            v-model="form.species_other"
            type="text"
            maxlength="40"
            class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          />
          <FieldError
            v-if="fieldErrors['species_other']"
            :message="t('pets.form.speciesOtherError')"
          />
        </div>
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-sm font-medium" for="pet-sex">{{ t('pets.form.sex') }}</label>
          <select
            id="pet-sex"
            v-model="form.sex"
            class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          >
            <option v-for="s in SEXES" :key="s" :value="s">{{ t(`pets.sex.${s}`) }}</option>
          </select>
        </div>
        <div>
          <label class="block text-sm font-medium" for="pet-neutered">{{
            t('pets.form.neutered')
          }}</label>
          <select
            id="pet-neutered"
            v-model="form.neutered"
            class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          >
            <option v-for="s in NEUTERED" :key="s" :value="s">{{ t(`pets.neutered.${s}`) }}</option>
          </select>
        </div>
      </div>

      <div>
        <label class="block text-sm font-medium" for="pet-breed">{{ t('pets.form.breed') }}</label>
        <input
          id="pet-breed"
          v-model="form.breed"
          type="text"
          maxlength="80"
          class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
        />
      </div>

      <fieldset class="space-y-2">
        <legend class="block text-sm font-medium">{{ t('pets.form.birthDate') }}</legend>
        <div class="flex items-center gap-3">
          <input
            v-model="birthDate"
            type="date"
            :max="civilToday()"
            class="rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
          />
          <label class="flex items-center gap-2 text-sm">
            <input v-model="approximate" type="checkbox" />
            {{ t('pets.form.approximate') }}
          </label>
        </div>
        <FieldError
          v-if="fieldErrors['birth_date.date']"
          :message="t('pets.form.birthDateError')"
        />
      </fieldset>

      <div>
        <label class="block text-sm font-medium" for="pet-markings">{{
          t('pets.form.markings')
        }}</label>
        <textarea
          id="pet-markings"
          v-model="form.markings"
          rows="3"
          maxlength="200"
          class="mt-1 w-full rounded-lg border border-surface-300 px-3 py-2 dark:border-surface-600 dark:bg-surface-800"
        ></textarea>
      </div>

      <div class="flex gap-3">
        <button
          type="submit"
          :disabled="saving"
          class="rounded-lg bg-green-600 px-4 py-2 font-medium text-white shadow-sm transition hover:bg-green-700 disabled:opacity-50"
          data-testid="submit-pet"
        >
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
        <button
          type="button"
          class="rounded-lg px-4 py-2 font-medium text-surface-600 dark:text-surface-300"
          @click="router.back()"
        >
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>
  </section>
</template>
