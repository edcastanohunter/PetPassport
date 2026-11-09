<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  ApiError,
  type QuoteRequestDetail,
  type SelectQuotePayload,
} from '@/shared/api/client'

const route = useRoute()
const router = useRouter()
const quoteRequestId = route.params.id as string

const loading = ref(false)
const error = ref<string | null>(null)
const quoteRequest = ref<QuoteRequestDetail | null>(null)

// Modal de Selección y Consentimiento (ARQ-013 / SEC-005)
interface QuoteItem {
  quote_id: string
  pharmacy_id: string
  pharmacy_name: string
  lines: Array<{
    item_id: string
    product_name: string
    substitution: boolean
    quantity: number
    unit_price_cents: number
    total_price_cents: number
  }>
  delivery_fee_cents: number
  total_cents: number
  estimated_delivery_at?: string
  estimated_ready_at?: string
}
const selectedQuoteForModal = ref<QuoteItem | null>(null)
const consentGranted = ref(false)
const selectingBusy = ref(false)
const selectError = ref<string | null>(null)

// Modal de Revisión de Sustituciones
const reviewingQuote = ref<QuoteItem | null>(null)
const acceptedSubstitutions = ref<Record<string, boolean>>({})

// Diálogo Cancelar Solicitud
const showCancelDialog = ref(false)
const cancelReason = ref('')
const cancellingBusy = ref(false)

// SSE y Polling Fallback
let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null

async function loadDetail() {
  try {
    const res = await api().GET('/quote-requests/{quoteRequestId}', {
      params: { path: { quoteRequestId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data) {
      quoteRequest.value = res.data
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar solicitud'
  }
}

// Countdown timer
const now = ref(Date.now())
let clockTimer: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  loading.value = true
  await loadDetail()
  loading.value = false

  clockTimer = setInterval(() => {
    now.value = Date.now()
  }, 1000)

  initSSE()
})

onUnmounted(() => {
  if (clockTimer) clearInterval(clockTimer)
  if (pollTimer) clearInterval(pollTimer)
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
})

function initSSE() {
  try {
    const sseUrl = `/api/v1/quote-requests/${quoteRequestId}/events`
    eventSource = new EventSource(sseUrl)

    eventSource.onmessage = () => {
      void loadDetail()
    }

    eventSource.onerror = () => {
      // Si falla SSE, cerramos e iniciamos polling fallback cada 10s
      if (eventSource) {
        eventSource.close()
        eventSource = null
      }
      if (!pollTimer) {
        pollTimer = setInterval(() => {
          void loadDetail()
        }, 10000)
      }
    }
  } catch {
    // Fallback a polling
    if (!pollTimer) {
      pollTimer = setInterval(() => {
        void loadDetail()
      }, 10000)
    }
  }
}

// Tiempo restante
const timeLeftFormatted = computed(() => {
  if (!quoteRequest.value?.expires_at) return null
  const exp = new Date(quoteRequest.value.expires_at).getTime()
  const diff = exp - now.value
  if (diff <= 0) return 'Vencida'
  const hours = Math.floor(diff / (1000 * 60 * 60))
  const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60))
  const seconds = Math.floor((diff % (1000 * 60)) / 1000)
  return `${hours}h ${minutes}m ${seconds}s`
})

function formatCents(cents: number) {
  return new Intl.NumberFormat('es-CO', {
    style: 'currency',
    currency: 'COP',
    maximumFractionDigits: 0,
  }).format(cents)
}

function openSelectModal(quote: QuoteItem) {
  selectedQuoteForModal.value = quote
  consentGranted.value = false
  selectError.value = null
}

function openSubstitutionsModal(quote: QuoteItem) {
  reviewingQuote.value = quote
  // Inicializar aceptadas por defecto
  quote.lines.forEach((l) => {
    if (l.substitution) {
      acceptedSubstitutions.value[l.item_id] = true
    }
  })
}

async function confirmSelectQuote() {
  if (!selectedQuoteForModal.value || !consentGranted.value) return
  selectingBusy.value = true
  selectError.value = null
  try {
    const q = selectedQuoteForModal.value
    const acceptedList = Object.keys(acceptedSubstitutions.value).filter(
      (id) => acceptedSubstitutions.value[id],
    )

    const payload: SelectQuotePayload = {
      pharmacy_id: q.pharmacy_id,
      quote_id: q.quote_id,
      accepted_substitutions: acceptedList.length > 0 ? acceptedList : undefined,
      consent_to_share_details: true,
    }

    const res = await api().POST('/quote-requests/{quoteRequestId}/select', {
      params: { path: { quoteRequestId } },
      body: payload,
    })
    if (res.error) throw new ApiError(res.error, res.response.status)

    const orderId = res.data?.order_id
    if (orderId) {
      await router.push({ name: 'order-detail', params: { id: orderId } })
    } else {
      await router.push({ name: 'orders' })
    }
  } catch (e) {
    selectError.value = e instanceof Error ? e.message : 'Error al seleccionar cotización'
  } finally {
    selectingBusy.value = false
  }
}

