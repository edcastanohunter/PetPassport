<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'
import ReasonDialog from './ReasonDialog.vue'

type AdminOCRMonitor = components['schemas']['AdminOCRMonitorResponse']
type AdminOCRSampleItem = components['schemas']['AdminOCRSampleItem']

const loading = ref(false)
const error = ref<string | null>(null)
const successMsg = ref<string | null>(null)

const monitor = ref<AdminOCRMonitor | null>(null)
const samples = ref<AdminOCRSampleItem[]>([])

// Revoke dialog
const revokingSample = ref<AdminOCRSampleItem | null>(null)
const dialogBusy = ref(false)

async function loadData() {
  loading.value = true
  error.value = null
  try {
    const [monRes, sampRes] = await Promise.all([
      api().GET('/admin/ocr/monitor'),
      api().GET('/admin/ocr/samples'),
    ])

    if (monRes.error) throw new ApiError(monRes.error, monRes.response.status)
    if (sampRes.error) throw new ApiError(sampRes.error, sampRes.response.status)

    monitor.value = monRes.data ?? null
    samples.value = sampRes.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar monitor de OCR'
  } finally {
    loading.value = false
  }
}

function openRevoke(s: AdminOCRSampleItem) {
  revokingSample.value = s
}

async function confirmRevoke(reason: string) {
  if (!revokingSample.value) return
  dialogBusy.value = true
  error.value = null
  successMsg.value = null

  const prescriptionId = revokingSample.value.prescription_id

  try {
    const res = await api().POST('/admin/prescriptions/{prescriptionId}/revoke', {
      params: { path: { prescriptionId } },
      body: { reason },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    successMsg.value = `Receta ${prescriptionId} revocada administrativamente de forma inmediata.`
    revokingSample.value = null
    await loadData()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al revocar receta médica'
  } finally {
    dialogBusy.value = false
  }
}

onMounted(() => {
  loadData()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-xl font-bold tracking-tight text-slate-900">
          SuperAdmin: Monitor de OCR y Calidad de Extracción (FE-011, OCR-010)
        </h1>
        <p class="mt-1 text-xs text-slate-500">
          Telemetría de ingestión de recetas externas, precisión de modelos, tiempos de inferencia y
          revocación de emergencia.
        </p>
      </div>

      <button
        type="button"
        @click="loadData"
        class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
      >
        Actualizar Métricas
      </button>
    </div>

    <div
      v-if="error"
      class="rounded-lg border border-rose-200 bg-rose-50 p-4 text-xs text-rose-800"
    >
      {{ error }}
    </div>

    <div
      v-if="successMsg"
      class="rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-xs text-emerald-800"
    >
      {{ successMsg }}
    </div>

    <!-- Métricas del Pipeline -->
    <div v-if="monitor" class="grid grid-cols-2 gap-4 sm:grid-cols-4">
      <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <span class="text-xs text-slate-500">Procesadas (24h)</span>
        <span class="mt-1 block text-2xl font-bold text-slate-900">{{ monitor.total_24h }}</span>
      </div>

      <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <span class="text-xs text-slate-500">Tasa de Éxito</span>
        <span class="mt-1 block text-2xl font-bold text-emerald-600"
          >{{ monitor.success_rate_percent }}%</span
        >
      </div>

      <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <span class="text-xs text-slate-500">Latencia Media</span>
        <span class="mt-1 block text-2xl font-bold text-slate-900"
          >{{ monitor.avg_latency_ms }} ms</span
        >
      </div>

      <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <span class="text-xs text-slate-500">Cola Pendiente</span>
        <span class="mt-1 block text-2xl font-bold text-indigo-600">{{ monitor.queue_depth }}</span>
      </div>
    </div>

    <!-- Muestras de Extracción y Auditoría -->
    <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm space-y-4">
      <h2 class="text-sm font-bold text-slate-900">
        Muestras Recientes de Extracción (Auditoría Criptográfica)
      </h2>

      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead class="border-b border-slate-200 bg-slate-50 text-slate-600">
            <tr>
              <th class="py-2.5 px-3">Receta ID</th>
              <th class="py-2.5 px-3">Estado</th>
              <th class="py-2.5 px-3">Confianza</th>
              <th class="py-2.5 px-3">Latencia</th>
              <th class="py-2.5 px-3">Fecha Procesado</th>
              <th class="py-2.5 px-3 text-right">Acción</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="samples.length === 0">
              <td colspan="6" class="py-6 text-center text-slate-400">
                No hay muestras registradas.
              </td>
            </tr>
            <tr v-for="s in samples" :key="s.id" class="hover:bg-slate-50">
              <td class="py-2.5 px-3 font-mono font-medium text-slate-800">
                {{ s.prescription_id }}
              </td>
              <td class="py-2.5 px-3">
                <span
                  class="rounded px-2 py-0.5 text-[10px] font-bold"
                  :class="
                    s.status === 'confirmed'
                      ? 'bg-emerald-100 text-emerald-800'
                      : 'bg-slate-100 text-slate-700'
                  "
                >
                  {{ s.status }}
                </span>
              </td>
              <td class="py-2.5 px-3 font-mono">
                <span
                  class="font-semibold"
                  :class="
                    s.confidence >= 0.85
                      ? 'text-emerald-700'
                      : s.confidence >= 0.6
                        ? 'text-amber-700'
                        : 'text-rose-700'
                  "
                >
                  {{ (s.confidence * 100).toFixed(0) }}%
                </span>
              </td>
              <td class="py-2.5 px-3 font-mono text-slate-600">{{ s.latency_ms }} ms</td>
              <td class="py-2.5 px-3 text-slate-500">
                {{ new Date(s.created_at).toLocaleString('es-CO') }}
              </td>
              <td class="py-2.5 px-3 text-right">
                <button
                  type="button"
                  @click="openRevoke(s)"
                  class="rounded bg-rose-50 px-2 py-1 text-[11px] font-semibold text-rose-700 hover:bg-rose-100"
                >
                  Revocar Receta
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Diálogo de Revocación Administrativa -->
    <ReasonDialog
      v-if="revokingSample"
      title="Revocar Receta Médica de Emergencia"
      :target="revokingSample.prescription_id"
      :busy="dialogBusy"
      @confirm="confirmRevoke"
      @cancel="revokingSample = null"
    />
  </div>
</template>
