<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type PharmacyOrderDetail } from '@/shared/api/client'

const route = useRoute()
const orgId = route.params.orgId as string
const orderId = route.params.orderId as string

const order = ref<PharmacyOrderDetail | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const actionBusy = ref(false)

// Modal Confirmar (ETA)
const showConfirmModal = ref(false)
const estimatedReadyAt = ref<string>('')

// Modal Rechazar
const showRejectModal = ref(false)
const rejectReason = ref('')

// Modal Despacho (Courier & Tracking)
const showDispatchModal = ref(false)
const courierName = ref('')
const trackingCode = ref('')

// Modal Entrega y Dispensación
const showDeliverModal = ref(false)
interface DispensationInput {
  prescriptionItemId: string
  productName: string
  batchNumber: string
  expiryDate: string
  quantityDispensed: number
}
const dispensationRows = ref<DispensationInput[]>([])

// Modal Cancelar
const showCancelModal = ref(false)
const cancelReason = ref('')

async function loadOrder() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/pharmacies/{orgId}/orders/{orderId}', {
      params: { path: { orgId, orderId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data) {
      order.value = res.data
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar pedido'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadOrder()
})

function formatCents(cents: number) {
  return new Intl.NumberFormat('es-CO', {
    style: 'currency',
    currency: 'COP',
    maximumFractionDigits: 0,
  }).format(cents)
}

// Acciones del ciclo de vida

function openConfirmModal() {
  const d = new Date()
  d.setHours(d.getHours() + 1)
  estimatedReadyAt.value = d.toISOString().slice(0, 16)
  showConfirmModal.value = true
}

async function confirmOrder() {
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/confirm', {
      params: { path: { orgId, orderId } },
      body: {
        estimated_ready_at: estimatedReadyAt.value
          ? new Date(estimatedReadyAt.value).toISOString()
          : undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showConfirmModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al confirmar pedido'
  } finally {
    actionBusy.value = false
  }
}

async function rejectOrder() {
  if (!rejectReason.value.trim()) return
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/reject', {
      params: { path: { orgId, orderId } },
      body: { reason: rejectReason.value },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showRejectModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al rechazar pedido'
  } finally {
    actionBusy.value = false
  }
}

