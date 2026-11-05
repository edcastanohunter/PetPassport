<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

interface DirectoryItem {
  id: string
  name: string
  city: string
  neighborhood?: string
  services: string[]
  phone?: string
  distance_km?: number
}

const q = ref('')
const city = ref('')
const selectedService = ref('')
const radius = ref(15)
const userGeo = ref<{ lat: number; lon: number } | null>(null)
const clinics = ref<DirectoryItem[]>([])
const loading = ref(false)
const geoLoading = ref(false)
const error = ref<string | null>(null)

const AVAILABLE_SERVICES = [
  { id: 'urgencias_24h', label: 'Urgencias 24h' },
  { id: 'cirugia', label: 'Cirugía' },
  { id: 'hospitalizacion', label: 'Hospitalización' },
  { id: 'laboratorio', label: 'Laboratorio' },
  { id: 'ecografia', label: 'Ecografía' },
  { id: 'vacunacion', label: 'Vacunación' },
  { id: 'odontologia', label: 'Odontología' },
  { id: 'dermatologia', label: 'Dermatología' },
]

onMounted(() => {
  search()
})

async function search() {
  loading.value = true
  error.value = null
  try {
    const params = new URLSearchParams()
    if (q.value.trim()) params.set('q', q.value.trim())
    if (city.value.trim()) params.set('city', city.value.trim())
    if (selectedService.value) params.set('service', selectedService.value)
    if (userGeo.value) {
      params.set('lat', String(userGeo.value.lat))
      params.set('lon', String(userGeo.value.lon))
      params.set('radius_km', String(radius.value))
    }

    const res = await fetch(`/api/v1/directory?${params.toString()}`)
    if (!res.ok) throw new Error('Error al consultar el directorio')
    const data = await res.json()
    clinics.value = data.data ?? []
  } catch (err: any) {
    error.value = err.message || 'Error al buscar clínicas'
  } finally {
    loading.value = false
  }
}

function locateMe() {
  if (!navigator.geolocation) {
    alert('Tu navegador no soporta geolocalización.')
    return
  }
  geoLoading.value = true
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      userGeo.value = {
        lat: pos.coords.latitude,
        lon: pos.coords.longitude,
      }
      geoLoading.value = false
      search()
    },
    (err) => {
      geoLoading.value = false
      alert('No se pudo obtener tu ubicación: ' + err.message)
    },
    { timeout: 10000, enableHighAccuracy: true },
  )
}

