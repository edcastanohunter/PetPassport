<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type OrderDetail } from '@/shared/api/client'

const route = useRoute()
const orderId = route.params.id as string

const order = ref<OrderDetail | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)

// Cancelación
const showCancelModal = ref(false)
const cancelReason = ref('')
const cancellingBusy = ref(false)

// Valoración de farmacia (PRD-022)
const ratingScore = ref(5)
const ratingComment = ref('')
const submittingRating = ref(false)
const ratingError = ref<string | null>(null)

async function submitRating() {
  submittingRating.value = true
  ratingError.value = null
  try {
    const res = await api().POST('/orders/{orderId}/rate', {
      params: { path: { orderId } },
      body: {
        score: ratingScore.value,
        comment: ratingComment.value.trim() || undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    await loadOrder()
  } catch (e) {
    ratingError.value = e instanceof Error ? e.message : 'Error al registrar valoración'
  } finally {
    submittingRating.value = false
  }
}

// SSE & Polling
let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null

async function loadOrder() {
  try {
    const res = await api().GET('/orders/{orderId}', {
      params: { path: { orderId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data) {
      order.value = res.data
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar pedido'
  }
}

onMounted(async () => {
  loading.value = true
  await loadOrder()
  loading.value = false
  initSSE()
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
})

function initSSE() {
  try {
    const sseUrl = `/api/v1/orders/${orderId}/events`
    eventSource = new EventSource(sseUrl)

    eventSource.onmessage = () => {
      void loadOrder()
    }

    eventSource.onerror = () => {
      if (eventSource) {
        eventSource.close()
        eventSource = null
      }
      if (!pollTimer) {
        pollTimer = setInterval(() => {
          void loadOrder()
        }, 10000)
      }
    }
  } catch {
    if (!pollTimer) {
      pollTimer = setInterval(() => {
        void loadOrder()
      }, 10000)
    }
  }
}

function formatCents(cents: number) {
  return new Intl.NumberFormat('es-CO', {
    style: 'currency',
    currency: 'COP',
    maximumFractionDigits: 0,
  }).format(cents)
}

// Pasos del ciclo de vida (Stepper)
const orderSteps = [
  { key: 'placed', label: 'Recibido' },
  { key: 'confirmed', label: 'Confirmado' },
  { key: 'preparing', label: 'En Preparación' },
  { key: 'ready_or_out', label: 'En Camino / Listo' },
  { key: 'delivered', label: 'Entregado' },
]

const currentStepIndex = computed(() => {
  if (!order.value) return 0
  const s = order.value.status
  switch (s) {
    case 'placed':
      return 0
    case 'confirmed':
      return 1
    case 'preparing':
      return 2
    case 'ready':
    case 'out_for_delivery':
      return 3
    case 'delivered':
      return 4
    default:
      return 0
  }
})

const isCancelled = computed(() => {
  return order.value?.status === 'cancelled' || order.value?.status === 'rejected'
})

const canCancel = computed(() => {
  return order.value?.status === 'placed' || order.value?.status === 'confirmed'
})

async function confirmCancelOrder() {
  if (!cancelReason.value.trim()) return
  cancellingBusy.value = true
  try {
    const res = await api().POST('/orders/{orderId}/cancel', {
      params: { path: { orderId } },
      body: { reason: cancelReason.value },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showCancelModal.value = false
    await loadOrder()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cancelar el pedido'
  } finally {
    cancellingBusy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <nav class="mb-2 text-sm text-slate-500">
          <router-link to="/app/orders" class="hover:text-emerald-600">Mis Pedidos</router-link>
          <span class="mx-2">/</span>
          <span class="text-slate-800">Seguimiento</span>
        </nav>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Seguimiento de Pedido</h1>
        <p class="mt-1 text-sm text-slate-500">
          Detalles y estado en tiempo real del despacho de tus medicamentos (FE-006).
        </p>
      </div>

      <div v-if="canCancel">
        <button
          type="button"
          @click="showCancelModal = true"
          class="rounded-lg border border-rose-300 bg-white px-4 py-2 text-xs font-semibold text-rose-700 hover:bg-rose-50"
        >
          Cancelar Pedido
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
    <div v-if="loading" class="py-12 text-center text-slate-500">
      Cargando detalle del pedido...
    </div>

    <div v-else-if="order" class="space-y-6">
      <!-- Stepper / Línea de tiempo -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h2 class="text-sm font-semibold text-slate-900">Estado del Pedido</h2>

        <!-- Si está cancelado -->
        <div
          v-if="isCancelled"
          class="mt-4 rounded-lg border border-rose-200 bg-rose-50 p-4 text-xs text-rose-800"
        >
          <strong>Pedido {{ order.status === 'rejected' ? 'Rechazado' : 'Cancelado' }}:</strong>
          {{ order.cancellation_reason || 'El pedido no continuará su proceso.' }}
        </div>

        <!-- Stepper normal -->
        <div v-else class="mt-6 flex items-center justify-between">
          <div
            v-for="(st, idx) in orderSteps"
            :key="st.key"
            class="flex flex-1 items-center"
            :class="{ 'flex-none': idx === orderSteps.length - 1 }"
          >
            <div class="flex flex-col items-center">
              <div
                class="flex h-8 w-8 items-center justify-center rounded-full text-xs font-bold transition"
                :class="
                  idx <= currentStepIndex
                    ? 'bg-emerald-600 text-white shadow-sm'
                    : 'bg-slate-100 text-slate-400'
                "
              >
                {{ idx < currentStepIndex ? '✓' : idx + 1 }}
              </div>
              <span
                class="mt-2 text-[11px] font-medium text-center"
                :class="
                  idx <= currentStepIndex ? 'text-emerald-700 font-semibold' : 'text-slate-400'
                "
              >
                {{ st.label }}
              </span>
            </div>

            <div
              v-if="idx < orderSteps.length - 1"
              class="h-0.5 flex-1 mx-2 transition"
              :class="idx < currentStepIndex ? 'bg-emerald-600' : 'bg-slate-200'"
            ></div>
          </div>
        </div>
      </div>

      <!-- Contacto de la Farmacia -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <h2 class="text-base font-bold text-slate-900">{{ order.pharmacy_name }}</h2>
            <p v-if="order.pharmacy_address" class="text-xs text-slate-500 mt-0.5">
              📍 {{ order.pharmacy_address }}
            </p>
          </div>

          <div v-if="order.pharmacy_phone">
            <a
              :href="'tel:' + order.pharmacy_phone"
              class="inline-flex items-center gap-2 rounded-lg bg-emerald-50 border border-emerald-200 px-4 py-2 text-xs font-bold text-emerald-800 hover:bg-emerald-100 transition"
            >
              📞 Llamar a la farmacia: {{ order.pharmacy_phone }}
            </a>
          </div>
        </div>
      </div>

      <!-- Información de Despacho y Envío -->
      <div
        v-if="order.tracking_code || order.courier_name"
        class="rounded-xl border border-purple-200 bg-purple-50/50 p-5 shadow-sm"
      >
        <h3 class="text-xs font-bold uppercase tracking-wider text-purple-900">
          Información de Envío
        </h3>
        <div class="mt-2 text-xs text-purple-800 flex flex-wrap gap-4">
          <span v-if="order.courier_name"
            >Transportadora: <strong>{{ order.courier_name }}</strong></span
          >
          <span v-if="order.tracking_code"
            >Guía de seguimiento: <strong class="font-mono">{{ order.tracking_code }}</strong></span
          >
        </div>
      </div>

      <!-- Productos y Precios -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h2 class="text-base font-bold text-slate-900">Productos del Pedido</h2>
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
                  Sustituto Aceptado
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

          <div class="flex justify-between pt-3 text-sm font-bold text-slate-900">
            <span>Total a Pagar</span>
            <span class="font-mono text-emerald-700">{{ formatCents(order.total_cents) }}</span>
          </div>
        </div>

        <div class="mt-4 rounded-lg bg-amber-50 p-3 text-xs text-amber-800">
          💳 <strong>Recordatorio:</strong> El pago se efectúa contra entrega o directamente en la
          farmacia.
        </div>
      </div>

      <!-- Valoración de la Farmacia (PRD-022) -->
      <div v-if="order.status === 'delivered'" class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <div v-if="order.rating" class="rounded-xl border border-emerald-200 bg-emerald-50/50 p-4" data-testid="order-rating-card">
          <div class="flex items-center gap-2">
            <span class="text-amber-500 font-bold text-lg">★</span>
            <h3 class="text-xs font-bold uppercase tracking-wider text-emerald-900">
              Tu valoración de la farmacia: {{ order.rating.score }} / 5 estrellas
            </h3>
          </div>
          <p v-if="order.rating.comment" class="mt-2 text-xs italic text-slate-700">
            "{{ order.rating.comment }}"
          </p>
          <span v-if="order.rating.rated_at" class="mt-1 block text-[10px] text-slate-400">
            Valorado el {{ new Date(order.rating.rated_at).toLocaleDateString() }}
          </span>
        </div>

        <div v-else data-testid="order-rate-form">
          <div class="flex items-center gap-2">
            <span class="text-amber-500 font-bold">⭐</span>
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-700">
              Valora tu experiencia con esta farmacia (PRD-022)
            </h3>
          </div>
          <p class="mt-1 text-xs text-slate-500">
            Tu opinión nos ayuda a medir el SLA de calidad y orientar a otros tutores de mascotas.
          </p>

          <div v-if="ratingError" class="mt-2 rounded bg-rose-50 p-2 text-xs text-rose-700">
            {{ ratingError }}
          </div>

          <div class="mt-3 flex items-center gap-2">
            <label class="text-xs font-medium text-slate-700">Puntuación:</label>
            <div class="flex gap-1">
              <button
                v-for="star in 5"
                :key="star"
                type="button"
                @click="ratingScore = star"
                class="text-xl transition-transform hover:scale-110 focus:outline-none"
                :class="star <= ratingScore ? 'text-amber-400' : 'text-slate-300'"
                :data-testid="'star-' + star"
              >
                ★
              </button>
            </div>
            <span class="text-xs font-bold text-slate-700 ml-2">{{ ratingScore }} / 5</span>
          </div>

          <div class="mt-3">
            <label class="block text-xs font-medium text-slate-700">Comentario opcional:</label>
            <textarea
              v-model="ratingComment"
              rows="2"
              maxlength="500"
              placeholder="¿Cómo fue la entrega, empaque y atención de la farmacia?"
              class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
              data-testid="rating-comment-input"
            ></textarea>
          </div>

          <div class="mt-3 flex justify-end">
            <button
              type="button"
              :disabled="submittingRating"
              @click="submitRating"
              class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
              data-testid="submit-rating-btn"
            >
              {{ submittingRating ? 'Enviando...' : 'Enviar Valoración' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Datos Compartidos con la Farmacia (ARQ-013 / SEC-005) -->
      <div class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <h3 class="text-xs font-bold uppercase tracking-wider text-slate-600">
          Datos Compartidos con la Farmacia (Minimización ARQ-013)
        </h3>
        <p class="mt-1 text-xs text-slate-500">
          De acuerdo con nuestra política de privacidad y minimización de datos, la farmacia
          únicamente tiene acceso a los siguientes datos para gestionar la entrega:
        </p>
        <ul class="mt-3 list-inside list-disc text-xs text-slate-700 space-y-1">
          <li>Medicamentos solicitados y cantidades</li>
          <li v-if="order.delivery_address">
            Dirección de entrega: {{ order.delivery_address.street }}
            {{ order.delivery_address.number }}, {{ order.delivery_address.city }}
          </li>
          <li>Teléfono de contacto de la cuenta</li>
        </ul>
      </div>
    </div>

    <!-- MODAL CANCELAR PEDIDO -->
    <div
      v-if="showCancelModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Cancelar Pedido</h3>
        <p class="text-xs text-slate-500">
          Por favor indica el motivo por el cual deseas cancelar el pedido. La farmacia será
          notificada de inmediato.
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Motivo de cancelación *</label>
          <textarea
            v-model="cancelReason"
            rows="3"
            placeholder="Ej. Cambio de planes o error en los productos"
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
            :disabled="!cancelReason.trim() || cancellingBusy"
            @click="confirmCancelOrder"
            class="rounded-lg bg-rose-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-rose-700 disabled:bg-slate-300"
          >
            {{ cancellingBusy ? 'Cancelando...' : 'Confirmar Cancelación' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