async function markPreparing() {
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/preparing', {
      params: { path: { orgId, orderId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al marcar en preparación'
  } finally {
    actionBusy.value = false
  }
}

async function markReady() {
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/ready', {
      params: { path: { orgId, orderId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al marcar listo'
  } finally {
    actionBusy.value = false
  }
}

function openDispatchModal() {
  courierName.value = 'Mensajería Express'
  trackingCode.value = 'GUIA-' + Math.floor(100000 + Math.random() * 900000)
  showDispatchModal.value = true
}

async function dispatchOrder() {
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/out-for-delivery', {
      params: { path: { orgId, orderId } },
      body: {
        courier_name: courierName.value || undefined,
        tracking_code: trackingCode.value || undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showDispatchModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al marcar en camino'
  } finally {
    actionBusy.value = false
  }
}

function openDeliverModal() {
  if (!order.value) return
  const nextYear = new Date()
  nextYear.setFullYear(nextYear.getFullYear() + 2)
  const expStr = nextYear.toISOString().slice(0, 10)

  dispensationRows.value = order.value.lines.map((l, idx) => ({
    prescriptionItemId: l.prescription_item_id || l.item_id,
    productName: l.product_name,
    batchNumber: 'LOTE-' + (202600 + idx),
    expiryDate: expStr,
    quantityDispensed: l.quantity,
  }))
  showDeliverModal.value = true
}

async function deliverOrder() {
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/deliver', {
      params: { path: { orgId, orderId } },
      body: {
        dispensation_records: dispensationRows.value.map((d) => ({
          prescription_item_id: d.prescriptionItemId,
          batch_number: d.batchNumber,
          expiry_date: d.expiryDate,
          quantity_dispensed: d.quantityDispensed,
        })),
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showDeliverModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al registrar entrega'
  } finally {
    actionBusy.value = false
  }
}

async function cancelOrder() {
  if (!cancelReason.value.trim()) return
  actionBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/orders/{orderId}/cancel', {
      params: { path: { orgId, orderId } },
      body: { reason: cancelReason.value },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showCancelModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cancelar pedido'
  } finally {
    actionBusy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <nav class="mb-2 text-sm text-slate-500">
          <router-link :to="`/pharmacy/${orgId}/orders`" class="hover:text-emerald-600">
            Pedidos de Farmacia
          </router-link>
          <span class="mx-2">/</span>
          <span class="text-slate-800">Detalle</span>
        </nav>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Gestión del Pedido</h1>
        <p class="mt-1 text-sm text-slate-500">
          Actualiza el estado operativo y registra los lotes dispensados (FE-008).
        </p>
      </div>

      <!-- Barra de Acciones Operativas según estado -->
      <div v-if="order" class="flex flex-wrap items-center gap-2">
        <!-- Estado: placed -->
        <template v-if="order.status === 'placed'">
          <button
            type="button"
            @click="showRejectModal = true"
            class="rounded-lg border border-rose-300 bg-white px-3 py-2 text-xs font-semibold text-rose-700 hover:bg-rose-50"
          >
            Rechazar Pedido
          </button>
          <button
            type="button"
            @click="openConfirmModal"
            class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700"
          >
            ✓ Confirmar con ETA
          </button>
        </template>

        <!-- Estado: confirmed -->
        <template v-else-if="order.status === 'confirmed'">
          <button
            type="button"
            @click="markPreparing"
            class="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-indigo-700"
          >
            ⚙️ Comenzar Preparación
          </button>
        </template>

        <!-- Estado: preparing -->
        <template v-else-if="order.status === 'preparing'">
          <button
            type="button"
            @click="markReady"
            class="rounded-lg bg-teal-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-teal-700"
          >
            📦 Marcar Listo
          </button>
        </template>

        <!-- Estado: ready -->
        <template v-else-if="order.status === 'ready'">
          <button
            v-if="order.fulfillment_type === 'delivery'"
            type="button"
            @click="openDispatchModal"
            class="rounded-lg bg-purple-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-purple-700"
          >
            🛵 Despachar (En Camino)
          </button>
          <button
            v-else
            type="button"
            @click="openDeliverModal"
            class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700"
          >
            ✓ Entregar y Registrar Dispensación
          </button>
        </template>

        <!-- Estado: out_for_delivery -->
        <template v-else-if="order.status === 'out_for_delivery'">
          <button
            type="button"
            @click="openDeliverModal"
            class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700"
          >
            ✓ Confirmar Entrega Realizada
          </button>
        </template>

        <!-- Cancelación de emergencia -->
        <button
          v-if="
            order.status !== 'delivered' &&
            order.status !== 'cancelled' &&
            order.status !== 'rejected'
          "
          type="button"
          @click="showCancelModal = true"
          class="rounded-lg border border-slate-200 px-3 py-2 text-xs font-medium text-slate-500 hover:text-rose-600"
        >
          Cancelar
        </button>
      </div>
    </div>

    <!-- Error -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="py-12 text-center text-slate-500">Cargando pedido...</div>

    <div v-else-if="order" class="space-y-6">
      <!-- Resumen y Estado -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <div class="flex items-center gap-2">
              <span
                class="rounded-full bg-emerald-100 px-3 py-1 text-xs font-bold text-emerald-800 uppercase tracking-wider"
              >
                Estado: {{ order.status }}
              </span>
              <span class="text-xs text-slate-400">·</span>
              <span class="text-xs font-medium text-slate-600">
                Modalidad: {{ order.fulfillment_type === 'delivery' ? 'Domicilio' : 'Retiro' }}
              </span>
            </div>
            <h2 class="mt-2 text-lg font-bold text-slate-900">
              Mascota: {{ order.pet_name || 'Sin nombre' }} ({{ order.pet_species || 'Especie' }})
            </h2>
          </div>

          <div class="text-right">
            <span class="text-xs text-slate-500">Total a cobrar:</span>
            <div class="font-mono text-xl font-bold text-emerald-700">
              {{ formatCents(order.total_cents) }}
            </div>
            <span class="text-[11px] text-amber-700 font-medium">Pago contra entrega</span>
          </div>
        </div>
      </div>

      <!-- Datos del Cliente (Minimizados ARQ-013) -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h3 class="text-xs font-bold uppercase tracking-wider text-slate-500">
          Datos del Tutor para Entrega
        </h3>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 text-xs">
          <div>
            <span class="text-slate-500">Nombre del Tutor:</span>
            <p class="font-semibold text-slate-900 text-sm">{{ order.tutor_name || 'Tutor' }}</p>
          </div>
          <div>
            <span class="text-slate-500">Teléfono de Contacto:</span>
            <p class="font-semibold text-slate-900 text-sm">
              <a :href="'tel:' + order.tutor_phone" class="text-emerald-600 hover:underline">
                {{ order.tutor_phone || 'No registrado' }}
              </a>
            </p>
          </div>
          <div v-if="order.delivery_address" class="sm:col-span-2">
            <span class="text-slate-500">Dirección de Entrega:</span>
            <p class="font-semibold text-slate-900">
              {{ order.delivery_address.street }} {{ order.delivery_address.number }}
              {{ order.delivery_address.complement }},
              {{
                order.delivery_address.neighborhood
                  ? order.delivery_address.neighborhood + ', '
                  : ''
              }}
              {{ order.delivery_address.city }}
            </p>
          </div>
        </div>
      </div>

      <!-- Productos a Despachar -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h3 class="text-base font-bold text-slate-900">Medicamentos y Productos</h3>
        <div class="mt-4 divide-y divide-slate-100 text-xs">
          <div
            v-for="line in order.lines"
            :key="line.item_id"
            class="flex items-center justify-between py-3"
          >
            <div>
              <div class="flex items-center gap-2">
                <span class="font-semibold text-slate-900">{{ line.product_name }}</span>
                <span
                  v-if="line.substitution"
                  class="rounded bg-purple-100 px-1.5 py-0.2 text-[10px] font-semibold text-purple-800"
                >
                  Sustituto Aceptado por Tutor
                </span>
              </div>
              <p class="text-slate-500">Cantidad: {{ line.quantity }} unidad(es)</p>
            </div>
            <div class="font-mono font-medium text-slate-900">
              {{ formatCents(line.total_price_cents) }}
            </div>
          </div>

          <div
            v-if="order.delivery_fee_cents && order.delivery_fee_cents > 0"
            class="flex justify-between py-2 text-slate-600 font-medium"
          >
            <span>Costo de Envío a Domicilio</span>
            <span class="font-mono">{{ formatCents(order.delivery_fee_cents) }}</span>
          </div>
        </div>
      </div>

      <!-- Historial de Dispensaciones (Lotes Registrados) -->
      <div
        v-if="order.dispensation_records && order.dispensation_records.length > 0"
        class="rounded-xl border border-emerald-200 bg-emerald-50/40 p-6 shadow-sm"
      >
        <h3 class="text-xs font-bold uppercase tracking-wider text-emerald-900">
          Registro Legal de Dispensación (Lotes y Fechas)
        </h3>
        <div class="mt-3 space-y-2 text-xs">
          <div
            v-for="d in order.dispensation_records"
            :key="d.prescription_item_id"
            class="flex justify-between items-center rounded bg-white p-3 border border-emerald-200"
          >
            <div>
              <p class="font-mono font-semibold text-slate-900">Lote: {{ d.batch_number }}</p>
              <p class="text-slate-500">Vencimiento: {{ d.expiry_date }}</p>
            </div>
            <span class="font-bold text-emerald-800">
              {{ d.quantity_dispensed }} unidad(es) dispensadas
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- MODAL CONFIRMAR CON ETA -->
    <div
      v-if="showConfirmModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Confirmar Aceptación del Pedido</h3>
        <p class="text-xs text-slate-500">
          Indica el tiempo estimado (ETA) en el que el pedido estará listo o entregado:
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700"
            >Fecha y Hora Estimada (ETA)</label
          >
          <input
            type="datetime-local"
            v-model="estimatedReadyAt"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          />
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showConfirmModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="actionBusy"
            @click="confirmOrder"
            class="rounded-lg bg-emerald-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            Confirmar Pedido
          </button>
        </div>
      </div>
    </div>

    <!-- MODAL RECHAZAR -->
    <div
      v-if="showRejectModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Rechazar Pedido</h3>
        <p class="text-xs text-slate-500">
          Indica el motivo por el cual no es posible atender este pedido:
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Motivo de rechazo *</label>
          <textarea
            v-model="rejectReason"
            rows="3"
            placeholder="Ej. Quiebre de stock sobrevenido o problema logístico"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          ></textarea>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showRejectModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Volver
          </button>
          <button
            type="button"
            :disabled="!rejectReason.trim() || actionBusy"
            @click="rejectOrder"
            class="rounded-lg bg-rose-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-rose-700 disabled:bg-slate-300"
          >
            Confirmar Rechazo
          </button>
        </div>
      </div>
    </div>

    <!-- MODAL DESPACHO / EN CAMINO -->
    <div
      v-if="showDispatchModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Despacho de Pedido</h3>
        <p class="text-xs text-slate-500">
          Ingresa los datos del repartidor o empresa de mensajería para el seguimiento del tutor:
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Transportadora / Mensajero</label>
          <input
            type="text"
            v-model="courierName"
            placeholder="Ej. Domiciliario propio o Servientrega"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-slate-700">Código de Guía / Tracking</label>
          <input
            type="text"
            v-model="trackingCode"
            placeholder="Ej. GUIA-123456"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          />
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showDispatchModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="actionBusy"
            @click="dispatchOrder"
            class="rounded-lg bg-purple-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-purple-700 disabled:bg-slate-300"
          >
            Confirmar Despacho
          </button>
        </div>
      </div>
    </div>

    <!-- MODAL ENTREGA Y DISPENSACIÓN -->
    <div
      v-if="showDeliverModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div
        class="w-full max-w-xl max-h-[90vh] overflow-y-auto rounded-2xl bg-white p-6 shadow-xl space-y-5"
      >
        <div class="flex items-center justify-between border-b pb-3">
          <h3 class="text-lg font-bold text-slate-900">Registrar Entrega y Lotes Dispensados</h3>
          <button
            type="button"
            @click="showDeliverModal = false"
            class="text-slate-400 hover:text-slate-600"
          >
            ✕
          </button>
        </div>

        <p class="text-xs text-slate-500">
          Para cumplimiento regulatorio y trazabilidad (SEC-006 / SEC-007), ingresa el lote y
          vencimiento de cada producto entregado:
        </p>

        <div class="space-y-4">
          <div
            v-for="(row, idx) in dispensationRows"
            :key="row.prescriptionItemId"
            class="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs space-y-2"
          >
            <div class="font-semibold text-slate-900">{{ idx + 1 }}. {{ row.productName }}</div>
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
              <div>
                <label class="block text-[11px] text-slate-600">Número de Lote *</label>
                <input
                  type="text"
                  v-model="row.batchNumber"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs font-mono"
                />
              </div>
              <div>
                <label class="block text-[11px] text-slate-600">Fecha de Vencimiento *</label>
                <input
                  type="date"
                  v-model="row.expiryDate"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs"
                />
              </div>
              <div>
                <label class="block text-[11px] text-slate-600">Cantidad Dispensada *</label>
                <input
                  type="number"
                  min="1"
                  v-model.number="row.quantityDispensed"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs font-mono"
                />
              </div>
            </div>
          </div>
        </div>

        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showDeliverModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="actionBusy"
            @click="deliverOrder"
            class="rounded-lg bg-emerald-600 px-6 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            Confirmar Entrega y Cerrar Pedido
          </button>
        </div>
      </div>
    </div>

    <!-- MODAL CANCELAR -->
    <div
      v-if="showCancelModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Cancelar Pedido</h3>
        <p class="text-xs text-slate-500">
          Indica el motivo de cancelación. El tutor será informado inmediatamente:
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Motivo *</label>
          <textarea
            v-model="cancelReason"
            rows="3"
            placeholder="Ej. Incidencia insalvable en local"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          ></textarea>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showCancelModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Volver
          </button>
          <button
            type="button"
            :disabled="!cancelReason.trim() || actionBusy"
            @click="cancelOrder"
            class="rounded-lg bg-rose-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-rose-700 disabled:bg-slate-300"
          >
            Confirmar Cancelación
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
