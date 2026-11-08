<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  ApiError,
  type PrescriptionDetail,
  type PrescriptionSummary,
} from '@/shared/api/client'
import { usePetStore } from '@/features/pets/petStore'

const route = useRoute()
const router = useRouter()
const petStore = usePetStore()

// Filtros
const selectedPetId = ref<string>('')
const filterStatus = ref<string>('')
const filterSource = ref<string>('')

// Estado de Prescripciones
const prescriptions = ref<PrescriptionSummary[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

// Modal de Selección de Mascota para Subir Receta
const showPetPickerModal = ref(false)

// Modal de Detalle
const selectedId = ref<string | null>(null)
const selectedDetail = ref<PrescriptionDetail | null>(null)
const loadingDetail = ref(false)

// Modal de Confirmación de Archivado
const archivingPrescription = ref<PrescriptionSummary | null>(null)
const archivingBusy = ref(false)

// Carga de mascotas y recetas
onMounted(async () => {
  try {
    await petStore.load('active')
  } catch {
    // ignorar error de carga de mascotas
  }

  // Pre-seleccionar mascota si viene por query param
  if (route.query.petId && typeof route.query.petId === 'string') {
    selectedPetId.value = route.query.petId
  }

  if (route.query.confirmed && typeof route.query.confirmed === 'string') {
    notice.value =
      '¡Receta confirmada con éxito! Ya puedes revisarla o solicitar cotización en farmacias.'
  }

  await loadPrescriptions()
})

async function loadPrescriptions() {
  loading.value = true
  error.value = null
  try {
    const queryParams: Record<string, string | number> = {
      limit: 50,
    }
    if (selectedPetId.value) {
      queryParams.pet_id = selectedPetId.value
    }
    if (filterStatus.value) {
      queryParams.status = filterStatus.value
    }
    if (filterSource.value) {
      queryParams.source = filterSource.value
    }

    const res = await api().GET('/me/prescriptions', {
      params: {
        query: queryParams as unknown as {
          pet_id?: string
          status?: string
          source?: 'issued' | 'external'
          limit?: number
        },
      },
    })

    if (res.error) throw new ApiError(res.error, res.response.status)
    prescriptions.value = res.data?.data ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar las recetas médicas'
  } finally {
    loading.value = false
  }
}

watch([selectedPetId, filterStatus, filterSource], () => {
  void loadPrescriptions()
})

// Mapeo de mascota por ID
function getPet(petId: string) {
  return petStore.items.find((p) => p.id === petId)
}

function getPetName(petId: string) {
  const p = getPet(petId)
  return p?.name ?? 'Mascota'
}

function getPetSpecies(petId: string) {
  const p = getPet(petId)
  return p?.species ?? ''
}

// Acción de "+ Subir receta externa"
function handleUploadExternal() {
  if (selectedPetId.value) {
    router.push({
      name: 'pet-prescription-new',
      params: { petId: selectedPetId.value },
    })
    return
  }

  if (petStore.items.length === 1) {
    router.push({
      name: 'pet-prescription-new',
      params: { petId: petStore.items[0].id },
    })
    return
  }

  if (petStore.items.length > 1) {
    showPetPickerModal.value = true
    return
  }

  // Si no tiene mascotas, enviar a crear mascota
  router.push({ name: 'pet-new' })
}

function selectPetForUpload(petId: string) {
  showPetPickerModal.value = false
  router.push({
    name: 'pet-prescription-new',
    params: { petId },
  })
}

// Visualización de detalle
async function viewDetail(id: string) {
  selectedId.value = id
  loadingDetail.value = true
  selectedDetail.value = null
  try {
    const res = await api().GET('/prescriptions/{prescriptionId}', {
      params: { path: { prescriptionId: id } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    selectedDetail.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar el detalle de la receta'
  } finally {
    loadingDetail.value = false
  }
}

function closeDetail() {
  selectedId.value = null
  selectedDetail.value = null
}

// Descarga / visualización de PDF
async function downloadPDF(id: string) {
  try {
    const res = await api().GET('/prescriptions/{prescriptionId}/pdf', {
      params: { path: { prescriptionId: id } },
    })
    if (res.data?.url) {
      window.open(res.data.url, '_blank')
    } else {
      notice.value = 'El PDF se está generando o no está disponible aún.'
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al obtener el PDF de la receta'
  }
}

// Visualización de imagen escaneada
async function viewPrescriptionImage(rxId: string, page: number) {
  try {
    const res = await api().GET('/prescriptions/{prescriptionId}/images/{page}', {
      params: { path: { prescriptionId: rxId, page } },
    })
    if (res.data?.url) {
      window.open(res.data.url, '_blank')
    } else {
      notice.value = 'No se pudo obtener el enlace de la imagen.'
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar la imagen de la receta'
  }
}

// Reabrir receta externa
async function reopenExternal(id: string) {
  error.value = null
  try {
    const res = await api().POST('/prescriptions/{prescriptionId}/reopen', {
      params: { path: { prescriptionId: id } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    notice.value = 'Receta reabierta para edición.'
    await loadPrescriptions()
    if (selectedId.value === id) {
      await viewDetail(id)
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Error al reabrir la receta'
  }
}

// Archivar receta
function promptArchive(rx: PrescriptionSummary) {
  archivingPrescription.value = rx
}

async function confirmArchive() {
  if (!archivingPrescription.value) return
  archivingBusy.value = true
  error.value = null
  try {
    const res = await api().POST('/prescriptions/{prescriptionId}/archive', {
      params: { path: { prescriptionId: archivingPrescription.value.id } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    notice.value = 'Receta archivada de tu lista.'
    archivingPrescription.value = null
    await loadPrescriptions()
    if (selectedId.value) {
      closeDetail()
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Error al archivar la receta'
  } finally {
    archivingBusy.value = false
  }
}

// Cotizar
function goToQuote(rxId: string) {
  router.push({
    path: '/app/quotes/new',
    query: { prescription_id: rxId },
  })
}

// Helpers de visualización de badges
function isExpiringSoon(validUntil?: string | null): boolean {
  if (!validUntil) return false
  const exp = new Date(validUntil).getTime()
  const now = Date.now()
  const diffDays = (exp - now) / (1000 * 60 * 60 * 24)
  return diffDays > 0 && diffDays <= 7
}

function isExpired(validUntil?: string | null, status?: string): boolean {
  if (status === 'expired') return true
  if (!validUntil) return false
  return new Date(validUntil).getTime() < Date.now()
}

function statusBadgeClass(rx: PrescriptionSummary) {
  if (rx.archived_by_guardian) {
    return 'bg-slate-100 text-slate-700 border-slate-300 dark:bg-slate-800 dark:text-slate-300'
  }
  if (rx.status === 'revoked') {
    return 'bg-rose-50 text-rose-700 border-rose-200 dark:bg-rose-950/40 dark:text-rose-400'
  }
  if (rx.status === 'exhausted') {
    return 'bg-slate-100 text-slate-700 border-slate-300 dark:bg-slate-800 dark:text-slate-300'
  }
  if (isExpired(rx.valid_until, rx.status)) {
    return 'bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-950/40 dark:text-orange-400'
  }
  if (isExpiringSoon(rx.valid_until)) {
    return 'bg-amber-50 text-amber-800 border-amber-300 dark:bg-amber-950/40 dark:text-amber-300'
  }
  if (rx.status === 'draft') {
    return 'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-950/40 dark:text-amber-400'
  }
  return 'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-400'
}

function statusLabel(rx: PrescriptionSummary) {
  if (rx.archived_by_guardian) return 'Archivada'
  if (rx.status === 'revoked') return 'Revocada'
  if (rx.status === 'exhausted') return 'Agotada'
  if (isExpired(rx.valid_until, rx.status)) return 'Expirada'
  if (isExpiringSoon(rx.valid_until)) return 'Por vencer'
  if (rx.status === 'draft') return 'Borrador'
  if (rx.status === 'confirmed') return 'Confirmada'
  return 'Vigente'
}

function formatDate(iso?: string | null): string {
  if (!iso) return 'N/A'
  try {
    const d = new Date(iso)
    return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
  } catch {
    return iso
  }
}
</script>

<template>
  <div class="mx-auto max-w-6xl px-4 py-6 space-y-6">
    <!-- Header -->
    <header class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-surface-900 dark:text-surface-50">
          Mis Recetas Médicas
        </h1>
        <p class="mt-1 text-sm text-surface-500 dark:text-surface-400">
          Gestiona las recetas electrónicas emitidas por clínicas y las recetas externas escaneadas
          de tus mascotas.
        </p>
      </div>
      <div>
        <button
          type="button"
          class="inline-flex items-center gap-2 rounded-xl bg-green-700 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-green-800 transition-colors"
          data-testid="btn-upload-external"
          @click="handleUploadExternal"
        >
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 4v16m8-8H4"
            />
          </svg>
          <span>Subir receta externa</span>
        </button>
      </div>
    </header>

    <!-- Feedback Alerts -->
    <div
      v-if="error"
      role="alert"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
    >
      {{ error }}
    </div>
    <div
      v-if="notice"
      role="status"
      class="rounded-xl border border-green-200 bg-green-50 p-4 text-sm text-green-800 dark:border-green-900 dark:bg-green-950 dark:text-green-300 flex items-center justify-between"
    >
      <span>{{ notice }}</span>
      <button
        type="button"
        class="text-green-700 dark:text-green-300 font-bold ml-4"
        @click="notice = null"
      >
        ✕
      </button>
    </div>

    <!-- Filters Bar -->
    <div
      class="flex flex-wrap items-center gap-3 rounded-2xl border border-surface-200 bg-surface-50 p-4 dark:border-surface-800 dark:bg-surface-900/60"
    >
      <!-- Pet Filter -->
      <div class="flex items-center gap-2">
        <label
          for="filter-pet"
          class="text-xs font-semibold text-surface-500 uppercase tracking-wider"
        >
          Mascota:
        </label>
        <select
          id="filter-pet"
          v-model="selectedPetId"
          class="rounded-lg border border-surface-300 bg-white px-3 py-1.5 text-sm text-surface-800 focus:border-green-600 focus:outline-none dark:border-surface-700 dark:bg-surface-800 dark:text-surface-200"
          data-testid="select-pet-filter"
        >
          <option value="">Todas mis mascotas</option>
          <option v-for="p in petStore.items" :key="p.id" :value="p.id">
            {{ p.name }} ({{ p.species }})
          </option>
        </select>
      </div>

      <!-- Status Filter -->
      <div class="flex items-center gap-2">
        <label
          for="filter-status"
          class="text-xs font-semibold text-surface-500 uppercase tracking-wider"
        >
          Estado:
        </label>
        <select
          id="filter-status"
          v-model="filterStatus"
          class="rounded-lg border border-surface-300 bg-white px-3 py-1.5 text-sm text-surface-800 focus:border-green-600 focus:outline-none dark:border-surface-700 dark:bg-surface-800 dark:text-surface-200"
          data-testid="select-status-filter"
        >
          <option value="">Todos los estados</option>
          <option value="issued">Vigentes / Emitidas</option>
          <option value="confirmed">Confirmadas</option>
          <option value="draft">Borradores</option>
          <option value="expired">Expiradas</option>
          <option value="exhausted">Agotadas</option>
          <option value="revoked">Revocadas</option>
          <option value="archived">Archivadas</option>
        </select>
      </div>

      <!-- Source Filter -->
      <div class="flex items-center gap-2">
        <label
          for="filter-source"
          class="text-xs font-semibold text-surface-500 uppercase tracking-wider"
        >
          Origen:
        </label>
        <select
          id="filter-source"
          v-model="filterSource"
          class="rounded-lg border border-surface-300 bg-white px-3 py-1.5 text-sm text-surface-800 focus:border-green-600 focus:outline-none dark:border-surface-700 dark:bg-surface-800 dark:text-surface-200"
          data-testid="select-source-filter"
        >
          <option value="">Todos los orígenes</option>
          <option value="issued">Emitidas por clínica</option>
          <option value="external">Recetas externas (OCR)</option>
        </select>
      </div>

      <div class="ml-auto text-xs text-surface-400">
        Mostrando {{ prescriptions.length }} recetas
      </div>
    </div>

    <!-- Loading State -->
    <div v-if="loading" class="py-12 text-center text-surface-500">
      <div
        class="inline-block h-8 w-8 animate-spin rounded-full border-4 border-solid border-green-600 border-r-transparent"
      />
      <p class="mt-3 text-sm">Cargando recetas...</p>
    </div>

    <!-- Empty State -->
    <div
      v-else-if="prescriptions.length === 0"
      class="rounded-2xl border-2 border-dashed border-surface-200 p-12 text-center dark:border-surface-800"
      data-testid="empty-prescriptions"
    >
      <div
        class="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-surface-100 text-surface-400 dark:bg-surface-800"
      >
        <svg class="h-8 w-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
          />
        </svg>
      </div>
      <h3 class="mt-4 text-lg font-semibold text-surface-800 dark:text-surface-100">
        No hay recetas que coincidan con la búsqueda
      </h3>
      <p class="mt-1 text-sm text-surface-500 max-w-md mx-auto">
        Puedes subir una receta física escaneando las fotos de tu veterinario o esperar a que tu
        clínica emita una receta electrónica.
      </p>
      <div class="mt-6">
        <button
          type="button"
          class="rounded-xl bg-green-700 px-5 py-2.5 text-sm font-semibold text-white hover:bg-green-800 transition-colors"
          @click="handleUploadExternal"
        >
          + Subir receta externa
        </button>
      </div>
    </div>

    <!-- Prescriptions Grid -->
    <div
      v-else
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
      data-testid="prescriptions-grid"
    >
      <div
        v-for="rx in prescriptions"
        :key="rx.id"
        class="flex flex-col justify-between rounded-2xl border border-surface-200 bg-white p-5 shadow-sm transition hover:shadow-md dark:border-surface-800 dark:bg-surface-900"
        :data-testid="`prescription-card-${rx.id}`"
      >
        <div class="space-y-4">
          <!-- Card Top: Pet info & Status Badges -->
          <div class="flex items-start justify-between gap-2">
            <div>
              <span
                class="text-xs font-semibold uppercase tracking-wider text-green-700 dark:text-green-400"
              >
                {{ getPetSpecies(rx.pet_id) || 'Mascota' }}
              </span>
              <h2 class="text-lg font-bold text-surface-900 dark:text-surface-50">
                {{ getPetName(rx.pet_id) }}
              </h2>
            </div>
            <span
              class="inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium"
              :class="statusBadgeClass(rx)"
              data-testid="status-badge"
            >
              {{ statusLabel(rx) }}
            </span>
          </div>

          <!-- Badges de Origen y Verificación -->
          <div class="flex flex-wrap gap-2 text-xs">
            <!-- Source Badge -->
            <span
              v-if="rx.source === 'issued'"
              class="inline-flex items-center gap-1 rounded-md bg-blue-50 px-2 py-1 font-medium text-blue-700 border border-blue-200 dark:bg-blue-950/40 dark:text-blue-300 dark:border-blue-900"
              data-testid="source-badge-issued"
            >
              <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4"
                />
              </svg>
              Emitida por clínica
            </span>
            <span
              v-else
              class="inline-flex items-center gap-1 rounded-md bg-purple-50 px-2 py-1 font-medium text-purple-700 border border-purple-200 dark:bg-purple-950/40 dark:text-purple-300 dark:border-purple-900"
              data-testid="source-badge-external"
            >
              <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                />
              </svg>
              Receta externa
            </span>

            <!-- Verification Badge -->
            <span
              v-if="rx.source === 'issued'"
              class="inline-flex items-center gap-1 rounded-md bg-emerald-50 px-2 py-1 font-medium text-emerald-700 border border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-900"
              data-testid="verification-badge-verified"
            >
              <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M5 13l4 4L19 7"
                />
              </svg>
              Verificada
            </span>
            <span
              v-else-if="rx.source === 'external' && rx.status === 'confirmed'"
              class="inline-flex items-center gap-1 rounded-md bg-amber-50 px-2 py-1 font-medium text-amber-800 border border-amber-200 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-900"
              data-testid="verification-badge-unverified"
            >
              <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                />
              </svg>
              Externa no verificada
            </span>

            <!-- Controlled Badge -->
            <span
              v-if="rx.has_controlled"
              class="inline-flex items-center gap-1 rounded-md bg-rose-50 px-2 py-1 font-medium text-rose-700 border border-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-900"
              data-testid="controlled-badge"
            >
              ⚠️ Controlado
            </span>
          </div>

          <!-- Validity & Refills Info -->
          <div
            class="space-y-1.5 text-xs text-surface-600 dark:text-surface-300 border-t border-surface-100 dark:border-surface-800/80 pt-3"
          >
            <div class="flex justify-between">
              <span class="text-surface-400">Emisión / Registro:</span>
              <span class="font-medium">{{ formatDate(rx.issued_at || rx.created_at) }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-surface-400">Válida hasta:</span>
              <span
                class="font-medium"
                :class="{ 'text-orange-600 font-bold': isExpiringSoon(rx.valid_until) }"
              >
                {{ formatDate(rx.valid_until) }}
              </span>
            </div>
            <div class="flex justify-between">
              <span class="text-surface-400">Dispensaciones / Refills:</span>
              <span class="font-medium">
                {{ rx.refills_used }} de {{ rx.refills_allowed }} utilizados
              </span>
            </div>
            <div class="flex justify-between">
              <span class="text-surface-400">Medicamentos:</span>
              <span class="font-medium font-semibold">{{ rx.items_count }} ítems</span>
            </div>
          </div>
        </div>

        <!-- Card Actions Footer -->
        <div class="mt-5 pt-3 border-t border-surface-100 dark:border-surface-800 space-y-2">
          <div class="flex gap-2">
            <!-- Botón Cotizar -->
            <button
              v-if="rx.status === 'issued' || rx.status === 'confirmed'"
              type="button"
              class="flex-1 rounded-xl bg-green-700 py-2 text-xs font-semibold text-white shadow-sm hover:bg-green-800 transition text-center"
              data-testid="btn-quote"
              @click="goToQuote(rx.id)"
            >
              Cotizar en farmacias
            </button>

            <!-- Botón Continuar revisión si está en borrador -->
            <button
              v-else-if="rx.status === 'draft' && rx.source === 'external'"
              type="button"
              class="flex-1 rounded-xl bg-amber-600 py-2 text-xs font-semibold text-white hover:bg-amber-700 transition text-center"
              data-testid="btn-continue-review"
              @click="
                router.push({
                  name: 'pet-prescription-new',
                  params: { petId: rx.pet_id },
                  query: { draft: rx.id },
                })
              "
            >
              Continuar revisión
            </button>

            <!-- Ver detalle modal -->
            <button
              type="button"
              class="rounded-xl border border-surface-300 px-3 py-2 text-xs font-semibold text-surface-700 hover:bg-surface-50 dark:border-surface-700 dark:text-surface-200 dark:hover:bg-surface-800 transition"
              data-testid="btn-view-detail"
              @click="viewDetail(rx.id)"
            >
              Ver detalle
            </button>
          </div>

          <div class="flex items-center justify-between text-xs pt-1">
            <!-- Descarga de PDF si es emitida -->
            <button
              v-if="rx.source === 'issued'"
              type="button"
              class="text-green-700 hover:underline dark:text-green-400 flex items-center gap-1"
              data-testid="btn-download-pdf"
              @click="downloadPDF(rx.id)"
            >
              <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
                />
              </svg>
              Descargar PDF
            </button>
            <span v-else />

            <!-- Archivar -->
            <button
              v-if="!rx.archived_by_guardian"
              type="button"
              class="text-surface-400 hover:text-surface-700 dark:hover:text-surface-200 hover:underline"
              data-testid="btn-archive"
              @click="promptArchive(rx)"
            >
              Archivar
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Modal Detalle de Receta -->
    <div
      v-if="selectedId"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-sm"
      data-testid="modal-prescription-detail"
    >
      <div
        class="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-3xl bg-white p-6 shadow-2xl dark:bg-surface-900 dark:text-surface-100"
      >
        <div
          class="flex items-start justify-between border-b border-surface-200 pb-4 dark:border-surface-800"
        >
          <div>
            <span
              class="text-xs font-semibold uppercase tracking-wider text-green-700 dark:text-green-400"
            >
              Detalle de Receta Médica
            </span>
            <h2 class="text-xl font-bold text-surface-900 dark:text-surface-50">
              {{ selectedDetail ? getPetName(selectedDetail.pet_id) : 'Cargando...' }}
            </h2>
          </div>
          <button
            type="button"
            class="rounded-lg p-2 text-surface-400 hover:bg-surface-100 hover:text-surface-700 dark:hover:bg-surface-800"
            data-testid="btn-close-modal"
            @click="closeDetail"
          >
            ✕
          </button>
        </div>

        <!-- Loading spinner in modal -->
        <div v-if="loadingDetail" class="py-12 text-center text-surface-500">
          <div
            class="inline-block h-8 w-8 animate-spin rounded-full border-4 border-solid border-green-600 border-r-transparent"
          />
          <p class="mt-3 text-sm">Cargando prescripción...</p>
        </div>

        <div v-else-if="selectedDetail" class="space-y-6 pt-4">
          <!-- Metadata Card -->
          <div
            class="grid grid-cols-2 sm:grid-cols-4 gap-4 rounded-2xl bg-surface-50 p-4 text-xs dark:bg-surface-800/60"
          >
            <div>
              <span class="block text-surface-400">Origen</span>
              <span class="font-semibold">{{
                selectedDetail.source === 'issued' ? 'Clínica' : 'Externa'
              }}</span>
            </div>
            <div>
              <span class="block text-surface-400">Estado</span>
              <span class="font-semibold uppercase">{{ selectedDetail.status }}</span>
            </div>
            <div>
              <span class="block text-surface-400">Válida hasta</span>
              <span class="font-semibold">{{ formatDate(selectedDetail.valid_until) }}</span>
            </div>
            <div>
              <span class="block text-surface-400">Refills Permitidos</span>
              <span class="font-semibold"
                >{{ selectedDetail.refills_used }} / {{ selectedDetail.refills_allowed }}</span
              >
            </div>
          </div>

          <!-- External Meta si aplica -->
          <div
            v-if="
              selectedDetail.external_meta &&
              (selectedDetail.external_meta.vet_name || selectedDetail.external_meta.clinic_name)
            "
            class="rounded-xl border border-surface-200 p-4 text-xs dark:border-surface-800"
          >
            <h3 class="font-bold text-surface-700 dark:text-surface-300 mb-2">
              Datos de la Prescripción Externa
            </h3>
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
              <div v-if="selectedDetail.external_meta.vet_name">
                <span class="text-surface-400">Veterinario:</span>
                {{ selectedDetail.external_meta.vet_name }}
              </div>
              <div v-if="selectedDetail.external_meta.clinic_name">
                <span class="text-surface-400">Clínica:</span>
                {{ selectedDetail.external_meta.clinic_name }}
              </div>
              <div v-if="selectedDetail.external_meta.issued_on">
                <span class="text-surface-400">Fecha en receta:</span>
                {{ selectedDetail.external_meta.issued_on }}
              </div>
            </div>
          </div>

          <!-- Items / Medicamentos -->
          <div>
            <h3 class="text-sm font-bold text-surface-900 dark:text-surface-50 mb-3">
              Medicamentos prescritos ({{ selectedDetail.items.length }})
            </h3>
            <div class="space-y-3">
              <div
                v-for="(it, idx) in selectedDetail.items"
                :key="idx"
                class="rounded-xl border border-surface-200 p-4 dark:border-surface-800 space-y-2"
              >
                <div class="flex items-start justify-between">
                  <div>
                    <h4 class="font-semibold text-surface-900 dark:text-surface-100">
                      {{ it.name }}
                    </h4>
                    <p class="text-xs text-surface-500">
                      Dosis: {{ it.dose.value }} {{ it.dose.unit }} · Vía: {{ it.route }} ·
                      Frecuencia: {{ it.frequency }}
                    </p>
                  </div>
                  <span
                    v-if="it.controlled"
                    class="rounded bg-rose-100 px-2 py-0.5 text-xs font-semibold text-rose-800"
                  >
                    Controlado
                  </span>
                </div>

                <div
                  class="flex flex-wrap gap-4 text-xs text-surface-600 dark:text-surface-300 pt-1 border-t border-surface-100 dark:border-surface-800"
                >
                  <div>
                    <span class="text-surface-400">Cantidad:</span>
                    {{ it.quantity.value }} {{ it.quantity.unit }}
                  </div>
                  <div v-if="it.duration_days">
                    <span class="text-surface-400">Duración:</span>
                    {{ it.duration_days }} días
                  </div>
                  <div>
                    <span class="text-surface-400">Sustitución:</span>
                    {{ it.substitution_allowed ? 'Permitida' : 'No permitida' }}
                  </div>
                </div>

                <p
                  v-if="it.instructions"
                  class="text-xs bg-surface-50 p-2 rounded-lg dark:bg-surface-800/80"
                >
                  <span class="font-semibold text-surface-500">Indicaciones:</span>
                  {{ it.instructions }}
                </p>
              </div>
            </div>
          </div>

          <!-- Imágenes de la Receta Externa si existen -->
          <div v-if="selectedDetail.images && selectedDetail.images.length > 0">
            <h3 class="text-sm font-bold text-surface-900 dark:text-surface-50 mb-3">
              Documentos escaneados ({{ selectedDetail.images.length }})
            </h3>
            <div class="flex flex-wrap gap-3">
              <button
                v-for="img in selectedDetail.images"
                :key="img.page"
                type="button"
                class="group relative flex flex-col items-center justify-center h-28 w-24 overflow-hidden rounded-xl border border-surface-200 bg-surface-50 p-2 text-center shadow-sm transition hover:border-green-600 hover:bg-green-50/30 dark:border-surface-700 dark:bg-surface-800"
                :data-testid="`btn-view-image-page-${img.page}`"
                @click="viewPrescriptionImage(selectedDetail.id, img.page)"
              >
                <svg
                  class="h-8 w-8 text-surface-400 group-hover:text-green-600 transition"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="1.5"
                    d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                  />
                </svg>
                <span class="mt-2 text-xs font-semibold text-surface-700 dark:text-surface-200">
                  Página {{ img.page }}
                </span>
                <span class="text-[10px] text-surface-400">Ver imagen</span>
              </button>
            </div>
          </div>

          <!-- Modal Actions Footer -->
          <div
            class="flex flex-wrap justify-between gap-3 border-t border-surface-200 pt-4 dark:border-surface-800"
          >
            <div class="flex gap-2">
              <button
                v-if="selectedDetail.source === 'external' && selectedDetail.status === 'confirmed'"
                type="button"
                class="rounded-xl border border-surface-300 px-4 py-2 text-xs font-semibold text-surface-700 hover:bg-surface-50 dark:border-surface-700 dark:text-surface-200"
                data-testid="btn-reopen"
                @click="reopenExternal(selectedDetail.id)"
              >
                Reabrir para corrección
              </button>
              <RouterLink
                :to="`/verify/prescriptions/${selectedDetail.id}`"
                target="_blank"
                class="rounded-xl border border-surface-300 px-4 py-2 text-xs font-semibold text-surface-700 hover:bg-surface-50 dark:border-surface-700 dark:text-surface-200"
              >
                Verificar públicamente
              </RouterLink>
            </div>

            <div class="flex gap-2">
              <button
                v-if="selectedDetail.status === 'issued' || selectedDetail.status === 'confirmed'"
                type="button"
                class="rounded-xl bg-green-700 px-5 py-2 text-xs font-semibold text-white shadow-sm hover:bg-green-800"
                @click="goToQuote(selectedDetail.id)"
              >
                Solicitar cotización
              </button>
              <button
                type="button"
                class="rounded-xl bg-surface-200 px-4 py-2 text-xs font-semibold text-surface-800 hover:bg-surface-300 dark:bg-surface-800 dark:text-surface-200"
                @click="closeDetail"
              >
                Cerrar
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Modal Selector de Mascota para Subir Receta -->
    <div
      v-if="showPetPickerModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-sm"
      data-testid="modal-pet-picker"
    >
      <div
        class="w-full max-w-md rounded-3xl bg-white p-6 shadow-2xl dark:bg-surface-900 dark:text-surface-100"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-50">
          ¿Para cuál de tus mascotas es la receta?
        </h3>
        <p class="mt-1 text-sm text-surface-500">
          Selecciona la mascota a la que pertenece la prescripción médica que deseas subir.
        </p>

        <div class="mt-4 space-y-2">
          <button
            v-for="p in petStore.items"
            :key="p.id"
            type="button"
            class="flex w-full items-center justify-between rounded-xl border border-surface-200 p-3 text-left transition hover:border-green-600 hover:bg-green-50/50 dark:border-surface-800 dark:hover:bg-surface-800"
            :data-testid="`btn-pick-pet-${p.id}`"
            @click="selectPetForUpload(p.id)"
          >
            <div>
              <span class="block font-semibold text-surface-900 dark:text-surface-50">{{
                p.name
              }}</span>
              <span class="block text-xs text-surface-400 capitalize"
                >{{ p.species }} · {{ p.breed || 'Sin raza' }}</span
              >
            </div>
            <span class="text-green-700 dark:text-green-400 font-bold">→</span>
          </button>
        </div>

        <div class="mt-6 flex justify-end">
          <button
            type="button"
            class="rounded-xl px-4 py-2 text-sm text-surface-500 hover:text-surface-700"
            @click="showPetPickerModal = false"
          >
            Cancelar
          </button>
        </div>
      </div>
    </div>

    <!-- Modal Confirmar Archivado -->
    <div
      v-if="archivingPrescription"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-sm"
      data-testid="modal-archive-confirm"
    >
      <div
        class="w-full max-w-sm rounded-3xl bg-white p-6 shadow-2xl dark:bg-surface-900 dark:text-surface-100"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-50">
          ¿Archivar receta médica?
        </h3>
        <p class="mt-2 text-sm text-surface-500">
          Esta receta de {{ getPetName(archivingPrescription.pet_id) }} se ocultará de tu bandeja
          activa, pero no alterará su validez clínica ni los pedidos existentes.
        </p>

        <div class="mt-6 flex justify-end gap-3">
          <button
            type="button"
            class="rounded-xl px-4 py-2 text-sm text-surface-500 hover:text-surface-700"
            :disabled="archivingBusy"
            @click="archivingPrescription = null"
          >
            Cancelar
          </button>
          <button
            type="button"
            class="rounded-xl bg-rose-600 px-4 py-2 text-sm font-semibold text-white hover:bg-rose-700"
            :disabled="archivingBusy"
            data-testid="btn-confirm-archive"
            @click="confirmArchive"
          >
            {{ archivingBusy ? 'Archivando...' : 'Archivar' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
