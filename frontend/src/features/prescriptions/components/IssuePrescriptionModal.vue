<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  api,
  ApiError,
  type PrescriptionItemDTO,
  type ProfessionalCredential,
} from '@/shared/api/client'

const props = defineProps<{
  orgId: string
  petId?: string
  encounterId?: string
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'issued', id: string): void
}>()

interface FormItem {
  name: string
  catalog_id?: string
  free_text?: string
  presentation: string
  dose_value: number
  dose_unit: string
  route: string
  frequency: string
  duration_days?: number
  quantity_value: number
  quantity_unit: string
  substitution_allowed: boolean
  controlled: boolean
  instructions: string
}

const loading = ref(false)
const submitting = ref(false)
const error = ref<string | null>(null)
const hasVerifiedCred = ref(true)
const signerName = ref('')
const previewMode = ref(false)

const form = reactive({
  petId: props.petId ?? '',
  country: 'CO',
  validUntil: '',
  refillsAllowed: 0,
  notes: '',
  items: [] as FormItem[],
})

function defaultItem(): FormItem {
  return {
    name: '',
    presentation: 'Comprimidos',
    dose_value: 1,
    dose_unit: 'mg',
    route: 'oral',
    frequency: 'cada 12 horas',
    duration_days: 7,
    quantity_value: 1,
    quantity_unit: 'pack',
    substitution_allowed: true,
    controlled: false,
    instructions: '',
  }
}

async function checkCredential() {
  try {
    const res = await api().GET('/me/credentials')
    if (res.data?.data) {
      const verified = (res.data.data as ProfessionalCredential[]).some(
        (c: ProfessionalCredential) =>
          c.status === 'verified' && c.country.toUpperCase() === form.country.toUpperCase(),
      )
      hasVerifiedCred.value = verified
    }
  } catch {
    hasVerifiedCred.value = false
  }
}

async function prefillFromEncounter() {
  if (!props.encounterId || !props.orgId) return
  loading.value = true
  try {
    const res = await api().GET('/encounters/{encounterId}', {
      params: { path: { encounterId: props.encounterId } },
    })
    if (res.data) {
      const enc = res.data
      if (enc.pet?.id) form.petId = enc.pet.id
      if (enc.plan?.medications) {
        form.items = enc.plan.medications.map((m) => {
          let freqStr = ''
          if (m.frequency?.once) freqStr = 'dosis única'
          else if (m.frequency?.every && m.frequency?.unit) {
            freqStr = `cada ${m.frequency.every} ${m.frequency.unit}`
          }
          return {
            name: m.medication_name,
            catalog_id: m.catalog_id ? m.catalog_id : undefined,
            free_text: m.free_text ? m.medication_name : undefined,
            presentation: m.presentation ?? '',
            dose_value: m.dose ?? 1,
            dose_unit: m.dose_unit ?? 'mg',
            route: m.route ?? 'oral',
            frequency: freqStr || 'cada 12 horas',
            duration_days: m.duration_days ?? 7,
            quantity_value: 1,
            quantity_unit: 'pack',
            substitution_allowed: true,
            controlled: false,
            instructions: m.instructions ?? '',
          }
        })
      }
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al precargar plan de la consulta'
  } finally {
    loading.value = false
  }
}

function addItem() {
  form.items.push(defaultItem())
}

function removeItem(index: number) {
  form.items.splice(index, 1)
}

const hasControlled = computed(() => form.items.some((it) => it.controlled))

watch(hasControlled, (controlled) => {
  if (controlled && form.refillsAllowed > 0) {
    form.refillsAllowed = 0
  }
})

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      error.value = null
      previewMode.value = false
      if (props.petId) form.petId = props.petId
      if (form.items.length === 0) {
        form.items = [defaultItem()]
      }
      // Por defecto 30 días de vigencia
      const d = new Date()
      d.setDate(d.getDate() + 30)
      form.validUntil = d.toISOString().split('T')[0]
      void checkCredential()
      if (props.encounterId) {
        void prefillFromEncounter()
      }
    }
  },
  { immediate: true },
)

function toPayloadItem(it: FormItem): PrescriptionItemDTO {
  return {
    name: it.name.trim(),
    catalog_id: it.catalog_id,
    free_text: it.catalog_id ? undefined : it.name.trim(),
    presentation: it.presentation,
    dose: { value: it.dose_value, unit: it.dose_unit },
    route: it.route,
    frequency: it.frequency,
    duration_days: it.duration_days,
    quantity: { value: it.quantity_value, unit: it.quantity_unit },
    substitution_allowed: it.substitution_allowed,
    controlled: it.controlled,
    instructions: it.instructions ? it.instructions.trim() : undefined,
  }
}

