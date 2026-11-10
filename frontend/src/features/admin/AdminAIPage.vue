<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'

type AdminAIBudget = components['schemas']['AdminAIBudgetResponse']
type AdminAIUsage = components['schemas']['AdminAIUsageResponse']

const loading = ref(false)
const saving = ref(false)
const error = ref<string | null>(null)
const successMsg = ref<string | null>(null)

const budget = ref<AdminAIBudget | null>(null)
const usage = ref<AdminAIUsage | null>(null)

const form = ref({
  monthly_budget_usd: 500,
  alert_threshold_percent: 80,
})

const spendPercent = computed(() => {
  if (!budget.value || budget.value.monthly_budget_usd <= 0) return 0
  return Math.min(
    100,
    Math.round((budget.value.current_spend_usd / budget.value.monthly_budget_usd) * 100),
  )
})

async function loadData() {
  loading.value = true
  error.value = null
  try {
    const [budgetRes, usageRes] = await Promise.all([
      api().GET('/admin/ai/budget'),
      api().GET('/admin/ai/usage'),
    ])

    if (budgetRes.error) throw new ApiError(budgetRes.error, budgetRes.response.status)
    if (usageRes.error) throw new ApiError(usageRes.error, usageRes.response.status)

    budget.value = budgetRes.data ?? null
    if (budgetRes.data) {
      form.value.monthly_budget_usd = budgetRes.data.monthly_budget_usd
      form.value.alert_threshold_percent = budgetRes.data.alert_threshold_percent
    }
    usage.value = usageRes.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar información de IA'
  } finally {
    loading.value = false
  }
}

async function updateBudget() {
  saving.value = true
  error.value = null
  successMsg.value = null
  try {
    const res = await api().PUT('/admin/ai/budget', {
      body: {
        monthly_budget_usd: form.value.monthly_budget_usd,
        alert_threshold_percent: form.value.alert_threshold_percent,
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    budget.value = res.data ?? null
    successMsg.value = 'Presupuesto de IA actualizado exitosamente.'
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al actualizar presupuesto de IA'
  } finally {
    saving.value = false
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
          SuperAdmin: Presupuesto y Consumo de IA (FE-011, OCR-008)
        </h1>
        <p class="mt-1 text-xs text-slate-500">
          Monitoreo de costes Anthropic Claude, alertas de umbral y política de corte automático por
          límite presupuestario.
        </p>
      </div>

      <button
        type="button"
        @click="loadData"
        class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-sm hover:bg-slate-50"
      >
        Actualizar Datos
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

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
      <!-- Tarjeta de Presupuesto Mensual -->
      <div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm space-y-4">
        <h2 class="text-sm font-bold text-slate-900">Presupuesto Mensual Asignado</h2>

        <div v-if="budget" class="space-y-3">
          <div class="flex justify-between items-baseline">
            <span class="text-2xl font-black text-slate-900"
              >${{ budget.current_spend_usd.toFixed(2) }}</span
            >
            <span class="text-xs text-slate-500"
              >de ${{ budget.monthly_budget_usd.toFixed(2) }} USD</span
            >
          </div>

          <!-- Barra de Progreso -->
          <div class="h-2 w-full rounded-full bg-slate-100 overflow-hidden">
            <div
              class="h-full rounded-full transition-all duration-300"
              :class="
                spendPercent >= budget.alert_threshold_percent ? 'bg-amber-500' : 'bg-emerald-500'
              "
              :style="{ width: spendPercent + '%' }"
            ></div>
          </div>

          <div class="flex justify-between text-[11px] text-slate-500">
            <span>Uso: {{ spendPercent }}%</span>
            <span>Alerta: {{ budget.alert_threshold_percent }}%</span>
          </div>

          <div class="pt-2 border-t">
            <span class="text-[11px] font-semibold text-slate-600 block"
              >Estado del Gateway IA:</span
            >
            <span
              class="mt-1 inline-block rounded-full px-2 py-0.5 text-[10px] font-bold"
              :class="
                budget.status === 'active'
                  ? 'bg-emerald-100 text-emerald-800'
                  : 'bg-rose-100 text-rose-800'
              "
            >
              {{
                budget.status === 'active'
                  ? '● Operativo (Permite extracciones)'
                  : '■ Cortado (Excedido)'
              }}
            </span>
          </div>
        </div>

        <!-- Formulario de Modificación de Presupuesto -->
        <form @submit.prevent="updateBudget" class="pt-3 border-t space-y-3 text-xs">
          <div>
            <label class="block font-medium text-slate-700">Límite Mensual (USD)</label>
            <input
              type="number"
              min="10"
              step="10"
              v-model.number="form.monthly_budget_usd"
              class="mt-1 w-full rounded border border-slate-300 p-1.5 font-mono"
            />
          </div>

          <div>
            <label class="block font-medium text-slate-700">Umbral de Alerta (%)</label>
            <input
              type="number"
              min="10"
              max="99"
              v-model.number="form.alert_threshold_percent"
              class="mt-1 w-full rounded border border-slate-300 p-1.5 font-mono"
            />
          </div>

          <button
            type="submit"
            :disabled="saving"
            class="w-full rounded-lg bg-slate-900 py-2 text-xs font-bold text-white shadow hover:bg-slate-800 disabled:opacity-50"
          >
            {{ saving ? 'Guardando...' : 'Actualizar Presupuesto' }}
          </button>
        </form>
      </div>

      <!-- Tarjeta de Consumo por Modelo -->
      <div
        class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm lg:col-span-2 space-y-4"
      >
        <div class="flex justify-between items-center">
          <h2 class="text-sm font-bold text-slate-900">
            Consumo del Período Actual ({{ usage?.month ?? 'Mes' }})
          </h2>
          <span class="text-xs font-mono font-bold text-slate-700">
            Total: ${{ usage?.total_cost_usd?.toFixed(2) ?? '0.00' }} USD ({{
              usage?.total_calls ?? 0
            }}
            llamadas)
          </span>
        </div>

        <div class="overflow-x-auto">
          <table class="w-full text-left text-xs">
            <thead class="border-b border-slate-200 bg-slate-50 text-slate-600">
              <tr>
                <th class="py-2.5 px-3">Modelo IA</th>
                <th class="py-2.5 px-3">Llamadas</th>
                <th class="py-2.5 px-3">Tokens Procesados</th>
                <th class="py-2.5 px-3 text-right">Coste (USD)</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100">
              <tr v-if="!usage || usage.by_model.length === 0">
                <td colspan="4" class="py-6 text-center text-slate-400">
                  Sin registros de uso en el período.
                </td>
              </tr>
              <tr v-for="m in usage?.by_model ?? []" :key="m.model" class="hover:bg-slate-50">
                <td class="py-2.5 px-3 font-mono font-medium text-slate-800">{{ m.model }}</td>
                <td class="py-2.5 px-3 font-mono">{{ m.calls.toLocaleString() }}</td>
                <td class="py-2.5 px-3 font-mono">{{ m.total_tokens.toLocaleString() }}</td>
                <td class="py-2.5 px-3 font-mono font-bold text-slate-900 text-right">
                  ${{ m.cost_usd.toFixed(4) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
