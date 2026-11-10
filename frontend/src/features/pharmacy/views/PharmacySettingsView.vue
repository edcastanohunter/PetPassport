<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  api,
  ApiError,
  type PharmacyIntegrationSettings,
  type PharmacyProductMappingItem,
} from '@/shared/api/client'
import type { components } from '@/shared/api/schema'

type PharmacyApiCredentials = components['schemas']['PharmacyApiCredentialsResponse']
type PharmacyRotatedCredentials = components['schemas']['PharmacyRotatedCredentialsResponse']
type PharmacySandboxCredentials = components['schemas']['PharmacySandboxCredentialsResponse']
type PharmacyWebhookConfig = components['schemas']['PharmacyWebhookConfigResponse']
type PharmacyWebhookDeliveryItem = components['schemas']['PharmacyWebhookDeliveryItem']
type PharmacyStats = components['schemas']['PharmacyStatsResponse']

const route = useRoute()
const orgId = route.params.orgId as string

type ActiveTab = 'general' | 'catalog' | 'credentials' | 'webhooks' | 'stats'
const activeTab = ref<ActiveTab>('general')

const loading = ref(false)
const savingSettings = ref(false)
const error = ref<string | null>(null)
const successMsg = ref<string | null>(null)

// 1. Configuración de Integración General y Áreas
const integration = ref<PharmacyIntegrationSettings>({
  level: 'portal',
  webhook_url: '',
  catalog_mapping_enabled: true,
  delivery_areas: [{ city: 'Bogotá', postal_codes: ['110111', '110221'] }],
})

// 2. Mapeos de catálogo
const mappings = ref<PharmacyProductMappingItem[]>([])
const loadingMappings = ref(false)
const showMappingModal = ref(false)
const editingCatalogId = ref<string | null>(null)
const mappingForm = ref({
  catalogId: '',
  sku: '',
  name: '',
  priceCents: 15000,
  inStock: true,
})
const savingMapping = ref(false)
const mappingError = ref<string | null>(null)

// 3. Credenciales API & Sandbox (FE-010, PTN-002, PTN-005)
const credentials = ref<PharmacyApiCredentials | null>(null)
const loadingCredentials = ref(false)
const rotatingCredentials = ref(false)
const rotatedSecretData = ref<PharmacyRotatedCredentials | null>(null)
const generatingSandbox = ref(false)
const sandboxData = ref<PharmacySandboxCredentials | null>(null)
const copiedSecret = ref(false)
const copiedSandbox = ref(false)

// 4. Webhooks y Registro de Entregas (FE-010, PTN-004, SEC-003)
const webhookConfig = ref<PharmacyWebhookConfig | null>(null)
const webhookForm = ref({
  url: '',
  events: ['quote_request.created', 'order.placed', 'order.updated', 'order.cancelled'],
  secret: '',
})
const loadingWebhook = ref(false)
const savingWebhook = ref(false)
const testingWebhook = ref(false)
const testWebhookResult = ref<{ status: string; status_code?: number; error?: string } | null>(null)
const deliveries = ref<PharmacyWebhookDeliveryItem[]>([])
const loadingDeliveries = ref(false)
const retryingDeliveryId = ref<string | null>(null)

// 5. Métricas y SLA (FE-010, PRD-016, PTN-008)
const stats = ref<PharmacyStats | null>(null)
const loadingStats = ref(false)

async function loadSettings() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/pharmacies/{orgId}/integration', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data) {
      integration.value = res.data
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar ajustes de integración'
  } finally {
    loading.value = false
  }
}

