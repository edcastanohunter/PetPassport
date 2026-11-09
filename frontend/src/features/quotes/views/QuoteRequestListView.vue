<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, ApiError, type QuoteRequestSummary } from '@/shared/api/client'

const items = ref<QuoteRequestSummary[]>([])
const loading = ref(false)
const filterStatus = ref<string>('')
const error = ref<string | null>(null)

async function loadRequests() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/quote-requests', {
      params: {
        query: filterStatus.value ? { status: filterStatus.value } : undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    items.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar solicitudes de cotización'
  } finally {
    loading.value = false
  }
}

watch(filterStatus, () => {
  void loadRequests()
})

onMounted(() => {
  void loadRequests()
})

function statusBadgeClass(status: string) {
  switch (status) {
    case 'open':
      return 'bg-emerald-50 text-emerald-700 border-emerald-200'
    case 'selected':
      return 'bg-blue-50 text-blue-700 border-blue-200'
    case 'cancelled':
      return 'bg-rose-50 text-rose-700 border-rose-200'
    case 'expired':
      return 'bg-slate-100 text-slate-700 border-slate-300'
    default:
      return 'bg-slate-50 text-slate-700 border-slate-200'
  }
}

function statusLabel(status: string) {
  switch (status) {
    case 'open':
      return 'Abierta'
    case 'selected':
      return 'Cotización Elegida'
    case 'cancelled':
      return 'Cancelada'
    case 'expired':
      return 'Vencida'
    default:
      return status
  }
}
</script>

<template>
  <div class="mx-auto max-w-5xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Mis Solicitudes de Cotización
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Historial y estado de tus solicitudes enviadas a farmacias aliadas.
        </p>
      </div>

      <router-link
        to="/app/prescriptions"
        class="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2.5 text-xs font-semibold text-white shadow-sm hover:bg-emerald-700"
      >
        <span>+ Cotizar una Receta</span>
      </router-link>
    </div>

    <!-- Filtros -->
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
        Todas
      </button>
      <button
        type="button"
        @click="filterStatus = 'open'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'open'
            ? 'bg-emerald-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Abiertas
      </button>
      <button
        type="button"
        @click="filterStatus = 'selected'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'selected'
            ? 'bg-blue-600 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Cotización Elegida
      </button>
      <button
        type="button"
        @click="filterStatus = 'expired'"
        class="rounded-full px-3 py-1 font-medium transition"
        :class="
          filterStatus === 'expired'
            ? 'bg-slate-700 text-white'
            : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50'
        "
      >
        Vencidas
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
      <span class="text-3xl">📋</span>
      <h3 class="mt-2 text-base font-semibold text-slate-800">No tienes solicitudes registradas</h3>
      <p class="mt-1 text-xs text-slate-500 max-w-sm mx-auto">
        Puedes solicitar cotización para cualquier receta médica activa de tus mascotas.
      </p>
      <div class="mt-4">
        <router-link
          to="/app/prescriptions"
          class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white shadow-sm hover:bg-emerald-700"
        >
          Ver mis recetas
        </router-link>
      </div>
    </div>

    <!-- Cards de Solicitudes -->
    <div v-else class="space-y-4">
      <div
        v-for="it in items"
        :key="it.id"
        class="flex flex-col justify-between gap-4 rounded-xl border border-slate-200 bg-white p-5 shadow-sm transition hover:shadow-md sm:flex-row sm:items-center"
      >
        <div>
          <div class="flex items-center gap-2">
            <span
              class="rounded-full border px-2.5 py-0.5 text-xs font-semibold"
              :class="statusBadgeClass(it.status)"
            >
              {{ statusLabel(it.status) }}
            </span>
            <span class="text-xs text-slate-400">·</span>
            <span class="text-xs text-slate-600">
              Modalidad:
              <strong>{{ it.fulfillment_type === 'delivery' ? 'Domicilio' : 'Retiro' }}</strong>
            </span>
          </div>

          <div class="mt-2 flex flex-wrap gap-4 text-xs text-slate-500">
            <span>📦 {{ it.items_count }} medicamentos solicitados</span>
            <span>💬 {{ it.quotes_count }} cotizaciones recibidas</span>
            <span>📅 Creada: {{ new Date(it.created_at).toLocaleDateString() }}</span>
          </div>
        </div>

        <div class="flex items-center gap-2">
          <router-link
            :to="{ name: 'quote-request-board', params: { id: it.id } }"
            class="rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white hover:bg-slate-800"
          >
            Ver Tablero Comparativo →
          </router-link>
        </div>
      </div>
    </div>
  </div>
</template>
