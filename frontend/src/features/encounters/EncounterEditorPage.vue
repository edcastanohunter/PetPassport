<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  isApiError,
  type ApiError,
  type Encounter,
  type EncounterType,
  type Member,
  type PetClinicalSummary,
  type VitalWarning,
} from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { useOrgStore } from '@/features/orgs/orgStore'
import PatientSummaryPanel from '@/features/records/PatientSummaryPanel.vue'
import AttachmentsPanel from './AttachmentsPanel.vue'
import AddendumForm from './AddendumForm.vue'
import EncounterDetail from './EncounterDetail.vue'
import { discardEncounter, getEncounter, signEncounter } from './encounterApi'
import {
  BODY_SYSTEMS,
  CLINICAL_SECTIONS,
  changedSections,
  draftFrom,
  emptyMedication,
  num,
  saveSections,
  signChecklist,
  snapshot,
  totalDose,
  dosePerAdministration,
  VITAL_FIELDS,
  type Draft,
  type SectionKey,
  type SignRequirement,
} from './encounterDraft'
import { useAutosave } from './useAutosave'

// Editor de consulta (PP-F2-FE-003/FE-004, PRD-001..005): navegación SOAP con
// completitud, autoguardado cada 5 s con If-Match y aviso aria-live, conflicto
// 409 con diferencias y recarga, constantes con avisos por especie, plan
// estructurado con cálculo de dosis, adjuntos, firma con checklist y, una vez
// firmada, vista de solo lectura con sello, adendas y PDF. El servidor es la
// autoridad: recepción edita S/O/constantes y ve A/P en solo lectura (E4).
const { t, te, d } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const orgs = useOrgStore()

const orgId = computed(() => String(route.params.orgId))
const encounterId = computed(() => String(route.params.encounterId))
const membership = computed(() => session.membershipFor(orgId.value))
const userId = computed(() => session.me?.id)
const isVet = computed(() => membership.value?.role === 'veterinarian')

const encounter = shallowRef<Encounter | null>(null)
const draft = ref<Draft | null>(null)
const state = ref<'loading' | 'ready' | 'notFound' | 'error'>('loading')
const warnings = ref<VitalWarning[]>([])
const vets = ref<Member[]>([])
const summary = shallowRef<PetClinicalSummary | null>(null)
const summaryPanel = ref<InstanceType<typeof PatientSummaryPanel> | null>(null)
const actionError = ref<string | null>(null)

const isDraft = computed(() => encounter.value?.status === 'draft')
const isAttending = computed(
  () => !!encounter.value?.attending_vet && encounter.value.attending_vet.id === userId.value,
)
const canClinical = computed(() => isDraft.value && isVet.value)
const canDiscard = computed(
  () =>
    isDraft.value &&
    (membership.value?.role === 'clinic_admin' || (isVet.value && isAttending.value)),
)

/** Secciones que este actor puede guardar (las demás nunca se envían). */
function editableSections(): SectionKey[] {
  const all: SectionKey[] = ['header', 'vitals', 'diagnoses', 'plan', 'followUp']
  return canClinical.value ? all : all.filter((s) => !CLINICAL_SECTIONS.includes(s))
}

let sent: Record<SectionKey, string> | null = null

function signature() {
  if (!draft.value) return ''
  const snap = snapshot(draft.value, sent ?? undefined)
  return JSON.stringify(editableSections().map((s) => snap[s]))
}

const autosave = useAutosave({
  signature,
  enabled: () => isDraft.value && !!draft.value,
  save: async () => {
    if (!encounter.value || !draft.value) return
    const snap = snapshot(draft.value, sent ?? undefined)
    const sections = sent
      ? changedSections(snap, sent).filter((s) => editableSections().includes(s))
      : []
    if (sections.length === 0) return
    const updated = await saveSections(encounter.value, draft.value, sections)
    sent = snap
    encounter.value = updated
    if (sections.includes('vitals')) warnings.value = updated.warnings ?? []
  },
})

function adopt(e: Encounter) {
  encounter.value = e
  draft.value = draftFrom(e)
  warnings.value = e.warnings ?? []
  sent = snapshot(draft.value)
  autosave.markClean()
  conflict.value = null
}

async function load() {
  state.value = 'loading'
  try {
    adopt(await getEncounter(encounterId.value))
    state.value = 'ready'
  } catch (e) {
    state.value = isApiError(e) && e.status === 404 ? 'notFound' : 'error'
  }
}

