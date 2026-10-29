import { createI18n } from 'vue-i18n'
import es from './locales/es.json'
import en from './locales/en.json'

export const SUPPORTED_LOCALES = ['es', 'en'] as const
export type AppLocale = (typeof SUPPORTED_LOCALES)[number]

// Formatos de fecha con Intl (FE-010): `d(fecha, 'short')`.
const datetimeFormats = {
  es: {
    short: { year: 'numeric', month: 'short', day: 'numeric' },
    long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
    time: { hour: '2-digit', minute: '2-digit', second: '2-digit' },
  },
  en: {
    short: { year: 'numeric', month: 'short', day: 'numeric' },
    long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
    time: { hour: '2-digit', minute: '2-digit', second: '2-digit' },
  },
} as const

const LOCALE_KEY = 'pp.locale'

/** Normaliza cualquier etiqueta BCP 47 a un idioma soportado (o null). */
export function normalizeLocale(tag: string | null | undefined): AppLocale | null {
  const base = tag?.slice(0, 2).toLowerCase()
  return SUPPORTED_LOCALES.find((l) => l === base) ?? null
}

// Idioma: preferencia del usuario (me.locale) > elección anónima > navegador > es
// (docs/phase-1/07 §6). La elección anónima es una preferencia de UI, no un dato
// personal, y va a localStorage (los tokens nunca).
function initialLocale(): AppLocale {
  if (typeof window === 'undefined') return 'es'
  let stored: string | null = null
  try {
    stored = window.localStorage.getItem(LOCALE_KEY)
  } catch {
    // almacenamiento bloqueado: se ignora
  }
  return normalizeLocale(stored) ?? normalizeLocale(navigator.language) ?? 'es'
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: 'es',
  messages: { es, en },
  datetimeFormats,
})

/** Aplica el idioma a vue-i18n y a `<html lang>` (FE-010) y lo recuerda. */
export function setLocale(tag: string | null | undefined, remember = true) {
  const locale = normalizeLocale(tag)
  if (!locale) return
  i18n.global.locale.value = locale
  if (typeof document !== 'undefined') document.documentElement.lang = locale
  if (!remember) return
  try {
    window.localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // almacenamiento bloqueado: se ignora
  }
}

export function currentLocale(): AppLocale {
  return i18n.global.locale.value as AppLocale
}
