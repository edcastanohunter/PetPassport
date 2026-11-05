<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { createSuggestion, listOrgSuggestions } from '@/features/medications/medicationsApi'
import type { MedicationSuggestion } from '@/features/medications/medicationsTypes'

const route = useRoute()
const { t } = useI18n()

const orgId = computed(() => String(route.params.orgId))
const suggestions = ref<MedicationSuggestion[]>([])
const loading = ref(false)
const filterStatus = ref('')

const showNewModal = ref(false)
const newName = ref('')
const newNotes = ref('')
const newBusy = ref(false)
const newError = ref<string | null>(null)

async function load() {
  if (!orgId.value) return
  loading.value = true
  try {
    suggestions.value = await listOrgSuggestions(orgId.value, filterStatus.value || undefined)
  } catch (err) {
    console.error('Error cargando sugerencias:', err)
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  if (!newName.value.trim() || !orgId.value) return
  newBusy.value = true
  newError.value = null
  try {
    await createSuggestion({
      organization_id: orgId.value,
      name: newName.value.trim(),
      notes: newNotes.value.trim() || undefined,
    })
    showNewModal.value = false
    newName.value = ''
    newNotes.value = ''
    await load()
  } catch (err: unknown) {
    newError.value = err instanceof Error ? err.message : 'Error registrando sugerencia'
  } finally {
    newBusy.value = false
  }
}

onMounted(() => {
  load()
})
</script>

<template>
  <main class="space-y-6 p-6" data-testid="org-medication-suggestions-page">
    <header class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-surface-900 dark:text-surface-100">
          {{ t('medications.orgSuggestionsTitle', 'Sugerencias de medicamentos') }}
        </h1>
        <p class="text-sm text-surface-600 dark:text-surface-400">
          {{
            t(
              'medications.orgSuggestionsSubtitle',
              'Revisa el estado de los fármacos propuestos al catálogo central por el equipo.',
            )
          }}
        </p>
      </div>

      <button
        type="button"
        class="btn-primary"
        data-testid="btn-new-suggestion"
        @click="showNewModal = true"
      >
        <span class="pi pi-plus mr-2" aria-hidden="true" />
        {{ t('medications.newSuggestion', 'Proponer medicamento') }}
      </button>
    </header>

    <div class="flex items-center gap-3">
      <select v-model="filterStatus" class="input max-w-xs" @change="load">
        <option value="">{{ t('common.allStatuses', 'Todos los estados') }}</option>
        <option value="pending">{{ t('common.statusPending', 'Pendiente') }}</option>
        <option value="approved">{{ t('common.statusApproved', 'Aprobado') }}</option>
        <option value="rejected">{{ t('common.statusRejected', 'Rechazado') }}</option>
      </select>
    </div>

    <div
      class="overflow-x-auto rounded-xl border border-surface-200 bg-white shadow-sm dark:border-surface-800 dark:bg-surface-900"
    >
      <table class="w-full text-left text-sm">
        <thead
          class="border-b border-surface-200 bg-surface-50 text-xs uppercase text-surface-500 dark:border-surface-800 dark:bg-surface-800/50"
        >
          <tr>
            <th scope="col" class="px-4 py-3">
              {{ t('medications.colName', 'Fármaco propuesto') }}
            </th>
            <th scope="col" class="px-4 py-3">{{ t('medications.colNotes', 'Notas enviadas') }}</th>
            <th scope="col" class="px-4 py-3">{{ t('medications.colStatus', 'Estado') }}</th>
            <th scope="col" class="px-4 py-3">{{ t('medications.colFeedback', 'Respuesta') }}</th>
            <th scope="col" class="px-4 py-3 text-right">{{ t('common.date', 'Fecha') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-surface-100 dark:divide-surface-800">
          <tr v-if="loading">
            <td colspan="5" class="p-8 text-center text-surface-500">
              <span class="pi pi-spin pi-spinner mr-2" aria-hidden="true" />
              {{ t('common.loading', 'Cargando...') }}
            </td>
          </tr>
          <tr v-else-if="suggestions.length === 0">
            <td colspan="5" class="p-8 text-center text-surface-500">
              {{
                t('medications.noSuggestions', 'No hay sugerencias registradas por esta clínica.')
              }}
            </td>
          </tr>
          <tr
            v-for="sug in suggestions"
            :key="sug.id"
            class="hover:bg-surface-50/60 dark:hover:bg-surface-800/40"
            data-testid="org-suggestion-row"
          >
            <td class="px-4 py-3 font-medium text-surface-900 dark:text-surface-100">
              {{ sug.name }}
            </td>
            <td class="px-4 py-3 text-surface-600 dark:text-surface-300">
              {{ sug.notes || '—' }}
            </td>
            <td class="px-4 py-3">
              <span
                class="rounded px-2 py-0.5 text-xs font-semibold"
                :class="{
                  'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300':
                    sug.status === 'pending',
                  'bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-300':
                    sug.status === 'approved',
                  'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300':
                    sug.status === 'rejected',
                }"
              >
                {{ sug.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-xs text-surface-500">
              {{ sug.review_notes || '—' }}
            </td>
            <td class="px-4 py-3 text-right text-xs text-surface-500">
              {{ new Date(sug.created_at).toLocaleDateString() }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Modal Nueva Sugerencia -->
    <div
      v-if="showNewModal"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
    >
      <div
        class="card w-full max-w-md space-y-4 rounded-xl bg-white p-6 shadow-2xl dark:bg-surface-900"
      >
        <h3 class="text-lg font-bold text-surface-900 dark:text-surface-100">
          {{ t('medications.newSuggestionTitle', 'Proponer fármaco al catálogo') }}
        </h3>

        <div
          v-if="newError"
          class="rounded bg-red-50 p-2 text-sm text-red-800 dark:bg-red-950 dark:text-red-200"
        >
          {{ newError }}
        </div>

        <form class="space-y-3" @submit.prevent="handleCreate">
          <label class="block">
            <span class="label">{{ t('medications.name', 'Nombre del medicamento') }} *</span>
            <input v-model="newName" type="text" class="input w-full" required />
          </label>

          <label class="block">
            <span class="label">{{
              t('medications.notes', 'Notas (indicación, concentración, laboratorio)')
            }}</span>
            <textarea v-model="newNotes" class="input w-full" rows="3" />
          </label>

          <div class="flex justify-end gap-2 pt-2">
            <button
              type="button"
              class="btn-secondary"
              :disabled="newBusy"
              @click="showNewModal = false"
            >
              {{ t('common.cancel', 'Cancelar') }}
            </button>
            <button type="submit" class="btn-primary" :disabled="newBusy || !newName.trim()">
              <span v-if="newBusy" class="pi pi-spin pi-spinner mr-1" aria-hidden="true" />
              {{ t('common.send', 'Enviar sugerencia') }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </main>
</template>
