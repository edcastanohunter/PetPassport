<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, type PrescriptionQuoteOptions } from '@/shared/api/client'

const route = useRoute()
const router = useRouter()
const prescriptionId = route.params.id as string

const step = ref<1 | 2 | 3 | 4>(1)
const loading = ref(false)
const submitting = ref(false)
const error = ref<string | null>(null)

// Datos cargados desde /prescriptions/{id}/quote-options
const options = ref<PrescriptionQuoteOptions | null>(null)

// Paso 1: Selección de ítems
interface ItemSelection {
  prescriptionItemId: string
  medicationName: string
  dose: string
  quantity: number
  unit: string
  isControlled: boolean
  selected: boolean
  substitutionAllowed: boolean
}
const items = ref<ItemSelection[]>([])

// Paso 2: Modalidad y dirección
const fulfillmentType = ref<'delivery' | 'pickup'>('delivery')
const address = ref({
  street: '',
  number: '',
  complement: '',
  neighborhood: '',
  city: 'Bogotá',
  postalCode: '',
})

// Paso 3: Selección de farmacias
const selectedPharmacyIds = ref<string[]>([])

// Cargar opciones
onMounted(async () => {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/prescriptions/{prescriptionId}/quote-options', {
      params: { path: { prescriptionId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data) {
      options.value = res.data
      items.value = res.data.items.map((it) => ({
        prescriptionItemId: it.prescription_item_id,
        medicationName: it.medication_name,
        dose: it.dose,
        quantity: it.quantity,
        unit: it.unit,
        isControlled: it.is_controlled,
        selected: !it.is_controlled, // por defecto seleccionados si no son controlados
        substitutionAllowed: true,
      }))
      // Pre-seleccionar todas las farmacias elegibles
      selectedPharmacyIds.value = res.data.pharmacies.map((p) => p.pharmacy_id)
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar opciones de cotización'
  } finally {
    loading.value = false
  }
})

// Validación Paso 1: al menos un ítem no controlado seleccionado
const selectedItems = computed(() => items.value.filter((it) => it.selected && !it.isControlled))
const hasControlledItems = computed(() => items.value.some((it) => it.isControlled))
const canProceedStep1 = computed(() => selectedItems.value.length > 0)

// Validación Paso 2: si delivery, calle y ciudad son requeridos
const canProceedStep2 = computed(() => {
  if (fulfillmentType.value === 'pickup') return true
  return address.value.street.trim() !== '' && address.value.city.trim() !== ''
})

// Validación Paso 3: al menos una farmacia seleccionada
const eligiblePharmacies = computed(() => {
  if (!options.value) return []
  return options.value.pharmacies.filter((ph: { fulfillment_types: string[] }) =>
    ph.fulfillment_types.includes(fulfillmentType.value),
  )
})
const canProceedStep3 = computed(() => selectedPharmacyIds.value.length > 0)

function toggleAllPharmacies() {
  if (selectedPharmacyIds.value.length === eligiblePharmacies.value.length) {
    selectedPharmacyIds.value = []
  } else {
    selectedPharmacyIds.value = eligiblePharmacies.value.map(
      (p: { pharmacy_id: string }) => p.pharmacy_id,
    )
  }
}

// Envío final (Paso 4)
async function submitQuoteRequest() {
  submitting.value = true
  error.value = null
  try {
    const payload = {
      prescription_id: prescriptionId,
      fulfillment_type: fulfillmentType.value,
      items: selectedItems.value.map((it) => ({
        prescription_item_id: it.prescriptionItemId,
        quantity: it.quantity,
        unit: it.unit,
        substitution_allowed: it.substitutionAllowed,
      })),
      delivery_address:
        fulfillmentType.value === 'delivery'
          ? {
              street: address.value.street,
              number: address.value.number,
              complement: address.value.complement,
              neighborhood: address.value.neighborhood,
              city: address.value.city,
              postal_code: address.value.postalCode,
            }
          : undefined,
      pharmacy_ids: selectedPharmacyIds.value,
    }

    const res = await api().POST('/quote-requests', {
      body: payload,
    })
    if (res.error) throw new ApiError(res.error, res.response.status)

    const qrId = res.data?.id
    if (qrId) {
      await router.push({ name: 'quote-request-board', params: { id: qrId } })
    } else {
      await router.push({ name: 'quote-requests' })
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al enviar solicitud de cotización'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl px-4 py-8">
    <!-- Breadcrumb & Header -->
    <div class="mb-6">
      <nav class="mb-2 text-sm text-slate-500">
        <router-link to="/app/prescriptions" class="hover:text-emerald-600">Recetas</router-link>
        <span class="mx-2">/</span>
        <span class="text-slate-800">Nueva Cotización</span>
      </nav>
      <h1 class="text-2xl font-bold tracking-tight text-slate-900">Cotizar Receta en Farmacias</h1>
      <p class="mt-1 text-sm text-slate-500">
        Solicita precios y tiempos de entrega a farmacias aliadas autorizadas (PRD-021).
      </p>
    </div>

    <!-- Stepper indicator -->
    <div class="mb-8 flex items-center justify-between border-b border-slate-200 pb-4">
      <div
        class="flex items-center gap-2"
        :class="step >= 1 ? 'font-semibold text-emerald-600' : 'text-slate-400'"
      >
        <span
          class="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold"
          :class="step >= 1 ? 'bg-emerald-600 text-white' : 'bg-slate-200 text-slate-600'"
          >1</span
        >
        <span>Medicamentos</span>
      </div>
      <div class="h-0.5 flex-1 bg-slate-200 mx-3"></div>
      <div
        class="flex items-center gap-2"
        :class="step >= 2 ? 'font-semibold text-emerald-600' : 'text-slate-400'"
      >
        <span
          class="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold"
          :class="step >= 2 ? 'bg-emerald-600 text-white' : 'bg-slate-200 text-slate-600'"
          >2</span
        >
        <span>Modalidad y Entrega</span>
      </div>
      <div class="h-0.5 flex-1 bg-slate-200 mx-3"></div>
      <div
        class="flex items-center gap-2"
        :class="step >= 3 ? 'font-semibold text-emerald-600' : 'text-slate-400'"
      >
        <span
          class="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold"
          :class="step >= 3 ? 'bg-emerald-600 text-white' : 'bg-slate-200 text-slate-600'"
          >3</span
        >
        <span>Farmacias</span>
      </div>
      <div class="h-0.5 flex-1 bg-slate-200 mx-3"></div>
      <div
        class="flex items-center gap-2"
        :class="step >= 4 ? 'font-semibold text-emerald-600' : 'text-slate-400'"
      >
        <span
          class="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold"
          :class="step >= 4 ? 'bg-emerald-600 text-white' : 'bg-slate-200 text-slate-600'"
          >4</span
        >
        <span>Confirmación</span>
      </div>
    </div>

    <!-- Alert de Error -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="py-12 text-center text-slate-500">
      Cargando opciones de cotización...
    </div>

    <div v-else-if="options">
      <!-- PASO 1: Selección de ítems -->
      <div v-if="step === 1" class="space-y-6">
        <!-- Aviso de sustancias controladas (RN-26) -->
        <div
          v-if="hasControlledItems"
          class="rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900"
        >
          <div class="flex items-start gap-3">
            <span class="text-xl">⚠️</span>
            <div>
              <strong class="font-semibold">Regulación sobre sustancias controladas:</strong>
              <p class="mt-1">
                Esta receta contiene medicamentos catalogados como sustancias de control especial
                (RN-26 / SEC-007). Por normativa legal, estos medicamentos están deshabilitados para
                cotización y entrega a domicilio. Deben ser retirados personalmente en una farmacia
                autorizada con la receta médica original física.
              </p>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
          <h2 class="text-lg font-semibold text-slate-900">
            Selecciona los medicamentos a cotizar
          </h2>
          <p class="mt-1 text-sm text-slate-500">
            Marca los ítems que deseas incluir y si permites que la farmacia proponga sustitutos
            equivalentes más económicos o disponibles.
          </p>

          <div class="mt-6 divide-y divide-slate-100">
            <div
              v-for="it in items"
              :key="it.prescriptionItemId"
              class="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between"
              :class="{ 'opacity-60': it.isControlled }"
            >
              <div class="flex items-start gap-3">
                <input
                  type="checkbox"
                  :disabled="it.isControlled"
                  v-model="it.selected"
                  class="mt-1 h-4 w-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                />
                <div>
                  <div class="flex items-center gap-2">
                    <span class="font-medium text-slate-900">{{ it.medicationName }}</span>
                    <span
                      v-if="it.isControlled"
                      class="rounded-full bg-rose-100 px-2 py-0.5 text-xs font-semibold text-rose-800 border border-rose-200"
                    >
                      Controlado · No cotizable
                    </span>
                  </div>
                  <p class="text-xs text-slate-500">
                    Dosis: {{ it.dose }} · Cantidad prescrita: {{ it.quantity }} {{ it.unit }}
                  </p>
                </div>
              </div>

              <!-- Opción de sustitución -->
              <div v-if="!it.isControlled" class="flex items-center gap-2 pl-7 sm:pl-0">
                <label class="flex items-center gap-2 text-xs text-slate-600 cursor-pointer">
                  <input
                    type="checkbox"
                    v-model="it.substitutionAllowed"
                    :disabled="!it.selected"
                    class="h-3.5 w-3.5 rounded border-slate-300 text-emerald-600"
                  />
                  <span>Permitir sustitutos equivalentes</span>
                </label>
              </div>
            </div>
          </div>
        </div>

        <!-- Botón Siguiente -->
        <div class="flex justify-end">
          <button
            type="button"
            :disabled="!canProceedStep1"
            @click="step = 2"
            class="rounded-lg bg-emerald-600 px-6 py-2.5 text-sm font-medium text-white shadow-sm hover:bg-emerald-700 disabled:cursor-not-allowed disabled:bg-slate-300"
          >
            Siguiente: Modalidad de Entrega →
          </button>
        </div>
      </div>

      <!-- PASO 2: Modalidad y Entrega -->
      <div v-if="step === 2" class="space-y-6">
        <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
          <h2 class="text-lg font-semibold text-slate-900">¿Cómo deseas recibir tu pedido?</h2>
          <p class="mt-1 text-sm text-slate-500">
            Elige si prefieres recibirlo en tu domicilio o retirarlo en el local de la farmacia.
          </p>

          <div class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label
              class="relative flex cursor-pointer rounded-lg border p-4 shadow-sm focus:outline-none"
              :class="
                fulfillmentType === 'delivery'
                  ? 'border-emerald-600 bg-emerald-50/40 ring-2 ring-emerald-600'
                  : 'border-slate-300 bg-white hover:border-slate-400'
              "
            >
              <input
                type="radio"
                name="fulfillment"
                value="delivery"
                v-model="fulfillmentType"
                class="sr-only"
              />
              <div class="flex flex-col">
                <span class="block text-sm font-medium text-slate-900">🛵 Entrega a Domicilio</span>
                <span class="mt-1 block text-xs text-slate-500">
                  La farmacia despachará los productos a la dirección indicada.
                </span>
              </div>
            </label>

            <label
              class="relative flex cursor-pointer rounded-lg border p-4 shadow-sm focus:outline-none"
              :class="
                fulfillmentType === 'pickup'
                  ? 'border-emerald-600 bg-emerald-50/40 ring-2 ring-emerald-600'
                  : 'border-slate-300 bg-white hover:border-slate-400'
              "
            >
              <input
                type="radio"
                name="fulfillment"
                value="pickup"
                v-model="fulfillmentType"
                class="sr-only"
              />
              <div class="flex flex-col">
                <span class="block text-sm font-medium text-slate-900">🏬 Retiro en Farmacia</span>
                <span class="mt-1 block text-xs text-slate-500">
                  Pasas a retirar el pedido una vez que la farmacia confirme que está listo.
                </span>
              </div>
            </label>
          </div>

          <!-- Campos de dirección si es delivery -->
          <div v-if="fulfillmentType === 'delivery'" class="mt-6 border-t border-slate-100 pt-6">
            <h3 class="text-sm font-semibold text-slate-800">Dirección de Entrega</h3>
            <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div class="sm:col-span-2">
                <label class="block text-xs font-medium text-slate-700"
                  >Calle / Carrera / Avenida *</label
                >
                <input
                  type="text"
                  v-model="address.street"
                  placeholder="Ej. Calle 123"
                  class="mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
                />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-700">Número / Puerta</label>
                <input
                  type="text"
                  v-model="address.number"
                  placeholder="Ej. # 45-67"
                  class="mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
                />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-700"
                  >Apto / Casa / Complemento</label
                >
                <input
                  type="text"
                  v-model="address.complement"
                  placeholder="Ej. Apto 302 Torre B"
                  class="mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
                />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-700">Barrio</label>
                <input
                  type="text"
                  v-model="address.neighborhood"
                  placeholder="Ej. Chapinero"
                  class="mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
                />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-700">Ciudad *</label>
                <input
                  type="text"
                  v-model="address.city"
                  placeholder="Ej. Bogotá"
                  class="mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
                />
              </div>
            </div>
          </div>
        </div>

        <div class="flex justify-between">
          <button
            type="button"
            @click="step = 1"
            class="rounded-lg border border-slate-300 px-5 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            ← Volver a Medicamentos
          </button>
          <button
            type="button"
            :disabled="!canProceedStep2"
            @click="step = 3"
            class="rounded-lg bg-emerald-600 px-6 py-2.5 text-sm font-medium text-white shadow-sm hover:bg-emerald-700 disabled:cursor-not-allowed disabled:bg-slate-300"
          >
            Siguiente: Seleccionar Farmacias →
          </button>
        </div>
      </div>

      <!-- PASO 3: Selección de Farmacias (SLA & Rating de PRD-021) -->
      <div v-if="step === 3" class="space-y-6">
        <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
          <div class="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
            <div>
              <h2 class="text-lg font-semibold text-slate-900">Farmacias Aliadas Disponibles</h2>
              <p class="mt-1 text-sm text-slate-500">
                Selecciona a qué farmacias deseas solicitar cotización. Las métricas de SLA reflejan
                su desempeño histórico.
              </p>
            </div>
            <button
              type="button"
              @click="toggleAllPharmacies"
              class="text-xs font-semibold text-emerald-600 hover:text-emerald-700 underline"
            >
              {{
                selectedPharmacyIds.length === eligiblePharmacies.length
                  ? 'Deseleccionar todas'
                  : 'Seleccionar todas'
              }}
            </button>
          </div>

          <div class="mt-6 space-y-4">
            <div
              v-for="ph in eligiblePharmacies"
              :key="ph.pharmacy_id"
              class="flex items-center justify-between rounded-lg border p-4 transition-all"
              :class="
                selectedPharmacyIds.includes(ph.pharmacy_id)
                  ? 'border-emerald-500 bg-emerald-50/20 shadow-sm'
                  : 'border-slate-200 hover:border-slate-300'
              "
            >
              <label class="flex flex-1 cursor-pointer items-start gap-3">
                <input
                  type="checkbox"
                  :value="ph.pharmacy_id"
                  v-model="selectedPharmacyIds"
                  class="mt-1 h-4 w-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                />
                <div>
                  <div class="flex items-center gap-2">
                    <span class="font-medium text-slate-900">{{ ph.name }}</span>
                    <span
                      class="flex items-center gap-1 rounded bg-amber-50 px-1.5 py-0.5 text-xs font-semibold text-amber-700 border border-amber-200"
                    >
                      ★ {{ ph.rating.toFixed(1) }}
                    </span>
                  </div>
                  <p class="text-xs text-slate-500">{{ ph.address }}</p>

                  <!-- Métricas de SLA (PRD-021) -->
                  <div class="mt-2 flex flex-wrap gap-4 text-xs text-slate-600">
                    <span class="flex items-center gap-1">
                      ⏱️ Cotiza en aprox. {{ Math.round(ph.average_quote_time_seconds / 60) }} min
                    </span>
                    <span class="flex items-center gap-1">
                      📦 {{ Math.round(ph.on_time_delivery_rate * 100) }}% entregas a tiempo
                    </span>
                  </div>
                </div>
              </label>
            </div>

            <div
              v-if="eligiblePharmacies.length === 0"
              class="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500"
            >
              No hay farmacias aliadas disponibles con la modalidad seleccionada en esta área.
            </div>
          </div>
        </div>

        <div class="flex justify-between">
          <button
            type="button"
            @click="step = 2"
            class="rounded-lg border border-slate-300 px-5 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            ← Volver a Modalidad
          </button>
          <button
            type="button"
            :disabled="!canProceedStep3"
            @click="step = 4"
            class="rounded-lg bg-emerald-600 px-6 py-2.5 text-sm font-medium text-white shadow-sm hover:bg-emerald-700 disabled:cursor-not-allowed disabled:bg-slate-300"
          >
            Siguiente: Revisar y Confirmar →
          </button>
        </div>
      </div>

      <!-- PASO 4: Resumen y Confirmación -->
      <div v-if="step === 4" class="space-y-6">
        <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
          <h2 class="text-lg font-semibold text-slate-900">Resumen de la Solicitud</h2>
          <p class="mt-1 text-sm text-slate-500">
            Revisa los detalles antes de enviar la solicitud a las farmacias seleccionadas.
          </p>

          <div class="mt-6 space-y-4 divide-y divide-slate-100">
            <!-- Ítems -->
            <div class="pb-4">
              <h3 class="text-xs font-semibold uppercase tracking-wider text-slate-500">
                Medicamentos a cotizar ({{ selectedItems.length }})
              </h3>
              <ul class="mt-2 space-y-1 text-sm text-slate-700">
                <li
                  v-for="it in selectedItems"
                  :key="it.prescriptionItemId"
                  class="flex justify-between"
                >
                  <span
                    >{{ it.medicationName }} ({{ it.dose }}) × {{ it.quantity }} {{ it.unit }}</span
                  >
                  <span class="text-xs text-slate-500">
                    {{ it.substitutionAllowed ? 'Sustitutos permitidos' : 'Solo recetado' }}
                  </span>
                </li>
              </ul>
            </div>

            <!-- Modalidad -->
            <div class="py-4">
              <h3 class="text-xs font-semibold uppercase tracking-wider text-slate-500">
                Modalidad de Entrega
              </h3>
              <p class="mt-1 text-sm text-slate-800">
                {{ fulfillmentType === 'delivery' ? 'Domicilio' : 'Retiro en local' }}
              </p>
              <p v-if="fulfillmentType === 'delivery'" class="text-xs text-slate-600">
                {{ address.street }} {{ address.number }} {{ address.complement }},
                {{ address.neighborhood ? address.neighborhood + ', ' : '' }}{{ address.city }}
              </p>
            </div>

            <!-- Farmacias invitadas -->
            <div class="py-4">
              <h3 class="text-xs font-semibold uppercase tracking-wider text-slate-500">
                Farmacias Invitadas ({{ selectedPharmacyIds.length }})
              </h3>
              <p class="mt-1 text-sm text-slate-700">
                Se enviará notificación a {{ selectedPharmacyIds.length }} farmacias aliadas.
                Recibirás sus cotizaciones en tu tablero comparativo.
              </p>
            </div>

            <!-- Aviso Legal Transparencia de Pago (PRD-021 / SEC-007) -->
            <div class="pt-4">
              <div
                class="rounded-lg border border-slate-200 bg-slate-50 p-4 text-xs text-slate-600"
              >
                <strong class="text-slate-800">Aviso importante:</strong>
                PetPassport no procesa pagos de medicamentos en esta etapa. El pago se efectúa
                directamente con la farmacia seleccionada al recibir el pedido o al retirarlo en el
                local. Tus datos de contacto solo se compartirán con la farmacia que elijas tras
                aprobar su cotización (ARQ-013 / minimización de datos).
              </div>
            </div>
          </div>
        </div>

        <div class="flex justify-between">
          <button
            type="button"
            @click="step = 3"
            class="rounded-lg border border-slate-300 px-5 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            ← Volver a Farmacias
          </button>
          <button
            type="button"
            :disabled="submitting"
            @click="submitQuoteRequest"
            class="rounded-lg bg-emerald-600 px-8 py-2.5 text-sm font-medium text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            {{ submitting ? 'Enviando Solicitud...' : 'Confirmar y Enviar Solicitud' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
