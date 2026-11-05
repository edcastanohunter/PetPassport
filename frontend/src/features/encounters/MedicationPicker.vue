<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MedicationDraft } from './encounterDraft'
import { createSuggestion, suggestMedications } from '@/features/medications/medicationsApi'
import type { SuggestMedicationItem } from '@/features/medications/medicationsTypes'

const props = defineProps<{
  modelValue: MedicationDraft
  species?: string
  orgId?: string
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: MedicationDraft]
  selected: [item: SuggestMedicationItem | null]
}>()

const { t } = useI18n()

const query = ref(props.modelValue.medication_name || '')
const suggestions = ref<SuggestMedicationItem[]>([])
const isOpen = ref(false)
const isLoading = ref(false)
const showSuggestModal = ref(false)

// Estado del modal de sugerir al catálogo
const suggestName = ref('')
const suggestNotes = ref('')
const suggestBusy = ref(false)
const suggestSuccess = ref(false)
const suggestError = ref<string | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

watch(
  () => props.modelValue.medication_name,
  (val) => {
    if (val !== query.value) {
      query.value = val || ''
    }
  },
)

const isCatalogLinked = computed(() => Boolean(props.modelValue.catalog_id))
const isFreeText = computed(() => Boolean(props.modelValue.free_text))

function onInput() {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!query.value.trim()) {
    suggestions.value = []
    isOpen.value = false
    return
  }

  debounceTimer = setTimeout(async () => {
    isLoading.value = true
    try {
      suggestions.value = await suggestMedications(query.value, props.species)
      isOpen.value = true
    } catch {
      suggestions.value = []
    } finally {
      isLoading.value = false
    }
  }, 200)
}

function selectCatalogItem(item: SuggestMedicationItem) {
  const updated: MedicationDraft = {
    ...props.modelValue,
    medication_name: item.name,
    catalog_id: item.catalog_id,
    catalog_name_snapshot: item.name,
    free_text: false,
    presentation: props.modelValue.presentation || item.presentation || item.concentration || '',
    route: (item.default_route as MedicationDraft['route']) || props.modelValue.route,
    dose_unit:
      (item.default_dose_unit as MedicationDraft['dose_unit']) || props.modelValue.dose_unit,
  }
  query.value = item.name
  isOpen.value = false
  emit('update:modelValue', updated)
  emit('selected', item)
}

function selectFreeText() {
  const updated: MedicationDraft = {
    ...props.modelValue,
    medication_name: query.value.trim(),
    catalog_id: null,
    catalog_name_snapshot: null,
    free_text: true,
  }
  isOpen.value = false
  emit('update:modelValue', updated)
  emit('selected', null)
}

function clearSelection() {
  const updated: MedicationDraft = {
    ...props.modelValue,
    medication_name: '',
    catalog_id: null,
    catalog_name_snapshot: null,
    free_text: false,
  }
  query.value = ''
  suggestions.value = []
  isOpen.value = false
  emit('update:modelValue', updated)
  emit('selected', null)
}

function openSuggestModal() {
  suggestName.value = query.value.trim()
  suggestNotes.value = ''
  suggestError.value = null
  suggestSuccess.value = false
  showSuggestModal.value = true
  isOpen.value = false
}

async function submitSuggestion() {
  if (!suggestName.value.trim()) return
  if (!props.orgId) {
    suggestError.value = 'Se requiere contexto de organización'
    return
  }

  suggestBusy.value = true
  suggestError.value = null
  try {
    await createSuggestion({
      organization_id: props.orgId,
      name: suggestName.value.trim(),
      notes: suggestNotes.value.trim() || undefined,
    })
    suggestSuccess.value = true
    // Auto-seleccionar como texto libre para no interrumpir el flujo del clínico
    selectFreeText()
    setTimeout(() => {
      showSuggestModal.value = false
      suggestSuccess.value = false
    }, 1200)
  } catch (err: unknown) {
    suggestError.value = err instanceof Error ? err.message : 'Error al enviar sugerencia'
  } finally {
    suggestBusy.value = false
  }
}
</script>