async function loadMappings() {
  loadingMappings.value = true
  try {
    const res = await api().GET('/pharmacies/{orgId}/product-mappings', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    mappings.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar mapeos de catálogo'
  } finally {
    loadingMappings.value = false
  }
}

async function loadCredentials() {
  loadingCredentials.value = true
  try {
    const res = await api().GET('/pharmacies/{orgId}/api-credentials', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    credentials.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar credenciales API'
  } finally {
    loadingCredentials.value = false
  }
}

async function rotateCredentials() {
  if (
    !confirm(
      '¿Rotar el secreto de API? La credencial anterior dejará de ser válida tras el período de gracia.',
    )
  )
    return
  rotatingCredentials.value = true
  error.value = null
  try {
    const res = await api().POST('/pharmacies/{orgId}/api-credentials/rotate', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    rotatedSecretData.value = res.data ?? null
    await loadCredentials()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al rotar credenciales API'
  } finally {
    rotatingCredentials.value = false
  }
}

async function generateSandboxCredentials() {
  generatingSandbox.value = true
  error.value = null
  try {
    const res = await api().POST('/pharmacies/{orgId}/api-credentials/sandbox', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    sandboxData.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al generar credenciales sandbox'
  } finally {
    generatingSandbox.value = false
  }
}

async function loadWebhookConfig() {
  loadingWebhook.value = true
  try {
    const res = await api().GET('/pharmacies/{orgId}/webhook', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    webhookConfig.value = res.data ?? null
    if (res.data) {
      webhookForm.value.url = res.data.url ?? ''
      webhookForm.value.events = res.data.events ?? [
        'quote_request.created',
        'order.placed',
        'order.updated',
        'order.cancelled',
      ]
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar configuración de webhook'
  } finally {
    loadingWebhook.value = false
  }
}

async function saveWebhook() {
  savingWebhook.value = true
  error.value = null
  successMsg.value = null
  try {
    const res = await api().PUT('/pharmacies/{orgId}/webhook', {
      params: { path: { orgId } },
      body: {
        url: webhookForm.value.url,
        events: webhookForm.value.events,
        secret: webhookForm.value.secret ? webhookForm.value.secret : undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    webhookConfig.value = res.data ?? null
    successMsg.value = 'Configuración de webhook actualizada exitosamente.'
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al guardar webhook'
  } finally {
    savingWebhook.value = false
  }
}

async function testWebhook() {
  testingWebhook.value = true
  testWebhookResult.value = null
  try {
    const res = await api().POST('/pharmacies/{orgId}/webhook/test', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    testWebhookResult.value = res.data ?? null
    await loadDeliveries()
  } catch (e) {
    testWebhookResult.value = {
      status: 'failed',
      error: e instanceof Error ? e.message : 'Error al enviar webhook de prueba',
    }
  } finally {
    testingWebhook.value = false
  }
}

async function loadDeliveries() {
  loadingDeliveries.value = true
  try {
    const res = await api().GET('/pharmacies/{orgId}/webhook/deliveries', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    deliveries.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar historial de entregas'
  } finally {
    loadingDeliveries.value = false
  }
}

async function retryDelivery(deliveryId: string) {
  retryingDeliveryId.value = deliveryId
  try {
    const res = await api().POST('/pharmacies/{orgId}/webhook/deliveries/{deliveryId}/retry', {
      params: { path: { orgId, deliveryId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    await loadDeliveries()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al reintentar entrega'
  } finally {
    retryingDeliveryId.value = null
  }
}

async function loadStats() {
  loadingStats.value = true
  try {
    const res = await api().GET('/pharmacies/{orgId}/stats', {
      params: { path: { orgId } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    stats.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar métricas de farmacia'
  } finally {
    loadingStats.value = false
  }
}

onMounted(async () => {
  await Promise.all([
    loadSettings(),
    loadMappings(),
    loadCredentials(),
    loadWebhookConfig(),
    loadDeliveries(),
    loadStats(),
  ])
})

async function saveSettings() {
  savingSettings.value = true
  error.value = null
  successMsg.value = null
  try {
    const res = await api().PUT('/pharmacies/{orgId}/integration', {
      params: { path: { orgId } },
      body: {
        level: integration.value.level,
        webhook_url:
          integration.value.level === 'direct_integration'
            ? integration.value.webhook_url
            : undefined,
        catalog_mapping_enabled: integration.value.catalog_mapping_enabled,
        delivery_areas: integration.value.delivery_areas,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    successMsg.value = 'Configuración técnica guardada exitosamente.'
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al guardar configuración'
  } finally {
    savingSettings.value = false
  }
}

function openAddMappingModal() {
  editingCatalogId.value = null
  mappingError.value = null
  mappingForm.value = {
    catalogId: '01930000-0000-7000-8000-00000000f001',
    sku: 'SKU-' + Math.floor(1000 + Math.random() * 9000),
    name: '',
    priceCents: 15000,
    inStock: true,
  }
  showMappingModal.value = true
}

function openEditMappingModal(item: PharmacyProductMappingItem) {
  editingCatalogId.value = item.catalog_id
  mappingError.value = null
  mappingForm.value = {
    catalogId: item.catalog_id,
    sku: item.pharmacy_sku,
    name: item.pharmacy_name,
    priceCents: item.price_cents,
    inStock: item.in_stock,
  }
  showMappingModal.value = true
}

async function saveMapping() {
  if (!mappingForm.value.name.trim() || !mappingForm.value.sku.trim()) return
  savingMapping.value = true
  mappingError.value = null
  try {
    const catId = editingCatalogId.value || mappingForm.value.catalogId
    const res = await api().PUT('/pharmacies/{orgId}/product-mappings/{catalogId}', {
      params: {
        path: { orgId, catalogId: catId },
      },
      body: {
        pharmacy_sku: mappingForm.value.sku,
        pharmacy_name: mappingForm.value.name,
        price_cents: mappingForm.value.priceCents,
        in_stock: mappingForm.value.inStock,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    showMappingModal.value = false
    await loadMappings()
  } catch (e) {
    mappingError.value = e instanceof Error ? e.message : 'Error al guardar mapeo'
  } finally {
    savingMapping.value = false
  }
}

async function deleteMapping(catalogId: string) {
  if (!confirm('¿Deseas eliminar este mapeo de producto?')) return
  try {
    const res = await api().DELETE('/pharmacies/{orgId}/product-mappings/{catalogId}', {
      params: {
        path: { orgId, catalogId },
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    await loadMappings()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al eliminar mapeo'
  }
}

function addDeliveryArea() {
  integration.value.delivery_areas.push({
    city: '',
    postal_codes: [],
  })
}

function removeDeliveryArea(idx: number) {
  if (integration.value.delivery_areas.length > 1) {
    integration.value.delivery_areas.splice(idx, 1)
  }
}

function copyToClipboard(text: string, isSandbox = false) {
  navigator.clipboard.writeText(text)
  if (isSandbox) {
    copiedSandbox.value = true
    setTimeout(() => {
      copiedSandbox.value = false
    }, 2500)
  } else {
    copiedSecret.value = true
    setTimeout(() => {
      copiedSecret.value = false
    }, 2500)
  }
}
</script>

<template>
  <div class="mx-auto max-w-5xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <nav class="mb-2 text-sm text-slate-500">
          <span>Portal de Farmacia</span>
          <span class="mx-2">/</span>
          <span class="text-slate-800">Configuración</span>
        </nav>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Configuración Técnica de Farmacia
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Modalidades de integración, credenciales API, webhooks y métricas (FE-009, FE-010).
        </p>
      </div>

      <div class="flex items-center gap-2">
        <a
          href="/developers/index.html"
          target="_blank"
          class="rounded-lg border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-xs font-semibold text-indigo-700 shadow-sm hover:bg-indigo-100"
        >
          📘 Portal Desarrolladores
        </a>
        <router-link
          :to="`/pharmacy/${orgId}/orders`"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
        >
          Ver Pedidos
        </router-link>
      </div>
    </div>

    <!-- Pestañas de Navegación -->
    <div class="mb-6 flex space-x-2 border-b border-slate-200">
      <button
        type="button"
        @click="activeTab = 'general'"
        class="border-b-2 py-2 px-4 text-xs font-bold transition"
        :class="
          activeTab === 'general'
            ? 'border-emerald-600 text-emerald-700'
            : 'border-transparent text-slate-500 hover:text-slate-800'
        "
      >
        Modalidad y Cobertura
      </button>
      <button
        type="button"
        @click="activeTab = 'catalog'"
        class="border-b-2 py-2 px-4 text-xs font-bold transition"
        :class="
          activeTab === 'catalog'
            ? 'border-emerald-600 text-emerald-700'
            : 'border-transparent text-slate-500 hover:text-slate-800'
        "
      >
        Mapeo de Productos
      </button>
      <button
        type="button"
        @click="activeTab = 'credentials'"
        class="border-b-2 py-2 px-4 text-xs font-bold transition"
        :class="
          activeTab === 'credentials'
            ? 'border-emerald-600 text-emerald-700'
            : 'border-transparent text-slate-500 hover:text-slate-800'
        "
      >
        Credenciales API & Sandbox
      </button>
      <button
        type="button"
        @click="activeTab = 'webhooks'"
        class="border-b-2 py-2 px-4 text-xs font-bold transition"
        :class="
          activeTab === 'webhooks'
            ? 'border-emerald-600 text-emerald-700'
            : 'border-transparent text-slate-500 hover:text-slate-800'
        "
      >
        Webhooks y Entregas
      </button>
      <button
        type="button"
        @click="activeTab = 'stats'"
        class="border-b-2 py-2 px-4 text-xs font-bold transition"
        :class="
          activeTab === 'stats'
            ? 'border-emerald-600 text-emerald-700'
            : 'border-transparent text-slate-500 hover:text-slate-800'
        "
      >
        Métricas y SLA
      </button>
    </div>

    <!-- Mensajes Globales -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>

    <div
      v-if="successMsg"
      class="mb-6 rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800"
    >
      {{ successMsg }}
    </div>

    <!-- TAB 1: Modalidad de Integración y Cobertura -->
    <div v-show="activeTab === 'general'" class="space-y-6">
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h2 class="text-base font-bold text-slate-900">Nivel de Integración Técnica (ARQ-003)</h2>
        <p class="mt-1 text-xs text-slate-500">
          Define si la farmacia opera a través del portal interactivo web o mediante API directa.
        </p>

        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label
            class="relative flex cursor-pointer rounded-lg border p-4 shadow-sm"
            :class="
              integration.level === 'portal'
                ? 'border-emerald-600 bg-emerald-50/40 ring-2 ring-emerald-600'
                : 'border-slate-300 hover:border-slate-400'
            "
          >
            <input type="radio" value="portal" v-model="integration.level" class="sr-only" />
            <div>
              <span class="block text-sm font-semibold text-slate-900">
                🌐 Nivel 1 — Portal Web
              </span>
              <span class="mt-1 block text-xs text-slate-500">
                Gestión interactiva a través del navegador: bandeja de cotizaciones, confirmación
                manual y registro de lotes.
              </span>
            </div>
          </label>

          <label
            class="relative flex cursor-pointer rounded-lg border p-4 shadow-sm"
            :class="
              integration.level === 'direct_integration'
                ? 'border-emerald-600 bg-emerald-50/40 ring-2 ring-emerald-600'
                : 'border-slate-300 hover:border-slate-400'
            "
          >
            <input
              type="radio"
              value="direct_integration"
              v-model="integration.level"
              class="sr-only"
            />
            <div>
              <span class="block text-sm font-semibold text-slate-900">
                ⚡ Nivel 2 — Integración Directa (API / Webhooks)
              </span>
              <span class="mt-1 block text-xs text-slate-500">
                Conexión automatizada entre tu ERP/pos y la plataforma vía webhooks firmados HMAC y
                API REST.
              </span>
            </div>
          </label>
        </div>

        <!-- Áreas de Entrega -->
        <div class="mt-6 border-t pt-4">
          <div class="flex items-center justify-between">
            <h3 class="text-xs font-bold uppercase text-slate-600">
              Áreas de Cobertura y Despacho
            </h3>
            <button
              type="button"
              @click="addDeliveryArea"
              class="text-xs font-semibold text-emerald-600 hover:underline"
            >
              + Agregar Ciudad
            </button>
          </div>

          <div class="mt-3 space-y-3">
            <div
              v-for="(area, idx) in integration.delivery_areas"
              :key="idx"
              class="flex items-center gap-3 rounded-lg border border-slate-200 p-3 bg-slate-50"
            >
              <div class="flex-1">
                <label class="block text-[11px] font-medium text-slate-600"
                  >Ciudad de Cobertura</label
                >
                <input
                  type="text"
                  v-model="area.city"
                  placeholder="Ej. Bogotá"
                  class="mt-1 w-full rounded border border-slate-300 p-1.5 text-xs bg-white"
                />
              </div>
              <button
                v-if="integration.delivery_areas.length > 1"
                type="button"
                @click="removeDeliveryArea(idx)"
                class="mt-4 text-xs font-medium text-rose-600 hover:underline"
              >
                Eliminar
              </button>
            </div>
          </div>
        </div>

        <div class="mt-6 flex justify-end border-t pt-4">
          <button
            type="button"
            :disabled="savingSettings"
            @click="saveSettings"
            class="rounded-lg bg-emerald-600 px-6 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            {{ savingSettings ? 'Guardando...' : 'Guardar Configuración Técnica' }}
          </button>
        </div>
      </div>
    </div>

    <!-- TAB 2: Catálogo y Mapeo de Productos -->
    <div v-show="activeTab === 'catalog'" class="space-y-6">
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <h2 class="text-base font-bold text-slate-900">Mapeo de Catálogo (SKUs de Farmacia)</h2>
            <p class="mt-1 text-xs text-slate-500">
              Relaciona tus códigos de barra o SKUs internos con el catálogo unificado de
              medicamentos.
            </p>
          </div>
          <button
            type="button"
            @click="openAddMappingModal"
            class="rounded-lg bg-slate-900 px-4 py-2 text-xs font-bold text-white shadow hover:bg-slate-800"
          >
            + Nuevo Mapeo de Producto
          </button>
        </div>

        <div class="mt-6 overflow-x-auto">
          <table class="w-full text-left text-xs">
            <thead class="border-b border-slate-200 bg-slate-50 text-slate-600">
              <tr>
                <th class="py-2.5 px-3">SKU Farmacia</th>
                <th class="py-2.5 px-3">Nombre en Farmacia</th>
                <th class="py-2.5 px-3">Nombre Estándar</th>
                <th class="py-2.5 px-3">Precio (COP)</th>
                <th class="py-2.5 px-3">Stock Activo</th>
                <th class="py-2.5 px-3 text-right">Acciones</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100">
              <tr v-if="mappings.length === 0">
                <td colspan="6" class="py-6 text-center text-slate-400">
                  No hay productos mapeados en el catálogo aún.
                </td>
              </tr>
              <tr v-for="m in mappings" :key="m.catalog_id" class="hover:bg-slate-50 transition">
                <td class="py-3 px-3 font-mono font-medium text-slate-800">{{ m.pharmacy_sku }}</td>
                <td class="py-3 px-3 font-semibold text-slate-900">{{ m.pharmacy_name }}</td>
                <td class="py-3 px-3 text-slate-500">{{ m.standard_name }}</td>
                <td class="py-3 px-3 font-mono font-medium text-emerald-700">
                  ${{ m.price_cents.toLocaleString('es-CO') }}
                </td>
                <td class="py-3 px-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-[10px] font-bold"
                    :class="
                      m.in_stock ? 'bg-emerald-100 text-emerald-800' : 'bg-slate-200 text-slate-600'
                    "
                  >
                    {{ m.in_stock ? 'En Stock' : 'Sin Stock' }}
                  </span>
                </td>
                <td class="py-3 px-3 text-right space-x-2">
                  <button
                    type="button"
                    @click="openEditMappingModal(m)"
                    class="text-xs font-semibold text-emerald-600 hover:underline"
                  >
                    Editar
                  </button>
                  <button
                    type="button"
                    @click="deleteMapping(m.catalog_id)"
                    class="text-xs font-semibold text-rose-600 hover:underline"
                  >
                    Eliminar
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- TAB 3: Credenciales API y Sandbox (FE-010, PTN-002, PTN-005) -->
    <div v-show="activeTab === 'credentials'" class="space-y-6">
      <!-- Credenciales de Producción -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <h2 class="text-base font-bold text-slate-900">Credenciales OAuth2 (Producción)</h2>
            <p class="mt-1 text-xs text-slate-500">
              Autenticación machine-to-machine mediante Client Credentials con audiencia exclusiva
              `petpassport-partner-api`.
            </p>
          </div>
          <button
            type="button"
            :disabled="rotatingCredentials"
            @click="rotateCredentials"
            class="rounded-lg border border-amber-300 bg-amber-50 px-4 py-2 text-xs font-bold text-amber-800 shadow-sm hover:bg-amber-100 disabled:opacity-50"
          >
            {{ rotatingCredentials ? 'Rotando Secreto...' : '🔄 Rotar Secreto API' }}
          </button>
        </div>

        <!-- Banner de secreto rotado recién generado -->
        <div
          v-if="rotatedSecretData"
          class="mt-4 rounded-lg border border-amber-300 bg-amber-50/70 p-4 text-xs text-amber-900"
        >
          <div class="flex items-center justify-between">
            <span class="font-bold">⚠️ ¡Atención! Nuevo client_secret generado</span>
            <button
              type="button"
              @click="copyToClipboard(rotatedSecretData.client_secret)"
              class="rounded bg-amber-200 px-2.5 py-1 text-[11px] font-bold text-amber-900 hover:bg-amber-300"
            >
              {{ copiedSecret ? '✓ Copiado' : 'Copiar Secreto' }}
            </button>
          </div>
          <p class="mt-1 text-slate-600">
            Copie y guarde este secreto en su gestor seguro inmediatamente. Por seguridad, no
            volverá a ser mostrado.
          </p>
          <div
            class="mt-2 font-mono text-[11px] bg-white p-2 rounded border border-amber-200 break-all"
          >
            {{ rotatedSecretData.client_secret }}
          </div>
        </div>

        <div v-if="credentials" class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 text-xs">
          <div class="rounded-lg border border-slate-100 bg-slate-50 p-4">
            <span class="text-slate-500 block font-medium">Client ID</span>
            <span class="mt-1 font-mono font-semibold text-slate-800 block break-all">
              {{ credentials.client_id ?? 'No generado' }}
            </span>
          </div>

          <div class="rounded-lg border border-slate-100 bg-slate-50 p-4">
            <span class="text-slate-500 block font-medium">Límite de Tasa (Rate Limit)</span>
            <span class="mt-1 font-semibold text-slate-800 block">
              {{ credentials.rate_limit_rpm ?? 600 }} req/min (lectura), 120 req/min (escritura)
            </span>
          </div>

          <div class="rounded-lg border border-slate-100 bg-slate-50 p-4">
            <span class="text-slate-500 block font-medium">Scopes Autorizados</span>
            <div class="mt-1 flex flex-wrap gap-1">
              <span
                v-for="sc in credentials.scopes ?? [
                  'quotes:read',
                  'quotes:submit',
                  'orders:manage',
                ]"
                :key="sc"
                class="rounded bg-slate-200 px-2 py-0.5 text-[10px] font-mono text-slate-700"
              >
                {{ sc }}
              </span>
            </div>
          </div>

          <div class="rounded-lg border border-slate-100 bg-slate-50 p-4">
            <span class="text-slate-500 block font-medium">Token Endpoint</span>
            <span class="mt-1 font-mono text-slate-700 text-[11px] block">
              POST /realms/petpassport/protocol/openid-connect/token
            </span>
          </div>
        </div>
      </div>

      <!-- Entorno Sandbox (ARQ-008, PTN-005) -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <h2 class="text-base font-bold text-slate-900">Entorno Sandbox Hermético (ARQ-008)</h2>
            <p class="mt-1 text-xs text-slate-500">
              Genere credenciales de prueba en el realm `petpassport-sandbox` para ejecutar la suite
              de certificación con `partner-sim`.
            </p>
          </div>
          <button
            type="button"
            :disabled="generatingSandbox"
            @click="generateSandboxCredentials"
            class="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-bold text-white shadow hover:bg-indigo-700 disabled:opacity-50"
          >
            {{ generatingSandbox ? 'Generando...' : '🧪 Generar Credenciales Sandbox' }}
          </button>
        </div>

        <div
          v-if="sandboxData"
          class="mt-4 rounded-lg border border-indigo-200 bg-indigo-50/50 p-4 text-xs text-indigo-950"
        >
          <div class="flex items-center justify-between">
            <span class="font-bold text-indigo-900">Credenciales de Pruebas Sandbox Emitidas</span>
            <button
              type="button"
              @click="copyToClipboard(JSON.stringify(sandboxData, null, 2), true)"
              class="rounded bg-indigo-200 px-2.5 py-1 text-[11px] font-bold text-indigo-900 hover:bg-indigo-300"
            >
              {{ copiedSandbox ? '✓ Copiado' : 'Copiar Configuración' }}
            </button>
          </div>

          <div class="mt-3 space-y-2 font-mono text-[11px]">
            <div>
              <span class="text-indigo-700 font-semibold">Client ID:</span>
              {{ sandboxData.client_id }}
            </div>
            <div>
              <span class="text-indigo-700 font-semibold">Client Secret:</span>
              {{ sandboxData.client_secret }}
            </div>
            <div>
              <span class="text-indigo-700 font-semibold">Token Endpoint:</span>
              {{ sandboxData.token_endpoint }}
            </div>
            <div>
              <span class="text-indigo-700 font-semibold">API Base URL:</span>
              {{ sandboxData.api_base_url }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- TAB 4: Webhooks y Entregas (FE-010, PTN-004, SEC-003) -->
    <div v-show="activeTab === 'webhooks'" class="space-y-6">
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h2 class="text-base font-bold text-slate-900">
          Configuración de Webhooks Salientes (PTN-004)
        </h2>
        <p class="mt-1 text-xs text-slate-500">
          Notificaciones HTTP salientes firmadas con HMAC-SHA256 (`X-PP-Signature`) y ventana
          anti-replay de 300 segundos.
        </p>

        <div class="mt-4 space-y-4 text-xs">
          <div>
            <label class="block font-semibold text-slate-700">URL del Endpoint HTTPS *</label>
            <input
              type="url"
              v-model="webhookForm.url"
              data-testid="input-webhook-url"
              placeholder="https://api.tu-farmacia.com/webhooks/petpassport"
              class="mt-1 w-full rounded border border-slate-300 p-2 font-mono text-xs"
            />
          </div>

          <div>
            <label class="block font-semibold text-slate-700"
              >Secreto de Firma HMAC (Opcional para actualizar)</label
            >
            <input
              type="text"
              v-model="webhookForm.secret"
              data-testid="input-webhook-secret"
              placeholder="whsec_dejar_vacio_para_mantener_el_actual"
              class="mt-1 w-full rounded border border-slate-300 p-2 font-mono text-xs"
            />
            <span class="text-[11px] text-slate-400">
              Estado:
              {{
                webhookConfig?.has_secret
                  ? '🔒 Secreto activo configurado'
                  : 'Sin secreto configurado'
              }}
            </span>
          </div>

          <div class="pt-2 flex justify-between items-center border-t">
            <button
              type="button"
              :disabled="testingWebhook"
              data-testid="btn-test-webhook"
              @click="testWebhook"
              class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50"
            >
              {{ testingWebhook ? 'Enviando Ping...' : '🔔 Probar Webhook (Ping Test)' }}
            </button>

            <button
              type="button"
              :disabled="savingWebhook"
              data-testid="btn-save-webhook"
              @click="saveWebhook"
              class="rounded-lg bg-emerald-600 px-5 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:opacity-50"
            >
              {{ savingWebhook ? 'Guardando...' : 'Guardar Webhook' }}
            </button>
          </div>

          <!-- Resultado del Test -->
          <div
            v-if="testWebhookResult"
            data-testid="test-webhook-result"
            class="rounded-lg p-3 text-xs"
            :class="
              testWebhookResult.status === 'delivered'
                ? 'bg-emerald-50 text-emerald-800 border border-emerald-200'
                : 'bg-rose-50 text-rose-800 border border-rose-200'
            "
          >
            <span class="font-bold">Resultado de Prueba:</span> Estado
            {{ testWebhookResult.status }}
            <span v-if="testWebhookResult.status_code">
              (HTTP {{ testWebhookResult.status_code }})</span
            >
            <span v-if="testWebhookResult.error"> - {{ testWebhookResult.error }}</span>
          </div>
        </div>
      </div>

      <!-- Registro de Entregas -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-bold text-slate-900">Historial de Entregas y Reintentos</h3>
          <button
            type="button"
            @click="loadDeliveries"
            class="text-xs font-semibold text-slate-600 hover:text-slate-900"
          >
            Actualizar
          </button>
        </div>

        <div class="mt-4 overflow-x-auto">
          <table class="w-full text-left text-xs">
            <thead class="border-b border-slate-200 bg-slate-50 text-slate-600">
              <tr>
                <th class="py-2.5 px-3">Evento</th>
                <th class="py-2.5 px-3">Estado</th>
                <th class="py-2.5 px-3">Código HTTP</th>
                <th class="py-2.5 px-3">Intentos</th>
                <th class="py-2.5 px-3">Fecha</th>
                <th class="py-2.5 px-3 text-right">Acción</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100">
              <tr v-if="deliveries.length === 0">
                <td colspan="6" class="py-6 text-center text-slate-400">
                  No se registran entregas de webhooks aún.
                </td>
              </tr>
              <tr v-for="d in deliveries" :key="d.id" class="hover:bg-slate-50">
                <td class="py-2.5 px-3 font-mono font-medium text-slate-800">{{ d.event_type }}</td>
                <td class="py-2.5 px-3">
                  <span
                    class="rounded px-2 py-0.5 text-[10px] font-bold"
                    :class="
                      d.status === 'delivered'
                        ? 'bg-emerald-100 text-emerald-800'
                        : 'bg-rose-100 text-rose-800'
                    "
                  >
                    {{ d.status }}
                  </span>
                </td>
                <td class="py-2.5 px-3 font-mono text-slate-600">{{ d.status_code ?? '—' }}</td>
                <td class="py-2.5 px-3 text-slate-600">{{ d.attempts }}</td>
                <td class="py-2.5 px-3 text-slate-500">
                  {{ new Date(d.created_at).toLocaleString('es-CO') }}
                </td>
                <td class="py-2.5 px-3 text-right">
                  <button
                    v-if="d.status !== 'delivered'"
                    type="button"
                    :disabled="retryingDeliveryId === d.id"
                    @click="retryDelivery(d.id)"
                    class="text-xs font-semibold text-indigo-600 hover:underline disabled:opacity-50"
                  >
                    {{ retryingDeliveryId === d.id ? 'Reintentando...' : 'Reintentar' }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- TAB 5: Métricas y SLA (FE-010, PRD-016, PTN-008) -->
    <div v-show="activeTab === 'stats'" class="space-y-6">
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h2 class="text-base font-bold text-slate-900">
          Panel de Rendimiento y Cumplimiento de SLA (PTN-008)
        </h2>
        <p class="mt-1 text-xs text-slate-500">
          Supervisión operativa conforme al acuerdo de nivel de servicio (meta: tasa de respuesta ≥
          70%, respuesta media ≤ 6 h laborables).
        </p>

        <div v-if="stats" class="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3">
          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Cotizaciones Recibidas</span>
            <span class="mt-1 block text-2xl font-bold text-slate-900">{{
              stats.quotes_received
            }}</span>
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Cotizaciones Emitidas</span>
            <span class="mt-1 block text-2xl font-bold text-emerald-700">{{
              stats.quotes_submitted
            }}</span>
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Pedidos Confirmados</span>
            <span class="mt-1 block text-2xl font-bold text-indigo-700">{{
              stats.orders_placed
            }}</span>
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Pedidos Entregados</span>
            <span class="mt-1 block text-2xl font-bold text-slate-900">{{
              stats.orders_completed
            }}</span>
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Tiempo Medio Respuesta</span>
            <span class="mt-1 block text-2xl font-bold text-slate-900"
              >{{ stats.avg_response_minutes }} min</span
            >
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Tasa de Conversión</span>
            <span class="mt-1 block text-2xl font-bold text-emerald-700"
              >{{ stats.conversion_rate_percent }}%</span
            >
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Ingresos Estimados</span>
            <span class="mt-1 block text-2xl font-bold text-emerald-800"
              >${{ Number(stats.estimated_revenue ?? 0).toLocaleString() }}</span
            >
          </div>

          <div class="rounded-xl border border-slate-100 bg-slate-50 p-4">
            <span class="text-xs text-slate-500">Valoración de Clientes</span>
            <span class="mt-1 block text-2xl font-bold text-amber-500"
              >★ {{ Number(stats.rating_avg ?? 5.0).toFixed(1) }} <span class="text-xs font-normal text-slate-500">({{ stats.rating_count ?? 0 }} valoraciones)</span></span
            >
          </div>
        </div>

        <!-- Desglose de pedidos por estado -->
        <div v-if="stats && stats.orders_by_status" class="mt-6 border-t pt-4">
          <h4 class="text-xs font-bold uppercase text-slate-600">Desglose de Pedidos por Estado</h4>
          <div class="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4 text-xs">
            <div
              v-for="(count, st) in stats.orders_by_status"
              :key="st"
              class="flex items-center justify-between rounded-lg border border-slate-200 bg-white p-2.5"
            >
              <span class="capitalize text-slate-600">{{ st }}</span>
              <span class="font-bold text-slate-900">{{ count }}</span>
            </div>
          </div>
        </div>

        <div
          v-if="stats"
          class="mt-6 rounded-lg border p-4 text-xs"
          :class="stats.avg_response_minutes <= 360 ? 'border-emerald-200 bg-emerald-50/50' : 'border-amber-200 bg-amber-50/50'"
        >
          <span class="font-bold" :class="stats.avg_response_minutes <= 360 ? 'text-emerald-900' : 'text-amber-900'">
            Estado de SLA: {{ stats.avg_response_minutes <= 360 ? 'Óptimo' : 'Atención Requerida (Tiempo de Respuesta Alto)' }}
          </span>
          <p class="mt-1 text-slate-600">
            {{ stats.avg_response_minutes <= 360 ? 'La farmacia cumple todos los parámetros de calidad del acuerdo B2B para mantener su asignación automática de cotizaciones.' : 'El tiempo promedio de respuesta excede el umbral objetivo de 6 horas.' }}
          </p>
        </div>
      </div>
    </div>

    <!-- MODAL AGREGAR / EDITAR MAPEO -->
    <div
      v-if="showMappingModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl space-y-4">
        <h3 class="text-lg font-bold text-slate-900">
          {{ editingCatalogId ? 'Editar Mapeo de Producto' : 'Nuevo Mapeo de Producto' }}
        </h3>

        <div v-if="mappingError" class="rounded bg-rose-50 p-3 text-xs text-rose-700">
          {{ mappingError }}
        </div>

        <div class="space-y-3 text-xs">
          <div>
            <label class="block font-medium text-slate-700">SKU / Código de Barra Interno *</label>
            <input
              type="text"
              v-model="mappingForm.sku"
              placeholder="Ej. SKU-770123"
              class="mt-1 w-full rounded border border-slate-300 p-2 font-mono"
            />
          </div>

          <div>
            <label class="block font-medium text-slate-700">Nombre Comercial en Farmacia *</label>
            <input
              type="text"
              v-model="mappingForm.name"
              placeholder="Ej. Amoxicilina 250mg Caja x 30"
              class="mt-1 w-full rounded border border-slate-300 p-2"
            />
          </div>

          <div>
            <label class="block font-medium text-slate-700"
              >Precio de Venta al Público (COP) *</label
            >
            <input
              type="number"
              min="0"
              step="500"
              v-model.number="mappingForm.priceCents"
              class="mt-1 w-full rounded border border-slate-300 p-2 font-mono"
            />
          </div>

          <div class="pt-2">
            <label class="flex items-center gap-2 cursor-pointer text-slate-700">
              <input
                type="checkbox"
                v-model="mappingForm.inStock"
                class="h-4 w-4 rounded border-slate-300 text-emerald-600"
              />
              <span class="font-medium">Producto con stock disponible para cotizar</span>
            </label>
          </div>
        </div>

        <div class="flex justify-end gap-3 pt-3">
          <button
            type="button"
            @click="showMappingModal = false"
            class="rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="savingMapping"
            @click="saveMapping"
            class="rounded-lg bg-emerald-600 px-5 py-2 text-xs font-bold text-white shadow hover:bg-emerald-700 disabled:bg-slate-300"
          >
            {{ savingMapping ? 'Guardando...' : 'Guardar Mapeo' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
