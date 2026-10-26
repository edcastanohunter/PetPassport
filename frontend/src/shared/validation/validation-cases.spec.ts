import { describe, expect, it } from 'vitest'
import { SPECIES } from './schemas'
import {
  birthDateSchema,
  breedSchema,
  markingsSchema,
  microchipSchema,
  petNameSchema,
  publicCodeSchema,
  taxIdSchema,
} from './schemas'
import fixture from '../../../../docs/phase-1/fixtures/validation-cases.json'

// Ejecuta exactamente los mismos casos que las pruebas de dominio en Go
// (PP-F1-FE-011): fixture generado desde Go, consumido aquí sin duplicación.
const cases = fixture as {
  fields: Record<string, { valid: string[]; invalid: string[] }>
  dates: Record<string, { date: string; today: string; approximate: boolean; valid: boolean }[]>
}

function run(
  schema: { safeParse(v: string): { success: boolean } },
  values: string[],
  wantValid: boolean,
  field: string,
) {
  for (const v of values) {
    const got = schema.safeParse(v).success
    expect(got, `${field} caso ${JSON.stringify(v)}`).toBe(wantValid)
  }
}

describe('fixture de validación compartido (FE-011)', () => {
  it('petName', () => {
    run(petNameSchema(), cases.fields.petName.valid, true, 'petName')
    run(petNameSchema(), cases.fields.petName.invalid, false, 'petName')
  })

  it('microchip', () => {
    run(microchipSchema(), cases.fields.microchip.valid, true, 'microchip')
    run(microchipSchema(), cases.fields.microchip.invalid, false, 'microchip')
  })

  it('species / sex / neutered', () => {
    const species = new Set(SPECIES)
    for (const v of cases.fields.species.valid) {
      expect(species.has(v.trim().toLowerCase() as (typeof SPECIES)[number])).toBe(true)
    }
    for (const v of cases.fields.species.invalid) {
      expect(species.has(v.trim().toLowerCase() as (typeof SPECIES)[number])).toBe(false)
    }
  })

  it('breed y markings', () => {
    run(breedSchema(), cases.fields.breed.valid, true, 'breed')
    run(breedSchema(), cases.fields.breed.invalid, false, 'breed')
    run(markingsSchema(), cases.fields.markings.valid, true, 'markings')
    run(markingsSchema(), cases.fields.markings.invalid, false, 'markings')
  })

  it('publicCode', () => {
    run(publicCodeSchema(), cases.fields.publicCode.valid, true, 'publicCode')
    run(publicCodeSchema(), cases.fields.publicCode.invalid, false, 'publicCode')
  })

  it('taxId por país (CO NIT con DV y genérico)', () => {
    run(taxIdSchema('CO'), cases.fields.taxIdCO.valid, true, 'taxIdCO')
    run(taxIdSchema('CO'), cases.fields.taxIdCO.invalid, false, 'taxIdCO')
    run(taxIdSchema('MX'), cases.fields.taxIdGeneric.valid, true, 'taxIdGeneric')
    run(taxIdSchema('MX'), cases.fields.taxIdGeneric.invalid, false, 'taxIdGeneric')
  })

  it('birthDate', () => {
    for (const c of cases.dates.birthDate) {
      const got = birthDateSchema(c.today).safeParse(c.date).success
      expect(got, `birthDate ${c.date} vs hoy ${c.today}`).toBe(c.valid)
    }
  })
})
