<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ReasonDialog from './ReasonDialog.vue'
import {
  adminApproveSuggestion,
  adminCreateMedication,
  adminDiscontinueMedication,
  adminGetUsageStats,
  adminImportMedications,
  adminListSuggestions,
  adminMergeMedication,
  adminRejectSuggestion,
  adminRevertImportBatch,
  adminUpdateMedication,
  listMedications,
} from '@/features/medications/medicationsApi'
import type {
  ImportBatch,
  Medication,
  MedicationForm,
  MedicationStatus,
  MedicationSuggestion,
  MedicationUsageStats,
} from '@/features/medications/medicationsTypes'

const { t } = useI18n()

type TabKey = 'catalog' | 'suggestions' | 'import' | 'stats'
const activeTab = ref<TabKey>('catalog')

// ------------------------------------------------------------------ Catálogo
const medications = ref<Medication[]>([])
const loadingMedications = ref(false)
const filterQ = ref('')
const filterSpecies = ref('')
const filterForm = ref<MedicationForm | ''>('')
const filterStatus = ref<MedicationStatus | ''>('')
const filterPrescription = ref<string>('')

const FORMS: MedicationForm[] = [
  'tablet',
  'capsule',
  'oral_solution',
  'injectable',
  'topical_spot_on',
  'topical_spray',
  'topical_shampoo',
  'otic_drops',
  'ophthalmic_drops',
  'paste',
  'powder',
]

async function loadCatalog() {
  loadingMedications.value = true
  try {
    const presc =
      filterPrescription.value === 'true'
        ? true
        : filterPrescription.value === 'false'
          ? false
          : undefined

    const res = await listMedications({
      q: filterQ.value.trim() || undefined,
      species: filterSpecies.value || undefined,
      form: filterForm.value || undefined,
      status: filterStatus.value || undefined,
      prescription_required: presc,
      limit: 50,
    })
    medications.value = res.data ?? []
  } catch (err) {
    console.error('Error cargando medicamentos:', err)
  } finally {
    loadingMedications.value = false
  }
}

watch([filterSpecies, filterForm, filterStatus, filterPrescription], () => {
  loadCatalog()
})

let searchDebounce: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchDebounce) clearTimeout(searchDebounce)
  searchDebounce = setTimeout(loadCatalog, 300)
}

// ------------------------------------------------------------------ Modales de acción
const selectedMed = ref<Medication | null>(null)
const showCreateModal = ref(false)
const showEditModal = ref(false)
const showDiscontinueDialog = ref(false)
const showMergeModal = ref(false)

function closeFormModals() {
  showCreateModal.value = false
  showEditModal.value = false
  showApproveModal.value = false
}

// Formulario de creación / edición
const formName = ref('')
const formGenericName = ref('')
const formForm = ref<MedicationForm>('tablet')
const formConcentration = ref('')
const formPresentation = ref('')
const formSpecies = ref('dog,cat')
const formRoutes = ref('oral')
const formPrescription = ref(false)
const formReason = ref('')
const formBusy = ref(false)
const formError = ref<string | null>(null)

function openCreateModal() {
  formName.value = ''
  formGenericName.value = ''
  formForm.value = 'tablet'
  formConcentration.value = ''
  formPresentation.value = ''
  formSpecies.value = 'dog,cat'
  formRoutes.value = 'oral'
  formPrescription.value = false
  formReason.value = ''
  formError.value = null
  showCreateModal.value = true
}

function openEditModal(med: Medication) {
  selectedMed.value = med
  formName.value = med.name
  formGenericName.value = med.generic_name || ''
  formForm.value = med.form
  formConcentration.value = med.concentration || ''
  formPresentation.value = med.presentation || ''
  formSpecies.value = (med.species || []).join(',')
  formRoutes.value = (med.routes || []).join(',')
  formPrescription.value = med.prescription_required
  formReason.value = ''
  formError.value = null
  showEditModal.value = true
}

async function handleSaveMedication(isNew: boolean) {
  if (!formName.value.trim() || formReason.value.trim().length < 10) {
    formError.value = 'El nombre y el motivo (mínimo 10 caracteres) son obligatorios.'
    return
  }

  formBusy.value = true
  formError.value = null
  try {
    const payload: Partial<Medication> & { reason: string } = {
      name: formName.value.trim(),
      generic_name: formGenericName.value.trim() || undefined,
      form: formForm.value,
      concentration: formConcentration.value.trim(),
      presentation: formPresentation.value.trim() || undefined,
      species: formSpecies.value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      routes: formRoutes.value
        .split(',')
        .map((r) => r.trim())
        .filter(Boolean),
      prescription_required: formPrescription.value,
      reason: formReason.value.trim(),
    }

    if (isNew) {
      await adminCreateMedication(payload)
      showCreateModal.value = false
    } else if (selectedMed.value) {
      await adminUpdateMedication(selectedMed.value.id, payload)
      showEditModal.value = false
    }
    await loadCatalog()
  } catch (err: unknown) {
    formError.value = err instanceof Error ? err.message : 'Error guardando medicamento'
  } finally {
    formBusy.value = false
  }
}

