import { z } from 'zod'
import type { ZodType } from 'zod'

// Esquemas zod espejo de los VOs de F1 (PP-F1-FE-011, DOM-004). La autoridad
// es el servidor; esto es UX. Los casos de prueba compartidos viven en
// docs/phase-1/fixtures/validation-cases.json (generado desde Go) y se
// ejecutan en validation-cases.spec.ts.

export const SPECIES = [
  'dog',
  'cat',
  'bird',
  'rabbit',
  'reptile',
  'rodent',
  'equine',
  'ferret',
  'fish',
  'other',
] as const
export const SEXES = ['male', 'female', 'unknown'] as const
export const NEUTERED = ['yes', 'no', 'unknown'] as const

/** 1–60; letras/espacios/'/-; sin control; trim y colapso de espacios. */
export function petNameSchema(): ZodType<string> {
  return (
    z
      .string()
      // Sin caracteres de control en el valor crudo (espejo de NewPetName:
      // se rechazan ANTES de recortar).
      .refine((s) => !/[\x00-\x1F\x7F]/.test(s))
      .pipe(
        z
          .string()
          .trim()
          .transform((s) => s.replace(/\s+/g, ' '))
          .pipe(
            z
              .string()
              .min(1)
              .max(60)
              // Letras Unicode, espacios, apóstrofe y guion.
              .regex(/^[\p{L} '’\-]+$/u),
          ),
      )
  )
}

/** ISO 11784/11785 (15 dígitos) o legado (9/10 alfanuméricos). */
export function microchipSchema(): ZodType<string> {
  return z
    .string()
    .transform((s) => s.replace(/[ \-_]/g, '').toUpperCase())
    .pipe(z.string().regex(/^(\d{15}|[A-Z0-9]{9,10})$/))
}

/** Fecha civil ≤ hoy y ≥ hoy − 40 años (la comparación la hace el caller con `today`). */
export function birthDateSchema(today: string): ZodType<string> {
  const min = addYears(today, -40)
  return z
    .string()
    .regex(/^\d{4}-\d{2}-\d{2}$/)
    .refine((d) => !Number.isNaN(Date.parse(d)), 'invalid')
    .refine((d) => d <= today, 'future')
    .refine((d) => d >= min, 'tooOld')
}

/** Raza: texto libre 1–80 en F1. */
export function breedSchema(): ZodType<string> {
  return z.string().trim().min(1).max(80)
}

/** Señas: ≤ 200. */
export function markingsSchema(): ZodType<string> {
  return z.string().trim().max(200)
}

/** species_other: obligatoria (1–40) si species = other. */
export function speciesOtherSchema(): ZodType<string> {
  return z.string().trim().min(1).max(40)
}

/** PublicCode de organización (6 chars Crockford base32, listo para M1.3). */
export function publicCodeSchema(): ZodType<string> {
  return z.string().regex(/^[0-9A-HJKMNP-TV-Z]{6}$/)
}

/** Formulario de mascota completo (crear/editar). */
export function petFormSchema(today: string) {
  return z
    .object({
      name: petNameSchema(),
      species: z.enum(SPECIES),
      species_other: z.string().trim().max(40).optional(),
      breed: breedSchema().optional().or(z.literal('')),
      sex: z.enum(SEXES),
      neutered: z.enum(NEUTERED),
      birth_date: z.object({ date: birthDateSchema(today), approximate: z.boolean() }).optional(),
      markings: markingsSchema().optional().or(z.literal('')),
    })
    .superRefine((v, ctx) => {
      if (v.species === 'other' && !v.species_other?.trim()) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['species_other'], message: 'required' })
      }
    })
}

export type PetForm = z.infer<ReturnType<typeof petFormSchema>>

function addYears(iso: string, years: number): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCFullYear(d.getUTCFullYear() + years)
  return d.toISOString().slice(0, 10)
}

/** Hoy (UTC) en formato civil. */
export function civilToday(): string {
  return new Date().toISOString().slice(0, 10)
}
