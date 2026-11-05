<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type PatientRow } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import CareLinkRequestPanel from './CareLinkRequestPanel.vue'
import { useOrgStore } from './orgStore'
import {
  listProvisionalPets,
  registerProvisionalPet,
  resendClaim,
  importProvisionalPets,
  type ProvisionalPetItem,
  type RegisterProvisionalPayload,
} from './provisionalPetsApi'

// Pacientes de la clínica (FE-008, FE-009, PRD-016):
// Pestañas "Pacientes vinculados" y "Pacientes provisionales".
// Pacientes vinculados: búsqueda simple (q), paginación por cursor, columnas foto/mascota/especie-raza/edad/tutor/vinculado.
// Pacientes provisionales: "Nuevo paciente sin cuenta", lista con estado de reclamo y "Reenviar invitación", importador CSV.
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const store = useOrgStore()

const orgId = computed(() => String(route.params.orgId))
const suspended = computed(() => session.membershipFor(orgId.value)?.org_status === 'suspended')

const activeTab = ref<'linked' | 'provisional'>('linked')

// Tab 1: Vinculados
const rows = ref<PatientRow[]>([])
const nextCursor = ref<string | null>(null)
const q = ref('')
const loading = ref(false)
const error = ref<string | null>(null)

// Tab 2: Provisionales
const provisionalRows = ref<ProvisionalPetItem[]>([])
const provisionalLoading = ref(false)
const provisionalError = ref<string | null>(null)
const provisionalNotice = ref<string | null>(null)
const provisionalFilter = ref('')

// Modal Nuevo Paciente Provisional
const showNewModal = ref(false)
const newBusy = ref(false)
const newForm = ref<RegisterProvisionalPayload>({
  pet: {
    name: '',
    species: 'canine',
    breed: '',
    sex: 'female',
    neutered: 'unknown',
    microchip: '',
  },
  guardian_contact: {
    email: '',
    phone: '',
  },
  send_invitation: true,
})

// Modal Importar CSV
const showImportModal = ref(false)
const importBusy = ref(false)
const importFile = ref<File | null>(null)
const importResult = ref<{ imported: number; errors: { row: number; error: string }[] } | null>(null)

async function fetchPage(reset: boolean) {
  if (suspended.value) return
  loading.value = true
  error.value = null
  try {
    const { data, error: apiError } = await api().GET('/organizations/{orgId}/patients', {
      params: {
        path: { orgId: orgId.value },
        query: {
          q: q.value.trim() || undefined,
          cursor: reset ? undefined : (nextCursor.value ?? undefined),
          limit: 20,
        },
      },
    })
    if (apiError) throw apiError
    rows.value = reset ? (data?.data ?? []) : [...rows.value, ...(data?.data ?? [])]
    nextCursor.value = data?.next_cursor ?? null
  } catch (e) {
    error.value =
      e instanceof ApiError ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
  } finally {
    loading.value = false
  }
}

async function fetchProvisional() {
  if (suspended.value) return
  provisionalLoading.value = true
  provisionalError.value = null
  try {
    provisionalRows.value = await listProvisionalPets(orgId.value, provisionalFilter.value || undefined)
  } catch (e: unknown) {
    provisionalError.value = e instanceof Error ? e.message : t('common.errorGeneric')
  } finally {
    provisionalLoading.value = false
  }
}

function switchTab(tab: 'linked' | 'provisional') {
  activeTab.value = tab
  if (tab === 'provisional' && provisionalRows.value.length === 0) {
    void fetchProvisional()
  }
}

onMounted(async () => {
  if (!suspended.value && (!store.current || store.current.id !== orgId.value)) {
    await store.load(orgId.value).catch(() => undefined)
  }
  await fetchPage(true)
})

function speciesLabel(s: string) {
  const key = `pets.species.${s}`
  return te(key) ? t(key) : s
}

function age(row: PatientRow) {
  const bd = row.pet.birth_date
  if (!bd) return null
  const b = new Date(`${bd.date}T00:00:00Z`)
  const now = new Date()
  let months =
    (now.getUTCFullYear() - b.getUTCFullYear()) * 12 + (now.getUTCMonth() - b.getUTCMonth())
  if (now.getUTCDate() < b.getUTCDate()) months--
  months = Math.max(months, 0)
  return t('pets.ageShort', { years: Math.floor(months / 12), months: months % 12 })
}

