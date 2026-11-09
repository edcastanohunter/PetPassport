<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { api, ApiError } from '@/shared/api/client'
import type { components } from '@/shared/api/schema'

type AdminRegulatoryRules = components['schemas']['AdminRegulatoryRulesResponse']

const selectedCountry = ref('CO')
const availableCountries = [
  { code: 'CO', name: 'Colombia' },
  { code: 'MX', name: 'México' },
  { code: 'AR', name: 'Argentina' },
  { code: 'CL', name: 'Chile' },
]

const loading = ref(false)
const error = ref<string | null>(null)
const rules = ref<AdminRegulatoryRules | null>(null)

async function loadRules() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/admin/regulatory-rules/{country}', {
      params: { path: { country: selectedCountry.value } },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    rules.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al cargar reglas regulatorias'
  } finally {
    loading.value = false
  }
}

watch(selectedCountry, () => {
  loadRules()
})

onMounted(() => {
  loadRules()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
      <div>
        <h1 class="text-xl font-bold tracking-tight text-slate-900">
          SuperAdmin: Reglas Regulatorias por Jurisdicción (FE-011, DOM-012)
        </h1>
        <p class="mt-1 text-xs text-slate-500">
          Parámetros de validez legal de recetas, retención documental y política de sustituciones
          según país.
        </p>
      </div>

      <div class="flex items-center gap-2">
        <label class="text-xs font-semibold text-slate-700">País:</label>
        <select
          v-model="selectedCountry"
          class="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 shadow-sm focus:border-emerald-500 focus:outline-none"
        >
          <option v-for="c in availableCountries" :key="c.code" :value="c.code">
            {{ c.name }} ({{ c.code }})
          </option>
        </select>
      </div>
    </div>

    <div
      v-if="error"
      class="rounded-lg border border-rose-200 bg-rose-50 p-4 text-xs text-rose-800"
    >
      {{ error }}
    </div>

    <div
      v-if="loading"
      class="rounded-xl border border-slate-200 bg-white p-8 text-center text-xs text-slate-400"
    >
      Cargando parámetros regulatorios...
    </div>

    <div v-else-if="rules" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">
          Validez y Prescripción
        </h3>
        <div class="mt-4 space-y-3 text-xs">
          <div class="flex justify-between items-center py-2 border-b border-slate-100">
            <span class="text-slate-600">Vigencia Máxima de Receta:</span>
            <span class="font-bold text-slate-900">{{ rules.max_validity_days }} días</span>
          </div>
          <div class="flex justify-between items-center py-2 border-b border-slate-100">
            <span class="text-slate-600">Retención Legal Obligatoria:</span>
            <span class="font-bold text-slate-900">{{ rules.retention_years }} años</span>
          </div>
        </div>
      </div>

      <div class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">
          Sustancias y Dispensación
        </h3>
        <div class="mt-4 space-y-3 text-xs">
          <div class="flex justify-between items-center py-2 border-b border-slate-100">
            <span class="text-slate-600">Registro Especial de Controlados:</span>
            <span
              class="rounded px-2 py-0.5 text-[10px] font-bold"
              :class="
                rules.requires_controlled_registry
                  ? 'bg-amber-100 text-amber-800'
                  : 'bg-slate-100 text-slate-700'
              "
            >
              {{ rules.requires_controlled_registry ? 'Exigido por Ley' : 'No Exigido' }}
            </span>
          </div>
          <div class="flex justify-between items-center py-2 border-b border-slate-100">
            <span class="text-slate-600">Sustituciones Farmacéuticas:</span>
            <span
              class="rounded px-2 py-0.5 text-[10px] font-bold"
              :class="
                rules.allowed_substitutions
                  ? 'bg-emerald-100 text-emerald-800'
                  : 'bg-rose-100 text-rose-800'
              "
            >
              {{ rules.allowed_substitutions ? 'Permitidas (Bioequivalentes)' : 'Prohibidas' }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
