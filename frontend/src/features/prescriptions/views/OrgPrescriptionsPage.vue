<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  api,
  ApiError,
  type PrescriptionDetail,
  type PrescriptionSummary,
} from '@/shared/api/client'
import IssuePrescriptionModal from '../components/IssuePrescriptionModal.vue'

const route = useRoute()
const orgId = route.params.orgId as string

const prescriptions = ref<PrescriptionSummary[]>([])
const loading = ref(false)
const filterStatus = ref<string>('')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

// Modal Emitir
const showIssueModal = ref(false)

// Detalle lateral
const selectedId = ref<string | null>(null)
const selectedDetail = ref<PrescriptionDetail | null>(null)
const loadingDetail = ref(false)

// Modal Revocación
const revokingId = ref<string | null>(null)
const revokeReason = ref('')
const revoking = ref(false)

async function loadPrescriptions() {
  loading.value = true
  error.value = null
  try {
    const res = await api.GET('/org/{orgId}/prescriptions', {
      params: {
        path: { orgId },
        query: filterStatus.value ? { status: filterStatus.value } : undefined,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    prescriptions.value = res.data?.data ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar recetas'
  } finally {
    loading.value = false
  }
}

async function viewDetail(id: string) {
  selectedId.value = id
  loadingDetail.value = true
  selectedDetail.value = null
  try {
    const res = await api.GET('/prescriptions/{id}', {
      params: { path: { id } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    selectedDetail.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar detalle'
  } finally {
    loadingDetail.value = false
  }
}

function closeDetail() {
  selectedId.value = null
  selectedDetail.value = null
}

function openRevokeDialog(id: string) {
  revokingId.value = id
  revokeReason.value = ''
}

function closeRevokeDialog() {
  revokingId.value = null
  revokeReason.value = ''
}

async function confirmRevoke() {
  if (!revokingId.value || !revokeReason.value.trim()) return
  revoking.value = true
  error.value = null
  try {
    const res = await api.POST('/prescriptions/{id}/revoke', {
      params: { path: { id: revokingId.value } },
      body: { reason: revokeReason.value.trim() },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    notice.value = 'Receta médica revocada.'
    closeRevokeDialog()
    await loadPrescriptions()
    if (selectedId.value === revokingId.value) {
      await viewDetail(selectedId.value)
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al revocar receta'
  } finally {
    revoking.value = false
  }
}

async function downloadPDF(id: string) {
  try {
    const res = await api.GET('/prescriptions/{id}/pdf', {
      params: { path: { id } },
    })
    if (res.data?.download_url) {
      window.open(res.data.download_url, '_blank')
    } else {
      notice.value = 'El PDF aún se está generando, intenta de nuevo en unos segundos.'
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al descargar PDF'
  }
}

function statusBadgeClass(status: string) {
  switch (status) {
    case 'issued':
    case 'confirmed':
      return 'bg-emerald-50 text-emerald-700 border-emerald-200'
    case 'draft':
      return 'bg-amber-50 text-amber-700 border-amber-200'
    case 'revoked':
      return 'bg-rose-50 text-rose-700 border-rose-200'
    case 'expired':
      return 'bg-orange-50 text-orange-700 border-orange-200'
    case 'exhausted':
      return 'bg-slate-100 text-slate-700 border-slate-300'
    default:
      return 'bg-slate-50 text-slate-700 border-slate-200'
  }
}

function statusLabel(status: string) {
  switch (status) {
    case 'issued':
      return 'Emitida'
    case 'draft':
      return 'Borrador'
    case 'confirmed':
      return 'Confirmada'
    case 'revoked':
      return 'Revocada'
    case 'expired':
      return 'Expirada'
    case 'exhausted':
      return 'Agotada'
    default:
      return status
  }
}

watch(filterStatus, () => {
  void loadPrescriptions()
})

onMounted(() => {
  void loadPrescriptions()
})
</script>

<template>
  <div class="mx-auto max-w-7xl px-4 py-8">
    <!-- Header -->
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Recetas Médicas Emitidas</h1>
        <p class="mt-1 text-sm text-slate-500">
          Gestiona las prescripciones electrónicas de la clínica, sus dispensaciones y verificación
          pública.
        </p>
      </div>

      <div class="flex items-center gap-3">
        <!-- Status filter -->
        <select
          v-model="filterStatus"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-700 focus:border-emerald-500 focus:outline-none"
        >
          <option value="">Todos los estados</option>
          <option value="issued">Emitidas</option>
          <option value="draft">Borradores</option>
          <option value="confirmed">Confirmadas</option>
          <option value="revoked">Revocadas</option>
          <option value="expired">Expiradas</option>
          <option value="exhausted">Agotadas</option>
        </select>

        <button
          type="button"
          class="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white shadow hover:bg-emerald-700"
          @click="showIssueModal = true"
        >
          <span>+ Emitir Receta</span>
        </button>
      </div>
    </div>

    <!-- Feedback messages -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>
    <div
      v-if="notice"
      class="mb-6 rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800"
    >
      {{ notice }}
    </div>

    <!-- Prescriptions Table -->
    <div v-if="loading" class="py-12 text-center text-sm text-slate-400">Cargando recetas...</div>
    <div
      v-else-if="prescriptions.length === 0"
      class="rounded-2xl border border-dashed border-slate-300 py-16 text-center"
    >
      <p class="text-base font-medium text-slate-600">No se encontraron recetas médicas.</p>
      <p class="mt-1 text-sm text-slate-400">Emite una nueva receta para un paciente o consulta.</p>
      <button
        type="button"
        class="mt-4 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white shadow hover:bg-emerald-700"
        @click="showIssueModal = true"
      >
        Emitir Receta
      </button>
    </div>
    <div v-else class="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
      <table class="min-w-full divide-y divide-slate-200 text-left text-sm">
        <thead class="bg-slate-50 text-xs font-semibold uppercase text-slate-500">
          <tr>
            <th class="px-6 py-3">Receta / Mascota</th>
            <th class="px-6 py-3">Emisión / Vigencia</th>
            <th class="px-6 py-3">Ítems</th>
            <th class="px-6 py-3">Refills</th>
            <th class="px-6 py-3">Estado</th>
            <th class="px-6 py-3 text-right">Acciones</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="rx in prescriptions" :key="rx.id" class="hover:bg-slate-50">
            <td class="px-6 py-4">
              <div class="font-medium text-slate-900">Mascota: {{ rx.pet_id.slice(0, 8) }}…</div>
              <div class="text-xs text-slate-400 font-mono">ID: {{ rx.id.slice(0, 8) }}…</div>
            </td>
            <td class="px-6 py-4 text-xs text-slate-600">
              <div>Emitida: {{ rx.issued_at ? rx.issued_at.slice(0, 10) : 'Borrador' }}</div>
              <div v-if="rx.valid_until" class="text-slate-400">Hasta: {{ rx.valid_until }}</div>
            </td>
            <td class="px-6 py-4 text-xs">
              <div class="font-medium text-slate-800">{{ rx.items_count }} medicamentos</div>
              <span
                v-if="rx.has_controlled"
                class="mt-0.5 inline-block rounded bg-rose-100 px-1.5 py-0.5 text-[10px] font-bold text-rose-800"
              >
                CONTROLADO
              </span>
            </td>
            <td class="px-6 py-4 text-xs text-slate-600">
              {{ rx.refills_used }} / {{ rx.refills_allowed }}
            </td>
            <td class="px-6 py-4">
              <span
                :class="statusBadgeClass(rx.status)"
                class="rounded-full border px-2.5 py-0.5 text-xs font-semibold capitalize"
              >
                {{ statusLabel(rx.status) }}
              </span>
            </td>
            <td class="px-6 py-4 text-right">
              <div class="flex items-center justify-end gap-2">
                <button
                  type="button"
                  class="rounded border border-slate-300 px-2.5 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-50"
                  @click="viewDetail(rx.id)"
                >
                  Detalle
                </button>
                <button
                  v-if="rx.status === 'issued' || rx.status === 'confirmed'"
                  type="button"
                  class="rounded bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-200"
                  @click="downloadPDF(rx.id)"
                >
                  PDF
                </button>
                <button
                  v-if="rx.status === 'issued' || rx.status === 'confirmed'"
                  type="button"
                  class="rounded border border-rose-300 px-2.5 py-1 text-xs font-semibold text-rose-700 hover:bg-rose-50"
                  @click="openRevokeDialog(rx.id)"
                >
                  Revocar
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Modal Detalle Lateral / Drawer -->
    <div
      v-if="selectedId"
      class="fixed inset-0 z-50 flex justify-end bg-slate-900/40 backdrop-blur-sm"
      @click.self="closeDetail"
    >
      <div class="flex h-full w-full max-w-xl flex-col bg-white shadow-2xl">
        <div class="flex items-center justify-between border-b border-slate-200 px-6 py-4">
          <h2 class="text-lg font-bold text-slate-900">Detalle de Receta Médica</h2>
          <button type="button" class="text-slate-400 hover:text-slate-600" @click="closeDetail">
            ✕
          </button>
        </div>

        <div v-if="loadingDetail" class="flex-1 p-6 text-center text-sm text-slate-400">
          Cargando...
        </div>
        <div v-else-if="selectedDetail" class="flex-1 overflow-y-auto p-6 space-y-6 text-sm">
          <!-- Status Banner -->
          <div
            class="flex items-center justify-between rounded-xl bg-slate-50 p-4 border border-slate-200"
          >
            <div>
              <div class="text-xs uppercase tracking-wider text-slate-500">Estado</div>
              <div class="mt-0.5 text-base font-bold capitalize text-slate-900">
                {{ statusLabel(selectedDetail.status) }}
              </div>
            </div>
            <span
              :class="statusBadgeClass(selectedDetail.status)"
              class="rounded-full border px-3 py-1 text-xs font-bold"
            >
              {{ selectedDetail.status }}
            </span>
          </div>

          <!-- Metadata -->
          <div class="grid grid-cols-2 gap-4 text-xs">
            <div>
              <span class="text-slate-400">ID:</span>
              <p class="font-mono text-slate-800 break-all">{{ selectedDetail.id }}</p>
            </div>
            <div>
              <span class="text-slate-400">Mascota:</span>
              <p class="font-mono text-slate-800 break-all">{{ selectedDetail.pet_id }}</p>
            </div>
            <div>
              <span class="text-slate-400">Vigencia:</span>
              <p class="font-semibold text-slate-800">
                {{ selectedDetail.valid_until ?? 'Indefinida' }}
              </p>
            </div>
            <div>
              <span class="text-slate-400">Verificaciones públicas:</span>
              <p class="font-semibold text-slate-800">
                {{ selectedDetail.verifications_count }} veces
              </p>
            </div>
          </div>

          <!-- Items -->
          <div>
            <h4 class="font-bold uppercase tracking-wider text-xs text-slate-500 mb-3">
              Medicamentos Prescritos
            </h4>
            <div class="space-y-3">
              <div
                v-for="it in selectedDetail.items"
                :key="it.item_id"
                class="rounded-lg border border-slate-200 bg-white p-3"
              >
                <div class="flex items-center justify-between font-bold text-slate-900">
                  <span>{{ it.name }}</span>
                  <span
                    v-if="it.controlled"
                    class="rounded bg-rose-100 px-2 py-0.5 text-xs text-rose-800"
                  >
                    CONTROLADO
                  </span>
                </div>
                <div class="mt-1 text-xs text-slate-600">
                  Dosis: {{ it.dose.value }} {{ it.dose.unit }} · {{ it.frequency }}
                  <span v-if="it.duration_days"> por {{ it.duration_days }} días</span>
                </div>
                <div class="mt-1 text-xs text-slate-500">
                  Cantidad: {{ it.quantity.value }} {{ it.quantity.unit }} · Sustitución:
                  {{ it.substitution_allowed ? 'Sí' : 'No' }}
                </div>
                <div v-if="it.instructions" class="mt-1.5 text-xs italic text-slate-600">
                  "{{ it.instructions }}"
                </div>
              </div>
            </div>
          </div>

          <!-- Dispensations -->
          <div v-if="selectedDetail.dispensations && selectedDetail.dispensations.length > 0">
            <h4 class="font-bold uppercase tracking-wider text-xs text-slate-500 mb-3">
              Historial de Dispensaciones
            </h4>
            <div class="space-y-2">
              <div
                v-for="(disp, i) in selectedDetail.dispensations"
                :key="i"
                class="rounded-lg bg-emerald-50 border border-emerald-200 p-3 text-xs"
              >
                <div class="font-bold text-emerald-900">Fecha: {{ disp.at }}</div>
                <div class="text-emerald-700">Farmacia: {{ disp.pharmacy_org_id }}</div>
              </div>
            </div>
          </div>

          <!-- Public Verification URL -->
          <div class="rounded-xl border border-slate-200 bg-slate-50 p-4">
            <div class="text-xs font-bold text-slate-700">Enlace de Verificación Pública:</div>
            <a
              :href="`/verify/prescriptions/${selectedDetail.id}`"
              target="_blank"
              class="mt-1 block font-mono text-xs text-emerald-600 hover:underline break-all"
            >
              /verify/prescriptions/{{ selectedDetail.id }}
            </a>
          </div>
        </div>

        <div class="border-t border-slate-200 px-6 py-4 flex justify-end gap-3 bg-slate-50">
          <button
            type="button"
            class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            @click="closeDetail"
          >
            Cerrar
          </button>
          <button
            v-if="selectedDetail?.status === 'issued' || selectedDetail?.status === 'confirmed'"
            type="button"
            class="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white shadow hover:bg-emerald-700"
            @click="downloadPDF(selectedDetail.id)"
          >
            Descargar PDF Oficial
          </button>
        </div>
      </div>
    </div>

    <!-- Modal Revocar -->
    <div
      v-if="revokingId"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-sm"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-2xl">
        <h3 class="text-lg font-bold text-slate-900">Revocar Receta Médica</h3>
        <p class="mt-1 text-sm text-slate-500">
          Esta acción invalidará la receta de forma permanente e impedirá dispensaciones futuras.
        </p>

        <textarea
          v-model="revokeReason"
          rows="3"
          placeholder="Motivo clínico o justificación de la revocación (requerido)..."
          class="mt-4 w-full rounded-lg border border-slate-300 p-3 text-sm focus:border-rose-500 focus:outline-none"
        ></textarea>

        <div class="mt-5 flex justify-end gap-3">
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            @click="closeRevokeDialog"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="!revokeReason.trim() || revoking"
            class="rounded-lg bg-rose-600 px-4 py-2 text-sm font-bold text-white hover:bg-rose-700 disabled:opacity-50"
            @click="confirmRevoke"
          >
            {{ revoking ? 'Revocando...' : 'Confirmar Revocación' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Modal Emitir Receta -->
    <IssuePrescriptionModal
      :org-id="orgId"
      :open="showIssueModal"
      @close="showIssueModal = false"
      @issued="loadPrescriptions"
    />
  </div>
</template>
