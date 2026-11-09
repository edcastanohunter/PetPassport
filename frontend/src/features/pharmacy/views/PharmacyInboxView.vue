<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  api,
  ApiError,
  type PharmacyInboxItem,
  type SubmitPharmacyQuotePayload,
} from '@/shared/api/client'

const route = useRoute()
const orgId = route.params.orgId as string

const activeTab = ref<'pending' | 'quoted' | 'declined' | 'expired'>('pending')
const items = ref<PharmacyInboxItem[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

// Modal de Cotización
const quotingItem = ref<PharmacyInboxItem | null>(null)
interface QuoteFormLine {
  itemId: string
  productName: string
  isSubstitution: boolean
  quantity: number
  unitPriceCents: number
}
const quoteLines = ref<QuoteFormLine[]>([])
const deliveryFeeCents = ref<number>(5000)
const estimatedTime = ref<string>('')
const submittingQuote = ref(false)
const quoteError = ref<string | null>(null)

// Modal de Declinación
const decliningItem = ref<PharmacyInboxItem | null>(null)
const declineReason = ref<'no_stock' | 'out_of_coverage' | 'cannot_fulfill' | 'other'>('no_stock')
const decliningBusy = ref(false)

// Panel Lateral de Receta
const viewingPrescriptionId = ref<string | null>(null)

async function loadInbox() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/pharmacies/{orgId}/inbox', {
      params: {
        path: { orgId },
        query: { status: activeTab.value },
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    items.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar bandeja de entrada'
  } finally {
    loading.value = false
  }
}

watch(activeTab, () => {
  void loadInbox()
})

onMounted(() => {
  void loadInbox()
})

function openQuoteModal(item: PharmacyInboxItem) {
  quotingItem.value = item
  quoteError.value = null
  // Generar línea inicial
  quoteLines.value = [
    {
      itemId: 'line-1',
      productName: 'Amoxicilina 250mg',
      isSubstitution: false,
      quantity: item.items_count || 1,
      unitPriceCents: 15000,
    },
  ]
  deliveryFeeCents.value = item.fulfillment_type === 'delivery' ? 6000 : 0
  const d = new Date()
  d.setHours(d.getHours() + 2)
  estimatedTime.value = d.toISOString().slice(0, 16)
}

function addQuoteLine() {
  quoteLines.value.push({
    itemId: 'line-' + (quoteLines.value.length + 1),
    productName: '',
    isSubstitution: false,
    quantity: 1,
    unitPriceCents: 10000,
  })
}

function removeQuoteLine(idx: number) {
  if (quoteLines.value.length > 1) {
    quoteLines.value.splice(idx, 1)
  }
}

const computedTotalCents = computed(() => {
  const linesSum = quoteLines.value.reduce((acc, l) => acc + l.quantity * l.unitPriceCents, 0)
  return linesSum + (deliveryFeeCents.value || 0)
})

async function submitQuote() {
  if (!quotingItem.value) return
  submittingQuote.value = true
  quoteError.value = null
  try {
    const payload: SubmitPharmacyQuotePayload = {
      lines: quoteLines.value.map((l) => ({
        item_id: l.itemId,
        product_name: l.productName,
        substitution: l.isSubstitution,
        quantity: l.quantity,
        unit_price_cents: l.unitPriceCents,
        total_price_cents: l.quantity * l.unitPriceCents,
      })),
      delivery_fee_cents: deliveryFeeCents.value,
      total_cents: computedTotalCents.value,
      estimated_delivery_at:
        quotingItem.value.fulfillment_type === 'delivery'
          ? new Date(estimatedTime.value).toISOString()
          : undefined,
      estimated_ready_at:
        quotingItem.value.fulfillment_type === 'pickup'
          ? new Date(estimatedTime.value).toISOString()
          : undefined,
      payment_on_delivery_only: true,
    }

    const res = await api().POST('/pharmacies/{orgId}/quote-requests/{quoteRequestId}/quotes', {
      params: {
        path: {
          orgId,
          quoteRequestId: quotingItem.value.quote_request_id,
        },
      },
      body: payload,
    })
    if (res.error) throw new ApiError(res.error, res.response.status)

    quotingItem.value = null
    await loadInbox()
  } catch (e) {
    quoteError.value = e instanceof Error ? e.message : 'Error al enviar cotización'
  } finally {
    submittingQuote.value = false
  }
}