// Descontinuar
function openDiscontinueDialog(med: Medication) {
  selectedMed.value = med
  showDiscontinueDialog.value = true
}

async function confirmDiscontinue(reason: string) {
  if (!selectedMed.value) return
  try {
    await adminDiscontinueMedication(selectedMed.value.id, reason)
    showDiscontinueDialog.value = false
    await loadCatalog()
  } catch (err) {
    console.error('Error descontinuando:', err)
  }
}

// Fusionar (Merge)
const mergeTargetId = ref('')
const mergeReason = ref('')
const mergeBusy = ref(false)
const mergeError = ref<string | null>(null)

function openMergeModal(med: Medication) {
  selectedMed.value = med
  mergeTargetId.value = ''
  mergeReason.value = ''
  mergeError.value = null
  showMergeModal.value = true
}

async function confirmMerge() {
  if (!selectedMed.value || !mergeTargetId.value || mergeReason.value.trim().length < 10) {
    mergeError.value =
      'Debes seleccionar el fármaco destino y dar un motivo de al menos 10 caracteres.'
    return
  }

  mergeBusy.value = true
  mergeError.value = null
  try {
    await adminMergeMedication(selectedMed.value.id, mergeTargetId.value, mergeReason.value.trim())
    showMergeModal.value = false
    await loadCatalog()
  } catch (err: unknown) {
    mergeError.value = err instanceof Error ? err.message : 'Error fusionando medicamentos'
  } finally {
    mergeBusy.value = false
  }
}

// ------------------------------------------------------------------ Sugerencias
const suggestions = ref<MedicationSuggestion[]>([])
const loadingSuggestions = ref(false)
const filterSugStatus = ref('pending')

async function loadSuggestions() {
  loadingSuggestions.value = true
  try {
    suggestions.value = await adminListSuggestions(filterSugStatus.value || undefined)
  } catch (err) {
    console.error('Error cargando sugerencias:', err)
  } finally {
    loadingSuggestions.value = false
  }
}

const selectedSug = ref<MedicationSuggestion | null>(null)
const showApproveModal = ref(false)
const showRejectDialog = ref(false)

function openApproveModal(sug: MedicationSuggestion) {
  selectedSug.value = sug
  formName.value = sug.name
  formGenericName.value = ''
  formForm.value = 'tablet'
  formConcentration.value = ''
  formPresentation.value = ''
  formSpecies.value = 'dog,cat'
  formRoutes.value = 'oral'
  formPrescription.value = false
  formReason.value = 'Aprobación de sugerencia enviada por veterinario'
  formError.value = null
  showApproveModal.value = true
}

async function confirmApprove() {
  if (!selectedSug.value || !formName.value.trim() || formReason.value.trim().length < 10) {
    formError.value = 'El nombre y motivo son obligatorios.'
    return
  }

  formBusy.value = true
  formError.value = null
  try {
    await adminApproveSuggestion(selectedSug.value.id, {
      name: formName.value.trim(),
      generic_name: formGenericName.value.trim() || undefined,
      form: formForm.value,
      concentration: formConcentration.value.trim(),
      presentation: formPresentation.value.trim() || undefined,
      species: formSpecies.value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      routes: formRoutes.value
        .split(',')
        .map((r) => r.trim())
        .filter(Boolean),
      prescription_required: formPrescription.value,
      reason: formReason.value.trim(),
    })
    showApproveModal.value = false
    await loadSuggestions()
  } catch (err: unknown) {
    formError.value = err instanceof Error ? err.message : 'Error aprobando sugerencia'
  } finally {
    formBusy.value = false
  }
}

function openRejectDialog(sug: MedicationSuggestion) {
  selectedSug.value = sug
  showRejectDialog.value = true
}

async function confirmReject(reason: string) {
  if (!selectedSug.value) return
  try {
    await adminRejectSuggestion(selectedSug.value.id, reason)
    showRejectDialog.value = false
    await loadSuggestions()
  } catch (err) {
    console.error('Error rechazando sugerencia:', err)
  }
}

// ------------------------------------------------------------------ Importación
const importSource = ref('ica')
const importDryRun = ref(true)
const importFile = ref<File | null>(null)
const importBusy = ref(false)
const importResult = ref<ImportBatch | null>(null)
const importError = ref<string | null>(null)
const showRevertDialog = ref(false)

function onFileChange(e: Event) {
  const target = e.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    importFile.value = target.files[0]
  }
}

