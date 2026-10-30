import { api } from '@/shared/api/client'
import type {
  Diagnosis,
  Encounter,
  EncounterType,
  FollowUp,
  MedicationItem,
  Objective,
  Plan,
  Vitals,
} from '@/shared/api/client'

// Borrador editable de una consulta (FE-003). Cada sección SOAP se guarda con
// su endpoint (PATCH cabecera/S/O, PUT constantes/diagnósticos/plan/control);
// el autoguardado compara la carga útil que se enviaría con la última enviada,
// de modo que las filas a medio escribir (sin texto, sin dosis) no disparan
// guardados que el servidor rechazaría.

export interface Draft {
  type: EncounterType
  reason: string
  started_at: string
  attending_vet_id: string | null
  subjective: string
  objective: { narrative: string; systems: Record<string, { status: string; note: string }> }
  vitals: Partial<Record<keyof Vitals, number | string | null>>
  diagnoses: { text: string; certainty: Diagnosis['certainty'] }[]
  plan: {
    medications: MedicationDraft[]
    procedures: { name: string; notes: string }[]
    recommendations: string
  }
  followUp: { due_date: string; reason: string }
}

export interface MedicationDraft {
  medication_name: string
  presentation: string
  dose: number | string | null
  dose_unit: MedicationItem['dose_unit']
  route: MedicationItem['route']
  once: boolean
  every: number | string | null
  unit: 'hours' | 'days'
  duration_days: number | string | null
  instructions: string
}

export const BODY_SYSTEMS = [
  'integumentary',
  'musculoskeletal',
  'cardiovascular',
  'respiratory',
  'digestive',
  'genitourinary',
  'nervous',
  'eyes',
  'ears',
  'lymph',
  'oral',
] as const

export const VITAL_FIELDS = [
  'weight_kg',
  'temp_c',
  'heart_rate_bpm',
  'resp_rate_rpm',
  'body_condition',
  'crt_seconds',
  'hydration_pct',
] as const

export type SectionKey = 'header' | 'vitals' | 'diagnoses' | 'plan' | 'followUp'

/** Secciones que solo edita el veterinario (encounter:clinical_edit, E4). */
export const CLINICAL_SECTIONS: readonly SectionKey[] = ['diagnoses', 'plan', 'followUp']

export function emptyMedication(): MedicationDraft {
  return {
    medication_name: '',
    presentation: '',
    dose: null,
    dose_unit: 'mg',
    route: 'oral',
    once: false,
    every: 24,
    unit: 'hours',
    duration_days: null,
    instructions: '',
  }
}

export function draftFrom(e: Encounter): Draft {
  // Todos los sistemas presentes ('' = no examinado) para editar sin mutar
  // el estado durante el render.
  const systems: Draft['objective']['systems'] = Object.fromEntries(
    BODY_SYSTEMS.map((s) => [s, { status: '', note: '' }]),
  )
  for (const [k, f] of Object.entries(e.objective?.systems ?? {})) {
    systems[k] = { status: f.status, note: f.note ?? '' }
  }
  return {
    type: e.type,
    reason: e.reason,
    started_at: e.started_at,
    attending_vet_id: e.attending_vet?.id ?? null,
    subjective: e.subjective ?? '',
    objective: { narrative: e.objective?.narrative ?? '', systems },
    vitals: { ...(e.vitals ?? {}) },
    diagnoses: e.diagnoses.map((d) => ({ text: d.text, certainty: d.certainty })),
    plan: {
      medications: (e.plan.medications ?? []).map((m) => ({
        medication_name: m.medication_name,
        presentation: m.presentation ?? '',
        dose: m.dose,
        dose_unit: m.dose_unit,
        route: m.route,
        once: m.frequency.once ?? false,
        every: m.frequency.every ?? null,
        unit: m.frequency.unit ?? 'hours',
        duration_days: m.duration_days ?? null,
        instructions: m.instructions ?? '',
      })),
      procedures: (e.plan.procedures ?? []).map((p) => ({ name: p.name, notes: p.notes ?? '' })),
      recommendations: e.plan.recommendations ?? '',
    },
    followUp: { due_date: e.follow_up?.due_date ?? '', reason: e.follow_up?.reason ?? '' },
  }
}

