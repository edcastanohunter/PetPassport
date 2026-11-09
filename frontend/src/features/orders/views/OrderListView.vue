<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, ApiError, type OrderSummary } from '@/shared/api/client'

const orders = ref<OrderSummary[]>([])
const loading = ref(false)
const filterStatus = ref<string>('')
const error = ref<string | null>(null)

async function loadOrders() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/orders', {
      params: {
        query: filterStatus.value ? { status: filterStatus.value } : undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    orders.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar pedidos'
  } finally {
    loading.value = false
  }
}

watch(filterStatus, () => {
  void loadOrders()
})

onMounted(() => {
  void loadOrders()
})

function formatCents(cents: number) {
  return new Intl.NumberFormat('es-CO', {
    style: 'currency',
    currency: 'COP',
    maximumFractionDigits: 0,
  }).format(cents)
}

function statusBadgeClass(status: string) {
  switch (status) {
    case 'placed':
      return 'bg-amber-50 text-amber-700 border-amber-200'
    case 'confirmed':
      return 'bg-blue-50 text-blue-700 border-blue-200'
    case 'preparing':
      return 'bg-indigo-50 text-indigo-700 border-indigo-200'
    case 'ready':
      return 'bg-teal-50 text-teal-700 border-teal-200'
    case 'out_for_delivery':
      return 'bg-purple-50 text-purple-700 border-purple-200'
    case 'delivered':
      return 'bg-emerald-50 text-emerald-700 border-emerald-200'
    case 'cancelled':
    case 'rejected':
      return 'bg-rose-50 text-rose-700 border-rose-200'
    default:
      return 'bg-slate-50 text-slate-700 border-slate-200'
  }
}

function statusLabel(status: string) {
  switch (status) {
    case 'placed':
      return 'Recibido'
    case 'confirmed':
      return 'Confirmado'
    case 'preparing':
      return 'En Preparación'
    case 'ready':
      return 'Listo'
    case 'out_for_delivery':
      return 'En Camino'
    case 'delivered':
      return 'Entregado'
    case 'cancelled':
      return 'Cancelado'
    case 'rejected':
      return 'Rechazado'
    default:
      return status
  }
}
</script>

<template>
  <div class="mx-auto max-w-5xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Mis Pedidos de Farmacia</h1>
        <p class="mt-1 text-sm text-slate-500">
          Seguimiento en tiempo real del despacho y entrega de tus medicamentos (FE-006).
        </p>
      </div>

      <router-link
        to="/app/quote-requests"
        class="inline-flex items-center gap-2 rounded-lg border border-slate-300 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
      >
        <span>Ver Solicitudes de Cotización</span>
      </router-link>
    </div>

    <!-- Pestañas de estado -->
    <div class="mb-6 flex items-center gap-2 overflow-x-auto pb-2 text-xs">
      <button
        type="button"
        @click="filterStatus = ''"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === ''
            ? 'bg-slate-900 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Todos
      </button>
      <button
        type="button"
        @click="filterStatus = 'placed'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'placed'
            ? 'bg-amber-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Recibidos
      </button>
      <button
        type="button"
        @click="filterStatus = 'preparing'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'preparing'
            ? 'bg-indigo-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        En Preparación
      </button>
      <button
        type="button"
        @click="filterStatus = 'out_for_delivery'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'out_for_delivery'
            ? 'bg-purple-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        En Camino
      </button>
      <button
        type="button"
        @click="filterStatus = 'delivered'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'delivered'
            ? 'bg-emerald-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Entregados
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
    <div v-if="loading" class="py-12 text-center text-slate-500">Cargando pedidos...</div>

    <!-- Lista vacía -->
    <div
      v-else-if="orders.length === 0"
      class="rounded-xl border border-dashed border-slate-300 p-12 text-center"
    >
      <span class="text-3xl">📦</span>
      <h3 class="mt-2 text-base font-semibold text-slate-800">No tienes pedidos en esta sección</h3>
      <p class="mt-1 text-xs text-slate-500 max-w-sm mx-auto">
        Cuando selecciones una cotización en tu tablero comparativo, aparecerá aquí para su
        seguimiento.
      </p>
    </div>

    <!-- Lista de Pedidos -->
    <div v-else class="space-y-4">
      <div
        v-for="o in orders"
        :key="o.id"
        class="flex flex-col justify-between gap-4 rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition hover:shadow-md sm:flex-row sm:items-center"
      >
        <div>
          <div class="flex items-center gap-2">
            <span
              class="rounded-full border px-2.5 py-0.5 text-xs font-semibold"
              :class="statusBadgeClass(o.status)"
            >
              {{ statusLabel(o.status) }}
            </span>
            <span class="text-xs text-slate-400">·</span>
            <span class="font-semibold text-slate-900 text-sm">{{ o.pharmacy_name }}</span>
          </div>

          <div class="mt-2 flex flex-wrap gap-4 text-xs text-slate-500">
            <span
              >💰 Total: <strong>{{ formatCents(o.total_cents) }}</strong></span
            >
            <span>Modalidad: {{ o.fulfillment_type === 'delivery' ? 'Domicilio' : 'Retiro' }}</span>
            <span>📅 {{ new Date(o.created_at).toLocaleDateString() }}</span>
          </div>
        </div>

        <div>
          <router-link
            :to="{ name: 'order-detail', params: { id: o.id } }"
            class="inline-block rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white shadow hover:bg-emerald-700"
          >
            Ver Seguimiento →
          </router-link>
        </div>
      </div>
    </div>
  </div>
</template>