async function handleImport() {
  if (!importFile.value) {
    importError.value = 'Selecciona un archivo CSV o JSON.'
    return
  }

  importBusy.value = true
  importError.value = null
  importResult.value = null
  try {
    const text = await importFile.value.text()
    let rows: unknown[] = []
    if (importFile.value.name.endsWith('.json')) {
      rows = JSON.parse(text)
    } else {
      // Parsear líneas CSV simples (encabezado en primera fila)
      const lines = text.split(/\r?\n/).filter((l) => l.trim().length > 0)
      if (lines.length > 1) {
        const headers = lines[0].split(',').map((h) => h.trim().replace(/^"|"$/g, ''))
        rows = lines.slice(1).map((line) => {
          const values = line.split(',').map((v) => v.trim().replace(/^"|"$/g, ''))
          const row: Record<string, string> = {}
          headers.forEach((h, idx) => {
            row[h] = values[idx] || ''
          })
          return row
        })
      }
    }

    importResult.value = await adminImportMedications({
      source: importSource.value,
      dry_run: importDryRun.value,
      rows,
    })
  } catch (err: unknown) {
    importError.value = err instanceof Error ? err.message : 'Error durante la importación'
  } finally {
    importBusy.value = false
  }
}

async function confirmRevertImport(reason: string) {
  if (!importResult.value?.id) return
  try {
    importResult.value = await adminRevertImportBatch(importResult.value.id, reason)
    showRevertDialog.value = false
    await loadCatalog()
  } catch (err) {
    console.error('Error revirtiendo importación:', err)
  }
}

// ------------------------------------------------------------------ Métricas
const stats = ref<MedicationUsageStats | null>(null)
const loadingStats = ref(false)

async function loadStats() {
  loadingStats.value = true
  try {
    stats.value = await adminGetUsageStats()
  } catch (err) {
    console.error('Error cargando métricas:', err)
  } finally {
    loadingStats.value = false
  }
}

watch(activeTab, (tab) => {
  if (tab === 'catalog') loadCatalog()
  else if (tab === 'suggestions') loadSuggestions()
  else if (tab === 'stats') loadStats()
})

onMounted(() => {
  loadCatalog()
})
</script>