/** Número de un input (`v-model.number` deja '' al vaciarlo). */
export function num(v: unknown): number | undefined {
  if (v === null || v === undefined || v === '') return undefined
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : undefined
}

function text(v: string): string | undefined {
  const s = v.trim()
  return s === '' ? undefined : s
}

// ------------------------------------------------------------- cargas útiles

export function headerPayload(d: Draft) {
  const systems: NonNullable<Objective['systems']> = {}
  for (const [k, f] of Object.entries(d.objective.systems)) {
    if (f.status === '' || f.status === 'not_examined') continue
    systems[k] = {
      status: f.status as 'normal' | 'abnormal',
      ...(text(f.note) ? { note: text(f.note) } : {}),
    }
  }
  const objective: Objective = {}
  if (text(d.objective.narrative)) objective.narrative = text(d.objective.narrative)
  if (Object.keys(systems).length) objective.systems = systems
  return {
    type: d.type,
    reason: d.reason.trim(),
    started_at: d.started_at,
    ...(d.attending_vet_id ? { attending_vet_id: d.attending_vet_id } : {}),
    subjective: d.subjective,
    objective,
  }
}

export function vitalsPayload(d: Draft): Vitals {
  const out: Record<string, unknown> = {}
  for (const k of VITAL_FIELDS) {
    const n = num(d.vitals[k])
    if (n !== undefined) out[k] = n
  }
  if (d.vitals.mucosa) out.mucosa = d.vitals.mucosa
  return out as Vitals
}

export function diagnosesPayload(d: Draft): Diagnosis[] {
  return d.diagnoses
    .filter((x) => x.text.trim() !== '')
    .map((x) => ({ text: x.text.trim(), certainty: x.certainty }))
}

export function medicationPayload(m: MedicationDraft): MedicationItem | null {
  const dose = num(m.dose)
  const every = num(m.every)
  if (!text(m.medication_name) || dose === undefined) return null
  if (!m.once && every === undefined) return null
  const item: MedicationItem = {
    medication_name: m.medication_name.trim(),
    dose,
    dose_unit: m.dose_unit,
    route: m.route,
    frequency: m.once ? { once: true } : { every, unit: m.unit },
  }
  if (text(m.presentation)) item.presentation = text(m.presentation)
  const days = num(m.duration_days)
  if (days !== undefined) item.duration_days = days
  if (text(m.instructions)) item.instructions = text(m.instructions)
  return item
}

export function planPayload(d: Draft): Plan {
  return {
    medications: d.plan.medications.map(medicationPayload).filter((m) => m !== null),
    procedures: d.plan.procedures
      .filter((p) => p.name.trim() !== '')
      .map((p) => ({ name: p.name.trim(), ...(text(p.notes) ? { notes: text(p.notes) } : {}) })),
    ...(text(d.plan.recommendations) ? { recommendations: text(d.plan.recommendations) } : {}),
  }
}

/** Control recomendado: null = sin control; incompleto = no se envía. */
export function followUpPayload(d: Draft): FollowUp | null | 'incomplete' {
  const date = d.followUp.due_date
  const reason = d.followUp.reason.trim()
  if (!date && !reason) return null
  if (!date || !reason) return 'incomplete'
  return { due_date: date, reason }
}

/**
 * Instantánea por sección de lo que se enviaría. Un control a medio escribir
 * (fecha sin motivo o al revés) conserva el valor de `prev`: no es un cambio
 * hasta que se completa o se vacía.
 */
export function snapshot(d: Draft, prev?: Record<SectionKey, string>): Record<SectionKey, string> {
  const f = followUpPayload(d)
  return {
    header: JSON.stringify(headerPayload(d)),
    vitals: JSON.stringify(vitalsPayload(d)),
    diagnoses: JSON.stringify(diagnosesPayload(d)),
    plan: JSON.stringify(planPayload(d)),
    followUp: f === 'incomplete' && prev ? prev.followUp : JSON.stringify(f),
  }
}

/** Secciones cuyo contenido difiere entre dos instantáneas. */
export function changedSections(
  a: Record<SectionKey, string>,
  b: Record<SectionKey, string>,
): SectionKey[] {
  return (Object.keys(a) as SectionKey[]).filter((k) => a[k] !== b[k])
}

