<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const orgId = computed(() => String(route.params.orgId ?? ''))

type CohortKind = 'vaccine_due' | 'no_visit_since' | 'diagnosis' | 'species_age_weight'

const selectedKind = ref<CohortKind>('vaccine_due')

// Parameters
const daysAhead = ref(30)
const monthsSince = ref(12)
const diagnosisQuery = ref('')
const selectedSpecies = ref('dog')
const minAge = ref(0)
const maxAge = ref(15)
const minWeight = ref(0)
const maxWeight = ref(50)

// Query state
const running = ref(false)
const queryResult = ref<{
  total_count: number
  patient_ids: string[]
  sample: Array<{
    id: string
    name: string
    species: string
    breed?: string
    guardian_name?: string
  }>
} | null>(null)
const error = ref<string | null>(null)

// Export modal
const showExportModal = ref(false)
const exportReason = ref('')
const exporting = ref(false)
const exportSuccess = ref(false)

// Notify campaign modal
const showNotifyModal = ref(false)
const campaignTemplate = ref('vaccine_reminder')
const campaignChannel = ref<'email' | 'push'>('email')
const notifying = ref(false)
const notifyResult = ref<{ enqueued: number; recipients: number } | null>(null)

function buildParams(): Record<string, any> {
  switch (selectedKind.value) {
    case 'vaccine_due':
      return { days_ahead: daysAhead.value }
    case 'no_visit_since':
      return { months: monthsSince.value }
    case 'diagnosis':
      return { icd10: diagnosisQuery.value.trim() }
    case 'species_age_weight':
      return {
        species: selectedSpecies.value,
        min_age_years: minAge.value,
        max_age_years: maxAge.value,
        min_weight_kg: minWeight.value,
        max_weight_kg: maxWeight.value,
      }
  }
}

