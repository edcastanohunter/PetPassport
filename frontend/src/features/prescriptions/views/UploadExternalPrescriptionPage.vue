<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  ApiError,
  type ExtractionResponse,
  type PrescriptionDetail,
  type PrescriptionItemDTO,
} from '@/shared/api/client'
import { usePetStore } from '@/features/pets/petStore'

const route = useRoute()
const router = useRouter()
const petStore = usePetStore()

const petId = route.params.petId as string

// Fases: 'upload' | 'analyzing' | 'review' | 'manual'
type Phase = 'upload' | 'analyzing' | 'review' | 'manual'
const currentPhase = ref<Phase>('upload')

// Estado de Mascota
const petName = computed(() => {
  if (petStore.current && petStore.current.id === petId) {
    return petStore.current.name
  }
  const found = petStore.items.find((p) => p.id === petId)
  return found?.name ?? 'tu mascota'
})

// Subida de Archivos
const files = ref<File[]>([])
const filePreviews = ref<string[]>([])
const country = ref('CO')
const fileError = ref<string | null>(null)

// Datos de la Receta y Extracción
const prescriptionId = ref<string | null>(null)
const prescriptionDetail = ref<PrescriptionDetail | null>(null)
const extraction = ref<ExtractionResponse | null>(null)
const pollTimer = ref<number | null>(null)
const pollAttempts = ref(0)
const analyzingStep = ref(1) // 1: Subiendo, 2: Extrayendo, 3: Homologando
const analysisWarning = ref<string | null>(null)
const isBudgetExceeded = ref(false)

// Visor de Imagen
const currentImagePage = ref(1)
const currentImageUrl = ref<string | null>(null)
const zoomLevel = ref(1)
const activeItemIndex = ref<number | null>(null)

// Cabecera editable
const vetName = ref('')
const clinicName = ref('')
const issuedOn = ref('')
const validUntil = ref('')
const refillsAllowed = ref(0)
const notes = ref('')

// Ítems editables
interface EditableItem {
  id: string
  name: string
  catalog_id?: string
  free_text?: string
  presentation?: string
  dose_value: number
  dose_unit: string
  route: string
  frequency: string
  duration_days?: number
  quantity_value: number
  quantity_unit: string
  substitution_allowed: boolean
  instructions: string
  controlled: boolean
  confidence?: number
  raw_text?: string
  candidates: { catalog_id: string; name: string; score: number }[]
  bbox?: number[]
}

const items = ref<EditableItem[]>([])
const attestationChecked = ref(false)
const submitting = ref(false)
const globalError = ref<string | null>(null)

onMounted(async () => {
  try {
    await petStore.loadPet(petId)
  } catch {
    // ignorar error de carga de mascota si ya está en store
  }
})

onUnmounted(() => {
  stopPolling()
  for (const url of filePreviews.value) {
    URL.revokeObjectURL(url)
  }
})

// Manejo de Archivos
function onFilesSelected(event: Event) {
  fileError.value = null
  const input = event.target as HTMLInputElement
  if (!input.files || input.files.length === 0) return

  const selected = Array.from(input.files)
  if (files.value.length + selected.length > 5) {
    fileError.value = 'No puedes subir más de 5 páginas en total.'
    return
  }

  for (const file of selected) {
    if (file.size > 10 * 1024 * 1024) {
      fileError.value = `El archivo ${file.name} supera el límite de 10 MB.`
      return
    }
    const validTypes = ['image/jpeg', 'image/png', 'image/webp', 'image/heic', 'application/pdf']
    if (!validTypes.includes(file.type) && !file.name.toLowerCase().endsWith('.heic')) {
      fileError.value = `Formato no compatible en ${file.name}. Usa JPEG, PNG o PDF.`
      return
    }

    files.value.push(file)
    if (file.type.startsWith('image/')) {
      filePreviews.value.push(URL.createObjectURL(file))
    } else {
      filePreviews.value.push('') // PDF sin preview directa
    }
  }
}

function removeFile(index: number) {
  if (filePreviews.value[index]) {
    URL.revokeObjectURL(filePreviews.value[index])
  }
  files.value.splice(index, 1)
  filePreviews.value.splice(index, 1)
}