function clearGeo() {
  userGeo.value = null
  search()
}
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 py-8 space-y-6">
    <div class="text-center space-y-2">
      <h1 class="text-3xl font-bold tracking-tight text-surface-900">
        Directorio Veterinario
      </h1>
      <p class="text-surface-600 max-w-2xl mx-auto">
        Encuentra clínicas veterinarias verificadas, servicios especializados y atención cerca de ti.
      </p>
    </div>

    <!-- Barra de búsqueda y filtros -->
    <div class="bg-surface-0 border border-surface-200 rounded-xl p-4 shadow-sm space-y-4">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div class="relative">
          <input
            v-model="q"
            type="text"
            placeholder="Buscar por nombre o servicio..."
            class="w-full px-3 py-2 text-sm border rounded-lg border-surface-300 focus:outline-none focus:ring-2 focus:ring-primary-500"
            @keyup.enter="search"
          />
        </div>
        <div>
          <input
            v-model="city"
            type="text"
            placeholder="Ciudad (ej. Bogotá, Medellín)..."
            class="w-full px-3 py-2 text-sm border rounded-lg border-surface-300 focus:outline-none focus:ring-2 focus:ring-primary-500"
            @keyup.enter="search"
          />
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="flex-1 px-4 py-2 text-sm font-medium rounded-lg text-white bg-primary-600 hover:bg-primary-700 transition"
            :disabled="loading"
            @click="search"
          >
            {{ loading ? 'Buscando...' : 'Buscar' }}
          </button>
          <button
            type="button"
            class="px-3 py-2 text-sm font-medium rounded-lg border border-surface-300 text-surface-700 hover:bg-surface-50 transition flex items-center gap-1.5"
            :class="{ 'border-primary-500 text-primary-700 bg-primary-50': !!userGeo }"
            :disabled="geoLoading"
            @click="userGeo ? clearGeo() : locateMe()"
          >
            <span class="pi pi-map-marker"></span>
            <span>{{ userGeo ? 'Quitar GPS' : 'Cerca de mí' }}</span>
          </button>
        </div>
      </div>

      <!-- Servicios Pills -->
      <div class="flex flex-wrap items-center gap-2 pt-2 border-t border-surface-100">
        <span class="text-xs font-medium text-surface-500 uppercase tracking-wider">Servicio:</span>
        <button
          type="button"
          class="px-2.5 py-1 text-xs rounded-full font-medium transition"
          :class="
            selectedService === ''
              ? 'bg-surface-900 text-white'
              : 'bg-surface-100 text-surface-700 hover:bg-surface-200'
          "
          @click="selectedService = ''; search()"
        >
          Todos
        </button>
        <button
          v-for="svc in AVAILABLE_SERVICES"
          :key="svc.id"
          type="button"
          class="px-2.5 py-1 text-xs rounded-full font-medium transition"
          :class="
            selectedService === svc.id
              ? 'bg-primary-600 text-white'
              : 'bg-surface-100 text-surface-700 hover:bg-surface-200'
          "
          @click="selectedService = (selectedService === svc.id ? '' : svc.id); search()"
        >
          {{ svc.label }}
        </button>
      </div>
    </div>

    <!-- Mensaje de error -->
    <div v-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg text-sm">
      {{ error }}
    </div>

    <!-- Estado cargando -->
    <div v-if="loading" class="text-center py-12 text-surface-500 text-sm">
      Buscando centros veterinarios...
    </div>

    <!-- Resultados -->
    <div v-else-if="clinics.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <div
        v-for="clinic in clinics"
        :key="clinic.id"
        class="bg-surface-0 border border-surface-200 rounded-xl p-5 shadow-sm hover:shadow transition flex flex-col justify-between space-y-4"
      >
        <div class="space-y-2">
          <div class="flex items-start justify-between gap-2">
            <h2 class="font-semibold text-surface-900 text-lg leading-tight">
              {{ clinic.name }}
            </h2>
            <span
              v-if="clinic.distance_km !== undefined"
              class="inline-block px-2 py-0.5 text-xs font-semibold rounded bg-primary-50 text-primary-700 shrink-0"
            >
              a {{ clinic.distance_km.toFixed(1) }} km
            </span>
          </div>

          <div class="text-sm text-surface-600 flex items-center gap-1.5">
            <span class="pi pi-compass text-surface-400"></span>
            <span>{{ clinic.city }}<span v-if="clinic.neighborhood">, {{ clinic.neighborhood }}</span></span>
          </div>

          <div v-if="clinic.phone" class="text-sm text-surface-600 flex items-center gap-1.5">
            <span class="pi pi-phone text-surface-400"></span>
            <a :href="`tel:${clinic.phone}`" class="text-primary-600 hover:underline">
              {{ clinic.phone }}
            </a>
          </div>

          <!-- Servicios ofrecidos -->
          <div v-if="clinic.services && clinic.services.length > 0" class="flex flex-wrap gap-1.5 pt-2">
            <span
              v-for="s in clinic.services"
              :key="s"
              class="px-2 py-0.5 text-xs rounded bg-surface-100 text-surface-700 font-medium"
            >
              {{ s.replace('_', ' ') }}
            </span>
          </div>
        </div>

        <div class="pt-2 border-t border-surface-100 flex items-center justify-end">
          <span class="text-xs text-primary-600 font-medium flex items-center gap-1">
            Clínica Verificada
            <span class="pi pi-check-circle text-green-600"></span>
          </span>
        </div>
      </div>
    </div>

    <!-- Sin resultados -->
    <div v-else class="text-center py-16 bg-surface-50 rounded-xl space-y-2">
      <div class="pi pi-search text-3xl text-surface-400"></div>
      <h3 class="font-medium text-surface-800">No se encontraron clínicas veterinarias</h3>
      <p class="text-sm text-surface-500 max-w-md mx-auto">
        Prueba a buscar con otros términos, cambiar la ciudad o ampliar el radio de búsqueda.
      </p>
    </div>
  </div>
</template>
