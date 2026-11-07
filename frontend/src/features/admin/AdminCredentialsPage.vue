<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError, type ProfessionalCredential } from '@/shared/api/client'

const credentials = ref<ProfessionalCredential[]>([])
const loading = ref(false)
const filterStatus = ref<string>('pending')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)

// Modal de rechazo
const rejectingId = ref<string | null>(null)
const rejectReason = ref('')
const processing = ref(false)

async function loadCredentials() {
  loading.value = true
  error.value = null
  try {
    const res = await api.GET('/admin/credentials', {
      params: {
        query: filterStatus.value ? { status: filterStatus.value } : undefined,
      },
    })
    if (res.error) {
      throw new ApiError(res.error, res.response.status)
    }
    credentials.value = res.data?.data ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar credenciales'
  } finally {
    loading.value = false
  }
}

async function verifyCredential(id: string) {
  processing.value = true
  error.value = null
  notice.value = null
  try {
    const res = await api.POST('/admin/credentials/{id}/verify', {
      params: { path: { id } },
    })
    if (res.error) {
      throw new ApiError(res.error, res.response.status)
    }
    notice.value = 'Credencial verificada exitosamente.'
    await loadCredentials()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al verificar credencial'
  } finally {
    processing.value = false
  }
}

function openRejectDialog(id: string) {
  rejectingId.value = id
  rejectReason.value = ''
}

function closeRejectDialog() {
  rejectingId.value = null
  rejectReason.value = ''
}

async function confirmReject() {
  if (!rejectingId.value || !rejectReason.value.trim()) return
  processing.value = true
  error.value = null
  notice.value = null
  try {
    const res = await api.POST('/admin/credentials/{id}/reject', {
      params: { path: { id: rejectingId.value } },
      body: { reason: rejectReason.value.trim() },
    })
    if (res.error) {
      throw new ApiError(res.error, res.response.status)
    }
    notice.value = 'Credencial rechazada.'
    closeRejectDialog()
    await loadCredentials()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al rechazar credencial'
  } finally {
    processing.value = false
  }
}

function statusBadgeClass(status: string) {
  switch (status) {
    case 'verified':
      return 'bg-emerald-50 text-emerald-700 border-emerald-200'
    case 'pending':
      return 'bg-amber-50 text-amber-700 border-amber-200'
    case 'rejected':
      return 'bg-rose-50 text-rose-700 border-rose-200'
    default:
      return 'bg-slate-50 text-slate-700 border-slate-200'
  }
}

onMounted(() => {
  void loadCredentials()
})
</script>