async function cancelRequest() {
  if (!cancelReason.value.trim()) return
  cancellingBusy.value = true
  try {
    const res = await api().POST('/quote-requests/{quoteRequestId}/cancel', {
      params: { path: { quoteRequestId } },
      body: { reason: cancelReason.value },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showCancelDialog.value = false
    await loadDetail()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cancelar solicitud'
  } finally {
    cancellingBusy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-6xl px-4 py-8">
    <!-- Breadcrumb & Header -->
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <nav class="mb-2 text-sm text-slate-500">
          <router-link to="/app/quote-requests" class="hover:text-emerald-600"
            >Mis Cotizaciones</router-link
          >
          <span class="mx-2">/</span>
          <span class="text-slate-800">Tablero Comparativo</span>
        </nav>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Tablero de Cotizaciones Recibidas
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Compara las ofertas de farmacias aliadas y elige la mejor opción (FE-005).
        </p>
      </div>

      <div class="flex items-center gap-3">
        <button
          v-if="quoteRequest?.status === 'open'"
          type="button"
          @click="showCancelDialog = true"
          class="rounded-lg border border-rose-300 px-4 py-2 text-xs font-semibold text-rose-700 hover:bg-rose-50"
        >
          Cancelar Solicitud
        </button>
      </div>
    </div>

    <!-- Alert de Error -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>

    <div v-if="loading" class="py-12 text-center text-slate-500">
      Cargando tablero de cotizaciones...
    </div>

    <div v-else-if="quoteRequest" class="space-y-8">
      <!-- Status & Countdown Banner -->
      <div
        class="flex flex-col justify-between gap-4 rounded-xl border p-5 sm:flex-row sm:items-center"
        :class="
          quoteRequest.status === 'open'
            ? 'border-emerald-200 bg-emerald-50/50'
            : quoteRequest.status === 'selected'
              ? 'border-blue-200 bg-blue-50/50'
              : 'border-slate-200 bg-slate-50'
        "
      >
        <div class="flex items-center gap-3">
          <span
            class="rounded-full px-3 py-1 text-xs font-bold uppercase tracking-wider"
            :class="
              quoteRequest.status === 'open'
                ? 'bg-emerald-600 text-white'
                : quoteRequest.status === 'selected'
                  ? 'bg-blue-600 text-white'
                  : 'bg-slate-400 text-white'
            "
          >
            {{
              quoteRequest.status === 'open'
                ? 'Abierta para cotizaciones'
                : quoteRequest.status === 'selected'
                  ? 'Cotización Seleccionada'
                  : 'Cerrada / ' + quoteRequest.status
            }}
          </span>
          <span class="text-xs text-slate-600">
            Modalidad:
            <strong class="text-slate-900">{{
              quoteRequest.fulfillment_type === 'delivery' ? 'Domicilio' : 'Retiro'
            }}</strong>
          </span>
        </div>

        <div v-if="quoteRequest.status === 'open'" class="flex items-center gap-2 text-sm">
          <span class="text-slate-500">⏱️ Tiempo restante para responder:</span>
          <strong class="font-mono text-emerald-800 font-semibold">{{ timeLeftFormatted }}</strong>
        </div>
      </div>

      <!-- Resumen de farmacias invitadas -->
      <div class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 class="text-sm font-semibold text-slate-900">Estado de Farmacias Invitadas</h2>
        <div class="mt-3 flex flex-wrap gap-3">
          <div
            v-for="inv in quoteRequest.invitations"
            :key="inv.pharmacy_id"
            class="flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium"
            :class="
              inv.status === 'quoted'
                ? 'border-emerald-300 bg-emerald-50 text-emerald-800'
                : inv.status === 'declined'
                  ? 'border-slate-200 bg-slate-50 text-slate-500 line-through'
                  : 'border-amber-200 bg-amber-50 text-amber-800'
            "
          >
            <span>{{ inv.pharmacy_name }}</span>
            <span class="text-[10px] font-bold uppercase">
              {{
                inv.status === 'quoted'
                  ? '✓ Cotizó'
                  : inv.status === 'declined'
                    ? '✗ Declinó'
                    : '⏳ Pendiente'
              }}
            </span>
          </div>
        </div>
      </div>

      <!-- Grid de Cotizaciones Recibidas -->
      <div>
        <div class="mb-4 flex items-center justify-between">
          <h2 class="text-lg font-bold text-slate-900">
            Cotizaciones Recibidas ({{ quoteRequest.quotes.length }})
          </h2>
          <span class="text-xs text-slate-500"> Actualizaciones en tiempo real activas </span>
        </div>

        <div
          v-if="quoteRequest.quotes.length === 0"
          class="rounded-xl border border-dashed border-slate-300 p-12 text-center"
        >
          <span class="text-3xl">⏳</span>
          <h3 class="mt-2 text-base font-semibold text-slate-800">
            Esperando ofertas de farmacias...
          </h3>
          <p class="mt-1 text-xs text-slate-500 max-w-md mx-auto">
            Las farmacias invitadas están revisando tu receta y disponibilidad de stock. Las
            cotizaciones aparecerán aquí automáticamente en tiempo real.
          </p>
        </div>

        <div v-else class="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="q in quoteRequest.quotes"
            :key="q.quote_id"
            class="flex flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition hover:shadow-md"
          >
            <div>
              <div class="flex items-start justify-between">
                <div>
                  <h3 class="text-base font-bold text-slate-900">{{ q.pharmacy_name }}</h3>
                  <p class="text-xs text-slate-500">Cotización formal</p>
                </div>
                <span
                  class="rounded bg-emerald-50 px-2 py-0.5 text-xs font-semibold text-emerald-700 border border-emerald-200"
                >
                  Total: {{ formatCents(q.total_cents) }}
                </span>
              </div>

              <!-- Desglose de ítems -->
              <div class="mt-4 space-y-2 border-t border-slate-100 pt-3 text-xs">
                <div
                  v-for="line in q.lines"
                  :key="line.item_id"
                  class="flex justify-between items-center text-slate-700"
                >
                  <div class="flex items-center gap-1.5">
                    <span>{{ line.product_name }} × {{ line.quantity }}</span>
                    <span
                      v-if="line.substitution"
                      class="rounded bg-purple-100 px-1 text-[10px] font-semibold text-purple-800"
                    >
                      Sustituto
                    </span>
                  </div>
                  <span class="font-medium text-slate-900">{{
                    formatCents(line.total_price_cents)
                  }}</span>
                </div>

                <div
                  v-if="q.delivery_fee_cents > 0"
                  class="flex justify-between text-slate-500 pt-1 border-t border-dashed border-slate-100"
                >
                  <span>Envío a domicilio</span>
                  <span>{{ formatCents(q.delivery_fee_cents) }}</span>
                </div>
              </div>

              <!-- Tiempos de entrega -->
              <div class="mt-4 rounded-lg bg-slate-50 p-2.5 text-xs text-slate-600 space-y-1">
                <div v-if="q.estimated_delivery_at">
                  🚚 <strong>Entrega estimada:</strong>
                  {{ new Date(q.estimated_delivery_at).toLocaleString() }}
                </div>
                <div v-else-if="q.estimated_ready_at">
                  🏬 <strong>Listo para retiro:</strong>
                  {{ new Date(q.estimated_ready_at).toLocaleString() }}
                </div>
                <div class="text-[11px] text-amber-700 font-medium">
                  💳 Pago contra entrega / en farmacia
                </div>
              </div>
            </div>

            <!-- Botones de Acción -->
            <div class="mt-5 space-y-2 border-t border-slate-100 pt-3">
              <button
                v-if="q.lines.some((l) => l.substitution)"
                type="button"
                @click="openSubstitutionsModal(q)"
                class="w-full rounded-md border border-purple-200 bg-purple-50 py-1.5 text-xs font-semibold text-purple-700 hover:bg-purple-100"
              >
                🔍 Ver sustitutos propuestos
              </button>

              <button
                v-if="quoteRequest.status === 'open'"
                type="button"
                @click="openSelectModal(q)"
                class="w-full rounded-lg bg-emerald-600 py-2.5 text-xs font-bold text-white shadow hover:bg-emerald-700 transition"
              >
                Seleccionar esta Oferta
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- MODAL DE SELECCIÓN Y CONSENTIMIENTO (ARQ-013 / SEC-005) -->
    <div
      v-if="selectedQuoteForModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-lg rounded-2xl bg-white p-6 shadow-xl space-y-5">
        <h3 class="text-lg font-bold text-slate-900">Confirmar Selección de Cotización</h3>
        <p class="text-xs text-slate-500">
          Estás por seleccionar la oferta de
          <strong>{{ selectedQuoteForModal.pharmacy_name }}</strong> por un total de
          <strong class="text-emerald-700">{{
            formatCents(selectedQuoteForModal.total_cents)
          }}</strong
          >.
        </p>

        <!-- Error en modal -->
        <div v-if="selectError" class="rounded bg-rose-50 p-3 text-xs text-rose-700">
          {{ selectError }}
        </div>

        <!-- Alerta de Pago en Farmacia (RN-26 / PRD-021) -->
        <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900">
          <strong>Aviso de pago:</strong>
          El pago no se realiza a través de la plataforma. Abonarás los
          {{ formatCents(selectedQuoteForModal.total_cents) }} directamente a la farmacia contra
          entrega o al retirar los productos.
        </div>

        <!-- Checkbox de Consentimiento de Datos (ARQ-013 / SEC-005) -->
        <label
          class="flex items-start gap-3 rounded-lg border border-slate-200 p-3 text-xs text-slate-700 cursor-pointer bg-slate-50"
        >
          <input
            type="checkbox"
            v-model="consentGranted"
            class="mt-0.5 h-4 w-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
          />
          <span>
            <strong>Consentimiento de datos compartidos:</strong> Autorizo expresamente compartir mi
            nombre, teléfono de contacto y dirección de entrega con
            <em>{{ selectedQuoteForModal.pharmacy_name }}</em> exclusivamente para la preparación y
            despacho de este pedido.
          </span>
        </label>

        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="selectedQuoteForModal = null"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="!consentGranted || selectingBusy"
            @click="confirmSelectQuote"
            class="rounded-lg bg-emerald-600 px-6 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300 disabled:cursor-not-allowed"
          >
            {{ selectingBusy ? 'Procesando Pedido...' : 'Confirmar Pedido' }}
          </button>
        </div>
      </div>
    </div>

    <!-- MODAL DE REVISIÓN DE SUSTITUCIONES -->
    <div
      v-if="reviewingQuote"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-lg rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Sustitutos Propuestos por la Farmacia</h3>
        <p class="text-xs text-slate-500">
          La farmacia <strong>{{ reviewingQuote.pharmacy_name }}</strong> ha propuesto alternativas
          equivalentes para los siguientes medicamentos prescritos:
        </p>

        <div class="divide-y divide-slate-100 max-h-60 overflow-y-auto">
          <div
            v-for="l in reviewingQuote.lines.filter((x) => x.substitution)"
            :key="l.item_id"
            class="py-3 text-xs space-y-2"
          >
            <div class="flex items-center justify-between font-semibold text-slate-900">
              <span>{{ l.product_name }}</span>
              <span>{{ formatCents(l.total_price_cents) }}</span>
            </div>
            <label class="flex items-center gap-2 cursor-pointer text-slate-700">
              <input
                type="checkbox"
                v-model="acceptedSubstitutions[l.item_id]"
                class="h-4 w-4 rounded border-slate-300 text-emerald-600"
              />
              <span>Acepto este medicamento sustituto propuesto</span>
            </label>
          </div>
        </div>

        <div class="flex justify-end pt-3">
          <button
            type="button"
            @click="reviewingQuote = null"
            class="rounded-lg bg-slate-800 px-5 py-2 text-xs font-semibold text-white hover:bg-slate-900"
          >
            Entendido
          </button>
        </div>
      </div>
    </div>

    <!-- DIÁLOGO CANCELAR SOLICITUD -->
    <div
      v-if="showCancelDialog"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Cancelar Solicitud de Cotización</h3>
        <p class="text-xs text-slate-500">
          Si cancelas, las farmacias invitadas serán notificadas y no podrás seleccionar ninguna
          cotización recibida.
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Motivo de cancelación *</label>
          <textarea
            v-model="cancelReason"
            rows="3"
            placeholder="Ej. Conseguí los medicamentos en otra parte o ya no los necesito"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          ></textarea>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="showCancelDialog = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Volver
          </button>
          <button
            type="button"
            :disabled="!cancelReason.trim() || cancellingBusy"
            @click="cancelRequest"
            class="rounded-lg bg-rose-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-rose-700 disabled:bg-slate-300"
          >
            {{ cancellingBusy ? 'Cancelando...' : 'Confirmar Cancelación' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