function open(row: PatientRow) {
  router.push({ name: 'org-patient', params: { orgId: orgId.value, petId: row.pet.id } })
}

function openProvisional(petId: string) {
  router.push({ name: 'org-patient', params: { orgId: orgId.value, petId } })
}

async function handleResendClaim(petId: string) {
  provisionalNotice.value = null
  provisionalError.value = null
  try {
    await resendClaim(orgId.value, petId)
    provisionalNotice.value = t('pets.provisional.resendSuccess')
    await fetchProvisional()
  } catch (e: unknown) {
    provisionalError.value = e instanceof Error ? e.message : t('common.errorGeneric')
  }
}

async function handleCreateProvisional() {
  newBusy.value = true
  provisionalError.value = null
  provisionalNotice.value = null
  try {
    await registerProvisionalPet(orgId.value, newForm.value)
    showNewModal.value = false
    // Limpiar formulario
    newForm.value = {
      pet: { name: '', species: 'canine', breed: '', sex: 'female', neutered: 'unknown', microchip: '' },
      guardian_contact: { email: '', phone: '' },
      send_invitation: true,
    }
    provisionalNotice.value = t('pets.provisional.createSuccess')
    await fetchProvisional()
  } catch (e: unknown) {
    provisionalError.value = e instanceof Error ? e.message : t('common.errorGeneric')
  } finally {
    newBusy.value = false
  }
}

function handleFileChange(event: Event) {
  const target = event.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    importFile.value = target.files[0]
  }
}

async function handleImportCSV() {
  if (!importFile.value) return
  importBusy.value = true
  importResult.value = null
  provisionalError.value = null
  try {
    const res = await importProvisionalPets(orgId.value, importFile.value)
    importResult.value = res
    if (res.imported > 0) {
      await fetchProvisional()
    }
  } catch (e: unknown) {
    provisionalError.value = e instanceof Error ? e.message : t('common.errorGeneric')
  } finally {
    importBusy.value = false
  }
}
</script>

