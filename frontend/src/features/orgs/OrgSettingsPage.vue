<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Organization } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { countries } from '@/shared/i18n/countries'
import { organizationFormSchema, type OrganizationForm } from '@/shared/validation/schemas'
import FieldError from '@/shared/ui/FieldError.vue'
import { useOrgStore } from './orgStore'

// Ajustes de organización (FE-007, PRD-012): edición del perfil con If-Match.
// Aviso: cambiar razón social o identificación fiscal de una organización
// verificada la devuelve a pending_verification.
const { t, locale } = useI18n()
const COUNTRIES = computed(() => countries(locale.value))
const route = useRoute()
const session = useSessionStore()
const store = useOrgStore()

const orgId = computed(() => String(route.params.orgId))
const form = reactive<OrganizationForm>({
  type: 'clinic',
  legal_name: '',
  display_name: '',
  tax_country: 'CO',
  tax_value: '',
  address: { country: 'CO', region: '', city: '', line1: '', line2: '', postal_code: '' },
  contact: { email: '', phone: '', website: '' },
})
const original = ref<Organization | null>(null)
const fieldErrors = ref<Record<string, string>>({})
const error = ref<string | null>(null)
const saved = ref(false)
const conflict = ref(false)
const saving = ref(false)

// Pestañas (General vs Directorio FE-007)
const activeTab = ref<'general' | 'directory'>('general')
const dirListed = ref(false)
const dirServices = ref<string[]>([])
const dirShowPhone = ref(true)
const dirNeighborhood = ref('')
const dirLat = ref<number | null>(null)
const dirLon = ref<number | null>(null)
const dirSaving = ref(false)
const dirSaved = ref(false)
const dirError = ref<string | null>(null)

const AVAILABLE_DIR_SERVICES = [
  'urgencias_24h', 'cirugia', 'hospitalizacion', 'laboratorio',
  'ecografia', 'vacunacion', 'odontologia', 'dermatologia',
  'oftalmologia', 'peluqueria'
]

function toggleService(s: string) {
  if (dirServices.value.includes(s)) {
    dirServices.value = dirServices.value.filter((x) => x !== s)
  } else if (dirServices.value.length < 10) {
    dirServices.value.push(s)
  }
}

function captureLocation() {
  if (!navigator.geolocation) return
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      dirLat.value = Math.round(pos.coords.latitude * 1000) / 1000
      dirLon.value = Math.round(pos.coords.longitude * 1000) / 1000
    },
    () => alert('No se pudo obtener la ubicación GPS'),
  )
}

async function saveDirectory() {
  dirSaving.value = true
  dirSaved.value = false
  dirError.value = null
  try {
    const res = await fetch(`/api/v1/organizations/${orgId.value}/directory-listing`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        listed: dirListed.value,
        services: dirServices.value,
        show_phone: dirShowPhone.value,
        neighborhood: dirNeighborhood.value ? dirNeighborhood.value : undefined,
        geo_lat: dirLat.value ?? undefined,
        geo_lon: dirLon.value ?? undefined,
      }),
    })
    if (!res.ok) throw new Error('Error guardando ficha de directorio')
    dirSaved.value = true
  } catch (err: any) {
    dirError.value = err.message || 'Error'
  } finally {
    dirSaving.value = false
  }
}

function fill(o: Organization) {
  original.value = o
  form.type = o.type
  form.legal_name = o.legal_name
  form.display_name = o.display_name
  form.tax_country = o.tax_id.country
  form.tax_value = o.tax_id.value
  form.address = {
    country: o.address.country,
    region: o.address.region ?? '',
    city: o.address.city,
    line1: o.address.line1,
    line2: o.address.line2 ?? '',
    postal_code: o.address.postal_code ?? '',
  }
  form.contact = {
    email: o.contact.email,
    phone: o.contact.phone,
    website: o.contact.website ?? '',
  }
}

async function load() {
  conflict.value = false
  try {
    fill(await store.load(orgId.value))
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  }
}

onMounted(load)

const legalChanged = computed(() => {
  const o = original.value
  if (!o) return false
  return (
    form.legal_name.trim() !== o.legal_name ||
    form.tax_country.toUpperCase() !== o.tax_id.country ||
    form.tax_value.trim().toUpperCase() !== o.tax_id.value
  )
})

function optional(v?: string) {
  const s = v?.trim()
  return s ? s : undefined
}

