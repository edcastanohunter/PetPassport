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
export function petNameSchema(): ZodType<string, string> {
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
export function microchipSchema(): ZodType<string, string> {
  return z
    .string()
    .transform((s) => s.replace(/[ \-_]/g, '').toUpperCase())
    .pipe(z.string().regex(/^(\d{15}|[A-Z0-9]{9,10})$/))
}

/** Fecha civil ≤ hoy y ≥ hoy − 40 años (la comparación la hace el caller con `today`). */
export function birthDateSchema(today: string): ZodType<string, string> {
  const min = addYears(today, -40)
  return z
    .string()
    .regex(/^\d{4}-\d{2}-\d{2}$/)
    .refine((d) => !Number.isNaN(Date.parse(d)), 'invalid')
    .refine((d) => d <= today, 'future')
    .refine((d) => d >= min, 'tooOld')
}

/** Raza: texto libre 1–80 en F1. */
export function breedSchema(): ZodType<string, string> {
  return z.string().trim().min(1).max(80)
}

/** Señas: ≤ 200. */
export function markingsSchema(): ZodType<string, string> {
  return z.string().trim().max(200)
}

/** species_other: obligatoria (1–40) si species = other. */
export function speciesOtherSchema(): ZodType<string, string> {
  return z.string().trim().min(1).max(40)
}

/** PublicCode de organización (6 chars Crockford base32, listo para M1.3). */
export function publicCodeSchema(): ZodType<string, string> {
  return z.string().regex(/^[0-9A-HJKMNP-TV-Z]{6}$/)
}

/** NIT colombiano con dígito de verificación (algoritmo DIAN, espejo de validNITCO). */
export function validNitCO(value: string): boolean {
  const [base, check, ...rest] = value.split('-')
  if (
    rest.length ||
    !base ||
    !check ||
    base.length < 3 ||
    base.length > 15 ||
    !/^\d$/.test(check)
  ) {
    return false
  }
  if (!/^\d+$/.test(base)) return false
  const factors = [3, 7, 13, 17, 19, 23, 29, 37, 41, 43, 47, 53, 59, 67, 71]
  let sum = 0
  for (let i = base.length - 1, idx = 0; i >= 0; i--, idx++) {
    sum += Number(base[i]) * factors[idx % factors.length]
  }
  let dv = sum % 11
  if (dv > 1) dv = 11 - dv
  return dv === Number(check)
}

/** Identificación fiscal por país (DOM-007): CO = NIT con DV; resto genérico. */
export function taxIdSchema(country: string): ZodType<string, string> {
  const isCO = country.trim().toUpperCase() === 'CO'
  return z
    .string()
    .transform((s) => {
      const v = s.trim().toUpperCase()
      return isCO ? v.replace(/[.\s]/g, '') : v
    })
    .pipe(isCO ? z.string().refine(validNitCO, 'invalid') : z.string().regex(/^[A-Z0-9.-]{5,20}$/))
}

/** Teléfono E.164 (se aceptan espacios, guiones, puntos y paréntesis). */
export function phoneE164Schema(): ZodType<string, string> {
  return z
    .string()
    .transform((s) => s.replace(/[\s().-]/g, ''))
    .pipe(z.string().regex(/^\+[1-9]\d{6,14}$/))
}

/** Email normalizado (lower). */
export function emailSchema(): ZodType<string, string> {
  return z.string().trim().toLowerCase().pipe(z.string().email().max(254))
}

/** Sitio web https opcional (vacío = sin web). */
export function websiteSchema(): ZodType<string, string> {
  return z
    .string()
    .trim()
    .refine((s) => s === '' || (/^https:\/\/\S+$/.test(s) && s.length <= 200), 'invalid')
}

export const ORG_TYPES = ['clinic', 'pharmacy'] as const
export type OrgType = (typeof ORG_TYPES)[number]
export const ROLES_BY_TYPE: Record<OrgType, readonly string[]> = {
  clinic: ['clinic_admin', 'veterinarian', 'receptionist'],
  pharmacy: ['pharmacy_admin', 'pharmacist'],
}
export const ADMIN_ROLES: readonly string[] = ['clinic_admin', 'pharmacy_admin']
export const CLINIC_ROLES: readonly string[] = ['clinic_admin', 'veterinarian', 'receptionist']

/** Formulario de organización (registro y ajustes). */
export function organizationFormSchema() {
  return z
    .object({
      type: z.enum(ORG_TYPES),
      legal_name: z.string().trim().min(2).max(120),
      display_name: z.string().trim().min(2).max(80),
      tax_country: z
        .string()
        .trim()
        .regex(/^[A-Za-z]{2}$/),
      tax_value: z.string(),
      address: z.object({
        country: z
          .string()
          .trim()
          .regex(/^[A-Za-z]{2}$/),
        region: z.string().trim().max(120).optional(),
        city: z.string().trim().min(1).max(120),
        line1: z.string().trim().min(1).max(200),
        line2: z.string().trim().max(200).optional(),
        postal_code: z.string().trim().max(20).optional(),
      }),
      contact: z.object({
        email: emailSchema(),
        phone: phoneE164Schema(),
        website: websiteSchema().optional(),
      }),
    })
    .superRefine((v, ctx) => {
      if (!taxIdSchema(v.tax_country).safeParse(v.tax_value).success) {
        ctx.addIssue({ code: 'custom', path: ['tax_value'], message: 'invalid' })
      }
    })
}

export type OrganizationForm = z.input<ReturnType<typeof organizationFormSchema>>

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
        ctx.addIssue({ code: 'custom', path: ['species_other'], message: 'required' })
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
