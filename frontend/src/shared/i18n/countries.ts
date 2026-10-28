// Países de lanzamiento (docs/phase-1/01 §1: LATAM + España). La lista completa
// ISO 3166-1 se carga desde el backend cuando haya catálogo (F2). Los nombres
// salen de Intl.DisplayNames en el idioma activo (FE-010).
export const COUNTRY_CODES = [
  'AR',
  'BO',
  'BR',
  'CL',
  'CO',
  'CR',
  'EC',
  'SV',
  'ES',
  'GT',
  'HN',
  'MX',
  'NI',
  'PA',
  'PY',
  'PE',
  'DO',
  'UY',
  'US',
  'VE',
] as const

/** Países con nombre localizado, ordenados según el idioma. */
export function countries(locale: string): { code: string; name: string }[] {
  const names = new Intl.DisplayNames([locale], { type: 'region' })
  return COUNTRY_CODES.map((code) => ({ code, name: names.of(code) ?? code })).sort((a, b) =>
    a.name.localeCompare(b.name, locale),
  )
}