onMounted(async () => {
  await load()
  try {
    vets.value = (await orgs.loadMembers(orgId.value)).filter(
      (m) => m.status === 'active' && m.role === 'veterinarian',
    )
  } catch {
    vets.value = []
  }
  window.addEventListener('keydown', onShortcut)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onShortcut))

// Peso prellenado con el último registrado (FE-003) en borradores sin peso.
const weightPrefilled = ref(false)
function onSummary(s: PetClinicalSummary) {
  summary.value = s
  const dr = draft.value
  if (!dr || !isDraft.value || !s.current_weight) return
  if (num(dr.vitals.weight_kg) === undefined) {
    dr.vitals.weight_kg = s.current_weight.weight_kg
    weightPrefilled.value = true
  }
}

// ------------------------------------------------------------ conflicto 409
const conflict = shallowRef<{ server: Encounter; sections: SectionKey[] } | null>(null)
async function showConflict() {
  if (!draft.value) return
  try {
    const server = await getEncounter(encounterId.value)
    const sections = changedSections(snapshot(draftFrom(server)), snapshot(draft.value))
    conflict.value = { server, sections }
  } catch {
    conflict.value = null
  }
}
function reloadFromServer() {
  if (conflict.value) adopt(conflict.value.server)
  else void load()
}

// ------------------------------------------------------------ navegación SOAP
const SECTIONS = [
  { id: 'reason', key: 'reason' },
  { id: 'subjective', key: 'subjective' },
  { id: 'objective', key: 'objective' },
  { id: 'assessment', key: 'assessment' },
  { id: 'plan', key: 'plan' },
  { id: 'attachments', key: 'attachments' },
  { id: 'followUp', key: 'followUp' },
] as const

const completeness = computed<Record<string, boolean>>(() => {
  const dr = draft.value
  if (!dr) return {} as Record<string, boolean>
  const snap = snapshot(dr)
  return {
    reason: dr.reason.trim() !== '' && !!dr.started_at,
    subjective: dr.subjective.trim() !== '',
    objective: snap.header.includes('"objective":{"'),
    assessment: snap.diagnoses !== '[]',
    plan: snap.plan !== '{"medications":[],"procedures":[]}',
    attachments: (encounter.value?.attachments.length ?? 0) > 0,
    followUp: snap.followUp !== 'null' && snap.followUp !== '"incomplete"',
  }
})

function go(id: string) {
  const el = document.getElementById(`sec-${id}`)
  el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  el?.focus({ preventScroll: true })
}
// Atajos: Alt+1…7 saltan a cada sección; Alt+S guarda ya.
function onShortcut(ev: KeyboardEvent) {
  if (!ev.altKey || ev.ctrlKey || ev.metaKey) return
  const idx = Number(ev.key) - 1
  if (idx >= 0 && idx < SECTIONS.length) {
    ev.preventDefault()
    go(SECTIONS[idx]!.id)
  } else if (ev.key.toLowerCase() === 's') {
    ev.preventDefault()
    void autosave.flush()
  }
}

// ------------------------------------------------------------ campos
const TYPES: EncounterType[] = [
  'consultation',
  'follow_up',
  'emergency',
  'vaccination',
  'surgery',
  'procedure',
  'other',
]
const CERTAINTIES = ['presumptive', 'confirmed', 'differential', 'ruled_out'] as const
const DOSE_UNITS = [
  'mg',
  'ml',
  'ui',
  'tablet',
  'capsule',
  'drop',
  'mg_per_kg',
  'ml_per_kg',
] as const
const ROUTES = [
  'oral',
  'sc',
  'im',
  'iv',
  'topical',
  'ophthalmic',
  'otic',
  'inhaled',
  'rectal',
] as const
const MUCOSA = ['pink', 'pale', 'white', 'cyanotic', 'icteric', 'hyperemic'] as const

const startedLocal = computed({
  get() {
    const v = draft.value?.started_at
    if (!v) return ''
    const dt = new Date(v)
    const pad = (x: number) => String(x).padStart(2, '0')
    return `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`
  },
  set(v: string) {
    if (draft.value && v) draft.value.started_at = new Date(v).toISOString()
  },
})