<template>
  <main class="space-y-6 p-6" data-testid="admin-medications-page">
    <header class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-surface-900 dark:text-surface-100">
          {{ t('admin.medications.title', 'Catálogo de medicamentos') }}
        </h1>
        <p class="text-sm text-surface-600 dark:text-surface-400">
          {{
            t(
              'admin.medications.subtitle',
              'Gestión centralizada de fármacos, sugerencias clínicas y métricas de uso.',
            )
          }}
        </p>
      </div>

      <button
        type="button"
        class="btn-primary"
        data-testid="btn-create-medication"
        @click="openCreateModal"
      >
        <span class="pi pi-plus mr-2" aria-hidden="true" />
        {{ t('admin.medications.createMedication', 'Nuevo medicamento') }}
      </button>
    </header>

    <!-- Pestañas de navegación -->
    <nav class="flex border-b border-surface-200 dark:border-surface-800" aria-label="Tabs">
      <button
        type="button"
        class="border-b-2 px-4 py-2 text-sm font-medium transition-colors"
        :class="
          activeTab === 'catalog'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300'
        "
        data-testid="tab-catalog"
        @click="activeTab = 'catalog'"
      >
        <span class="pi pi-database mr-2" aria-hidden="true" />
        {{ t('admin.medications.tabCatalog', 'Catálogo') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-4 py-2 text-sm font-medium transition-colors"
        :class="
          activeTab === 'suggestions'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300'
        "
        data-testid="tab-suggestions"
        @click="activeTab = 'suggestions'"
      >
        <span class="pi pi-inbox mr-2" aria-hidden="true" />
        {{ t('admin.medications.tabSuggestions', 'Bandeja de sugerencias') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-4 py-2 text-sm font-medium transition-colors"
        :class="
          activeTab === 'import'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300'
        "
        data-testid="tab-import"
        @click="activeTab = 'import'"
      >
        <span class="pi pi-upload mr-2" aria-hidden="true" />
        {{ t('admin.medications.tabImport', 'Importación en lote') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-4 py-2 text-sm font-medium transition-colors"
        :class="
          activeTab === 'stats'
            ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
            : 'border-transparent text-surface-500 hover:border-surface-300 hover:text-surface-700 dark:hover:text-surface-300'
        "
        data-testid="tab-stats"
        @click="activeTab = 'stats'"
      >
        <span class="pi pi-chart-bar mr-2" aria-hidden="true" />
        {{ t('admin.medications.tabStats', 'Adopción y métricas') }}
      </button>
    </nav>

    <!-- Contenido: Catálogo -->
    <section v-if="activeTab === 'catalog'" class="space-y-4" data-testid="section-catalog">
      <!-- Filtros -->
      <div class="grid gap-3 sm:grid-cols-2 md:grid-cols-5">
        <input
          v-model="filterQ"
          type="search"
          class="input"
          :placeholder="t('common.searchPlaceholder', 'Buscar por nombre...')"
          data-testid="filter-q"
          @input="onSearchInput"
        />

        <select v-model="filterSpecies" class="input" data-testid="filter-species">
          <option value="">{{ t('admin.medications.allSpecies', 'Todas las especies') }}</option>
          <option value="dog">{{ t('pets.species.dog', 'Canino') }}</option>
          <option value="cat">{{ t('pets.species.cat', 'Felino') }}</option>
        </select>

        <select v-model="filterForm" class="input" data-testid="filter-form">
          <option value="">{{ t('admin.medications.allForms', 'Todas las formas') }}</option>
          <option v-for="f in FORMS" :key="f" :value="f">{{ f }}</option>
        </select>

        <select v-model="filterStatus" class="input" data-testid="filter-status">
          <option value="">{{ t('admin.medications.allStatuses', 'Todos los estados') }}</option>
          <option value="active">{{ t('admin.medications.statusActive', 'Activo') }}</option>
          <option value="discontinued">
            {{ t('admin.medications.statusDiscontinued', 'Descontinuado') }}
          </option>
          <option value="merged">{{ t('admin.medications.statusMerged', 'Fusionado') }}</option>
        </select>

        <select v-model="filterPrescription" class="input" data-testid="filter-prescription">
          <option value="">{{ t('admin.medications.allPrescriptions', 'Receta (todos)') }}</option>
          <option value="true">
            {{ t('admin.medications.prescriptionYes', 'Requiere receta') }}
          </option>
          <option value="false">{{ t('admin.medications.prescriptionNo', 'Venta libre') }}</option>
        </select>
      </div>

      <!-- Tabla de medicamentos -->
      <div
        class="overflow-x-auto rounded-xl border border-surface-200 bg-white shadow-sm dark:border-surface-800 dark:bg-surface-900"
      >
        <table class="w-full text-left text-sm">
          <thead
            class="border-b border-surface-200 bg-surface-50 text-xs uppercase text-surface-500 dark:border-surface-800 dark:bg-surface-800/50"
          >
            <tr>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.colName', 'Medicamento') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.colForm', 'Forma / Dosis') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.colSpecies', 'Especies') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.colPrescription', 'Receta') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.colStatus', 'Estado') }}
              </th>
              <th scope="col" class="px-4 py-3 text-right">
                {{ t('admin.medications.colUsage', 'Uso') }}
              </th>
              <th scope="col" class="px-4 py-3 text-right">
                {{ t('common.actions', 'Acciones') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-surface-100 dark:divide-surface-800">
            <tr v-if="loadingMedications">
              <td colspan="7" class="p-8 text-center text-surface-500">
                <span class="pi pi-spin pi-spinner mr-2" aria-hidden="true" />
                {{ t('common.loading', 'Cargando medicamentos...') }}
              </td>
            </tr>
            <tr v-else-if="medications.length === 0">
              <td colspan="7" class="p-8 text-center text-surface-500">
                {{ t('admin.medications.empty', 'No se encontraron medicamentos en el catálogo.') }}
              </td>
            </tr>
            <tr
              v-for="m in medications"
              :key="m.id"
              class="hover:bg-surface-50/60 dark:hover:bg-surface-800/40"
              data-testid="medication-table-row"
            >
              <td class="px-4 py-3">
                <div class="font-medium text-surface-900 dark:text-surface-100">{{ m.name }}</div>
                <div v-if="m.generic_name" class="text-xs text-surface-500">
                  {{ m.generic_name }}
                </div>
              </td>
              <td class="px-4 py-3">
                <span>{{ m.form }}</span>
                <span v-if="m.concentration" class="text-surface-500">
                  · {{ m.concentration }}</span
                >
              </td>
              <td class="px-4 py-3">
                <span class="text-xs">{{ (m.species || []).join(', ') }}</span>
              </td>
              <td class="px-4 py-3">
                <span
                  v-if="m.prescription_required"
                  class="rounded bg-red-100 px-2 py-0.5 text-xs font-semibold text-red-700 dark:bg-red-950 dark:text-red-300"
                >
                  {{ t('medications.prescriptionRequired', 'Receta') }}
                </span>
                <span v-else class="text-xs text-surface-400">—</span>
              </td>
              <td class="px-4 py-3">
                <span
                  class="rounded px-2 py-0.5 text-xs font-semibold"
                  :class="{
                    'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-300':
                      m.status === 'active',
                    'bg-surface-200 text-surface-700 dark:bg-surface-800 dark:text-surface-300':
                      m.status === 'discontinued',
                    'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300':
                      m.status === 'merged',
                  }"
                >
                  {{ m.status }}
                </span>
              </td>
              <td class="px-4 py-3 text-right font-medium">
                {{ m.usage_count }}
              </td>
              <td class="space-x-2 px-4 py-3 text-right">
                <button
                  type="button"
                  class="text-xs text-primary-600 hover:underline dark:text-primary-400"
                  data-testid="btn-edit-med"
                  @click="openEditModal(m)"
                >
                  {{ t('common.edit', 'Editar') }}
                </button>
                <button
                  v-if="m.status === 'active'"
                  type="button"
                  class="text-xs text-amber-600 hover:underline dark:text-amber-400"
                  data-testid="btn-merge-med"
                  @click="openMergeModal(m)"
                >
                  {{ t('admin.medications.merge', 'Fusionar') }}
                </button>
                <button
                  v-if="m.status === 'active'"
                  type="button"
                  class="text-xs text-red-600 hover:underline dark:text-red-400"
                  data-testid="btn-discontinue-med"
                  @click="openDiscontinueDialog(m)"
                >
                  {{ t('admin.medications.discontinue', 'Descontinuar') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Contenido: Sugerencias -->
    <section v-if="activeTab === 'suggestions'" class="space-y-4" data-testid="section-suggestions">
      <div class="flex items-center gap-3">
        <select v-model="filterSugStatus" class="input max-w-xs" @change="loadSuggestions">
          <option value="pending">
            {{ t('admin.medications.sugPending', 'Pendientes de revisión') }}
          </option>
          <option value="approved">{{ t('admin.medications.sugApproved', 'Aprobadas') }}</option>
          <option value="rejected">{{ t('admin.medications.sugRejected', 'Rechazadas') }}</option>
          <option value="">{{ t('admin.medications.sugAll', 'Todas') }}</option>
        </select>
      </div>

      <div
        class="overflow-x-auto rounded-xl border border-surface-200 bg-white shadow-sm dark:border-surface-800 dark:bg-surface-900"
      >
        <table class="w-full text-left text-sm">
          <thead
            class="border-b border-surface-200 bg-surface-50 text-xs uppercase text-surface-500 dark:border-surface-800 dark:bg-surface-800/50"
          >
            <tr>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.sugName', 'Nombre sugerido') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.sugNotes', 'Notas / Indicaciones') }}
              </th>
              <th scope="col" class="px-4 py-3">
                {{ t('admin.medications.sugStatus', 'Estado') }}
              </th>
              <th scope="col" class="px-4 py-3">{{ t('admin.medications.sugDate', 'Fecha') }}</th>
              <th scope="col" class="px-4 py-3 text-right">
                {{ t('common.actions', 'Acciones') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-surface-100 dark:divide-surface-800">
            <tr v-if="loadingSuggestions">
              <td colspan="5" class="p-8 text-center text-surface-500">
                <span class="pi pi-spin pi-spinner mr-2" aria-hidden="true" />
                {{ t('common.loading', 'Cargando sugerencias...') }}
              </td>
            </tr>
            <tr v-else-if="suggestions.length === 0">
              <td colspan="5" class="p-8 text-center text-surface-500">
                {{ t('admin.medications.sugEmpty', 'No hay sugerencias en esta bandeja.') }}
              </td>
            </tr>
            <tr
              v-for="sug in suggestions"
              :key="sug.id"
              class="hover:bg-surface-50/60 dark:hover:bg-surface-800/40"
              data-testid="suggestion-table-row"
            >
              <td class="px-4 py-3 font-medium text-surface-900 dark:text-surface-100">
                {{ sug.name }}
              </td>
              <td class="px-4 py-3 text-surface-600 dark:text-surface-300">
                {{ sug.notes || '—' }}
              </td>
              <td class="px-4 py-3">
                <span
                  class="rounded px-2 py-0.5 text-xs font-semibold"
                  :class="{
                    'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300':
                      sug.status === 'pending',
                    'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-300':
                      sug.status === 'approved',
                    'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300':
                      sug.status === 'rejected',
                  }"
                >
                  {{ sug.status }}
                </span>
              </td>
              <td class="px-4 py-3 text-xs text-surface-500">
                {{ new Date(sug.created_at).toLocaleDateString() }}
              </td>
              <td class="space-x-2 px-4 py-3 text-right">
                <template v-if="sug.status === 'pending'">
                  <button
                    type="button"
                    class="btn-primary py-1 text-xs"
                    data-testid="btn-approve-sug"
                    @click="openApproveModal(sug)"
                  >
                    {{ t('admin.medications.approve', 'Aprobar') }}
                  </button>
                  <button
                    type="button"
                    class="btn-secondary py-1 text-xs text-red-600 hover:text-red-700"
                    data-testid="btn-reject-sug"
                    @click="openRejectDialog(sug)"
                  >
                    {{ t('admin.medications.reject', 'Rechazar') }}
                  </button>
                </template>
                <span v-else class="text-xs text-surface-400">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Contenido: Importación en lote -->
    <section v-if="activeTab === 'import'" class="space-y-6 max-w-2xl" data-testid="section-import">
      <div
        class="card space-y-4 rounded-xl border border-surface-200 bg-white p-6 dark:border-surface-800 dark:bg-surface-900"
      >
        <h2 class="text-lg font-bold text-surface-900 dark:text-surface-100">
          {{ t('admin.medications.importTitle', 'Carga de catálogo regulatorio') }}
        </h2>
        <p class="text-sm text-surface-600 dark:text-surface-400">
          {{
            t(
              'admin.medications.importSubtitle',
              'Soporta archivos CSV o JSON de entidades regulatorias (ICA Colombia). La identidad se preserva por (nombre, forma, concentración).',
            )
          }}
        </p>

        <div
          v-if="importError"
          class="rounded bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        >
          {{ importError }}
        </div>

        <form class="space-y-4" @submit.prevent="handleImport">
          <label class="block">
            <span class="label">{{
              t('admin.medications.importSource', 'Fuente reguladora')
            }}</span>
            <select v-model="importSource" class="input w-full">
              <option value="ica">ICA Colombia (Medicamentos Veterinarios)</option>
              <option value="manual">Manual / Otro</option>
            </select>
          </label>

          <label class="block">
            <span class="label">{{ t('admin.medications.importFile', 'Archivo CSV o JSON') }}</span>
            <input
              type="file"
              accept=".csv,.json"
              class="input w-full"
              data-testid="import-file-input"
              @change="onFileChange"
            />
          </label>

          <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300">
            <input v-model="importDryRun" type="checkbox" data-testid="import-dryrun-checkbox" />
            <span>{{
              t(
                'admin.medications.dryRunLabel',
                'Modo simulación (Dry Run: valida sin modificar la base de datos)',
              )
            }}</span>
          </label>

          <button
            type="submit"
            class="btn-primary"
            :disabled="importBusy || !importFile"
            data-testid="import-submit-btn"
          >
            <span v-if="importBusy" class="pi pi-spin pi-spinner mr-2" aria-hidden="true" />
            {{
              importDryRun
                ? t('admin.medications.runDryRun', 'Previsualizar importación')
                : t('admin.medications.runImport', 'Ejecutar importación')
            }}
          </button>
        </form>

        <!-- Resultados de importación -->
        <div
          v-if="importResult"
          class="mt-4 rounded-xl border border-surface-200 bg-surface-50 p-4 dark:border-surface-800 dark:bg-surface-800/40"
          data-testid="import-results"
        >
          <h3 class="font-semibold text-surface-900 dark:text-surface-100">
            {{ t('admin.medications.resultsTitle', 'Resultado del lote') }}
          </h3>
          <div class="mt-2 grid grid-cols-3 gap-2 text-center">
            <div class="rounded-lg bg-white p-3 dark:bg-surface-800">
              <div class="text-xs text-surface-500">
                {{ t('admin.medications.resCreated', 'Creados') }}
              </div>
              <div class="text-xl font-bold text-green-600">{{ importResult.created }}</div>
            </div>
            <div class="rounded-lg bg-white p-3 dark:bg-surface-800">
              <div class="text-xs text-surface-500">
                {{ t('admin.medications.resUpdated', 'Actualizados') }}
              </div>
              <div class="text-xl font-bold text-primary-600">{{ importResult.updated }}</div>
            </div>
            <div class="rounded-lg bg-white p-3 dark:bg-surface-800">
              <div class="text-xs text-surface-500">
                {{ t('admin.medications.resSkipped', 'Omitidos') }}
              </div>
              <div class="text-xl font-bold text-amber-600">{{ importResult.skipped }}</div>
            </div>
          </div>

          <div v-if="importResult.errors && importResult.errors.length > 0" class="mt-3">
            <h4 class="text-xs font-semibold text-red-600">
              {{ t('admin.medications.errorsList', 'Errores detectados:') }}
            </h4>
            <ul class="mt-1 list-disc pl-5 text-xs text-red-700 dark:text-red-300">
              <li v-for="(err, i) in importResult.errors.slice(0, 10)" :key="i">{{ err }}</li>
            </ul>
          </div>

          <div
            v-if="!importDryRun && importResult.id && !importResult.reverted_at"
            class="mt-4 flex justify-end"
          >
            <button
              type="button"
              class="btn-secondary text-xs text-red-600"
              data-testid="btn-revert-batch"
              @click="showRevertDialog = true"
            >
              <span class="pi pi-undo mr-1" aria-hidden="true" />
              {{ t('admin.medications.revertBatch', 'Revertir lote') }}
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- Contenido: Métricas de adopción -->
    <section v-if="activeTab === 'stats'" class="space-y-6" data-testid="section-stats">
      <div v-if="loadingStats" class="p-8 text-center text-surface-500">
        <span class="pi pi-spin pi-spinner mr-2" aria-hidden="true" />
        {{ t('common.loading', 'Cargando métricas...') }}
      </div>
      <div v-else-if="stats" class="space-y-6">
        <!-- Indicadores Clave -->
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div
            class="card rounded-xl border border-surface-200 bg-white p-5 shadow-sm dark:border-surface-800 dark:bg-surface-900"
          >
            <div class="text-xs font-medium uppercase text-surface-500">
              {{ t('admin.medications.totalItems', 'Total prescripciones') }}
            </div>
            <div class="mt-2 text-3xl font-extrabold text-surface-900 dark:text-surface-100">
              {{ stats.total_plan_items }}
            </div>
          </div>

          <div
            class="card rounded-xl border border-surface-200 bg-white p-5 shadow-sm dark:border-surface-800 dark:bg-surface-900"
          >
            <div class="text-xs font-medium uppercase text-surface-500">
              {{ t('admin.medications.catalogItems', 'Vinculadas a catálogo') }}
            </div>
            <div class="mt-2 text-3xl font-extrabold text-primary-600 dark:text-primary-400">
              {{ stats.catalog_linked_items }}
            </div>
          </div>

          <div
            class="card rounded-xl border border-surface-200 bg-white p-5 shadow-sm dark:border-surface-800 dark:bg-surface-900"
          >
            <div class="text-xs font-medium uppercase text-surface-500">
              {{ t('admin.medications.freeTextItems', 'Texto libre') }}
            </div>
            <div class="mt-2 text-3xl font-extrabold text-amber-600 dark:text-amber-400">
              {{ stats.free_text_items }}
            </div>
          </div>

          <div
            class="card rounded-xl border border-surface-200 bg-white p-5 shadow-sm dark:border-surface-800 dark:bg-surface-900"
          >
            <div class="text-xs font-medium uppercase text-surface-500">
              {{ t('admin.medications.adoptionRate', 'Tasa de adopción') }}
            </div>
            <div class="mt-2 text-3xl font-extrabold text-green-600 dark:text-green-400">
              {{ stats.adoption_rate_pct.toFixed(1) }}%
            </div>
          </div>
        </div>

        <!-- Top 10 medicamentos más usados -->
        <div
          class="rounded-xl border border-surface-200 bg-white p-6 shadow-sm dark:border-surface-800 dark:bg-surface-900"
        >
          <h2 class="text-lg font-bold text-surface-900 dark:text-surface-100">
            {{ t('admin.medications.topMedsTitle', 'Top 10 medicamentos más prescritos') }}
          </h2>
          <div class="mt-4 overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead
                class="border-b border-surface-200 bg-surface-50 text-xs uppercase text-surface-500 dark:border-surface-800 dark:bg-surface-800/50"
              >
                <tr>
                  <th scope="col" class="px-4 py-2">#</th>
                  <th scope="col" class="px-4 py-2">
                    {{ t('admin.medications.colName', 'Medicamento') }}
                  </th>
                  <th scope="col" class="px-4 py-2 text-right">
                    {{ t('admin.medications.colUsage', 'Veces prescrito') }}
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-surface-100 dark:divide-surface-800">
                <tr v-if="!stats.top_medications || stats.top_medications.length === 0">
                  <td colspan="3" class="p-4 text-center text-surface-500">
                    {{
                      t('admin.medications.topMedsEmpty', 'Aún no hay prescripciones registradas.')
                    }}
                  </td>
                </tr>
                <tr
                  v-for="(top, i) in stats.top_medications"
                  :key="top.catalog_id"
                  class="hover:bg-surface-50/50"
                >
                  <td class="px-4 py-2.5 font-bold text-surface-400">{{ i + 1 }}</td>
                  <td class="px-4 py-2.5 font-medium text-surface-900 dark:text-surface-100">
                    {{ top.name }}
                  </td>
                  <td class="px-4 py-2.5 text-right font-bold text-primary-600">
                    {{ top.usage_count }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </section>

    <!-- Modal Crear / Editar Medicamento -->
    <div
      v-if="showCreateModal || showEditModal || showApproveModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
    >
      <div
        class="card w-full max-w-lg space-y-4 rounded-xl bg-white p-6 shadow-2xl dark:bg-surface-900"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-100">
          {{
            showCreateModal
              ? 'Nuevo medicamento'
              : showEditModal
                ? 'Editar medicamento'
                : 'Aprobar sugerencia'
          }}
        </h3>

        <div
          v-if="formError"
          class="rounded bg-red-50 p-2 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        >
          {{ formError }}
        </div>

        <form
          class="space-y-3"
          @submit.prevent="
            showApproveModal ? confirmApprove() : handleSaveMedication(showCreateModal)
          "
        >
          <div class="grid gap-2 sm:grid-cols-2">
            <label class="block">
              <span class="label">Nombre comercial *</span>
              <input v-model="formName" type="text" class="input w-full" required />
            </label>
            <label class="block">
              <span class="label">Nombre genérico</span>
              <input v-model="formGenericName" type="text" class="input w-full" />
            </label>
          </div>

          <div class="grid gap-2 sm:grid-cols-2">
            <label class="block">
              <span class="label">Forma farmacéutica *</span>
              <select v-model="formForm" class="input w-full">
                <option v-for="f in FORMS" :key="f" :value="f">{{ f }}</option>
              </select>
            </label>
            <label class="block">
              <span class="label">Concentración *</span>
              <input
                v-model="formConcentration"
                type="text"
                class="input w-full"
                placeholder="ej. 500 mg"
                required
              />
            </label>
          </div>

          <div class="grid gap-2 sm:grid-cols-2">
            <label class="block">
              <span class="label">Especies (separadas por coma)</span>
              <input v-model="formSpecies" type="text" class="input w-full" placeholder="dog,cat" />
            </label>
            <label class="block">
              <span class="label">Vías (separadas por coma)</span>
              <input
                v-model="formRoutes"
                type="text"
                class="input w-full"
                placeholder="oral,sc,iv"
              />
            </label>
          </div>

          <label class="flex items-center gap-2 text-sm">
            <input v-model="formPrescription" type="checkbox" />
            <span>Requiere receta médica</span>
          </label>

          <label class="block">
            <span class="label">Motivo de auditoría * (mínimo 10 caracteres)</span>
            <textarea
              v-model="formReason"
              class="input w-full"
              rows="2"
              required
              minlength="10"
              placeholder="Motivo de la acción para auditoría..."
            />
          </label>

          <div class="flex justify-end gap-2 pt-2">
            <button
              type="button"
              class="btn-secondary"
              :disabled="formBusy"
              @click="closeFormModals"
            >
              {{ t('common.cancel', 'Cancelar') }}
            </button>
            <button type="submit" class="btn-primary" :disabled="formBusy">
              <span v-if="formBusy" class="pi pi-spin pi-spinner mr-1" aria-hidden="true" />
              {{ t('common.save', 'Guardar') }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Modal Fusionar (Merge) -->
    <div
      v-if="showMergeModal && selectedMed"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
    >
      <div
        class="card w-full max-w-lg space-y-4 rounded-xl bg-white p-6 shadow-2xl dark:bg-surface-900"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-100">
          Fusionar medicamento
        </h3>
        <p class="text-sm text-surface-600 dark:text-surface-400">
          El medicamento <strong>{{ selectedMed.name }}</strong> quedará marcado como
          <em>merged</em> y todas las consultas futuras se redirigirán al destino seleccionado.
        </p>

        <div
          v-if="mergeError"
          class="rounded bg-red-50 p-2 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        >
          {{ mergeError }}
        </div>

        <form class="space-y-3" @submit.prevent="confirmMerge">
          <label class="block">
            <span class="label">Medicamento destino *</span>
            <select v-model="mergeTargetId" class="input w-full" required>
              <option value="">Seleccione el medicamento destino...</option>
              <option
                v-for="m in medications.filter(
                  (x) => x.id !== selectedMed?.id && x.status === 'active',
                )"
                :key="m.id"
                :value="m.id"
              >
                {{ m.name }} ({{ m.form }} - {{ m.concentration }})
              </option>
            </select>
          </label>

          <label class="block">
            <span class="label">Motivo de fusión * (mínimo 10 caracteres)</span>
            <textarea
              v-model="mergeReason"
              class="input w-full"
              rows="2"
              required
              minlength="10"
              placeholder="Explica el motivo de la fusión..."
            />
          </label>

          <div class="flex justify-end gap-2 pt-2">
            <button
              type="button"
              class="btn-secondary"
              :disabled="mergeBusy"
              @click="showMergeModal = false"
            >
              {{ t('common.cancel', 'Cancelar') }}
            </button>
            <button type="submit" class="btn-primary" :disabled="mergeBusy">
              <span v-if="mergeBusy" class="pi pi-spin pi-spinner mr-1" aria-hidden="true" />
              {{ t('admin.medications.confirmMerge', 'Confirmar fusión') }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Diálogo de motivo para Descontinuar -->
    <ReasonDialog
      v-if="showDiscontinueDialog && selectedMed"
      title="Descontinuar medicamento"
      :target="selectedMed.name"
      @confirm="confirmDiscontinue"
      @cancel="showDiscontinueDialog = false"
    />

    <!-- Diálogo de motivo para Rechazar sugerencia -->
    <ReasonDialog
      v-if="showRejectDialog && selectedSug"
      title="Rechazar sugerencia de medicamento"
      :target="selectedSug.name"
      @confirm="confirmReject"
      @cancel="showRejectDialog = false"
    />

    <!-- Diálogo de motivo para Revertir Lote -->
    <ReasonDialog
      v-if="showRevertDialog && importResult"
      title="Revertir lote de importación"
      :target="importResult.id"
      @confirm="confirmRevertImport"
      @cancel="showRevertDialog = false"
    />
  </main>
</template>
