import { createI18n } from 'vue-i18n'
import es from './locales/es.json'
import en from './locales/en.json'

// Formatos de fecha con Intl (FE-010): `d(fecha, 'short')`.
const datetimeFormats = {
  es: {
    short: { year: 'numeric', month: 'short', day: 'numeric' },
    long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
  },
  en: {
    short: { year: 'numeric', month: 'short', day: 'numeric' },
    long: { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
  },
} as const

// Idioma: preferencia del usuario (M1.1) > navegador > es (docs/phase-1/07 §6).
const browser = typeof navigator !== 'undefined' ? navigator.language.slice(0, 2) : 'es'
export const i18n = createI18n({
  legacy: false,
  locale: browser === 'en' ? 'en' : 'es',
  fallbackLocale: 'es',
  messages: { es, en },
  datetimeFormats,
})