function warningFor(field: string) {
  return warnings.value.find((w) => w.field === field)
}
function system(s: string) {
  return draft.value!.objective.systems[s]!
}
function label(prefix: string, key: string) {
  const k = `${prefix}.${key}`
  return te(k) ? t(k) : key
}
const weightKg = computed(() => num(draft.value?.vitals.weight_kg))

// ------------------------------------------------------------ firma (FE-004)
const checklist = computed(() => (draft.value ? signChecklist(draft.value, userId.value) : []))
const serverMissing = ref<SignRequirement[]>([])
const confirmSign = ref(false)
const signing = ref(false)

function errorText(e: unknown) {
  return isApiError(e) ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

async function sign() {
  if (!encounter.value) return
  signing.value = true
  actionError.value = null
  serverMissing.value = []
  try {
    await autosave.flush()
    if (autosave.status.value === 'conflict' || autosave.status.value === 'error') {
      confirmSign.value = false
      return
    }
    const signed = await signEncounter(encounter.value.id, encounter.value.version)
    adopt(signed)
    confirmSign.value = false
    void summaryPanel.value?.afterCommand()
  } catch (e) {
    confirmSign.value = false
    if (isApiError(e) && e.code === 'ENCOUNTER_INCOMPLETE') {
      serverMissing.value = e.errors.map((x) => x.field as SignRequirement)
    } else if (isApiError(e) && e.code === 'CONFLICT_VERSION') {
      await showConflict()
    }
    actionError.value = errorText(e)
  } finally {
    signing.value = false
  }
}

// ------------------------------------------------------------ descartar
const discarding = ref(false)
const discardReason = ref('')
async function discard() {
  if (!encounter.value || !discardReason.value.trim()) return
  actionError.value = null
  try {
    await discardEncounter(encounter.value.id, discardReason.value.trim())
    await router.push({ name: 'org-encounters', params: { orgId: orgId.value } })
  } catch (e) {
    actionError.value = errorText(e)
  }
}

async function refresh() {
  try {
    const e = await getEncounter(encounterId.value)
    // Solo cambian adjuntos/versión: se conserva el borrador local.
    encounter.value = e
  } catch (err) {
    actionError.value = errorText(err)
  }
}

function onAddendum(e: Encounter) {
  adopt(e)
  void summaryPanel.value?.afterCommand()
}

const saveLabel = computed(() => {
  switch (autosave.status.value) {
    case 'saving':
      return t('encounters.autosave.saving')
    case 'saved':
      return t('encounters.autosave.saved', {
        time: d(autosave.savedAt.value ?? new Date(), 'time'),
      })
    case 'error':
      return t('encounters.autosave.error', { reason: errorMessage(autosave.error.value) })
    case 'conflict':
      return t('encounters.autosave.conflict')
    default:
      return ''
  }
})
function errorMessage(e: ApiError | null) {
  return e ? t(`errors.${e.code}`, t('errors.GENERIC')) : t('errors.GENERIC')
}

const visibleMeds = computed(() => draft.value?.plan.medications ?? [])
</script>

<template>
  <section class="space-y-4">
    <RouterLink :to="{ name: 'org-encounters', params: { orgId } }" class="text-sm underline">
      ← {{ t('encounters.list.title') }}
    </RouterLink>

    <p v-if="state === 'loading'" class="text-sm text-surface-500">{{ t('common.loading') }}</p>
    <p v-else-if="state === 'notFound'" class="rounded-lg bg-surface-200/40 p-6 text-center">
      {{ t('encounters.notFound') }}
    </p>
    <p v-else-if="state === 'error'" class="rounded-lg bg-red-50 p-4 text-red-700" role="alert">
      {{ t('errors.GENERIC') }}
    </p>

    <template v-if="encounter && draft">
      <header class="flex flex-wrap items-center gap-3">
        <div class="flex-1">
          <p class="text-sm text-surface-500">
            <RouterLink
              :to="{ name: 'org-patient', params: { orgId, petId: encounter.pet.id } }"
              class="underline"
              >{{ encounter.pet.name }}</RouterLink
            >
            · {{ label('pets.species', encounter.pet.species) }} ·
            {{ t(`encounters.status.${encounter.status}`) }}
            <template v-if="encounter.abandoned"> · {{ t('encounters.abandoned') }}</template>
          </p>
          <h1 class="text-2xl font-bold">
            {{ isDraft ? t('encounters.editor.title') : t('encounters.detail.title') }}
          </h1>
        </div>
        <p
          v-if="isDraft"
          class="text-sm"
          :class="
            autosave.status.value === 'error' || autosave.status.value === 'conflict'
              ? 'text-red-700 dark:text-red-300'
              : 'text-surface-500'
          "
          role="status"
          aria-live="polite"
          data-testid="autosave-status"
        >
          {{ saveLabel }}
        </p>
      </header>

      <p
        v-if="actionError"
        class="rounded bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-200"
        role="alert"
      >
        {{ actionError }}
      </p>

      <div
        v-if="autosave.status.value === 'conflict'"
        class="space-y-2 rounded-xl border border-red-300 bg-red-50 p-4 text-sm text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-100"
        role="alert"
        data-testid="conflict"
      >
        <p class="font-semibold">{{ t('encounters.conflict.title') }}</p>
        <p>{{ t('encounters.conflict.body') }}</p>
        <div v-if="conflict" class="space-y-1">
          <p v-if="conflict.sections.length === 0">{{ t('encounters.conflict.noDiff') }}</p>
          <ul v-else class="list-disc pl-5" data-testid="conflict-sections">
            <li v-for="s in conflict.sections" :key="s">
              {{ t(`encounters.conflict.sections.${s}`) }}
            </li>
          </ul>
        </div>
        <div class="flex gap-2">
          <button v-if="!conflict" type="button" class="btn-secondary" @click="showConflict">
            {{ t('encounters.conflict.diff') }}
          </button>
          <button
            type="button"
            class="btn-primary"
            data-testid="conflict-reload"
            @click="reloadFromServer"
          >
            {{ t('encounters.conflict.reload') }}
          </button>
        </div>
      </div>

      <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <!-- Consulta firmada: solo lectura, sello, adendas y PDF (FE-004) -->
        <div v-if="!isDraft">
          <EncounterDetail :encounter="encounter">
            <template #after>
              <AddendumForm
                v-if="encounter.status === 'signed' && isVet && isAttending"
                :encounter="encounter"
                @added="onAddendum"
                @changed="refresh"
              />
            </template>
          </EncounterDetail>
        </div>

        <!-- Borrador: editor SOAP -->
        <div v-else class="space-y-6">
          <nav
            class="sticky top-0 z-10 -mx-1 overflow-x-auto bg-surface-0 px-1 py-2 dark:bg-surface-950"
            :aria-label="t('encounters.editor.nav')"
          >
            <ol class="flex gap-2 text-sm" data-testid="soap-nav">
              <li v-for="(s, i) in SECTIONS" :key="s.id">
                <button
                  type="button"
                  class="whitespace-nowrap rounded border border-surface-200 px-2 py-1 dark:border-surface-800"
                  :aria-keyshortcuts="`Alt+${i + 1}`"
                  @click="go(s.id)"
                >
                  <span
                    :class="
                      completeness[s.id]
                        ? 'pi pi-check-circle text-emerald-700 dark:text-emerald-400'
                        : 'pi pi-circle text-surface-500'
                    "
                    aria-hidden="true"
                  />
                  {{ t(`encounters.nav.${s.key}`) }}
                  <span class="sr-only">{{
                    completeness[s.id]
                      ? t('encounters.nav.complete')
                      : t('encounters.nav.incomplete')
                  }}</span>
                </button>
              </li>
            </ol>
          </nav>

          <p
            v-if="!canClinical"
            class="rounded bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-100"
            role="note"
          >
            {{ t('encounters.editor.readOnlyClinical') }}
          </p>

          <!-- Motivo y constantes -->
          <section
            id="sec-reason"
            tabindex="-1"
            class="space-y-3 outline-none"
            aria-labelledby="h-reason"
          >
            <h2 id="h-reason" class="text-lg font-semibold">{{ t('encounters.nav.reason') }}</h2>
            <div class="grid gap-3 sm:grid-cols-3">
              <label>
                <span class="label">{{ t('encounters.fields.type') }}</span>
                <select v-model="draft.type" class="input">
                  <option v-for="ty in TYPES" :key="ty" :value="ty">
                    {{ t(`encounters.type.${ty}`) }}
                  </option>
                </select>
              </label>
              <label>
                <span class="label">{{ t('encounters.fields.startedAt') }}</span>
                <input v-model="startedLocal" type="datetime-local" class="input" />
              </label>
              <label>
                <span class="label">{{ t('encounters.attendingVet') }}</span>
                <select v-model="draft.attending_vet_id" class="input" data-testid="attending-vet">
                  <option :value="null">{{ t('encounters.fields.noVet') }}</option>
                  <option v-for="v in vets" :key="v.user_id" :value="v.user_id">
                    {{ v.display_name }}
                  </option>
                  <option
                    v-if="
                      draft.attending_vet_id &&
                      !vets.some((v) => v.user_id === draft!.attending_vet_id)
                    "
                    :value="draft.attending_vet_id"
                  >
                    {{ encounter.attending_vet?.display_name ?? draft.attending_vet_id }}
                  </option>
                </select>
              </label>
            </div>
            <label class="block">
              <span class="label">{{ t('encounters.fields.reason') }}</span>
              <input
                v-model="draft.reason"
                class="input"
                maxlength="200"
                required
                data-testid="reason"
              />
            </label>

            <h3 class="font-semibold">{{ t('encounters.sections.vitals') }}</h3>
            <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <label v-for="f in VITAL_FIELDS" :key="f">
                <span class="label">{{ t(`encounters.vitals.${f}`) }}</span>
                <input
                  v-model.number="draft.vitals[f]"
                  type="number"
                  step="any"
                  min="0"
                  class="input"
                  :aria-describedby="warningFor(f) ? `warn-${f}` : undefined"
                  :data-testid="`vital-${f}`"
                  @input="f === 'weight_kg' && (weightPrefilled = false)"
                />
                <span
                  v-if="warningFor(f)"
                  :id="`warn-${f}`"
                  class="mt-1 block text-xs text-amber-800 dark:text-amber-200"
                  :data-testid="`warning-${f}`"
                >
                  <span class="pi pi-exclamation-triangle mr-1" aria-hidden="true" />{{
                    warningFor(f)!.message
                  }}
                </span>
                <span
                  v-if="f === 'weight_kg' && weightPrefilled && summary?.current_weight"
                  class="mt-1 block text-xs text-surface-500"
                >
                  {{
                    t('encounters.vitals.prefilled', {
                      date: d(summary.current_weight.measured_at, 'short'),
                    })
                  }}
                </span>
              </label>
              <label>
                <span class="label">{{ t('encounters.vitals.mucosa') }}</span>
                <select v-model="draft.vitals.mucosa" class="input">
                  <option :value="undefined">—</option>
                  <option v-for="m in MUCOSA" :key="m" :value="m">
                    {{ t(`encounters.mucosa.${m}`) }}
                  </option>
                </select>
              </label>
            </div>
          </section>

          <!-- S -->
          <section
            id="sec-subjective"
            tabindex="-1"
            class="space-y-2 outline-none"
            aria-labelledby="h-s"
          >
            <h2 id="h-s" class="text-lg font-semibold">
              {{ t('encounters.sections.subjective') }}
            </h2>
            <textarea
              v-model="draft.subjective"
              class="input min-h-32"
              maxlength="5000"
              :aria-label="t('encounters.sections.subjective')"
              data-testid="subjective"
            />
          </section>

          <!-- O -->
          <section
            id="sec-objective"
            tabindex="-1"
            class="space-y-3 outline-none"
            aria-labelledby="h-o"
          >
            <h2 id="h-o" class="text-lg font-semibold">{{ t('encounters.sections.objective') }}</h2>
            <textarea
              v-model="draft.objective.narrative"
              class="input min-h-24"
              maxlength="5000"
              :aria-label="t('encounters.sections.objective')"
            />
            <table class="w-full text-left text-sm">
              <thead>
                <tr class="text-surface-500">
                  <th scope="col">{{ t('encounters.fields.system') }}</th>
                  <th scope="col">{{ t('encounters.fields.finding') }}</th>
                  <th scope="col">{{ t('encounters.fields.note') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="s in BODY_SYSTEMS" :key="s">
                  <th scope="row" class="font-normal">{{ t(`encounters.systems.${s}`) }}</th>
                  <td :data-label="t('encounters.fields.finding')">
                    <select
                      v-model="system(s).status"
                      class="input"
                      :aria-label="t(`encounters.systems.${s}`)"
                    >
                      <option value="">{{ t('encounters.finding.not_examined') }}</option>
                      <option value="normal">{{ t('encounters.finding.normal') }}</option>
                      <option value="abnormal">{{ t('encounters.finding.abnormal') }}</option>
                    </select>
                  </td>
                  <td :data-label="t('encounters.fields.note')">
                    <input
                      v-model="system(s).note"
                      class="input"
                      maxlength="500"
                      :disabled="!system(s).status"
                      :aria-label="`${t(`encounters.systems.${s}`)} — ${t('encounters.fields.note')}`"
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </section>

          <!-- A -->
          <section
            id="sec-assessment"
            tabindex="-1"
            class="space-y-2 outline-none"
            aria-labelledby="h-a"
          >
            <h2 id="h-a" class="text-lg font-semibold">
              {{ t('encounters.sections.assessment') }}
            </h2>
            <fieldset :disabled="!canClinical" class="space-y-2">
              <div
                v-for="(dx, i) in draft.diagnoses"
                :key="i"
                class="flex flex-wrap items-end gap-2"
                data-testid="diagnosis-row"
              >
                <label class="min-w-48 flex-1">
                  <span class="label">{{ t('encounters.fields.diagnosis', { n: i + 1 }) }}</span>
                  <input v-model="dx.text" class="input" maxlength="200" />
                </label>
                <label>
                  <span class="label">{{ t('encounters.fields.certainty') }}</span>
                  <select v-model="dx.certainty" class="input">
                    <option v-for="c in CERTAINTIES" :key="c" :value="c">
                      {{ t(`encounters.certainty.${c}`) }}
                    </option>
                  </select>
                </label>
                <button type="button" class="btn-secondary" @click="draft.diagnoses.splice(i, 1)">
                  {{ t('common.delete') }}
                </button>
              </div>
              <button
                type="button"
                class="btn-secondary"
                data-testid="add-diagnosis"
                @click="draft.diagnoses.push({ text: '', certainty: 'presumptive' })"
              >
                <span class="pi pi-plus mr-2" aria-hidden="true" />{{
                  t('encounters.fields.addDiagnosis')
                }}
              </button>
            </fieldset>
          </section>

          <!-- P -->
          <section id="sec-plan" tabindex="-1" class="space-y-3 outline-none" aria-labelledby="h-p">
            <h2 id="h-p" class="text-lg font-semibold">{{ t('encounters.sections.plan') }}</h2>
            <fieldset :disabled="!canClinical" class="space-y-3">
              <legend class="font-semibold">{{ t('encounters.plan.medications') }}</legend>
              <div
                v-for="(m, i) in visibleMeds"
                :key="i"
                class="space-y-2 rounded-lg border border-surface-200 p-3 dark:border-surface-800"
                data-testid="medication-row"
              >
                <div class="grid gap-2 sm:grid-cols-4">
                  <label class="sm:col-span-2">
                    <span class="label">{{ t('encounters.plan.medication') }}</span>
                    <input v-model="m.medication_name" class="input" maxlength="200" />
                  </label>
                  <label class="sm:col-span-2">
                    <span class="label">{{ t('encounters.plan.presentation') }}</span>
                    <input v-model="m.presentation" class="input" maxlength="200" />
                  </label>
                  <label>
                    <span class="label">{{ t('encounters.plan.dose') }}</span>
                    <input v-model.number="m.dose" type="number" step="any" min="0" class="input" />
                  </label>
                  <label>
                    <span class="label">{{ t('encounters.plan.unit') }}</span>
                    <select v-model="m.dose_unit" class="input">
                      <option v-for="u in DOSE_UNITS" :key="u" :value="u">
                        {{ t(`encounters.doseUnit.${u}`) }}
                      </option>
                    </select>
                  </label>
                  <label>
                    <span class="label">{{ t('encounters.plan.route') }}</span>
                    <select v-model="m.route" class="input">
                      <option v-for="r in ROUTES" :key="r" :value="r">
                        {{ t(`encounters.route.${r}`) }}
                      </option>
                    </select>
                  </label>
                  <label>
                    <span class="label">{{ t('encounters.plan.durationDays') }}</span>
                    <input
                      v-model.number="m.duration_days"
                      type="number"
                      min="1"
                      class="input"
                      :disabled="m.once"
                    />
                  </label>
                  <label class="flex items-center gap-2 text-sm">
                    <input v-model="m.once" type="checkbox" />{{ t('encounters.plan.once') }}
                  </label>
                  <label v-if="!m.once">
                    <span class="label">{{ t('encounters.plan.every') }}</span>
                    <input v-model.number="m.every" type="number" min="1" class="input" />
                  </label>
                  <label v-if="!m.once">
                    <span class="label">{{ t('encounters.plan.unit') }}</span>
                    <select v-model="m.unit" class="input">
                      <option value="hours">{{ t('encounters.plan.hours') }}</option>
                      <option value="days">{{ t('encounters.plan.daysUnit') }}</option>
                    </select>
                  </label>
                </div>
                <label class="block">
                  <span class="label">{{ t('encounters.plan.instructions') }}</span>
                  <input v-model="m.instructions" class="input" maxlength="500" />
                </label>
                <p
                  v-if="dosePerAdministration(m, weightKg)"
                  class="text-sm text-surface-500"
                  data-testid="dose-calc"
                >
                  {{
                    t('encounters.plan.perDose', {
                      amount: dosePerAdministration(m, weightKg)!.amount,
                      unit: dosePerAdministration(m, weightKg)!.unit,
                      weight: weightKg,
                    })
                  }}
                  <template v-if="totalDose(m, weightKg)">
                    ·
                    {{
                      t('encounters.plan.total', {
                        amount: totalDose(m, weightKg)!.amount,
                        unit: totalDose(m, weightKg)!.unit,
                      })
                    }}
                  </template>
                </p>
                <button
                  type="button"
                  class="btn-secondary"
                  @click="draft.plan.medications.splice(i, 1)"
                >
                  {{ t('common.delete') }}
                </button>
              </div>
              <button
                type="button"
                class="btn-secondary"
                data-testid="add-medication"
                @click="draft.plan.medications.push(emptyMedication())"
              >
                <span class="pi pi-plus mr-2" aria-hidden="true" />{{
                  t('encounters.plan.addMedication')
                }}
              </button>

              <p class="font-semibold">{{ t('encounters.plan.procedures') }}</p>
              <div
                v-for="(p, i) in draft.plan.procedures"
                :key="`p${i}`"
                class="flex flex-wrap items-end gap-2"
              >
                <label class="flex-1">
                  <span class="label">{{ t('encounters.plan.procedure') }}</span>
                  <input v-model="p.name" class="input" maxlength="200" />
                </label>
                <label class="flex-1">
                  <span class="label">{{ t('encounters.fields.note') }}</span>
                  <input v-model="p.notes" class="input" maxlength="500" />
                </label>
                <button
                  type="button"
                  class="btn-secondary"
                  @click="draft.plan.procedures.splice(i, 1)"
                >
                  {{ t('common.delete') }}
                </button>
              </div>
              <button
                type="button"
                class="btn-secondary"
                @click="draft.plan.procedures.push({ name: '', notes: '' })"
              >
                <span class="pi pi-plus mr-2" aria-hidden="true" />{{
                  t('encounters.plan.addProcedure')
                }}
              </button>

              <label class="block">
                <span class="label">{{ t('encounters.plan.recommendations') }}</span>
                <textarea
                  v-model="draft.plan.recommendations"
                  class="input min-h-20"
                  maxlength="5000"
                />
              </label>
            </fieldset>
          </section>

          <!-- Adjuntos -->
          <section
            id="sec-attachments"
            tabindex="-1"
            class="space-y-2 outline-none"
            aria-labelledby="h-att"
          >
            <h2 id="h-att" class="text-lg font-semibold">
              {{ t('encounters.sections.attachments') }}
            </h2>
            <AttachmentsPanel
              :encounter-id="encounter.id"
              :attachments="encounter.attachments"
              can-upload
              can-delete
              :can-change-visibility="canClinical"
              @uploaded="refresh"
              @changed="refresh"
            />
          </section>

          <!-- Control -->
          <section
            id="sec-followUp"
            tabindex="-1"
            class="space-y-2 outline-none"
            aria-labelledby="h-f"
          >
            <h2 id="h-f" class="text-lg font-semibold">{{ t('encounters.sections.followUp') }}</h2>
            <fieldset :disabled="!canClinical" class="flex flex-wrap items-end gap-3">
              <label>
                <span class="label">{{ t('encounters.fields.dueDate') }}</span>
                <input v-model="draft.followUp.due_date" type="date" class="input" />
              </label>
              <label class="min-w-48 flex-1">
                <span class="label">{{ t('encounters.fields.followUpReason') }}</span>
                <input v-model="draft.followUp.reason" class="input" maxlength="200" />
              </label>
              <button
                type="button"
                class="btn-secondary"
                @click="draft.followUp = { due_date: '', reason: '' }"
              >
                {{ t('encounters.fields.clearFollowUp') }}
              </button>
            </fieldset>
          </section>

          <!-- Firma -->
          <section
            class="space-y-3 rounded-xl border border-surface-200 p-4 dark:border-surface-800"
            aria-labelledby="h-sign"
          >
            <h2 id="h-sign" class="text-lg font-semibold">{{ t('encounters.sign.title') }}</h2>
            <ul class="space-y-1 text-sm" data-testid="sign-checklist">
              <li
                v-for="c in checklist"
                :key="c.id"
                :class="
                  serverMissing.includes(c.id) ? 'font-semibold text-red-700 dark:text-red-300' : ''
                "
                :data-testid="`check-${c.id}`"
              >
                <span
                  :class="
                    c.ok && !serverMissing.includes(c.id)
                      ? 'pi pi-check text-emerald-700 dark:text-emerald-400'
                      : 'pi pi-times text-red-700 dark:text-red-300'
                  "
                  aria-hidden="true"
                />
                {{ t(`encounters.sign.req.${c.id}`) }}
                <span class="sr-only">{{
                  c.ok ? t('encounters.nav.complete') : t('encounters.nav.incomplete')
                }}</span>
              </li>
            </ul>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="btn-primary"
                :disabled="
                  !checklist.every((c) => c.ok) || signing || autosave.status.value === 'conflict'
                "
                data-testid="sign-open"
                @click="confirmSign = true"
              >
                <span class="pi pi-pen-to-square mr-2" aria-hidden="true" />{{
                  t('encounters.sign.button')
                }}
              </button>
              <button
                v-if="canDiscard"
                type="button"
                class="btn-secondary"
                data-testid="discard-open"
                @click="discarding = !discarding"
              >
                {{ t('encounters.discard.button') }}
              </button>
            </div>
            <div v-if="discarding" class="flex flex-wrap items-end gap-2">
              <label class="min-w-48 flex-1">
                <span class="label">{{ t('encounters.discard.reason') }}</span>
                <input v-model="discardReason" class="input" maxlength="500" />
              </label>
              <button
                type="button"
                class="btn-primary"
                :disabled="!discardReason.trim()"
                @click="discard"
              >
                {{ t('encounters.discard.confirm') }}
              </button>
            </div>
          </section>
        </div>

        <aside
          class="space-y-4 lg:sticky lg:top-4 lg:self-start"
          :aria-label="t('records.summary.title')"
        >
          <h2 class="font-semibold">{{ t('records.summary.title') }}</h2>
          <PatientSummaryPanel
            ref="summaryPanel"
            :pet-id="encounter.pet.id"
            compact
            @loaded="onSummary"
          />
          <div
            v-if="encounter.type === 'vaccination' && isDraft"
            class="rounded-lg border border-dashed border-surface-200 p-3 text-sm text-surface-500 dark:border-surface-800"
            data-testid="vaccine-panel"
          >
            <p class="font-semibold">{{ t('encounters.vaccinePanel.title') }}</p>
            <p>{{ t('encounters.vaccinePanel.soon') }}</p>
          </div>
        </aside>
      </div>

      <!-- Confirmación de firma -->
      <div
        v-if="confirmSign"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
        role="dialog"
        aria-modal="true"
        aria-labelledby="sign-dialog-title"
        @keydown.esc="confirmSign = false"
      >
        <div class="max-w-md space-y-4 rounded-xl bg-surface-0 p-6 shadow-xl dark:bg-surface-900">
          <h2 id="sign-dialog-title" class="text-lg font-semibold">
            {{ t('encounters.sign.confirmTitle') }}
          </h2>
          <p>{{ t('encounters.sign.confirmBody') }}</p>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn-secondary" @click="confirmSign = false">
              {{ t('common.cancel') }}
            </button>
            <button
              type="button"
              class="btn-primary"
              :disabled="signing"
              data-testid="sign-confirm"
              autofocus
              @click="sign"
            >
              {{ signing ? t('encounters.sign.signing') : t('encounters.sign.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
