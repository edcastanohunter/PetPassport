<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError, type ProfessionalCredential } from '@/shared/api/client'
import { countries } from '@/shared/i18n/countries'
import { useI18n } from 'vue-i18n'

const { locale } = useI18n()
const COUNTRIES = computed(() => countries(locale.value))

const credentials = ref<ProfessionalCredential[]>([])
const loading = ref(false)
const submitting = ref(false)
const error = ref<string | null>(null)
const success = ref<string | null>(null)
const showForm = ref(false)

const form = reactive({
  country: 'CO',
  kind: 'veterinarian' as 'veterinarian' | 'pharmacist',
  registry_number: '',
  issuer: '',
  issued_at: '',
  expires_at: '',
  evidence_object_key: '',
})

async function loadCredentials() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/me/credentials')
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

async function submitCredential() {
  if (!form.registry_number || !form.issuer || !form.issued_at || !form.evidence_object_key) {
    error.value = 'Por favor completa todos los campos requeridos.'
    return
  }
  submitting.value = true
  error.value = null
  success.value = null
  try {
    const res = await api().POST('/me/credentials', {
      params: {
        header: { 'Idempotency-Key': crypto.randomUUID() },
      },
      body: {
        country: form.country,
        kind: form.kind,
        registry_number: form.registry_number.trim(),
        issuer: form.issuer.trim(),
        issued_at: form.issued_at,
        expires_at: form.expires_at ? form.expires_at : undefined,
        evidence_object_key: form.evidence_object_key.trim(),
      },
    })
    if (res.error) {
      throw new ApiError(res.error, res.response.status)
    }
    success.value = 'Credencial registrada para revisión de moderación.'
    showForm.value = false
    form.registry_number = ''
    form.issuer = ''
    form.issued_at = ''
    form.expires_at = ''
    form.evidence_object_key = ''
    await loadCredentials()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al registrar credencial'
  } finally {
    submitting.value = false
  }
}

async function deleteCredential(id: string) {
  if (!confirm('¿Seguro que deseas eliminar esta credencial?')) return
  error.value = null
  try {
    const res = await api().DELETE('/me/credentials/{credentialId}', {
      params: { path: { credentialId: id } },
    })
    if (res.error) {
      throw new ApiError(res.error, res.response.status)
    }
    await loadCredentials()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al eliminar credencial'
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

function statusLabel(status: string) {
  switch (status) {
    case 'verified':
      return 'Verificada'
    case 'pending':
      return 'En revisión'
    case 'rejected':
      return 'Rechazada'
    default:
      return status
  }
}

onMounted(() => {
  void loadCredentials()
})
</script>

<template>
  <div class="mx-auto max-w-4xl px-4 py-8">
    <div class="mb-6 flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Credenciales Profesionales</h1>
        <p class="mt-1 text-sm text-slate-500">
          Registra tu matrícula profesional para emitir recetas y dispensar medicamentos válidos.
        </p>
      </div>
      <button
        v-if="!showForm"
        type="button"
        class="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white shadow hover:bg-emerald-700 focus:outline-none focus:ring-2 focus:ring-emerald-500 focus:ring-offset-2"
        @click="showForm = true"
      >
        <span>+ Añadir Credencial</span>
      </button>
    </div>

    <!-- Feedback messages -->
    <div
      v-if="error"
      class="mb-6 rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"
    >
      {{ error }}
    </div>
    <div
      v-if="success"
      class="mb-6 rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800"
    >
      {{ success }}
    </div>

    <!-- Form modal / card -->
    <div v-if="showForm" class="mb-8 rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
      <div class="mb-4 flex items-center justify-between border-b border-slate-100 pb-3">
        <h2 class="text-lg font-semibold text-slate-900">Registrar Credencial Profesional</h2>
        <button type="button" class="text-slate-400 hover:text-slate-600" @click="showForm = false">
          ✕
        </button>
      </div>

      <form @submit.prevent="submitCredential" class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >País</label
            >
            <select
              v-model="form.country"
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            >
              <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">
                {{ c.name }} ({{ c.code }})
              </option>
            </select>
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Tipo de Profesión</label
            >
            <select
              v-model="form.kind"
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            >
              <option value="veterinarian">Médico Veterinario</option>
              <option value="pharmacist">Farmacéutico / Regente</option>
            </select>
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Número de Matrícula / Registro</label
            >
            <input
              v-model="form.registry_number"
              type="text"
              placeholder="Ej. COMVEZCOL-12345"
              required
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            />
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Entidad Emisora</label
            >
            <input
              v-model="form.issuer"
              type="text"
              placeholder="Ej. Consejo Profesional / Colegio Veterinario"
              required
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            />
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Fecha de Expedición</label
            >
            <input
              v-model="form.issued_at"
              type="date"
              required
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            />
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Fecha de Vencimiento (opcional)</label
            >
            <input
              v-model="form.expires_at"
              type="date"
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            />
          </div>

          <div class="sm:col-span-2">
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Clave de Documento de Soporte (PDF / Imagen)</label
            >
            <input
              v-model="form.evidence_object_key"
              type="text"
              placeholder="credentials/soporte-matricula.pdf"
              required
              class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
            />
            <p class="mt-1 text-xs text-slate-400">
              Adjunta copia de tu tarjeta profesional o certificado de vigencia.
            </p>
          </div>
        </div>

        <div class="flex justify-end gap-3 pt-3">
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            @click="showForm = false"
          >
            Cancelar
          </button>
          <button
            type="submit"
            :disabled="submitting"
            class="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
          >
            {{ submitting ? 'Enviando...' : 'Enviar para Verificación' }}
          </button>
        </div>
      </form>
    </div>

    <!-- Credentials List -->
    <div v-if="loading" class="py-12 text-center text-sm text-slate-400">
      Cargando credenciales...
    </div>
    <div
      v-else-if="credentials.length === 0"
      class="rounded-xl border border-dashed border-slate-300 py-12 text-center"
    >
      <p class="text-sm text-slate-500">No tienes credenciales profesionales registradas.</p>
      <button
        type="button"
        class="mt-3 text-sm font-medium text-emerald-600 hover:text-emerald-700"
        @click="showForm = true"
      >
        Registrar mi primera credencial
      </button>
    </div>
    <div v-else class="space-y-4">
      <div
        v-for="c in credentials"
        :key="c.id"
        class="flex flex-col justify-between gap-4 rounded-xl border border-slate-200 bg-white p-5 shadow-sm sm:flex-row sm:items-center"
      >
        <div>
          <div class="flex items-center gap-3">
            <span class="font-bold text-slate-900">{{ c.registry_number }}</span>
            <span
              :class="statusBadgeClass(c.status)"
              class="rounded-full border px-2.5 py-0.5 text-xs font-semibold capitalize"
            >
              {{ statusLabel(c.status) }}
            </span>
            <span class="rounded bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600">
              {{ c.country }} · {{ c.kind === 'veterinarian' ? 'Veterinario' : 'Farmacéutico' }}
            </span>
          </div>
          <div class="mt-1 text-sm text-slate-600">
            <span>{{ c.issuer }}</span>
            <span class="mx-2 text-slate-300">•</span>
            <span>Expedida: {{ c.issued_at }}</span>
            <span v-if="c.expires_at" class="ml-2 text-slate-400">(Vence: {{ c.expires_at }})</span>
          </div>
        </div>

        <div class="flex items-center gap-2">
          <button
            v-if="c.status !== 'verified'"
            type="button"
            class="text-xs text-rose-600 hover:underline"
            @click="deleteCredential(c.id)"
          >
            Eliminar
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