<template>
  <section class="space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-4">
      <h1 class="text-2xl font-bold">{{ t('orgs.nav.patients') }}</h1>
      <div v-if="!suspended" class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          class="btn-primary text-sm"
          data-testid="btn-new-provisional"
          @click="showNewModal = true"
        >
          <span class="pi pi-plus mr-1" />
          {{ t('pets.provisional.newButton') }}
        </button>
        <button
          type="button"
          class="btn-secondary text-sm"
          data-testid="btn-import-csv"
          @click="showImportModal = true"
        >
          <span class="pi pi-upload mr-1" />
          {{ t('pets.provisional.importButton') }}
        </button>
      </div>
    </header>

    <p
      v-if="suspended"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
      role="alert"
      data-testid="org-suspended"
    >
      {{ t('errors.ORGANIZATION_SUSPENDED') }}
    </p>

    <template v-else>
      <!-- Selector de pestañas -->
      <nav class="flex border-b border-surface-200 dark:border-surface-800" aria-label="Pestañas de pacientes">
        <button
          type="button"
          class="px-4 py-2 text-sm font-medium border-b-2"
          :class="activeTab === 'linked' ? 'border-primary-600 text-primary-600' : 'border-transparent text-surface-500 hover:text-surface-700'"
          data-testid="tab-linked"
          @click="switchTab('linked')"
        >
          {{ t('pets.provisional.linkedTab') }}
        </button>
        <button
          type="button"
          class="px-4 py-2 text-sm font-medium border-b-2"
          :class="activeTab === 'provisional' ? 'border-primary-600 text-primary-600' : 'border-transparent text-surface-500 hover:text-surface-700'"
          data-testid="tab-provisional"
          @click="switchTab('provisional')"
        >
          {{ t('pets.provisional.tab') }}
        </button>
      </nav>

      <!-- TAB 1: PACIENTES VINCULADOS -->
      <div v-if="activeTab === 'linked'" class="space-y-4">
        <form class="flex gap-2" role="search" @submit.prevent="fetchPage(true)">
          <label class="sr-only" for="patients-q">{{ t('orgs.patients.search') }}</label>
          <input
            id="patients-q"
            v-model="q"
            type="search"
            maxlength="80"
            class="input"
            :placeholder="t('orgs.patients.searchPlaceholder')"
          />
          <button type="submit" class="btn-secondary">{{ t('orgs.patients.search') }}</button>
        </form>

        <p
          v-if="error"
          class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
          role="alert"
        >
          {{ error }}
        </p>

        <div
          v-if="!loading && !rows.length && !error"
          class="rounded-xl border border-dashed border-surface-200 p-8 text-center text-surface-500 dark:border-surface-800"
          data-testid="patients-empty"
        >
          <template v-if="q">{{ t('orgs.patients.noResults') }}</template>
          <template v-else>
            {{ t('orgs.patients.empty', { code: store.current?.public_code ?? '' }) }}
          </template>
        </div>

        <table v-if="rows.length" class="hidden w-full text-left text-sm md:table">
          <thead class="text-surface-500">
            <tr>
              <th class="py-2">
                <span class="sr-only">{{ t('orgs.patients.photo') }}</span>
              </th>
              <th>{{ t('orgs.patients.pet') }}</th>
              <th>{{ t('orgs.patients.speciesBreed') }}</th>
              <th>{{ t('orgs.patients.age') }}</th>
              <th>{{ t('orgs.patients.guardian') }}</th>
              <th>{{ t('orgs.patients.linkedAt') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in rows"
              :key="row.pet.id"
              class="cursor-pointer border-t border-surface-200 hover:bg-surface-200/30 dark:border-surface-800"
              data-testid="patient-row"
              @click="open(row)"
            >
              <td class="py-2">
                <img
                  v-if="row.pet.photo"
                  :src="row.pet.photo.thumb_url"
                  :alt="t('pets.photoAlt', { name: row.pet.name })"
                  width="40"
                  height="40"
                  loading="lazy"
                  decoding="async"
                  class="h-10 w-10 rounded-full object-cover"
                />
                <span
                  v-else
                  class="flex h-10 w-10 items-center justify-center rounded-full bg-surface-200/60 font-bold"
                  aria-hidden="true"
                >
                  {{ row.pet.name.charAt(0).toUpperCase() }}
                </span>
              </td>
              <td>
                <RouterLink
                  :to="{ name: 'org-patient', params: { orgId, petId: row.pet.id } }"
                  class="font-medium underline"
                  @click.stop
                >
                  {{ row.pet.name }}
                </RouterLink>
                <span v-if="row.pet.status === 'deceased'" class="ml-1 text-xs text-surface-500"
                  >({{ t('pets.inMemoriam') }})</span
                >
              </td>
              <td>
                {{ speciesLabel(row.pet.species)
                }}<template v-if="row.pet.breed"> · {{ row.pet.breed }}</template>
              </td>
              <td>{{ age(row) ?? '—' }}</td>
              <td>
                {{ row.guardian.display_name }}
                <a
                  v-if="row.guardian.phone"
                  :href="`tel:${row.guardian.phone}`"
                  class="block text-xs underline"
                  @click.stop
                >
                  {{ row.guardian.phone }}
                </a>
              </td>
              <td>{{ d(new Date(row.linked_at), 'short') }}</td>
            </tr>
          </tbody>
        </table>

        <ul v-if="rows.length" class="space-y-3 md:hidden">
          <li v-for="row in rows" :key="row.pet.id">
            <button type="button" class="card w-full" @click="open(row)">
              <span class="block font-semibold">{{ row.pet.name }}</span>
              <span class="block text-sm text-surface-500">
                {{ speciesLabel(row.pet.species) }} · {{ row.guardian.display_name }}
              </span>
            </button>
          </li>
        </ul>

        <button
          v-if="nextCursor"
          type="button"
          class="btn-secondary"
          :disabled="loading"
          data-testid="patients-more"
          @click="fetchPage(false)"
        >
          {{ t('orgs.patients.more') }}
        </button>
        <p v-if="loading" class="text-surface-500">{{ t('common.loading') }}</p>

        <CareLinkRequestPanel :org-id="orgId" />
      </div>

      <!-- TAB 2: PACIENTES PROVISIONALES (PP-F3-FE-009) -->
      <div v-else class="space-y-4" data-testid="provisional-tab-content">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <label for="provisional-filter" class="text-xs text-surface-500">{{ t('pets.provisional.statusFilter') }}</label>
            <select
              id="provisional-filter"
              v-model="provisionalFilter"
              class="input py-1 text-sm"
              @change="fetchProvisional"
            >
              <option value="">{{ t('common.allStatuses') }}</option>
              <option value="pending">{{ t('pets.provisional.statusPending') }}</option>
              <option value="claimed">{{ t('pets.provisional.statusClaimed') }}</option>
              <option value="expired">{{ t('pets.provisional.statusExpired') }}</option>
            </select>
          </div>
          <button
            type="button"
            class="btn-secondary text-sm"
            :disabled="provisionalLoading"
            @click="fetchProvisional"
          >
            <span class="pi pi-refresh mr-1" :class="{ 'animate-spin': provisionalLoading }" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <div
          v-if="provisionalNotice"
          class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200"
          role="status"
        >
          {{ provisionalNotice }}
        </div>

        <div
          v-if="provisionalError"
          class="rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
          role="alert"
        >
          {{ provisionalError }}
        </div>

        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="text-surface-500 border-b border-surface-200 dark:border-surface-800">
              <tr>
                <th class="py-2">{{ t('orgs.patients.pet') }}</th>
                <th class="py-2">{{ t('pets.provisional.microchip') }}</th>
                <th class="py-2">{{ t('pets.provisional.contact') }}</th>
                <th class="py-2">{{ t('pets.provisional.claimStatus') }}</th>
                <th class="py-2 text-center">{{ t('pets.provisional.resends') }}</th>
                <th class="py-2 text-right">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="p in provisionalRows"
                :key="p.pet_id"
                class="border-b border-surface-100 hover:bg-surface-200/30 dark:border-surface-900 cursor-pointer"
                data-testid="provisional-row"
                @click="openProvisional(p.pet_id)"
              >
                <td class="py-2 font-medium">
                  {{ p.name }}
                  <span class="block text-xs text-surface-500">
                    {{ speciesLabel(p.species) }}<template v-if="p.breed"> · {{ p.breed }}</template>
                  </span>
                </td>
                <td class="py-2 font-mono text-xs">{{ p.microchip || '—' }}</td>
                <td class="py-2 font-mono text-xs">{{ p.contact_masked || '—' }}</td>
                <td class="py-2">
                  <span
                    class="rounded px-2 py-0.5 text-xs font-semibold"
                    :class="{
                      'bg-amber-100 text-amber-800': p.claim_status === 'pending',
                      'bg-emerald-100 text-emerald-800': p.claim_status === 'claimed',
                      'bg-surface-200 text-surface-700': p.claim_status === 'expired',
                    }"
                  >
                    {{ t(`pets.provisional.status_${p.claim_status}`) }}
                  </span>
                </td>
                <td class="py-2 text-center text-xs font-mono">
                  {{ p.resend_count }} / 3
                </td>
                <td class="py-2 text-right" @click.stop>
                  <button
                    v-if="p.claim_status === 'pending'"
                    type="button"
                    class="btn-secondary py-1 text-xs"
                    :disabled="p.resend_count >= 3"
                    data-testid="btn-resend-claim"
                    @click="handleResendClaim(p.pet_id)"
                  >
                    {{ t('pets.provisional.resend') }}
                  </button>
                </td>
              </tr>
              <tr v-if="provisionalRows.length === 0">
                <td colspan="6" class="py-6 text-center text-surface-400">
                  {{ t('pets.provisional.empty') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- MODAL NUEVO PACIENTE PROVISIONAL (PP-F3-FE-009) -->
    <div
      v-if="showNewModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
    >
      <form
        class="card w-full max-w-lg space-y-4 p-6"
        @submit.prevent="handleCreateProvisional"
      >
        <h2 class="text-xl font-bold">{{ t('pets.provisional.modalTitle') }}</h2>
        <p class="text-xs text-surface-500">{{ t('pets.provisional.modalSubtitle') }}</p>

        <div class="space-y-3 text-sm">
          <div>
            <label class="block font-medium mb-1">{{ t('pets.form.name') }} *</label>
            <input v-model="newForm.pet.name" required class="input" />
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block font-medium mb-1">{{ t('pets.form.species') }} *</label>
              <select v-model="newForm.pet.species" class="input">
                <option value="canine">{{ t('pets.species.dog') }}</option>
                <option value="feline">{{ t('pets.species.cat') }}</option>
                <option value="other">{{ t('pets.species.other') }}</option>
              </select>
            </div>
            <div>
              <label class="block font-medium mb-1">{{ t('pets.form.breed') }}</label>
              <input v-model="newForm.pet.breed" class="input" />
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block font-medium mb-1">{{ t('pets.form.sex') }}</label>
              <select v-model="newForm.pet.sex" class="input">
                <option value="female">{{ t('pets.sex.female') }}</option>
                <option value="male">{{ t('pets.sex.male') }}</option>
                <option value="unknown">{{ t('pets.sex.unknown') }}</option>
              </select>
            </div>
            <div>
              <label class="block font-medium mb-1">{{ t('pets.microchip') }}</label>
              <input v-model="newForm.pet.microchip" class="input" placeholder="15 dígitos" />
            </div>
          </div>

          <div class="border-t border-surface-200 pt-3 dark:border-surface-800 space-y-2">
            <h3 class="font-semibold text-xs text-surface-600 uppercase">{{ t('pets.provisional.guardianContact') }}</h3>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs mb-1">{{ t('pets.provisional.guardianEmail') }}</label>
                <input v-model="newForm.guardian_contact.email" type="email" class="input" />
              </div>
              <div>
                <label class="block text-xs mb-1">{{ t('pets.provisional.guardianPhone') }}</label>
                <input v-model="newForm.guardian_contact.phone" type="tel" class="input" />
              </div>
            </div>
            <label class="flex items-center gap-2 pt-1 text-xs">
              <input v-model="newForm.send_invitation" type="checkbox" />
              <span>{{ t('pets.provisional.sendInviteNow') }}</span>
            </label>
          </div>
        </div>

        <div class="flex justify-end gap-2 pt-3">
          <button
            type="button"
            class="btn-secondary text-sm"
            :disabled="newBusy"
            @click="showNewModal = false"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn-primary text-sm"
            :disabled="newBusy || !newForm.pet.name"
            data-testid="btn-submit-provisional"
          >
            {{ newBusy ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </div>

    <!-- MODAL IMPORTAR CSV (PP-F3-FE-009) -->
    <div
      v-if="showImportModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
    >
      <form
        class="card w-full max-w-lg space-y-4 p-6"
        @submit.prevent="handleImportCSV"
      >
        <h2 class="text-xl font-bold">{{ t('pets.provisional.importTitle') }}</h2>
        <p class="text-xs text-surface-500">{{ t('pets.provisional.importSubtitle') }}</p>

        <div class="space-y-3">
          <input
            type="file"
            accept=".csv"
            class="block w-full text-sm text-surface-500 file:mr-4 file:py-2 file:px-4 file:rounded-md file:border-0 file:text-sm file:font-semibold file:bg-primary-50 file:text-primary-700 hover:file:bg-primary-100"
            @change="handleFileChange"
          />

          <div
            v-if="importResult"
            class="rounded-lg p-3 text-xs space-y-1"
            :class="importResult.errors.length ? 'bg-amber-50 text-amber-900' : 'bg-emerald-50 text-emerald-900'"
          >
            <p class="font-semibold">
              {{ t('pets.provisional.importedCount', { count: importResult.imported }) }}
            </p>
            <ul v-if="importResult.errors.length" class="list-disc pl-4 text-xs text-red-700">
              <li v-for="err in importResult.errors" :key="err.row">
                Fila {{ err.row }}: {{ err.error }}
              </li>
            </ul>
          </div>
        </div>

        <div class="flex justify-end gap-2 pt-3">
          <button
            type="button"
            class="btn-secondary text-sm"
            :disabled="importBusy"
            @click="showImportModal = false"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            class="btn-primary text-sm"
            :disabled="importBusy || !importFile"
            data-testid="btn-confirm-import"
          >
            {{ importBusy ? t('common.loading') : t('pets.provisional.importAction') }}
          </button>
        </div>
      </form>
    </div>
  </section>
</template>