async function save() {
  saved.value = false
  error.value = null
  conflict.value = false
  const parsed = organizationFormSchema().safeParse(form)
  if (!parsed.success) {
    const errs: Record<string, string> = {}
    for (const issue of parsed.error.issues) errs[issue.path.join('.')] ??= 'INVALID'
    fieldErrors.value = errs
    return
  }
  fieldErrors.value = {}
  saving.value = true
  try {
    const { data, error: apiError } = await api().PATCH('/organizations/{orgId}', {
      params: {
        path: { orgId: orgId.value },
        header: { 'If-Match': original.value?.version ?? 0 },
      },
      body: {
        legal_name: form.legal_name.trim(),
        display_name: form.display_name.trim(),
        tax_id: { country: form.tax_country.toUpperCase(), value: form.tax_value.trim() },
        address: {
          country: form.address.country.toUpperCase(),
          region: optional(form.address.region),
          city: form.address.city.trim(),
          line1: form.address.line1.trim(),
          line2: optional(form.address.line2),
          postal_code: optional(form.address.postal_code),
        },
        contact: {
          email: form.contact.email.trim(),
          phone: form.contact.phone.trim(),
          website: optional(form.contact.website) ?? null,
        },
      },
    })
    if (apiError) throw apiError
    fill(data as Organization)
    store.current = data as Organization
    saved.value = true
    // El estado de la org (p. ej. vuelta a pendiente) viaja en /me.memberships.
    await session.loadMe(true)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'CONFLICT_VERSION') {
      conflict.value = true
    } else if (e instanceof ApiError) {
      const errs = e.fieldErrors()
      fieldErrors.value = errs['tax_id'] ? { tax_value: errs['tax_id'] } : errs
      error.value = t(`errors.${e.code}`, t('errors.GENERIC'))
    } else {
      error.value = t('errors.GENERIC')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="mx-auto max-w-xl space-y-6">
    <h1 class="text-2xl font-bold">{{ t('orgs.nav.settings') }}</h1>

    <!-- Tabs General / Directorio (FE-007) -->
    <div class="flex border-b border-surface-200 gap-4 text-sm font-medium">
      <button
        type="button"
        class="pb-2 border-b-2 transition"
        :class="
          activeTab === 'general'
            ? 'border-primary-600 text-primary-600'
            : 'border-transparent text-surface-500 hover:text-surface-700'
        "
        @click="activeTab = 'general'"
      >
        General
      </button>
      <button
        type="button"
        class="pb-2 border-b-2 transition"
        :class="
          activeTab === 'directory'
            ? 'border-primary-600 text-primary-600'
            : 'border-transparent text-surface-500 hover:text-surface-700'
        "
        @click="activeTab = 'directory'"
      >
        Directorio Público
      </button>
    </div>

    <!-- Pestaña Directorio (FE-007) -->
    <div v-if="activeTab === 'directory'" class="space-y-6">
      <div class="space-y-1">
        <h2 class="text-lg font-semibold text-surface-900">Ficha en Directorio Público</h2>
        <p class="text-sm text-surface-600">
          Configura cómo aparece tu centro veterinario en el directorio público para que los tutores te encuentren.
        </p>
      </div>

      <p v-if="dirSaved" class="p-3 bg-green-50 text-green-700 text-sm rounded-lg">
        Ficha de directorio guardada con éxito.
      </p>
      <p v-if="dirError" class="p-3 bg-red-50 text-red-700 text-sm rounded-lg">
        {{ dirError }}
      </p>

      <form class="space-y-5" @submit.prevent="saveDirectory">
        <label class="flex items-start gap-3 p-3 bg-surface-50 rounded-lg border border-surface-200 cursor-pointer">
          <input v-model="dirListed" type="checkbox" class="mt-1" />
          <span>
            <span class="block font-medium text-surface-900">Listar en el Directorio Público</span>
            <span class="block text-xs text-surface-500">
              Permite que cualquier persona encuentre tu centro por ciudad o cercanía.
            </span>
          </span>
        </label>

        <div>
          <label class="label">Barrio o zona</label>
          <input
            v-model="dirNeighborhood"
            type="text"
            placeholder="Ej. El Poblado, Chapinero, etc."
            class="input"
          />
        </div>

        <label class="flex items-center gap-2 cursor-pointer text-sm">
          <input v-model="dirShowPhone" type="checkbox" />
          <span>Mostrar teléfono de contacto en el directorio</span>
        </label>

        <div class="space-y-2">
          <label class="label">Servicios ofrecidos (hasta 10)</label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="s in AVAILABLE_DIR_SERVICES"
              :key="s"
              type="button"
              class="px-2.5 py-1 text-xs rounded-full font-medium transition"
              :class="
                dirServices.includes(s)
                  ? 'bg-primary-600 text-white'
                  : 'bg-surface-100 text-surface-700 hover:bg-surface-200'
              "
              @click="toggleService(s)"
            >
              {{ s.replace('_', ' ') }}
            </button>
          </div>
        </div>

        <div class="space-y-2 p-3 bg-surface-50 rounded-lg border border-surface-200 text-sm">
          <div class="font-medium text-surface-800">Ubicación Geográfica (Geo)</div>
          <div class="grid grid-cols-2 gap-2">
            <div>
              <label class="text-xs text-surface-500">Latitud (redondeada a 3 decimales)</label>
              <input v-model.number="dirLat" type="number" step="0.001" class="input text-xs" />
            </div>
            <div>
              <label class="text-xs text-surface-500">Longitud (redondeada a 3 decimales)</label>
              <input v-model.number="dirLon" type="number" step="0.001" class="input text-xs" />
            </div>
          </div>
          <button
            type="button"
            class="text-xs text-primary-600 hover:underline flex items-center gap-1 mt-1"
            @click="captureLocation"
          >
            <span class="pi pi-map-marker"></span>
            Usar ubicación actual GPS
          </button>
        </div>

        <button type="submit" class="btn-primary" :disabled="dirSaving">
          {{ dirSaving ? 'Guardando...' : 'Guardar ficha de directorio' }}
        </button>
      </form>
    </div>

    <!-- Pestaña General -->
    <div v-else-if="activeTab === 'general'" class="space-y-6">
    <p
      v-if="conflict"
      class="rounded-lg bg-amber-50 p-4 text-amber-800 dark:bg-amber-950 dark:text-amber-200"
      role="alert"
    >
      {{ t('orgs.settings.conflict') }}
      <button type="button" class="ml-2 underline" @click="load">{{ t('pets.reload') }}</button>
    </p>
    <p
      v-if="error"
      class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-if="saved"
      class="rounded-lg bg-green-50 p-4 text-green-800 dark:bg-green-950 dark:text-green-200"
      role="status"
    >
      {{ t('orgs.settings.saved') }}
    </p>

    <p
      class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200"
    >
      {{ t('orgs.settings.verificationWarning') }}
    </p>

    <form v-if="original" class="space-y-4" novalidate @submit.prevent="save">
      <div>
        <label class="label" for="set-display">{{ t('orgs.form.displayName') }}</label>
        <input id="set-display" v-model="form.display_name" maxlength="80" class="input" />
        <FieldError field="set-display" :code="fieldErrors['display_name']" />
      </div>
      <div>
        <label class="label" for="set-legal">{{ t('orgs.form.legalName') }}</label>
        <input id="set-legal" v-model="form.legal_name" maxlength="120" class="input" />
        <FieldError field="set-legal" :code="fieldErrors['legal_name']" />
      </div>
      <div class="grid grid-cols-3 gap-3">
        <div>
          <label class="label" for="set-tax-country">{{ t('orgs.form.taxCountry') }}</label>
          <select id="set-tax-country" v-model="form.tax_country" class="input">
            <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
        </div>
        <div class="col-span-2">
          <label class="label" for="set-tax-value">{{ t('orgs.form.taxValue') }}</label>
          <input
            id="set-tax-value"
            v-model="form.tax_value"
            maxlength="32"
            class="input font-mono"
          />
          <FieldError field="set-tax-value" :code="fieldErrors['tax_value']" />
        </div>
      </div>
      <p
        v-if="legalChanged && original.status === 'active'"
        class="text-sm font-medium text-amber-700 dark:text-amber-300"
        role="status"
        data-testid="reset-warning"
      >
        {{ t('orgs.settings.willReset') }}
      </p>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="label" for="set-city">{{ t('orgs.form.city') }}</label>
          <input id="set-city" v-model="form.address.city" maxlength="120" class="input" />
          <FieldError field="set-city" :code="fieldErrors['address.city']" />
        </div>
        <div>
          <label class="label" for="set-region">{{ t('orgs.form.region') }}</label>
          <input id="set-region" v-model="form.address.region" maxlength="120" class="input" />
        </div>
      </div>
      <div>
        <label class="label" for="set-line1">{{ t('orgs.form.line1') }}</label>
        <input id="set-line1" v-model="form.address.line1" maxlength="200" class="input" />
        <FieldError field="set-line1" :code="fieldErrors['address.line1']" />
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="label" for="set-email">{{ t('orgs.form.email') }}</label>
          <input id="set-email" v-model="form.contact.email" type="email" class="input" />
          <FieldError field="set-email" :code="fieldErrors['contact.email']" />
        </div>
        <div>
          <label class="label" for="set-phone">{{ t('orgs.form.phone') }}</label>
          <input id="set-phone" v-model="form.contact.phone" type="tel" class="input" />
          <FieldError field="set-phone" :code="fieldErrors['contact.phone']" />
        </div>
      </div>
      <div>
        <label class="label" for="set-web"
          >{{ t('orgs.form.website') }} ({{ t('common.optional') }})</label
        >
        <input id="set-web" v-model="form.contact.website" type="url" class="input" />
        <FieldError field="set-web" :code="fieldErrors['contact.website']" />
      </div>
      <button type="submit" class="btn-primary" :disabled="saving" data-testid="save-org">
        {{ saving ? t('common.saving') : t('common.save') }}
      </button>
    </form>
    </div>
  </section>
</template>
