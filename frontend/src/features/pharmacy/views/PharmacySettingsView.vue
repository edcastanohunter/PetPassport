<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  api,
  ApiError,
  type PharmacyIntegrationSettings,
  type PharmacyProductMappingItem,
} from '@/shared/api/client'

const route = useRoute()
const orgId = route.params.orgId as string

const loading = ref(false)
const savingSettings = ref(false)
const error = ref<string | null>(null)
const successMsg = ref<string | null>(null)

// Configuración técnica
const integration = ref<PharmacyIntegrationSettings>({
  level: 'portal',
  webhook_url: '',
  catalog_mapping_enabled: true,
  delivery_areas: [{ city: 'Bogotá', postal_codes: ['110111', '110221'] }],
})

// Mapeos de catálogo
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

onMounted(async () => {
  await Promise.all([loadSettings(), loadMappings()])
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
          Modalidades de integración, áreas de despacho y catálogo de productos (FE-009).
        </p>
      </div>

      <div class="flex items-center gap-2">
        <router-link
          :to="`/pharmacy/${orgId}/orders`"
          class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
        >
          Ver Pedidos
        </router-link>
      </div>
    </div>

    <!-- Mensajes -->
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

    <!-- SECCIÓN 1: Modalidad de Integración -->
    <div class="space-y-6">
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

        <!-- Webhook URL si es nivel 2 -->
        <div v-if="integration.level === 'direct_integration'" class="mt-6 border-t pt-4">
          <label class="block text-xs font-semibold text-slate-700"
            >Webhook URL (HTTPS obligatorio, anti-SSRF)</label
          >
          <input
            type="url"
            v-model="integration.webhook_url"
            placeholder="https://api.tu-farmacia.com/v1/inbound-webhook"
            class="mt-1 block w-full rounded-md border border-slate-300 p-2 text-xs font-mono focus:border-emerald-500 focus:outline-none"
          />
          <span class="text-[11px] text-slate-400">
            Debe ser una URL pública HTTPS válida para recibir notificaciones automáticas de
            pedidos.
          </span>
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

        <!-- Guardar Configuración -->
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

      <!-- SECCIÓN 2: Catálogo y Mapeo de Productos (PRD-021) -->
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

        <!-- Tabla de Mapeos -->
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
