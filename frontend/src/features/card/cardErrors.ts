import { isApiError } from '@/shared/api/client'

// Mensaje traducible para un error de la API del carnet: usa
// `errors.<CODE>` si existe y cae en el genérico.
export function cardErrorKey(e: unknown, te: (k: string) => boolean): string {
  if (isApiError(e) && te(`errors.${e.code}`)) return `errors.${e.code}`
  return 'errors.GENERIC'
}