async function runCohortQuery() {
  running.value = true
  error.value = null
  queryResult.value = null
  notifyResult.value = null
  try {
    const res = await fetch(`/api/v1/organizations/${orgId.value}/cohorts/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: selectedKind.value,
        params: buildParams(),
      }),
    })
    if (!res.ok) {
      const p = await res.json().catch(() => ({}))
      throw new Error(p.detail || p.title || 'Error al ejecutar consulta de cohorte')
    }
    queryResult.value = await res.json()
  } catch (err: any) {
    error.value = err.message || 'Error al ejecutar cohorte'
  } finally {
    running.value = false
  }
}

async function exportCohort() {
  if (!exportReason.value.trim()) {
    alert('Debes indicar un motivo de exportación para la auditoría de seguridad.')
    return
  }
  exporting.value = true
  try {
    const res = await fetch(`/api/v1/organizations/${orgId.value}/cohorts/export`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: selectedKind.value,
        params: buildParams(),
        reason: exportReason.value.trim(),
      }),
    })
    if (!res.ok) {
      const p = await res.json().catch(() => ({}))
      throw new Error(p.detail || p.title || 'Error al exportar cohorte')
    }
    const blob = await res.blob()
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `cohorte_${selectedKind.value}_${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)

    exportSuccess.value = true
    setTimeout(() => {
      showExportModal.value = false
      exportSuccess.value = false
      exportReason.value = ''
    }, 1500)
  } catch (err: any) {
    alert(err.message || 'Error en la exportación')
  } finally {
    exporting.value = false
  }
}

async function sendCampaign() {
  notifying.value = true
  try {
    const res = await fetch(`/api/v1/organizations/${orgId.value}/cohorts/notify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: selectedKind.value,
        params: buildParams(),
        template: campaignTemplate.value,
        channel: campaignChannel.value,
      }),
    })
    if (!res.ok) {
      const p = await res.json().catch(() => ({}))
      throw new Error(p.detail || p.title || 'Error al enviar recordatorios')
    }
    notifyResult.value = await res.json()
    showNotifyModal.value = false
  } catch (err: any) {
    alert(err.message || 'Error en la campaña')
  } finally {
    notifying.value = false
  }
}
</script>

<template>
  <div class="max-w-5xl mx-auto px-4 py-6 space-y-6">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold text-surface-900">Cohortes y Campañas Clínicas</h1>
        <p class="text-sm text-surface-600">
          Segmenta pacientes para medicina preventiva, avisos de vacunación y seguimiento clínico.
        </p>
      </div>

      <div v-if="queryResult && queryResult.total_count > 0" class="flex items-center gap-2">
        <button
          type="button"
          class="px-3 py-1.5 text-sm font-medium rounded-lg border border-surface-300 text-surface-700 hover:bg-surface-50 transition flex items-center gap-1.5"
          @click="showExportModal = true"
        >
          <span class="pi pi-download"></span>
          Exportar CSV
        </button>
        <button
          type="button"
          class="px-3 py-1.5 text-sm font-medium rounded-lg text-white bg-primary-600 hover:bg-primary-700 transition flex items-center gap-1.5"
          @click="showNotifyModal = true"
        >
          <span class="pi pi-send"></span>
          Enviar Campaña
        </button>
      </div>
    </div>

    <!-- Selector de cohorte y parámetros -->
    <div class="bg-surface-0 border border-surface-200 rounded-xl p-5 shadow-sm space-y-5">
      <div>
        <label class="text-xs font-semibold text-surface-500 uppercase tracking-wider block mb-2">
          Tipo de Cohorte
        </label>
        <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-2">
          <button
            type="button"
            class="p-3 text-left rounded-lg border text-sm transition"
            :class="
              selectedKind === 'vaccine_due'
                ? 'border-primary-600 bg-primary-50 text-primary-900 font-medium'
                : 'border-surface-200 hover:bg-surface-50 text-surface-700'
            "
            @click="selectedKind = 'vaccine_due'"
          >
            <div class="font-semibold">Vacunación Próxima</div>
            <div class="text-xs text-surface-500 mt-1">Pacientes con vacunas a vencer</div>
          </button>

          <button
            type="button"
            class="p-3 text-left rounded-lg border text-sm transition"
            :class="
              selectedKind === 'no_visit_since'
                ? 'border-primary-600 bg-primary-50 text-primary-900 font-medium'
                : 'border-surface-200 hover:bg-surface-50 text-surface-700'
            "
            @click="selectedKind = 'no_visit_since'"
          >
            <div class="font-semibold">Sin Visita Reciente</div>
            <div class="text-xs text-surface-500 mt-1">Sin consulta en N meses</div>
          </button>

          <button
            type="button"
            class="p-3 text-left rounded-lg border text-sm transition"
            :class="
              selectedKind === 'diagnosis'
                ? 'border-primary-600 bg-primary-50 text-primary-900 font-medium'
                : 'border-surface-200 hover:bg-surface-50 text-surface-700'
            "
            @click="selectedKind = 'diagnosis'"
          >
            <div class="font-semibold">Diagnóstico / Condición</div>
            <div class="text-xs text-surface-500 mt-1">Por código CIE-10 o término</div>
          </button>

          <button
            type="button"
            class="p-3 text-left rounded-lg border text-sm transition"
            :class="
              selectedKind === 'species_age_weight'
                ? 'border-primary-600 bg-primary-50 text-primary-900 font-medium'
                : 'border-surface-200 hover:bg-surface-50 text-surface-700'
            "
            @click="selectedKind = 'species_age_weight'"
          >
            <div class="font-semibold">Especie, Edad y Peso</div>
            <div class="text-xs text-surface-500 mt-1">Segmentación geriátrica/peso</div>
          </button>
        </div>
      </div>

      <!-- Parámetros específicos -->
      <div class="pt-4 border-t border-surface-100">
        <div v-if="selectedKind === 'vaccine_due'" class="flex items-center gap-3">
          <label class="text-sm font-medium text-surface-700">Días hacia adelante:</label>
          <input
            v-model.number="daysAhead"
            type="number"
            min="1"
            max="180"
            class="w-24 px-3 py-1.5 text-sm border rounded-lg border-surface-300"
          />
          <span class="text-xs text-surface-500">Busca vacunas con fecha límite en los próximos {{ daysAhead }} días</span>
        </div>

        <div v-else-if="selectedKind === 'no_visit_since'" class="flex items-center gap-3">
          <label class="text-sm font-medium text-surface-700">Meses sin consulta:</label>
          <input
            v-model.number="monthsSince"
            type="number"
            min="1"
            max="60"
            class="w-24 px-3 py-1.5 text-sm border rounded-lg border-surface-300"
          />
          <span class="text-xs text-surface-500">Pacientes cuya última visita fue hace más de {{ monthsSince }} meses</span>
        </div>

        <div v-else-if="selectedKind === 'diagnosis'" class="flex items-center gap-3 max-w-md">
          <label class="text-sm font-medium text-surface-700">Término / CIE-10:</label>
          <input
            v-model="diagnosisQuery"
            type="text"
            placeholder="Ej. diabetes, otitis, E11..."
            class="flex-1 px-3 py-1.5 text-sm border rounded-lg border-surface-300"
          />
        </div>

        <div v-else-if="selectedKind === 'species_age_weight'" class="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm">
          <div>
            <label class="font-medium text-surface-700 block mb-1">Especie:</label>
            <select v-model="selectedSpecies" class="w-full px-3 py-1.5 text-sm border rounded-lg border-surface-300">
              <option value="dog">Perro</option>
              <option value="cat">Gato</option>
              <option value="rabbit">Conejo</option>
            </select>
          </div>
          <div>
            <label class="font-medium text-surface-700 block mb-1">Rango de Edad (años):</label>
            <div class="flex items-center gap-2">
              <input v-model.number="minAge" type="number" min="0" class="w-20 px-2 py-1.5 text-sm border rounded-lg border-surface-300" />
              <span>a</span>
              <input v-model.number="maxAge" type="number" min="0" class="w-20 px-2 py-1.5 text-sm border rounded-lg border-surface-300" />
            </div>
          </div>
          <div>
            <label class="font-medium text-surface-700 block mb-1">Rango de Peso (kg):</label>
            <div class="flex items-center gap-2">
              <input v-model.number="minWeight" type="number" min="0" class="w-20 px-2 py-1.5 text-sm border rounded-lg border-surface-300" />
              <span>a</span>
              <input v-model.number="maxWeight" type="number" min="0" class="w-20 px-2 py-1.5 text-sm border rounded-lg border-surface-300" />
            </div>
          </div>
        </div>
      </div>

      <div class="flex items-center justify-end pt-2">
        <button
          type="button"
          class="px-4 py-2 text-sm font-medium rounded-lg text-white bg-primary-600 hover:bg-primary-700 transition"
          :disabled="running"
          @click="runCohortQuery"
        >
          {{ running ? 'Calculando cohorte...' : 'Ejecutar Consulta' }}
        </button>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg text-sm">
      {{ error }}
    </div>

    <!-- Resultado de Campaña -->
    <div v-if="notifyResult" class="p-4 bg-green-50 text-green-800 rounded-lg text-sm flex items-center justify-between">
      <div>
        <span class="font-semibold">Campaña encolada con éxito:</span>
        {{ notifyResult.enqueued }} recordatorios encolados para {{ notifyResult.recipients }} tutores receptores.
      </div>
      <button type="button" class="text-xs text-green-900 underline" @click="notifyResult = null">Cerrar</button>
    </div>

    <!-- Resultados de la cohorte -->
    <div v-if="queryResult" class="bg-surface-0 border border-surface-200 rounded-xl p-5 shadow-sm space-y-4">
      <div class="flex items-center justify-between">
        <div class="space-y-0.5">
          <h2 class="text-lg font-semibold text-surface-900">
            Pacientes identificados: {{ queryResult.total_count }}
          </h2>
          <p class="text-xs text-surface-500">
            Muestra representativa de los primeros resultados (hasta 50 pacientes).
          </p>
        </div>
      </div>

      <div v-if="queryResult.sample && queryResult.sample.length > 0" class="overflow-x-auto">
        <table class="w-full text-left text-sm text-surface-700">
          <thead class="text-xs uppercase bg-surface-50 text-surface-500 border-b border-surface-200">
            <tr>
              <th class="px-4 py-3">Nombre</th>
              <th class="px-4 py-3">Especie / Raza</th>
              <th class="px-4 py-3">Tutor</th>
              <th class="px-4 py-3">ID Paciente</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-surface-100">
            <tr v-for="p in queryResult.sample" :key="p.id" class="hover:bg-surface-50 transition">
              <td class="px-4 py-3 font-medium text-surface-900">{{ p.name }}</td>
              <td class="px-4 py-3">{{ p.species }} {{ p.breed ? `(${p.breed})` : '' }}</td>
              <td class="px-4 py-3 text-surface-600">{{ p.guardian_name || 'Sin tutor asignado' }}</td>
              <td class="px-4 py-3 font-mono text-xs text-surface-400">{{ p.id.slice(0, 8) }}...</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="text-center py-8 text-surface-500 text-sm">
        No se encontraron pacientes que cumplan los criterios de la cohorte.
      </div>
    </div>

    <!-- Modal Exportación CSV con Motivo Obligatorio (SEC-006) -->
    <div
      v-if="showExportModal"
      class="fixed inset-0 bg-surface-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50"
    >
      <div class="bg-surface-0 rounded-xl p-6 max-w-md w-full shadow-lg space-y-4">
        <h3 class="font-semibold text-lg text-surface-900">Motivo de Exportación CSV</h3>
        <p class="text-xs text-surface-600">
          Por seguridad y cumplimiento normativo (SEC-006), cada exportación de pacientes queda registrada en la auditoría con el motivo indicado.
        </p>

        <div>
          <label class="label">Motivo (ej. Campaña externa de vacunación antirrábica):</label>
          <textarea
            v-model="exportReason"
            rows="3"
            class="w-full p-2.5 text-sm border rounded-lg border-surface-300"
            placeholder="Especifica el propósito de la descarga..."
          ></textarea>
        </div>

        <p v-if="exportSuccess" class="text-xs text-green-600 font-medium">
          Descarga iniciada correctamente.
        </p>

        <div class="flex items-center justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3 py-1.5 text-sm rounded-lg border border-surface-300 text-surface-700 hover:bg-surface-50"
            :disabled="exporting"
            @click="showExportModal = false"
          >
            Cancelar
          </button>
          <button
            type="button"
            class="px-4 py-1.5 text-sm rounded-lg bg-primary-600 text-white font-medium hover:bg-primary-700"
            :disabled="exporting || !exportReason.trim()"
            @click="exportCohort"
          >
            {{ exporting ? 'Descargando...' : 'Confirmar y Descargar' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Modal Campaña de Recordatorios -->
    <div
      v-if="showNotifyModal"
      class="fixed inset-0 bg-surface-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50"
    >
      <div class="bg-surface-0 rounded-xl p-6 max-w-md w-full shadow-lg space-y-4">
        <h3 class="font-semibold text-lg text-surface-900">Enviar Recordatorios a la Cohorte</h3>
        <p class="text-xs text-surface-600">
          Se enviarán notificaciones a los tutores con consentimiento activo para medicina preventiva.
        </p>

        <div class="space-y-3 text-sm">
          <div>
            <label class="label">Plantilla:</label>
            <select v-model="campaignTemplate" class="w-full p-2 border rounded-lg border-surface-300">
              <option value="vaccine_reminder">Recordatorio de Vacunación</option>
              <option value="general_reminder">Revisión Preventiva General</option>
            </select>
          </div>

          <div>
            <label class="label">Canal:</label>
            <div class="flex items-center gap-4">
              <label class="flex items-center gap-1.5 cursor-pointer">
                <input v-model="campaignChannel" type="radio" value="email" />
                <span>Correo Electrónico</span>
              </label>
              <label class="flex items-center gap-1.5 cursor-pointer">
                <input v-model="campaignChannel" type="radio" value="push" />
                <span>Web Push (PWA)</span>
              </label>
            </div>
          </div>

          <div class="p-3 bg-amber-50 text-amber-800 text-xs rounded-lg space-y-1">
            <div class="font-semibold">Política de enfriamiento (7 días):</div>
            <div>Solo se puede enviar una campaña a la misma cohorte cada 7 días para evitar spam.</div>
          </div>
        </div>

        <div class="flex items-center justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3 py-1.5 text-sm rounded-lg border border-surface-300 text-surface-700 hover:bg-surface-50"
            :disabled="notifying"
            @click="showNotifyModal = false"
          >
            Cancelar
          </button>
          <button
            type="button"
            class="px-4 py-1.5 text-sm rounded-lg bg-primary-600 text-white font-medium hover:bg-primary-700"
            :disabled="notifying"
            @click="sendCampaign"
          >
            {{ notifying ? 'Encolando...' : 'Confirmar Envío' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