async function handleDraft() {
  if (!form.petId) {
    error.value = 'Debes indicar el ID de la mascota.'
    return
  }
  if (form.items.length === 0 || form.items.some((i) => !i.name.trim())) {
    error.value = 'Completa el nombre de todos los medicamentos.'
    return
  }
  submitting.value = true
  error.value = null
  try {
    const res = await api().POST('/organizations/{orgId}/prescriptions', {
      params: {
        path: { orgId: props.orgId },
        header: { 'Idempotency-Key': crypto.randomUUID() },
      },
      body: {
        pet_id: form.petId,
        country: form.country,
        encounter_id: props.encounterId ? props.encounterId : undefined,
        validity: {
          valid_until: form.validUntil ? form.validUntil : undefined,
          refills_allowed: form.refillsAllowed,
        },
        notes: form.notes ? form.notes.trim() : undefined,
        items: form.items.map(toPayloadItem),
      },
    })
    if (res.error) throw new ApiError(res.error, res.response.status)
    if (res.data?.id) {
      emit('issued', res.data.id)
      emit('close')
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al guardar borrador'
  } finally {
    submitting.value = false
  }
}

async function handleSignAndIssue() {
  if (!hasVerifiedCred.value) {
    error.value = 'Se requiere credencial profesional verificada para firmar y emitir.'
    return
  }
  if (!signerName.value.trim()) {
    error.value = 'Debes ingresar el nombre del profesional firmante.'
    return
  }
  submitting.value = true
  error.value = null
  try {
    // 1. Crear borrador
    const draftRes = await api().POST('/organizations/{orgId}/prescriptions', {
      params: {
        path: { orgId: props.orgId },
        header: { 'Idempotency-Key': crypto.randomUUID() },
      },
      body: {
        pet_id: form.petId,
        country: form.country,
        encounter_id: props.encounterId ? props.encounterId : undefined,
        validity: {
          valid_until: form.validUntil ? form.validUntil : undefined,
          refills_allowed: form.refillsAllowed,
        },
        notes: form.notes ? form.notes.trim() : undefined,
        items: form.items.map(toPayloadItem),
      },
    })
    if (draftRes.error) throw new ApiError(draftRes.error, draftRes.response.status)
    const rxId = draftRes.data?.id
    if (!rxId) throw new Error('No se recibió el ID de la receta')

    // 2. Firmar y emitir
    const issueRes = await api().POST('/prescriptions/{prescriptionId}/issue', {
      params: {
        path: { prescriptionId: rxId },
        header: { 'If-Match': draftRes.data?.version ?? 1 },
      },
      body: { signer_name: signerName.value.trim() },
    })
    if (issueRes.error) throw new ApiError(issueRes.error, issueRes.response.status)

    emit('issued', rxId)
    emit('close')
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al emitir la receta'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-sm"
  >
    <div class="flex max-h-[90vh] w-full max-w-3xl flex-col rounded-2xl bg-white shadow-2xl">
      <!-- Modal Header -->
      <div class="flex items-center justify-between border-b border-slate-200 px-6 py-4">
        <div>
          <h2 class="text-xl font-bold text-slate-900">
            {{ previewMode ? 'Vista Previa y Firma de Receta' : 'Emitir Receta Médica' }}
          </h2>
          <p class="text-xs text-slate-500">
            Prescripción electrónica firmada e inmutable con QR de verificación pública.
          </p>
        </div>
        <button type="button" class="text-slate-400 hover:text-slate-600" @click="emit('close')">
          ✕
        </button>
      </div>

      <!-- Credential warning banner -->
      <div
        v-if="!hasVerifiedCred"
        class="border-b border-amber-200 bg-amber-50 px-6 py-3 text-xs text-amber-800 flex items-center gap-2"
      >
        <span>⚠️</span>
        <div>
          <strong>Atención:</strong> No tienes una credencial profesional verificada para
          {{ form.country }}. Podrás guardar el borrador, pero la firma formal requiere verificación
          en
          <a href="/app/profile/credentials" class="underline font-semibold">tus credenciales</a>.
        </div>
      </div>

      <div v-if="error" class="border-b border-rose-200 bg-rose-50 px-6 py-3 text-xs text-rose-800">
        {{ error }}
      </div>

      <!-- Modal Body -->
      <div class="flex-1 overflow-y-auto px-6 py-4">
        <!-- EDIT MODE -->
        <div v-if="!previewMode" class="space-y-6">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div>
              <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
                >ID Mascota</label
              >
              <input
                v-model="form.petId"
                type="text"
                placeholder="UUID de la mascota"
                required
                class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
              />
            </div>
            <div>
              <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
                >Vigente hasta</label
              >
              <input
                v-model="form.validUntil"
                type="date"
                required
                class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
              />
            </div>
            <div>
              <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600">
                Refills permitidos (0-5)
              </label>
              <input
                v-model.number="form.refillsAllowed"
                type="number"
                min="0"
                max="5"
                :disabled="hasControlled"
                class="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm disabled:bg-slate-100 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
              />
              <span v-if="hasControlled" class="text-[10px] text-amber-600">
                Controlados no admiten refills (R4).
              </span>
            </div>
          </div>

          <!-- Items list -->
          <div>
            <div class="mb-2 flex items-center justify-between">
              <h3 class="text-sm font-bold uppercase tracking-wider text-slate-800">
                Medicamentos e Indicaciones
              </h3>
              <button
                type="button"
                class="text-xs font-semibold text-emerald-600 hover:text-emerald-700"
                @click="addItem"
              >
                + Añadir Medicamento
              </button>
            </div>

            <div class="space-y-4">
              <div
                v-for="(it, idx) in form.items"
                :key="idx"
                class="relative rounded-xl border border-slate-200 bg-slate-50 p-4"
              >
                <button
                  v-if="form.items.length > 1"
                  type="button"
                  class="absolute right-3 top-3 text-slate-400 hover:text-rose-600 text-xs"
                  @click="removeItem(idx)"
                >
                  Eliminar
                </button>

                <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <div class="sm:col-span-2">
                    <label class="block text-xs font-medium text-slate-600"
                      >Nombre del Medicamento / Principio</label
                    >
                    <input
                      v-model="it.name"
                      type="text"
                      placeholder="Ej. Amoxicilina + Ácido Clavulánico"
                      required
                      class="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm focus:border-emerald-500 focus:outline-none"
                    />
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600">Presentación</label>
                    <input
                      v-model="it.presentation"
                      type="text"
                      placeholder="Ej. Tabletas 500mg"
                      class="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm focus:border-emerald-500 focus:outline-none"
                    />
                  </div>

                  <div>
                    <label class="block text-xs font-medium text-slate-600">Dosis</label>
                    <div class="mt-1 flex gap-1">
                      <input
                        v-model.number="it.dose_value"
                        type="number"
                        step="any"
                        min="0"
                        class="w-2/3 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
                      />
                      <input
                        v-model="it.dose_unit"
                        type="text"
                        placeholder="mg/ml"
                        class="w-1/3 rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-sm"
                      />
                    </div>
                  </div>

                  <div>
                    <label class="block text-xs font-medium text-slate-600">Frecuencia</label>
                    <input
                      v-model="it.frequency"
                      type="text"
                      placeholder="Ej. cada 12 horas"
                      required
                      class="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
                    />
                  </div>

                  <div>
                    <label class="block text-xs font-medium text-slate-600">Duración (Días)</label>
                    <input
                      v-model.number="it.duration_days"
                      type="number"
                      min="1"
                      class="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
                    />
                  </div>

                  <div>
                    <label class="block text-xs font-medium text-slate-600"
                      >Cantidad a Dispensar</label
                    >
                    <div class="mt-1 flex gap-1">
                      <input
                        v-model.number="it.quantity_value"
                        type="number"
                        step="any"
                        min="1"
                        class="w-1/2 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
                      />
                      <select
                        v-model="it.quantity_unit"
                        class="w-1/2 rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-sm"
                      >
                        <option value="pack">Pack / Caja</option>
                        <option value="unit">Unidad</option>
                        <option value="ml">ml</option>
                        <option value="g">g</option>
                      </select>
                    </div>
                  </div>

                  <div class="flex items-center gap-4 pt-4 sm:col-span-2">
                    <label class="flex items-center gap-2 text-xs font-medium text-slate-700">
                      <input
                        v-model="it.substitution_allowed"
                        type="checkbox"
                        class="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                      />
                      Permite sustitución por genérico
                    </label>

                    <label class="flex items-center gap-2 text-xs font-medium text-rose-700">
                      <input
                        v-model="it.controlled"
                        type="checkbox"
                        class="rounded border-slate-300 text-rose-600 focus:ring-rose-500"
                      />
                      Medicamento de control especial
                    </label>
                  </div>

                  <div class="sm:col-span-3">
                    <label class="block text-xs font-medium text-slate-600"
                      >Instrucciones / Posología</label
                    >
                    <textarea
                      v-model="it.instructions"
                      rows="2"
                      placeholder="Indicaciones para el tutor (ej. administrar con alimento)"
                      class="mt-1 block w-full rounded-lg border border-slate-300 bg-white p-2 text-sm focus:border-emerald-500 focus:outline-none"
                    ></textarea>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div>
            <label class="block text-xs font-semibold uppercase tracking-wider text-slate-600"
              >Notas Adicionales</label
            >
            <textarea
              v-model="form.notes"
              rows="2"
              placeholder="Observaciones clínicas generales para la receta..."
              class="mt-1 block w-full rounded-lg border border-slate-300 p-2 text-sm focus:border-emerald-500 focus:outline-none"
            ></textarea>
          </div>
        </div>

        <!-- PREVIEW MODE -->
        <div v-else class="space-y-6">
          <div class="rounded-xl border border-slate-200 bg-slate-50 p-6 text-sm">
            <div class="mb-4 flex items-center justify-between border-b border-slate-200 pb-3">
              <div>
                <h4 class="font-bold text-slate-900">Resumen de la Receta Médica</h4>
                <p class="text-xs text-slate-500">
                  Mascota: {{ form.petId }} · Vence: {{ form.validUntil }}
                </p>
              </div>
              <span class="rounded bg-emerald-100 px-2.5 py-1 text-xs font-bold text-emerald-800">
                Refills: {{ form.refillsAllowed }}
              </span>
            </div>

            <div class="space-y-3">
              <div
                v-for="(it, i) in form.items"
                :key="i"
                class="rounded-lg bg-white p-3 border border-slate-200"
              >
                <div class="flex items-center justify-between font-bold text-slate-800">
                  <span>{{ it.name }} {{ it.presentation ? `(${it.presentation})` : '' }}</span>
                  <span
                    v-if="it.controlled"
                    class="rounded bg-rose-100 px-2 py-0.5 text-xs text-rose-800"
                  >
                    CONTROLADO
                  </span>
                </div>
                <div class="mt-1 text-xs text-slate-600">
                  Dosis: {{ it.dose_value }} {{ it.dose_unit }} · {{ it.frequency }} por
                  {{ it.duration_days }} días · Dispensar: {{ it.quantity_value }}
                  {{ it.quantity_unit }}
                </div>
                <div v-if="it.instructions" class="mt-1 text-xs italic text-slate-500">
                  "{{ it.instructions }}"
                </div>
              </div>
            </div>

            <div class="mt-6 border-t border-slate-200 pt-4">
              <label class="block text-xs font-bold uppercase tracking-wider text-slate-700">
                Nombre del Profesional Firmante (requerido para firma electrónica)
              </label>
              <input
                v-model="signerName"
                type="text"
                placeholder="Ej. Dr. Carlos Méndez - MVZ"
                required
                class="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm focus:border-emerald-500 focus:outline-none"
              />
              <p class="mt-1 text-[11px] text-slate-400">
                Al firmar, se generará el documento inmutable con ContentHash SHA-256 según RFC 8785
                y código QR único.
              </p>
            </div>
          </div>
        </div>
      </div>

      <!-- Modal Footer -->
      <div
        class="flex items-center justify-between border-t border-slate-200 bg-slate-50 px-6 py-4"
      >
        <div>
          <button
            v-if="!previewMode"
            type="button"
            :disabled="submitting"
            class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
            @click="handleDraft"
          >
            Guardar Borrador
          </button>
          <button
            v-else
            type="button"
            class="rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            @click="previewMode = false"
          >
            ← Volver a Editar
          </button>
        </div>

        <div class="flex gap-3">
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100"
            @click="emit('close')"
          >
            Cancelar
          </button>
          <button
            v-if="!previewMode"
            type="button"
            class="rounded-lg bg-emerald-600 px-5 py-2 text-sm font-semibold text-white shadow hover:bg-emerald-700"
            @click="previewMode = true"
          >
            Vista Previa y Emitir →
          </button>
          <button
            v-else
            type="button"
            :disabled="submitting || !hasVerifiedCred || !signerName.trim()"
            class="rounded-lg bg-emerald-700 px-6 py-2 text-sm font-bold text-white shadow hover:bg-emerald-800 disabled:opacity-50"
            @click="handleSignAndIssue"
          >
            {{ submitting ? 'Firmando...' : 'Firmar y Emitir' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