<template>
  <div class="mx-auto max-w-6xl px-4 py-8">
    <div class="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">
          Moderación de Credenciales Profesionales
        </h1>
        <p class="mt-1 text-sm text-slate-500">
          Revisa y valida tarjetas y matrículas profesionales de veterinarios y farmacéuticos.
        </p>
      </div>

      <!-- Filter tabs -->
      <div class="flex rounded-lg border border-slate-200 bg-slate-50 p-1 text-sm font-medium">
        <button
          v-for="st in [
            { id: 'pending', label: 'Pendientes' },
            { id: 'verified', label: 'Verificadas' },
            { id: 'rejected', label: 'Rechazadas' },
            { id: '', label: 'Todas' },
          ]"
          :key="st.id"
          type="button"
          :class="[
            filterStatus === st.id
              ? 'bg-white text-slate-900 shadow-sm'
              : 'text-slate-600 hover:text-slate-900',
            'rounded-md px-3 py-1.5 transition',
          ]"
          @click="
            filterStatus = st.id
            loadCredentials()
          "
        >
          {{ st.label }}
        </button>
      </div>
    </div>

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

    <div v-if="loading" class="py-12 text-center text-sm text-slate-400">
      Cargando credenciales...
    </div>
    <div
      v-else-if="credentials.length === 0"
      class="rounded-xl border border-dashed border-slate-300 py-12 text-center"
    >
      <p class="text-sm text-slate-500">No hay credenciales en esta categoría.</p>
    </div>
    <div v-else class="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
      <table class="min-w-full divide-y divide-slate-200 text-left text-sm">
        <thead class="bg-slate-50 text-xs font-semibold uppercase text-slate-500">
          <tr>
            <th class="px-6 py-3">Profesional / Matrícula</th>
            <th class="px-6 py-3">País / Profesión</th>
            <th class="px-6 py-3">Entidad Emisora</th>
            <th class="px-6 py-3">Fechas</th>
            <th class="px-6 py-3">Soporte</th>
            <th class="px-6 py-3">Estado</th>
            <th class="px-6 py-3 text-right">Acciones</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="c in credentials" :key="c.id" class="hover:bg-slate-50">
            <td class="px-6 py-4 font-medium text-slate-900">
              <div>{{ c.registry_number }}</div>
              <div class="text-xs text-slate-400">Usuario: {{ c.user_id.slice(0, 8) }}…</div>
            </td>
            <td class="px-6 py-4 text-slate-600">
              <span class="font-semibold">{{ c.country }}</span> · {{ c.kind }}
            </td>
            <td class="px-6 py-4 text-slate-600">{{ c.issuer }}</td>
            <td class="px-6 py-4 text-xs text-slate-500">
              <div>Exp: {{ c.issued_at }}</div>
              <div v-if="c.expires_at">Vence: {{ c.expires_at }}</div>
            </td>
            <td class="px-6 py-4 text-xs">
              <span class="font-mono text-slate-500" :title="c.evidence_object_key">
                {{ c.evidence_object_key.split('/').pop() }}
              </span>
            </td>
            <td class="px-6 py-4">
              <span
                :class="statusBadgeClass(c.status)"
                class="rounded-full border px-2.5 py-0.5 text-xs font-semibold capitalize"
              >
                {{ c.status }}
              </span>
              <div v-if="c.reason" class="mt-1 text-xs text-rose-600" :title="c.reason">
                Motivo: {{ c.reason.slice(0, 25) }}…
              </div>
            </td>
            <td class="px-6 py-4 text-right">
              <div class="flex items-center justify-end gap-2" v-if="c.status === 'pending'">
                <button
                  type="button"
                  :disabled="processing"
                  class="rounded bg-emerald-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-emerald-700 disabled:opacity-50"
                  @click="verifyCredential(c.id)"
                >
                  Verificar
                </button>
                <button
                  type="button"
                  :disabled="processing"
                  class="rounded border border-rose-300 px-2.5 py-1 text-xs font-semibold text-rose-700 hover:bg-rose-50 disabled:opacity-50"
                  @click="openRejectDialog(c.id)"
                >
                  Rechazar
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Modal Rechazo -->
    <div
      v-if="rejectingId"
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm"
    >
      <div class="w-full max-w-md rounded-xl bg-white p-6 shadow-xl">
        <h3 class="text-lg font-bold text-slate-900">Rechazar Credencial</h3>
        <p class="mt-1 text-sm text-slate-500">
          Indica el motivo del rechazo para informar al profesional.
        </p>

        <textarea
          v-model="rejectReason"
          rows="3"
          placeholder="Ej. Documento ilegible o matrícula no coincide con registros oficiales."
          class="mt-4 w-full rounded-lg border border-slate-300 p-3 text-sm focus:border-rose-500 focus:outline-none focus:ring-1 focus:ring-rose-500"
        ></textarea>

        <div class="mt-5 flex justify-end gap-3">
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            @click="closeRejectDialog"
          >
            Cancelar
          </button>
          <button
            type="button"
            :disabled="!rejectReason.trim() || processing"
            class="rounded-lg bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-700 disabled:opacity-50"
            @click="confirmReject"
          >
            Confirmar Rechazo
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