// Iniciar Análisis OCR
async function startAnalysis() {
  if (files.value.length === 0) {
    fileError.value = 'Selecciona al menos una foto o archivo de tu receta.'
    return
  }

  currentPhase.value = 'analyzing'
  analyzingStep.value = 1
  analysisWarning.value = null
  isBudgetExceeded.value = false
  globalError.value = null

  try {
    const formData = new FormData()
    for (const f of files.value) {
      formData.append('files', f)
    }
    formData.append('country', country.value)

    const res = await api().POST('/pets/{petId}/prescriptions/external', {
      params: { path: { petId } },
      body: formData as unknown as { files: string[]; country?: string },
    })

    if (res.error) {
      if (res.response.status === 503) {
        isBudgetExceeded.value = true
        throw new Error('AI_BUDGET_EXCEEDED')
      }
      throw new ApiError(res.error, res.response.status)
    }

    if (!res.data?.id) {
      throw new Error('Respuesta inválida del servidor')
    }

    prescriptionId.value = res.data.id
    prescriptionDetail.value = res.data

    analyzingStep.value = 2
    startPollingExtraction(res.data.id)
  } catch (err: unknown) {
    if (err instanceof Error && err.message === 'AI_BUDGET_EXCEEDED') {
      isBudgetExceeded.value = true
    } else {
      globalError.value = err instanceof Error ? err.message : 'Error al subir receta externa'
      currentPhase.value = 'upload'
    }
  }
}

function startPollingExtraction(id: string) {
  stopPolling()
  pollAttempts.value = 0

  pollTimer.value = window.setInterval(async () => {
    pollAttempts.value++
    if (pollAttempts.value > 25) {
      // Timeout tras ~35 s
      stopPolling()
      analysisWarning.value =
        'El análisis está tardando más de lo habitual. Puedes revisar los datos manualmente.'
      switchToManualCapture()
      return
    }

    try {
      const res = await api().GET('/prescriptions/{prescriptionId}/extraction', {
        params: { path: { prescriptionId: id } },
      })

      if (res.data) {
        extraction.value = res.data
        const st = res.data.status

        if (st === 'done') {
          stopPolling()
          analyzingStep.value = 3
          setTimeout(() => {
            populateReviewFromExtraction(res.data)
            currentPhase.value = 'review'
            void loadViewerImage(id, 1)
          }, 600)
        } else if (st === 'reupload_requested') {
          stopPolling()
          analysisWarning.value =
            res.data.reason ??
            'La imagen no tiene suficiente nitidez para extraer los datos con precisión.'
        } else if (st === 'failed') {
          stopPolling()
          globalError.value = res.data.reason ?? 'No se pudo procesar la receta.'
        }
      }
    } catch {
      // Continuar intentando
    }
  }, 1500)
}

