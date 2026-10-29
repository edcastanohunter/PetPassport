import { describe, expect, it } from 'vitest'
import type { Encounter } from '@/shared/api/client'
import {
  changedSections,
  diagnosesPayload,
  dosePerAdministration,
  draftFrom,
  emptyMedication,
  followUpPayload,
  headerPayload,
  planPayload,
  signChecklist,
  snapshot,
  totalDose,
  vitalsPayload,
} from './encounterDraft'

const VET = 'vet-1'
function encounter(over: Partial<Encounter> = {}): Encounter {
  return {
    id: 'enc-1',
    version: 3,
    pet: { id: 'pet-1', name: 'Luna', species: 'dog' },
    organization_id: 'org-1',
    type: 'consultation',
    status: 'draft',
    started_at: '2026-10-20T09:30:00Z',
    reason: 'Vómitos',
    attending_vet: { id: VET, display_name: 'Dra. Pérez' },
    diagnoses: [],
    plan: {},
    attachments: [],
    addenda: [],
    created_at: '2026-10-20T09:30:00Z',
    updated_at: '2026-10-20T09:30:00Z',
    ...over,
  }
}

describe('borrador de consulta (FE-003)', () => {
  it('las filas incompletas no se envían ni ensucian la sección', () => {
    const d = draftFrom(encounter())
    const before = snapshot(d)
    d.diagnoses.push({ text: '  ', certainty: 'presumptive' })
    d.plan.medications.push({ ...emptyMedication(), medication_name: 'Maropitant' }) // sin dosis
    d.plan.procedures.push({ name: '', notes: 'x' })
    d.followUp.due_date = '2026-10-27' // sin motivo
    expect(changedSections(snapshot(d, before), before)).toEqual([])
    expect(diagnosesPayload(d)).toEqual([])
    expect(planPayload(d)).toEqual({ medications: [], procedures: [] })
    expect(followUpPayload(d)).toBe('incomplete')
  })

  it('cambios reales marcan solo su sección', () => {
    const d = draftFrom(encounter())
    const before = snapshot(d)
    d.vitals.weight_kg = 12.5
    d.diagnoses.push({ text: 'Gastroenteritis', certainty: 'confirmed' })
    expect(changedSections(snapshot(d), before).sort()).toEqual(['diagnoses', 'vitals'])
  })

  it('constantes: vacíos fuera, números normalizados', () => {
    const d = draftFrom(encounter())
    d.vitals = { weight_kg: '12.5', temp_c: '', heart_rate_bpm: 110, mucosa: 'pink' }
    expect(vitalsPayload(d)).toEqual({ weight_kg: 12.5, heart_rate_bpm: 110, mucosa: 'pink' })
  })

  it('objetivo: solo sistemas examinados y textos recortados', () => {
    const d = draftFrom(encounter())
    d.objective.narrative = '  Abdomen doloroso '
    d.objective.systems.digestive = { status: 'abnormal', note: ' dolor ' }
    expect(headerPayload(d).objective).toEqual({
      narrative: 'Abdomen doloroso',
      systems: { digestive: { status: 'abnormal', note: 'dolor' } },
    })
  })

  it('ida y vuelta: un encuentro del servidor no produce cambios', () => {
    const e = encounter({
      vitals: { weight_kg: 12 },
      objective: { narrative: 'n', systems: { eyes: { status: 'normal' } } },
      diagnoses: [{ text: 'X', certainty: 'differential' }],
      plan: {
        medications: [
          {
            medication_name: 'A',
            dose: 1,
            dose_unit: 'mg_per_kg',
            route: 'sc',
            frequency: { every: 24, unit: 'hours' },
            duration_days: 3,
          },
        ],
        recommendations: 'Dieta',
      },
      follow_up: { due_date: '2026-10-27', reason: 'Control' },
    })
    const a = snapshot(draftFrom(e))
    const b = snapshot(draftFrom(structuredClone(e)))
    expect(changedSections(a, b)).toEqual([])
    expect(JSON.parse(a.plan).medications[0].frequency).toEqual({ every: 24, unit: 'hours' })
  })

  it('dosis por toma y total con mg/kg y peso', () => {
    const m = {
      ...emptyMedication(),
      dose: 1,
      dose_unit: 'mg_per_kg' as const,
      every: 12,
      duration_days: 5,
    }
    expect(dosePerAdministration(m, 12.5)).toEqual({ amount: 12.5, unit: 'mg' })
    expect(totalDose(m, 12.5)).toEqual({ amount: 125, unit: 'mg' })
    expect(dosePerAdministration({ ...m, dose_unit: 'mg' }, 12.5)).toBeNull()
    expect(dosePerAdministration(m, undefined)).toBeNull()
  })
})

describe('checklist de firma (FE-004, E5–E7)', () => {
  const now = new Date('2026-10-21T00:00:00Z')

  it('motivo + examen físico, o un diagnóstico, satisfacen el contenido', () => {
    const d = draftFrom(encounter())
    const content = () => signChecklist(d, VET, now).find((c) => c.id === 'content')!.ok
    expect(content()).toBe(false)
    d.objective.narrative = 'Exploración'
    expect(content()).toBe(true)
    d.objective.narrative = ''
    d.diagnoses.push({ text: 'Otitis', certainty: 'confirmed' })
    expect(content()).toBe(true)
  })

  it('solo firma el responsable y no en el futuro', () => {
    const d = draftFrom(encounter({ started_at: '2026-10-22T00:00:00Z' }))
    const byId = Object.fromEntries(signChecklist(d, 'otro', now).map((c) => [c.id, c.ok]))
    expect(byId).toMatchObject({ attending_vet: true, is_attending: false, started_at: false })
  })
})