function openDeclineModal(item: PharmacyInboxItem) {
  decliningItem.value = item
  declineReason.value = 'no_stock'
}

async function confirmDecline() {
  if (!decliningItem.value) return
  decliningBusy.value = true
  try {
    const res = await api().POST('/pharmacies/{orgId}/quote-requests/{quoteRequestId}/decline', {
      params: {
        path: {
          orgId,
          quoteRequestId: decliningItem.value.quote_request_id,
        },
      },
      body: { reason: declineReason.value },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)

    decliningItem.value = null
    await loadInbox()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al declinar solicitud'
  } finally {
    decliningBusy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-6xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <nav class="mb-2 text-sm text-slate-500">
          <span>Portal de Farmacia</span>
          <span class="mx-2">/</span>
          <span class="text-slate-800">Bandeja de Entrada</span>
        </nav>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Solicitudes de Cotización Recibidas
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Revisa y responde cotizaciones de tutores en tu zona de cobertura (FE-007).
        </p>
      </div>

      <div class="flex items-center gap-2">
        <router-link
          :to="`/pharmacy/${orgId}/orders`"
          class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
        >
          Gestionar Pedidos
        </router-link>
        <router-link
          :to="`/pharmacy/${orgId}/settings`"
          class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
        >
          Configuración Técnica
        </router-link>
      </div>
    </div>

    <!-- Pestañas de estado -->
    <div class="mb-6 flex items-center gap-2 border-b border-slate-200 pb-2 text-xs">
      <button
        type="button"
        @click="activeTab = 'pending'"
        class="rounded-lg px-4 py-2 font-semibold transition"
        :class="
          activeTab === 'pending'
            ? 'bg-amber-100 text-amber-900'
            : 'text-slate-600 hover:bg-slate-100'
        "
      >
        ⏳ Pendientes
      </button>
      <button
        type="button"
        @click="activeTab = 'quoted'"
        class="rounded-lg px-4 py-2 font-semibold transition"
        :class="
          activeTab === 'quoted'
            ? 'bg-emerald-100 text-emerald-900'
            : 'text-slate-600 hover:bg-slate-100'
        "
      >
        ✓ Cotizadas
      </button>
      <button
        type="button"
        @click="activeTab = 'declined'"
        class="rounded-lg px-4 py-2 font-semibold transition"
        :class="
          activeTab === 'declined'
            ? 'bg-slate-200 text-slate-800'
            : 'text-slate-600 hover:bg-slate-100'
        "
      >
        ✗ Declinadas
      </button>
      <button
        type="button"
        @click="activeTab = 'expired'"
        class="rounded-lg px-4 py-2 font-semibold transition"
        :class="
          activeTab === 'expired'
            ? 'bg-slate-200 text-slate-800'
            : 'text-slate-600 hover:bg-slate-100'
        "
      >
        ⏱️ Vencidas
      </button>
    </div>

    <!-- Error -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="py-12 text-center text-slate-500">Cargando solicitudes...</div>

    <!-- Lista vacía -->
    <div
      v-else-if="items.length === 0"
      class="rounded-xl border border-dashed border-slate-300 p-12 text-center"
    >
      <span class="text-3xl">📬</span>
      <h3 class="mt-2 text-base font-semibold text-slate-800">
        No hay solicitudes en esta pestaña
      </h3>
      <p class="mt-1 text-xs text-slate-500">
        Las solicitudes de cotización aparecerán automáticamente cuando los tutores las envíen.
      </p>
    </div>

    <!-- Grid de Solicitudes -->
    <div v-else class="space-y-4">
      <div
        v-for="it in items"
        :key="it.quote_request_id"
        class="flex flex-col justify-between gap-4 rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition hover:shadow-md sm:flex-row sm:items-center"
      >
        <div>
          <div class="flex items-center gap-2">
            <span
              class="rounded-full px-2.5 py-0.5 text-xs font-semibold"
              :class="
                it.status === 'quoted'
                  ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                  : it.status === 'declined'
                    ? 'bg-slate-100 text-slate-600'
                    : 'bg-amber-50 text-amber-700 border border-amber-200'
              "
            >
              {{
                it.status === 'quoted'
                  ? 'Cotizada'
                  : it.status === 'declined'
                    ? 'Declinada'
                    : 'Pendiente de Respuesta'
              }}
            </span>
            <span class="text-xs text-slate-400">·</span>
            <span class="text-xs text-slate-600">
              Especie: <strong class="text-slate-900">{{ it.pet_species }}</strong>
            </span>
            <span class="text-xs text-slate-400">·</span>
            <span class="text-xs text-slate-600">
              Modalidad:
              <strong>{{
                it.fulfillment_type === 'delivery'
                  ? 'Domicilio (' + (it.delivery_city || 'Zona urbana') + ')'
                  : 'Retiro en local'
              }}</strong>
            </span>
          </div>

          <div class="mt-2 flex flex-wrap gap-4 text-xs text-slate-500">
            <span>💊 {{ it.items_count }} medicamento(s) solicitados</span>
            <span>📅 Recibida: {{ new Date(it.invited_at).toLocaleTimeString() }}</span>
            <span>⏱️ Vence: {{ new Date(it.expires_at).toLocaleTimeString() }}</span>
          </div>
        </div>

        <!-- Acciones para la farmacia -->
        <div class="flex items-center gap-2">
          <button
            type="button"
            @click="viewingPrescriptionId = it.prescription_id"
            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50"
          >
            👁️ Ver Receta
          </button>

          <template v-if="it.status === 'pending'">
            <button
              type="button"
              @click="openDeclineModal(it)"
              class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 hover:bg-rose-100"
            >
              Declinar
            </button>
            <button
              type="button"
              @click="openQuoteModal(it)"
              class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700"
            >
              Cotizar Oferta
            </button>
          </template>
        </div>
      </div>
    </div>

    <!-- MODAL COTIZAR OFERTA (FE-007) -->
    <div
      v-if="quotingItem"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div
        class="w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-2xl bg-white p-6 shadow-xl space-y-5"
      >
        <div class="flex items-center justify-between border-b pb-3">
          <h3 class="text-lg font-bold text-slate-900">Enviar Cotización a Tutor</h3>
          <button
            type="button"
            @click="quotingItem = null"
            class="text-slate-400 hover:text-slate-600"
          >
            ✕
          </button>
        </div>

        <div v-if="quoteError" class="rounded bg-rose-50 p-3 text-xs text-rose-700">
          {{ quoteError }}
        </div>

        <!-- Líneas de cotización -->
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <h4 class="text-xs font-bold uppercase text-slate-500">Medicamentos y Precios</h4>
            <button
              type="button"
              @click="addQuoteLine"
              class="text-xs font-semibold text-emerald-600 hover:underline"
            >
              + Agregar Producto
            </button>
          </div>

          <div
            v-for="(line, idx) in quoteLines"
            :key="line.itemId"
            class="rounded-lg border border-slate-200 p-3 text-xs space-y-3 bg-slate-50"
          >
            <div class="flex items-center justify-between">
              <span class="font-semibold text-slate-800">Producto #{{ idx + 1 }}</span>
              <button
                v-if="quoteLines.length > 1"
                type="button"
                @click="removeQuoteLine(idx)"
                class="text-rose-600 hover:underline text-[11px]"
              >
                Eliminar
              </button>
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div class="sm:col-span-2">
                <label class="block text-[11px] font-medium text-slate-600"
                  >Nombre del Producto / SKU *</label
                >
                <input
                  type="text"
                  v-model="line.productName"
                  placeholder="Ej. Amoxicilina Genfar 250mg"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs focus:border-emerald-500 focus:outline-none"
                />
              </div>

              <div>
                <label class="block text-[11px] font-medium text-slate-600">Cantidad *</label>
                <input
                  type="number"
                  min="1"
                  v-model.number="line.quantity"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs focus:border-emerald-500 focus:outline-none"
                />
              </div>

              <div>
                <label class="block text-[11px] font-medium text-slate-600"
                  >Precio Unitario (COP) *</label
                >
                <input
                  type="number"
                  min="0"
                  step="500"
                  v-model.number="line.unitPriceCents"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs focus:border-emerald-500 focus:outline-none"
                />
              </div>

              <div class="sm:col-span-2 flex items-center pt-4">
                <label class="flex items-center gap-2 cursor-pointer text-slate-700">
                  <input
                    type="checkbox"
                    v-model="line.isSubstitution"
                    class="h-3.5 w-3.5 rounded border-slate-300 text-emerald-600"
                  />
                  <span>Es un producto sustituto / genérico equivalente</span>
                </label>
              </div>
            </div>
          </div>
        </div>

        <!-- Costos y Tiempos -->
        <div class="grid grid-cols-1 gap-4 border-t pt-4 sm:grid-cols-2 text-xs">
          <div>
            <label class="block font-medium text-slate-700">Costo de Domicilio (COP)</label>
            <input
              type="number"
              min="0"
              step="500"
              v-model.number="deliveryFeeCents"
              class="mt-1 w-full rounded border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
            />
          </div>

          <div>
            <label class="block font-medium text-slate-700"
              >Tiempo Estimado de Entrega / Retiro (ETA)</label
            >
            <input
              type="datetime-local"
              v-model="estimatedTime"
              class="mt-1 w-full rounded border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
            />
          </div>
        </div>

        <!-- Total Calculado -->
        <div
          class="rounded-lg bg-emerald-50 p-4 text-emerald-900 flex justify-between items-center text-sm font-bold"
        >
          <span>Total de la Oferta al Tutor:</span>
          <span class="font-mono text-base"
            >${{ computedTotalCents.toLocaleString('es-CO') }} COP</span
          >
        </div>

        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="quotingItem = null"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="submittingQuote"
            @click="submitQuote"
            class="rounded-lg bg-emerald-600 px-6 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            {{ submittingQuote ? 'Enviando Oferta...' : 'Enviar Cotización' }}
          </button>
        </div>
      </div>
    </div>

    <!-- DIÁLOGO DECLINAR COTIZACIÓN -->
    <div
      v-if="decliningItem"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">Declinar Solicitud de Cotización</h3>
        <p class="text-xs text-slate-500">
          Indica el motivo por el cual la farmacia no puede cotizar esta receta:
        </p>
        <div>
          <label class="block text-xs font-medium text-slate-700">Motivo de declinación *</label>
          <select
            v-model="declineReason"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs focus:border-emerald-500 focus:outline-none"
          >
            <option value="no_stock">Sin stock de los productos</option>
            <option value="out_of_coverage">Fuera de zona de cobertura</option>
            <option value="cannot_fulfill">No es posible despachar en el tiempo requerido</option>
            <option value="other">Otro motivo</option>
          </select>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button
            type="button"
            @click="decliningItem = null"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Volver
          </button>
          <button
            type="button"
            :disabled="decliningBusy"
            @click="confirmDecline"
            class="rounded-lg bg-rose-600 px-5 py-2 text-xs font-semibold text-white shadow hover:bg-rose-700 disabled:bg-slate-300"
          >
            {{ decliningBusy ? 'Declinando...' : 'Confirmar Declinación' }}
          </button>
        </div>
      </div>
    </div>

    <!-- VISOR LATERAL DE RECETA -->
    <div
      v-if="viewingPrescriptionId"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-lg rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <div class="flex items-center justify-between border-b pb-3">
          <h3 class="text-base font-bold text-slate-900">Verificación de Receta Médica</h3>
          <button
            type="button"
            @click="viewingPrescriptionId = null"
            class="text-slate-400 hover:text-slate-600"
          >
            ✕
          </button>
        </div>

        <p class="text-xs text-slate-600">
          ID de la receta médica:
          <code class="font-mono bg-slate-100 px-1 py-0.5 rounded">{{
            viewingPrescriptionId
          }}</code>
        </p>

        <div class="rounded-lg bg-slate-50 border p-4 text-xs space-y-2">
          <p class="font-medium text-slate-800">Acceso seguro a verificación:</p>
          <p class="text-slate-500">
            Puedes consultar la autenticidad e integridad de la receta en el servicio público de
            verificación o validar recetas externas.
          </p>
          <div class="pt-2">
            <a
              :href="'/verify/prescriptions/' + viewingPrescriptionId"
              target="_blank"
              class="inline-flex items-center gap-1 font-semibold text-emerald-600 hover:underline"
            >
              <span>Abrir verificación oficial de receta ↗</span>
            </a>
          </div>
        </div>

        <div class="flex justify-end pt-2">
          <button
            type="button"
            @click="viewingPrescriptionId = null"
            class="rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white"
          >
            Cerrar
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