/**
 * Envía las secciones pedidas en orden, encadenando la versión: cada
 * respuesta trae la nueva versión que va en el If-Match siguiente.
 */
export async function saveSections(
  e: Encounter,
  d: Draft,
  sections: SectionKey[],
): Promise<Encounter> {
  let cur = e
  const path = () => ({ encounterId: cur.id })
  const header = () => ({ 'If-Match': cur.version })
  const take = (res: { data?: Encounter; error?: unknown }) => {
    if (res.error) throw res.error
    cur = res.data as Encounter
  }
  for (const s of sections) {
    switch (s) {
      case 'header':
        take(
          await api().PATCH('/encounters/{encounterId}', {
            params: { path: path(), header: header() },
            body: headerPayload(d),
          }),
        )
        break
      case 'vitals':
        take(
          await api().PUT('/encounters/{encounterId}/vitals', {
            params: { path: path(), header: header() },
            body: vitalsPayload(d),
          }),
        )
        break
      case 'diagnoses':
        take(
          await api().PUT('/encounters/{encounterId}/diagnoses', {
            params: { path: path(), header: header() },
            body: { diagnoses: diagnosesPayload(d) },
          }),
        )
        break
      case 'plan':
        take(
          await api().PUT('/encounters/{encounterId}/plan', {
            params: { path: path(), header: header() },
            body: planPayload(d),
          }),
        )
        break
      case 'followUp': {
        const f = followUpPayload(d)
        if (f === 'incomplete') break
        if (f === null) {
          if (!cur.follow_up) break
          const res = await api().DELETE('/encounters/{encounterId}/follow-up', {
            params: { path: path(), header: header() },
          })
          if (res.error) throw res.error
          cur = (res.data as Encounter | undefined) ?? cur
        } else {
          take(
            await api().PUT('/encounters/{encounterId}/follow-up', {
              params: { path: path(), header: header() },
              body: f,
            }),
          )
        }
        break
      }
    }
  }
  return cur
}

// --------------------------------------------------- dosis (FE-003, plan)

/** Dosis por toma en mg/ml cuando la unidad es por kg y hay peso. */
export function dosePerAdministration(m: MedicationDraft, weightKg: number | undefined) {
  const dose = num(m.dose)
  if (dose === undefined || weightKg === undefined) return null
  if (m.dose_unit === 'mg_per_kg') return { amount: round(dose * weightKg), unit: 'mg' }
  if (m.dose_unit === 'ml_per_kg') return { amount: round(dose * weightKg), unit: 'ml' }
  return null
}

/** Dosis total del tratamiento (por toma × número de tomas). */
export function totalDose(m: MedicationDraft, weightKg: number | undefined) {
  const per = dosePerAdministration(m, weightKg)
  if (!per) return null
  if (m.once) return per
  const every = num(m.every)
  const days = num(m.duration_days)
  if (!every || !days) return null
  const perDay = m.unit === 'hours' ? 24 / every : 1 / every
  return { amount: round(per.amount * Math.ceil(perDay * days)), unit: per.unit }
}

function round(n: number) {
  return Math.round(n * 100) / 100
}

// ----------------------------------------------- requisitos de firma (FE-004)

export type SignRequirement = 'attending_vet' | 'is_attending' | 'content' | 'started_at'

/**
 * Lista de requisitos de firma (E5–E7) evaluados en el cliente; el servidor
 * repite la comprobación y su ENCOUNTER_INCOMPLETE marca los mismos ítems.
 */
export function signChecklist(d: Draft, userId: string | undefined, now = new Date()) {
  const objective = headerPayload(d).objective
  const hasObjective = !!objective.narrative || !!objective.systems
  return [
    { id: 'attending_vet' as const, ok: !!d.attending_vet_id },
    { id: 'is_attending' as const, ok: !!d.attending_vet_id && d.attending_vet_id === userId },
    {
      id: 'content' as const,
      ok: diagnosesPayload(d).length > 0 || (d.reason.trim() !== '' && hasObjective),
    },
    { id: 'started_at' as const, ok: !!d.started_at && new Date(d.started_at) <= now },
  ]
}
