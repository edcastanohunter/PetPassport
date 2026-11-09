<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'
import ReasonDialog from './ReasonDialog.vue'

type AdminPartnerSummary = components['schemas']['AdminPartnerSummary']

const loading = ref(false)
const error = ref<string | null>(null)
const successMsg = ref<string | null>(null)
const partners = ref<AdminPartnerSummary[]>([])

// Dialog state
const pendingAction = ref<{ type: 'suspend' | 'reactivate'; partner: AdminPartnerSummary } | null>(
  null,
)
const dialogBusy = ref(false)

async function loadPartners() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/admin/partners')
    if (res.error) throw new ApiError(res.error, res.response.status)
    partners.value = res.data?.items ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar lista de partners'
  } finally {
    loading.value = false
  }
}

function openSuspend(p: AdminPartnerSummary) {
  pendingAction.value = { type: 'suspend', partner: p }
}

function openReactivate(p: AdminPartnerSummary) {
  pendingAction.value = { type: 'reactivate', partner: p }
}

async function confirmAction(reason: string) {
  if (!pendingAction.value) return
  dialogBusy.value = true
  error.value = null
  successMsg.value = null

  const orgId = pendingAction.value.partner.organization_id
  const isSuspend = pendingAction.value.type === 'suspend'

  try {
    if (isSuspend) {
      const res = await api().POST('/admin/partners/{orgId}/suspend', {
        params: { path: { orgId } },
        body: { reason },
      })
      if (res.error) throw new ApiError(res.error, res.response.status)
      successMsg.value = `Partner ${pendingAction.value.partner.display_name} suspendido exitosamente.`
    } else {
      const res = await api().POST('/admin/partners/{orgId}/reactivate', {
        params: { path: { orgId } },
      })
      if (res.error) throw new ApiError(res.error, res.response.status)
      successMsg.value = `Partner ${pendingAction.value.partner.display_name} reactivado exitosamente.`
    }
    pendingAction.value = null
    await loadPartners()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al ejecutar acción administrativa'
  } finally {
    dialogBusy.value = false
  }
}

onMounted(() => {
  loadPartners()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-xl font-bold tracking-tight text-slate-900">
          SuperAdmin: Red de Partners B2B (FE-011, PRD-018)
        </h1>
        <p class="mt-1 text-xs text-slate-500">
          Gestión de farmacias asociadas, nivel de integración, estado operativo y suspensión
          administrativa.
        </p>
      </div>

      <button
        type="button"
        @click="loadPartners"
        class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
      >
        Actualizar Lista
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

    <div class="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm">
      <table class="w-full text-left text-xs">
        <thead class="border-b border-slate-200 bg-slate-50 text-slate-600">
          <tr>
            <th class="py-3 px-4">Farmacia / Razón Social</th>
            <th class="py-3 px-4">Nivel Integración</th>
            <th class="py-3 px-4">Estado</th>
            <th class="py-3 px-4">Webhooks</th>
            <th class="py-3 px-4">Cotizaciones</th>
            <th class="py-3 px-4">Pedidos</th>
            <th class="py-3 px-4 text-right">Acciones</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-if="loading">
            <td colspan="7" class="py-8 text-center text-slate-400">Cargando partners...</td>
          </tr>
          <tr v-else-if="partners.length === 0">
            <td colspan="7" class="py-8 text-center text-slate-400">
              No hay partners registrados.
            </td>
          </tr>
          <tr v-for="p in partners" :key="p.organization_id" class="hover:bg-slate-50 transition">
            <td class="py-3 px-4">
              <span class="block font-semibold text-slate-900">{{ p.display_name }}</span>
              <span class="font-mono text-[11px] text-slate-400">{{ p.organization_id }}</span>
            </td>
            <td class="py-3 px-4">
              <span
                class="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase"
                :class="
                  p.mode === 'api' ? 'bg-indigo-100 text-indigo-800' : 'bg-slate-100 text-slate-700'
                "
              >
                {{ p.mode }}
              </span>
            </td>
            <td class="py-3 px-4">
              <span
                class="rounded-full px-2 py-0.5 text-[10px] font-bold"
                :class="{
                  'bg-emerald-100 text-emerald-800': p.status === 'active',
                  'bg-rose-100 text-rose-800': p.status === 'suspended',
                  'bg-amber-100 text-amber-800': p.status === 'onboarding',
                }"
              >
                {{ p.status }}
              </span>
            </td>
            <td class="py-3 px-4">
              <span
                class="text-[11px] font-medium"
                :class="p.webhook_configured ? 'text-emerald-700' : 'text-slate-400'"
              >
                {{ p.webhook_configured ? '✓ Activo' : '— Inactivo' }}
              </span>
            </td>
            <td class="py-3 px-4 font-mono">{{ p.total_quotes ?? 0 }}</td>
            <td class="py-3 px-4 font-mono">{{ p.total_orders ?? 0 }}</td>
            <td class="py-3 px-4 text-right space-x-2">
              <button
                v-if="p.status === 'active'"
                type="button"
                @click="openSuspend(p)"
                class="rounded bg-rose-50 px-2.5 py-1 text-[11px] font-semibold text-rose-700 hover:bg-rose-100"
              >
                Suspender
              </button>
              <button
                v-if="p.status === 'suspended'"
                type="button"
                @click="openReactivate(p)"
                class="rounded bg-emerald-50 px-2.5 py-1 text-[11px] font-semibold text-emerald-700 hover:bg-emerald-100"
              >
                Reactivar
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Diálogo con Motivo de Auditoría -->
    <ReasonDialog
      v-if="pendingAction"
      :title="pendingAction.type === 'suspend' ? 'Suspender Partner B2B' : 'Reactivar Partner B2B'"
      :target="pendingAction.partner.display_name"
      :busy="dialogBusy"
      @confirm="confirmAction"
      @cancel="pendingAction = null"
    />
  </div>
</template>