<template>
  <div class="relative w-full space-y-1">
    <!-- Chip de medicamento seleccionado del catálogo -->
    <div
      v-if="isCatalogLinked"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-primary-200 bg-primary-50/50 p-2 text-sm dark:border-primary-800 dark:bg-primary-950/30"
      data-testid="medication-catalog-chip"
    >
      <div class="flex items-center gap-2">
        <span
          class="pi pi-check-circle text-primary-600 dark:text-primary-400"
          aria-hidden="true"
        />
        <span class="font-medium text-surface-900 dark:text-surface-100">
          {{ modelValue.catalog_name_snapshot || modelValue.medication_name }}
        </span>
        <span
          class="rounded bg-primary-100 px-2 py-0.5 text-xs font-semibold text-primary-800 dark:bg-primary-900 dark:text-primary-200"
        >
          {{ t('medications.catalogItem', 'Catálogo') }}
        </span>
      </div>
      <button
        v-if="!disabled"
        type="button"
        class="text-xs text-surface-500 hover:text-surface-800 dark:hover:text-surface-200"
        data-testid="medication-clear-btn"
        @click="clearSelection"
      >
        <span class="pi pi-times mr-1" aria-hidden="true" />{{ t('common.change', 'Cambiar') }}
      </button>
    </div>

    <!-- Entrada de búsqueda cuando no está fijado al catálogo -->
    <div v-else class="relative">
      <div class="relative">
        <input
          v-model="query"
          type="text"
          class="input w-full pr-8"
          :placeholder="
            t('medications.searchPlaceholder', 'Buscar medicamento en catálogo o escribir libre...')
          "
          :disabled="disabled"
          data-testid="medication-autocomplete-input"
          @input="onInput"
          @focus="query.trim() && onInput()"
        />
        <span
          v-if="isLoading"
          class="pi pi-spin pi-spinner absolute right-2.5 top-2.5 text-surface-400"
          aria-hidden="true"
        />
        <span
          v-else-if="isFreeText"
          class="absolute right-2 top-2 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900/50 dark:text-amber-300"
        >
          {{ t('medications.freeText', 'Texto libre') }}
        </span>
      </div>

      <!-- Menú desplegable con sugerencias -->
      <div
        v-if="isOpen && !disabled"
        class="absolute z-20 mt-1 max-h-64 w-full overflow-auto rounded-lg border border-surface-200 bg-white py-1 shadow-lg dark:border-surface-800 dark:bg-surface-900"
        data-testid="medication-suggestions-dropdown"
      >
        <ul role="listbox">
          <li
            v-for="item in suggestions"
            :key="item.catalog_id"
            class="cursor-pointer border-b border-surface-100 px-3 py-2 hover:bg-surface-100 dark:border-surface-800/50 dark:hover:bg-surface-800"
            role="option"
            data-testid="suggestion-catalog-option"
            @click="selectCatalogItem(item)"
          >
            <div class="flex items-center justify-between">
              <span class="font-medium text-surface-900 dark:text-surface-100">{{
                item.name
              }}</span>
              <span
                v-if="item.prescription_required"
                class="rounded bg-red-100 px-1.5 py-0.5 text-xs font-semibold text-red-700 dark:bg-red-950 dark:text-red-300"
              >
                {{ t('medications.prescriptionRequired', 'Receta') }}
              </span>
            </div>
            <div class="mt-0.5 flex flex-wrap gap-2 text-xs text-surface-500">
              <span v-if="item.form">{{ item.form }}</span>
              <span v-if="item.concentration">· {{ item.concentration }}</span>
              <span v-if="item.presentation">· {{ item.presentation }}</span>
            </div>
          </li>

          <!-- Opción: Usar como texto libre -->
          <li
            v-if="query.trim()"
            class="cursor-pointer px-3 py-2 text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-950/40"
            role="option"
            data-testid="suggestion-freetext-option"
            @click="selectFreeText"
          >
            <span class="pi pi-pencil mr-1 text-xs" aria-hidden="true" />
            {{ t('medications.useAsFreeText', 'Usar como texto libre') }}: "<strong>{{
              query
            }}</strong
            >"
          </li>

          <!-- Opción: Sugerir al catálogo -->
          <li
            v-if="orgId && query.trim()"
            class="cursor-pointer border-t border-surface-100 px-3 py-2 text-xs text-surface-600 hover:bg-surface-100 dark:border-surface-800 dark:text-surface-300 dark:hover:bg-surface-800"
            data-testid="suggestion-propose-option"
            @click="openSuggestModal"
          >
            <span class="pi pi-plus-circle mr-1 text-xs" aria-hidden="true" />
            {{ t('medications.suggestToCatalog', '¿No está listado? Sugerir al catálogo') }}
          </li>
        </ul>
      </div>
    </div>

    <!-- Botón secundario para sugerir si es texto libre -->
    <div v-if="isFreeText && !disabled && orgId" class="flex items-center justify-end">
      <button
        type="button"
        class="text-xs text-primary-600 hover:underline dark:text-primary-400"
        data-testid="propose-medication-btn"
        @click="openSuggestModal"
      >
        <span class="pi pi-send mr-1 text-xs" aria-hidden="true" />
        {{ t('medications.proposeForCatalog', 'Sugerir este fármaco al catálogo general') }}
      </button>
    </div>

    <!-- Modal para sugerir medicamento al catálogo (PP-F3-FE-003) -->
    <div
      v-if="showSuggestModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      data-testid="suggest-medication-modal"
    >
      <div
        class="card w-full max-w-md space-y-4 rounded-xl bg-white p-5 shadow-2xl dark:bg-surface-900"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-100">
          {{ t('medications.suggestTitle', 'Sugerir fármaco al catálogo') }}
        </h3>
        <p class="text-sm text-surface-600 dark:text-surface-400">
          {{
            t(
              'medications.suggestDesc',
              'Los administradores revisarán el medicamento para homologarlo en el catálogo.',
            )
          }}
        </p>

        <div
          v-if="suggestSuccess"
          class="rounded-lg bg-green-50 p-3 text-sm text-green-800 dark:bg-green-950 dark:text-green-200"
        >
          {{ t('medications.suggestSuccess', '¡Sugerencia enviada correctamente!') }}
        </div>
        <div
          v-if="suggestError"
          class="rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        >
          {{ suggestError }}
        </div>

        <form class="space-y-3" @submit.prevent="submitSuggestion">
          <label class="block">
            <span class="label">{{ t('medications.name', 'Nombre del medicamento') }} *</span>
            <input
              v-model="suggestName"
              type="text"
              class="input w-full"
              required
              data-testid="suggest-name-input"
            />
          </label>

          <label class="block">
            <span class="label">{{
              t('medications.notes', 'Notas adicionales (concentración, fabricante, etc.)')
            }}</span>
            <textarea
              v-model="suggestNotes"
              class="input w-full"
              rows="3"
              data-testid="suggest-notes-input"
            />
          </label>

          <div class="flex justify-end gap-2 pt-2">
            <button
              type="button"
              class="btn-secondary"
              :disabled="suggestBusy"
              @click="showSuggestModal = false"
            >
              {{ t('common.cancel', 'Cancelar') }}
            </button>
            <button
              type="submit"
              class="btn-primary"
              :disabled="suggestBusy || !suggestName.trim()"
              data-testid="suggest-submit-btn"
            >
              <span v-if="suggestBusy" class="pi pi-spin pi-spinner mr-1" aria-hidden="true" />
              {{ t('common.send', 'Enviar sugerencia') }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