function stopPolling() {
  if (pollTimer.value !== null) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

async function retryAnalysis() {
  if (!prescriptionId.value) return
  analysisWarning.value = null
  globalError.value = null
  currentPhase.value = 'analyzing'
  analyzingStep.value = 2

  try {
    await api().POST('/prescriptions/{prescriptionId}/extraction/retry', {
      params: { path: { prescriptionId: prescriptionId.value } },
    })
    startPollingExtraction(prescriptionId.value)
  } catch (e: unknown) {
    globalError.value = e instanceof Error ? e.message : 'No se pudo reintentar la extracción'
    switchToManualCapture()
  }
}

// Carga de Visor
async function loadViewerImage(rxId: string, page: number) {
  currentImagePage.value = page
  try {
    const res = await api().GET('/prescriptions/{prescriptionId}/images/{page}', {
      params: { path: { prescriptionId: rxId, page } },
    })
    if (res.data?.url) {
      currentImageUrl.value = res.data.url
    } else if (filePreviews.value[page - 1]) {
      currentImageUrl.value = filePreviews.value[page - 1]
    }
  } catch {
    if (filePreviews.value[page - 1]) {
      currentImageUrl.value = filePreviews.value[page - 1]
    }
  }
}

// Poblar formulario tras análisis
function populateReviewFromExtraction(ext: ExtractionResponse) {
  const data = ext.extraction
  if (!data) return

  if (data.header) {
    vetName.value = data.header.vet_name ?? ''
    clinicName.value = data.header.clinic_name ?? ''
    issuedOn.value = data.header.issued_on ?? new Date().toISOString().split('T')[0]
  }

  // Vigencia por defecto: 30 días
  const defValid = new Date()
  defValid.setDate(defValid.getDate() + 30)
  validUntil.value = defValid.toISOString().split('T')[0]
  refillsAllowed.value = 0

  items.value = (data.items ?? []).map((it, idx) => {
    const candList = it.product?.candidates ?? []
    const firstCand = candList[0]

    // Convertir dosis si viene en formato texto u objeto
    let dVal = 1
    let dUnit = 'mg'
    if (it.dose) {
      if (typeof it.dose === 'string') {
        const match = it.dose.match(/(\d+(?:[.,]\d+)?)\s*([a-zA-Z%]+)?/)
        if (match) {
          dVal = parseFloat(match[1].replace(',', '.'))
          dUnit = match[2] || 'mg'
        }
      } else if (typeof it.dose === 'object' && it.dose !== null && 'value' in it.dose) {
        const dObj = it.dose as Record<string, unknown>
        dVal = Number(dObj.value) || 1
        dUnit = typeof dObj.unit === 'string' ? dObj.unit : 'mg'
      }
    }

    let qVal = 1
    let qUnit = 'pack'
    if (it.quantity) {
      if (typeof it.quantity === 'string') {
        const match = it.quantity.match(/(\d+)\s*([a-zA-Z]+)?/)
        if (match) {
          qVal = parseInt(match[1], 10)
          qUnit = match[2] || 'pack'
        }
      } else if (
        typeof it.quantity === 'object' &&
        it.quantity !== null &&
        'value' in it.quantity
      ) {
        const qObj = it.quantity as Record<string, unknown>
        qVal = Number(qObj.value) || 1
        qUnit = typeof qObj.unit === 'string' ? qObj.unit : 'pack'
      }
    }

    return {
      id: `item-${idx + 1}`,
      name: firstCand?.name ?? it.product?.text ?? it.raw_text ?? 'Medicamento',
      catalog_id: firstCand?.catalog_id,
      free_text: firstCand ? undefined : (it.product?.text ?? it.raw_text),
      dose_value: dVal,
      dose_unit: dUnit,
      route: 'oral',
      frequency: it.frequency || 'cada 24 horas',
      duration_days: it.duration ? parseInt(it.duration, 10) || 7 : 7,
      quantity_value: qVal,
      quantity_unit: qUnit,
      substitution_allowed: true,
      instructions: '',
      controlled: false,
      confidence: it.confidence?.overall ?? 0.85,
      raw_text: it.raw_text,
      candidates: candList.map((c) => ({
        catalog_id: c.catalog_id,
        name: c.name,
        score: c.score,
      })),
      bbox: it.bbox,
    }
  })

  if (items.value.length === 0) {
    addItem()
  }
}

function switchToManualCapture() {
  currentPhase.value = 'manual'
  if (items.value.length === 0) {
    addItem()
  }
  if (!issuedOn.value) {
    issuedOn.value = new Date().toISOString().split('T')[0]
  }
  if (!validUntil.value) {
    const d = new Date()
    d.setDate(d.getDate() + 30)
    validUntil.value = d.toISOString().split('T')[0]
  }
  if (prescriptionId.value) {
    void loadViewerImage(prescriptionId.value, 1)
  }
}

// Manipulación de Ítems
function addItem() {
  items.value.push({
    id: `item-${Date.now()}`,
    name: '',
    dose_value: 1,
    dose_unit: 'mg',
    route: 'oral',
    frequency: 'cada 24 horas',
    duration_days: 7,
    quantity_value: 1,
    quantity_unit: 'pack',
    substitution_allowed: true,
    instructions: '',
    controlled: false,
    confidence: 1.0,
    candidates: [],
  })
}

function removeItem(idx: number) {
  items.value.splice(idx, 1)
}

function selectCandidate(item: EditableItem, cand: { catalog_id: string; name: string }) {
  item.catalog_id = cand.catalog_id
  item.name = cand.name
  item.free_text = undefined
}

function setFreeText(item: EditableItem) {
  item.catalog_id = undefined
  item.free_text = item.name
}

// Bounding box style
const bboxStyle = computed(() => {
  if (activeItemIndex.value === null) return null
  const it = items.value[activeItemIndex.value]
  if (!it?.bbox || it.bbox.length < 4) return null
  const [x, y, w, h] = it.bbox
  return {
    left: `${x * 100}%`,
    top: `${y * 100}%`,
    width: `${w * 100}%`,
    height: `${h * 100}%`,
  }
})

function confidenceBadgeClass(score?: number) {
  if (score === undefined) return 'bg-slate-100 text-slate-700'
  if (score >= 0.85) return 'bg-emerald-100 text-emerald-800 border-emerald-300'
  if (score >= 0.6) return 'bg-amber-100 text-amber-800 border-amber-300'
  return 'bg-rose-100 text-rose-800 border-rose-300'
}

function confidenceLabel(score?: number) {
  if (score === undefined) return 'Manual'
  if (score >= 0.85) return 'Alta confianza'
  if (score >= 0.6) return 'Confianza media'
  return 'Revisar con cuidado'
}

// Confirmar Receta
async function confirmPrescription() {
  if (!prescriptionId.value) return
  if (!attestationChecked.value) {
    globalError.value = 'Debes confirmar que los medicamentos coinciden con tu receta.'
    return
  }
  if (items.value.length === 0) {
    globalError.value = 'La receta debe contener al menos un medicamento.'
    return
  }

  submitting.value = true
  globalError.value = null

  try {
    const payloadItems: PrescriptionItemDTO[] = items.value.map((it, idx) => ({
      name: it.name.trim(),
      catalog_id: it.catalog_id,
      free_text: it.free_text,
      dose: { value: it.dose_value, unit: it.dose_unit },
      route: it.route,
      frequency: it.frequency,
      duration_days: it.duration_days,
      quantity: { value: it.quantity_value, unit: it.quantity_unit },
      substitution_allowed: it.substitution_allowed,
      instructions: it.instructions,
      controlled: it.controlled,
      extraction_ref: idx + 1,
    }))

    const res = await api().POST('/prescriptions/{prescriptionId}/confirm', {
      params: { path: { prescriptionId: prescriptionId.value } },
      body: {
        items: payloadItems,
        validity: {
          valid_until: validUntil.value || undefined,
          refills_allowed: refillsAllowed.value,
        },
        external_meta: {
          vet_name: vetName.value || undefined,
          clinic_name: clinicName.value || undefined,
          issued_on: issuedOn.value || undefined,
        },
        notes: notes.value || undefined,
        attestation: true,
      },
    })

    if (res.error) throw new ApiError(res.error, res.response.status)

    router.push({
      path: '/app/prescriptions',
      query: { confirmed: prescriptionId.value },
    })
  } catch (e: unknown) {
    globalError.value = e instanceof Error ? e.message : 'Error al confirmar receta'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-7xl px-4 py-8">
    <!-- Header -->
    <div class="mb-6 flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Subir Receta Externa para {{ petName }}
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Fotografía la receta emitida por tu clínica u hospital externo para digitalizarla con
          inteligencia artificial.
        </p>
      </div>
      <button
        type="button"
        class="text-sm font-medium text-slate-600 hover:text-slate-900"
        @click="router.push('/app/prescriptions')"
      >
        ← Volver a Mis Recetas
      </button>
    </div>

    <!-- Feedback Banner -->
    <div
      v-if="globalError"
      class="mb-6 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ globalError }}
    </div>

    <!-- FASE 1: SUBIDA DE ARCHIVOS -->
    <div v-if="currentPhase === 'upload'" class="space-y-6">
      <!-- Consejos de captura -->
      <div
        class="rounded-xl border border-emerald-200 bg-emerald-50/50 p-4 text-sm text-emerald-900"
      >
        <h3 class="font-semibold text-emerald-950">Consejos para una lectura óptima:</h3>
        <ul class="mt-2 list-inside list-disc space-y-1 text-xs sm:text-sm text-emerald-800">
          <li>Coloca la receta en una superficie plana y bien iluminada.</li>
          <li>Evita sombras, reflejos o dedos tapando los medicamentos.</li>
          <li>Asegúrate de que todo el documento quede encuadrado y enfocado.</li>
          <li>Puedes subir hasta 5 páginas en formatos JPEG, PNG o PDF.</li>
        </ul>
      </div>

      <!-- Dropzone -->
      <div
        class="relative flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-slate-300 bg-white p-8 text-center hover:border-emerald-500"
      >
        <svg class="h-12 w-12 text-slate-400" stroke="currentColor" fill="none" viewBox="0 0 24 24">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M3 9a2 2 0 012-2h.93a2 2 0 001.664-.89l.812-1.22A2 2 0 0110.07 4h3.86a2 2 0 011.664.89l.812 1.22A2 2 0 0018.07 7H19a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2V9z"
          />
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M15 13a3 3 0 11-6 0 3 3 0 016 0z"
          />
        </svg>

        <div class="mt-4 flex flex-col items-center gap-2 text-sm text-slate-600 sm:flex-row">
          <label
            class="cursor-pointer rounded-lg bg-emerald-600 px-4 py-2 font-medium text-white shadow hover:bg-emerald-700"
          >
            <span>Tomar foto o seleccionar archivo</span>
            <input
              type="file"
              data-testid="dropzone-input"
              accept="image/jpeg,image/png,image/webp,image/heic,application/pdf"
              capture="environment"
              multiple
              class="sr-only"
              @change="onFilesSelected"
            />
          </label>
          <span class="text-xs text-slate-400">o arrastra tus archivos aquí</span>
        </div>
        <p class="mt-2 text-xs text-slate-400">
          JPEG, PNG, HEIC, PDF hasta 10 MB por página (máx. 5 páginas)
        </p>

        <p v-if="fileError" class="mt-3 text-sm font-medium text-rose-600">{{ fileError }}</p>
      </div>

      <!-- Previsualización de páginas seleccionadas -->
      <div v-if="files.length > 0" class="space-y-4">
        <h3 class="text-sm font-semibold text-slate-800">
          Páginas preparadas ({{ files.length }} de 5):
        </h3>
        <div class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-5">
          <div
            v-for="(_, idx) in files"
            :key="idx"
            class="group relative overflow-hidden rounded-xl border border-slate-200 bg-slate-50 p-2 shadow-sm"
          >
            <div
              class="flex h-32 items-center justify-center overflow-hidden rounded-lg bg-slate-100"
            >
              <img
                v-if="filePreviews[idx]"
                :src="filePreviews[idx]"
                alt="Preview"
                class="h-full w-full object-cover"
              />
              <span v-else class="text-xs font-medium text-slate-500">PDF / Documento</span>
            </div>
            <div class="mt-2 flex items-center justify-between text-xs">
              <span class="font-medium text-slate-700">Pág. {{ idx + 1 }}</span>
              <button
                type="button"
                class="text-rose-600 hover:text-rose-800"
                @click="removeFile(idx)"
              >
                Eliminar
              </button>
            </div>
          </div>
        </div>

        <div class="flex justify-end gap-3 pt-4">
          <button
            type="button"
            class="rounded-lg bg-slate-100 px-4 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-200"
            @click="switchToManualCapture"
          >
            Captura manual sin IA
          </button>
          <button
            type="button"
            data-testid="btn-start-analysis"
            class="rounded-lg bg-emerald-600 px-6 py-2.5 text-sm font-semibold text-white shadow hover:bg-emerald-700"
            @click="startAnalysis"
          >
            Analizar con IA (OCR)
          </button>
        </div>
      </div>
    </div>

    <!-- FASE 2: ANALIZANDO (STEPPER) -->
    <div
      v-else-if="currentPhase === 'analyzing'"
      class="mx-auto max-w-xl rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm"
    >
      <!-- Stepper Visual -->
      <div class="mb-8 flex items-center justify-between">
        <div class="flex flex-col items-center">
          <div
            class="flex h-10 w-10 items-center justify-center rounded-full text-xs font-bold"
            :class="
              analyzingStep >= 1 ? 'bg-emerald-600 text-white' : 'bg-slate-100 text-slate-400'
            "
          >
            ✓
          </div>
          <span class="mt-2 text-xs font-medium text-slate-700">Subida</span>
        </div>
        <div
          class="h-1 flex-1 bg-slate-200 mx-2"
          :class="{ '!bg-emerald-600': analyzingStep >= 2 }"
        ></div>
        <div class="flex flex-col items-center">
          <div
            class="flex h-10 w-10 items-center justify-center rounded-full text-xs font-bold"
            :class="
              analyzingStep >= 2
                ? 'bg-emerald-600 text-white animate-pulse'
                : 'bg-slate-100 text-slate-400'
            "
          >
            2
          </div>
          <span class="mt-2 text-xs font-medium text-slate-700">Visión OCR</span>
        </div>
        <div
          class="h-1 flex-1 bg-slate-200 mx-2"
          :class="{ '!bg-emerald-600': analyzingStep >= 3 }"
        ></div>
        <div class="flex flex-col items-center">
          <div
            class="flex h-10 w-10 items-center justify-center rounded-full text-xs font-bold"
            :class="
              analyzingStep >= 3 ? 'bg-emerald-600 text-white' : 'bg-slate-100 text-slate-400'
            "
          >
            3
          </div>
          <span class="mt-2 text-xs font-medium text-slate-700">Catálogo</span>
        </div>
      </div>

      <!-- Estado actual -->
      <div v-if="!analysisWarning && !isBudgetExceeded" class="space-y-3">
        <div
          class="inline-block h-10 w-10 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent"
        ></div>
        <h3 class="text-lg font-semibold text-slate-900">Analizando tu receta médica...</h3>
        <p class="text-sm text-slate-500">
          Extrayendo medicamentos, posologías y datos del veterinario (tiempo estimado: ≤ 25 s).
        </p>
      </div>

      <!-- Warning: Reupload Requested -->
      <div
        v-if="analysisWarning"
        class="mt-6 rounded-xl border border-amber-200 bg-amber-50 p-5 text-left"
      >
        <h4 class="font-semibold text-amber-950">Se requiere nueva toma o revisión:</h4>
        <p class="mt-1 text-sm text-amber-800">{{ analysisWarning }}</p>
        <div class="mt-4 flex flex-wrap gap-3">
          <button
            type="button"
            data-testid="btn-reupload"
            class="rounded-lg bg-amber-700 px-4 py-2 text-xs font-semibold text-white hover:bg-amber-800"
            @click="currentPhase = 'upload'"
          >
            Tomar nueva foto
          </button>
          <button
            type="button"
            class="rounded-lg border border-amber-300 bg-white px-4 py-2 text-xs font-medium text-amber-800 hover:bg-amber-100"
            @click="retryAnalysis"
          >
            Reintentar análisis
          </button>
          <button
            type="button"
            class="rounded-lg bg-slate-100 px-4 py-2 text-xs font-medium text-slate-700 hover:bg-slate-200"
            @click="switchToManualCapture"
          >
            Revisar manualmente
          </button>
        </div>
      </div>

      <!-- Presupuesto Excedido (503 Fallback) -->
      <div
        v-if="isBudgetExceeded"
        class="mt-6 rounded-xl border border-rose-200 bg-rose-50 p-5 text-left"
      >
        <h4 class="font-semibold text-rose-950">Servicio de IA saturado</h4>
        <p class="mt-1 text-sm text-rose-800">
          El análisis automático ha alcanzado su límite mensual de consumo. Puedes ingresar los
          datos de tu receta directamente en el formulario.
        </p>
        <button
          type="button"
          data-testid="btn-continue-manual"
          class="mt-4 rounded-lg bg-rose-700 px-4 py-2 text-xs font-semibold text-white hover:bg-rose-800"
          @click="switchToManualCapture"
        >
          Continuar con captura manual
        </button>
      </div>
    </div>

    <!-- FASE 3: REVISIÓN LADO A LADO / MANUAL -->
    <div
      v-else-if="currentPhase === 'review' || currentPhase === 'manual'"
      class="grid grid-cols-1 gap-8 lg:grid-cols-12"
    >
      <!-- COLUMNA IZQUIERDA: VISOR DE IMAGEN (5 cols) -->
      <div class="lg:col-span-5 space-y-4">
        <div class="sticky top-6 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
          <div class="mb-3 flex items-center justify-between">
            <span class="text-xs font-semibold uppercase tracking-wider text-slate-500">
              Receta Original (Pág. {{ currentImagePage }})
            </span>
            <div class="flex items-center gap-1">
              <button
                type="button"
                class="rounded border border-slate-300 px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100"
                @click="zoomLevel = Math.max(0.75, zoomLevel - 0.25)"
              >
                -
              </button>
              <span class="px-1 text-xs text-slate-600">{{ Math.round(zoomLevel * 100) }}%</span>
              <button
                type="button"
                class="rounded border border-slate-300 px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100"
                @click="zoomLevel = Math.min(2.5, zoomLevel + 0.25)"
              >
                +
              </button>
              <button
                type="button"
                class="ml-1 rounded border border-slate-300 px-2 py-1 text-xs text-slate-500 hover:bg-slate-100"
                @click="zoomLevel = 1"
              >
                Reset
              </button>
            </div>
          </div>

          <!-- Selector de páginas si hay más de 1 -->
          <div v-if="files.length > 1" class="mb-3 flex gap-2">
            <button
              v-for="p in files.length"
              :key="p"
              type="button"
              class="rounded-lg px-3 py-1 text-xs font-medium"
              :class="
                currentImagePage === p
                  ? 'bg-emerald-600 text-white'
                  : 'bg-slate-100 text-slate-700 hover:bg-slate-200'
              "
              @click="prescriptionId ? loadViewerImage(prescriptionId, p) : (currentImagePage = p)"
            >
              Página {{ p }}
            </button>
          </div>

          <!-- Contenedor con Scroll/Zoom e indicador BBox -->
          <div
            class="relative h-[550px] w-full overflow-auto rounded-xl border border-slate-200 bg-slate-900/5 p-2 flex items-center justify-center"
          >
            <div
              class="relative transition-transform duration-150 origin-top"
              :style="{ transform: `scale(${zoomLevel})` }"
            >
              <img
                v-if="currentImageUrl"
                :src="currentImageUrl"
                alt="Receta escaneada"
                class="max-w-full rounded shadow"
              />
              <div v-else class="flex h-64 w-64 items-center justify-center text-xs text-slate-400">
                Imagen no disponible
              </div>

              <!-- Resaltado BBox del ítem activo -->
              <div
                v-if="bboxStyle"
                class="absolute border-2 border-emerald-500 bg-emerald-500/20 pointer-events-none rounded transition-all"
                :style="bboxStyle"
              ></div>
            </div>
          </div>
        </div>
      </div>

      <!-- COLUMNA DERECHA: FORMULARIO DE CORRECCIÓN HUMANA (7 cols) -->
      <div class="lg:col-span-7 space-y-6">
        <!-- Cabecera de la Receta -->
        <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm space-y-4">
          <h3 class="text-sm font-bold uppercase tracking-wider text-slate-700">
            1. Datos del Profesional y Emisión
          </h3>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label class="block text-xs font-semibold text-slate-700"
                >Veterinario / Profesional</label
              >
              <input
                v-model="vetName"
                type="text"
                placeholder="Ej. Dra. Camila Martínez"
                class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
              />
            </div>
            <div>
              <label class="block text-xs font-semibold text-slate-700"
                >Clínica / Centro Veterinario</label
              >
              <input
                v-model="clinicName"
                type="text"
                placeholder="Ej. Clínica Veterinaria San Martín"
                class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
              />
            </div>
            <div>
              <label class="block text-xs font-semibold text-slate-700">Fecha de Emisión</label>
              <input
                v-model="issuedOn"
                type="date"
                class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
              />
            </div>
            <div>
              <label class="block text-xs font-semibold text-slate-700">Válida Hasta</label>
              <input
                v-model="validUntil"
                type="date"
                class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
              />
            </div>
          </div>
        </div>

        <!-- Lista de Medicamentos -->
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-bold uppercase tracking-wider text-slate-700">
              2. Medicamentos Detectados ({{ items.length }})
            </h3>
            <button
              type="button"
              class="text-xs font-semibold text-emerald-600 hover:text-emerald-700"
              @click="addItem"
            >
              + Añadir medicamento
            </button>
          </div>

          <div
            v-for="(it, idx) in items"
            :key="it.id"
            class="rounded-2xl border bg-white p-5 shadow-sm space-y-4 transition-all"
            :class="
              activeItemIndex === idx
                ? 'border-emerald-500 ring-2 ring-emerald-500/20'
                : 'border-slate-200'
            "
            @focusin="activeItemIndex = idx"
          >
            <!-- Barra superior del ítem -->
            <div
              class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 pb-3"
            >
              <div class="flex items-center gap-2">
                <span class="text-xs font-bold text-slate-500">#{{ idx + 1 }}</span>
                <span
                  class="rounded-full border px-2 py-0.5 text-xs font-semibold"
                  :class="confidenceBadgeClass(it.confidence)"
                >
                  {{ confidenceLabel(it.confidence) }}
                </span>
                <span v-if="it.raw_text" class="text-xs text-slate-400 italic truncate max-w-xs">
                  Texto leído: "{{ it.raw_text }}"
                </span>
              </div>
              <button
                type="button"
                class="text-xs text-rose-500 hover:text-rose-700"
                @click="removeItem(idx)"
              >
                Eliminar ítem
              </button>
            </div>

            <!-- Selección de Producto / Catálogo -->
            <div>
              <label class="block text-xs font-semibold text-slate-700"
                >Nombre del Medicamento</label
              >
              <input
                v-model="it.name"
                :data-testid="'item-name-' + idx"
                type="text"
                placeholder="Nombre comercial o genérico"
                class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium focus:border-emerald-500 focus:outline-none"
              />

              <!-- Candidatos propuestos por el Catálogo -->
              <div v-if="it.candidates && it.candidates.length > 0" class="mt-2 space-y-1">
                <span class="text-xs text-slate-500">Coincidencias en catálogo:</span>
                <div class="flex flex-wrap gap-2">
                  <button
                    v-for="cand in it.candidates"
                    :key="cand.catalog_id"
                    type="button"
                    class="rounded-lg border px-2.5 py-1 text-xs font-medium transition-colors"
                    :class="
                      it.catalog_id === cand.catalog_id
                        ? 'border-emerald-600 bg-emerald-50 text-emerald-800'
                        : 'border-slate-200 bg-slate-50 text-slate-700 hover:bg-slate-100'
                    "
                    @click="selectCandidate(it, cand)"
                  >
                    {{ cand.name }} · {{ Math.round(cand.score * 100) }}%
                  </button>
                  <button
                    type="button"
                    class="rounded-lg border border-dashed border-slate-300 px-2.5 py-1 text-xs text-slate-500 hover:bg-slate-50"
                    @click="setFreeText(it)"
                  >
                    Usar como texto libre
                  </button>
                </div>
              </div>
            </div>

            <!-- Posología: Dosis, Vía, Frecuencia, Duración -->
            <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div>
                <label class="block text-xs font-semibold text-slate-700">Dosis</label>
                <div class="mt-1 flex gap-1">
                  <input
                    v-model.number="it.dose_value"
                    type="number"
                    step="0.1"
                    min="0.1"
                    class="w-16 rounded-lg border border-slate-300 px-2 py-1.5 text-sm"
                  />
                  <input
                    v-model="it.dose_unit"
                    type="text"
                    placeholder="mg"
                    class="w-16 rounded-lg border border-slate-300 px-2 py-1.5 text-sm"
                  />
                </div>
              </div>

              <div>
                <label class="block text-xs font-semibold text-slate-700">Vía</label>
                <select
                  v-model="it.route"
                  class="mt-1 w-full rounded-lg border border-slate-300 px-2 py-1.5 text-sm bg-white"
                >
                  <option value="oral">Oral</option>
                  <option value="topical">Tópica</option>
                  <option value="otic">Ótica</option>
                  <option value="ophthalmic">Oftálmica</option>
                  <option value="subcutaneous">Subcutánea</option>
                  <option value="intramuscular">Intramuscular</option>
                  <option value="inhalation">Inhalatoria</option>
                </select>
              </div>

              <div>
                <label class="block text-xs font-semibold text-slate-700">Frecuencia</label>
                <input
                  v-model="it.frequency"
                  type="text"
                  placeholder="cada 12 horas"
                  class="mt-1 w-full rounded-lg border border-slate-300 px-2 py-1.5 text-sm"
                />
              </div>

              <div>
                <label class="block text-xs font-semibold text-slate-700">Duración (días)</label>
                <input
                  v-model.number="it.duration_days"
                  type="number"
                  min="1"
                  class="mt-1 w-full rounded-lg border border-slate-300 px-2 py-1.5 text-sm"
                />
              </div>
            </div>

            <!-- Cantidad y Sustitución -->
            <div class="flex flex-wrap items-center justify-between gap-4 pt-2">
              <div class="flex items-center gap-2">
                <label class="text-xs font-semibold text-slate-700">Cantidad a dispensar:</label>
                <input
                  v-model.number="it.quantity_value"
                  type="number"
                  min="1"
                  class="w-16 rounded-lg border border-slate-300 px-2 py-1 text-sm"
                />
                <input
                  v-model="it.quantity_unit"
                  type="text"
                  placeholder="pack / caja"
                  class="w-24 rounded-lg border border-slate-300 px-2 py-1 text-sm"
                />
              </div>

              <label
                class="flex items-center gap-2 cursor-pointer text-xs font-medium text-slate-700"
              >
                <input
                  v-model="it.substitution_allowed"
                  type="checkbox"
                  class="h-4 w-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                />
                <span>Permitir sustitución equivalente en farmacia</span>
              </label>
            </div>
          </div>
        </div>

        <!-- Confirmación y Atestación Obligatoria -->
        <div class="rounded-2xl border border-slate-200 bg-slate-50 p-6 space-y-4">
          <div
            class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-xs sm:text-sm text-emerald-900"
          >
            <p class="font-medium">Nota sobre recetas externas:</p>
            <p class="mt-1 text-xs text-emerald-800">
              Esta receta se registrará como
              <span class="font-semibold">Externa no verificada</span>. Las farmacias aliadas
              examinarán la imagen adjunta para comprobar su autenticidad antes de cotizar o
              despachar.
            </p>
          </div>

          <label class="flex items-start gap-3 cursor-pointer">
            <input
              v-model="attestationChecked"
              data-testid="checkbox-attestation"
              type="checkbox"
              class="mt-0.5 h-5 w-5 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
            />
            <span class="text-sm font-semibold text-slate-900">
              Confirmo que los medicamentos, dosis y cantidades aquí detallados coinciden fielmente
              con la receta expedida por mi médico veterinario.
            </span>
          </label>

          <div class="flex justify-end gap-3 pt-2">
            <button
              type="button"
              class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
              @click="router.push('/app/prescriptions')"
            >
              Cancelar
            </button>
            <button
              type="button"
              data-testid="btn-confirm-prescription"
              class="rounded-lg bg-emerald-600 px-6 py-2.5 text-sm font-semibold text-white shadow hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed"
              :disabled="!attestationChecked || submitting || items.length === 0"
              @click="confirmPrescription"
            >
              <span v-if="submitting">Confirmando receta...</span>
              <span v-else>Confirmar Receta</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
