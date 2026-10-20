import { createI18n } from 'vue-i18n'
import es from './locales/es.json'
import en from './locales/en.json'

// Idioma: preferencia del usuario (M1.1) > navegador > es (docs/phase-1/07 §6).
const browser = typeof navigator !== 'undefined' ? navigator.language.slice(0, 2) : 'es'
export const i18n = createI18n({
  legacy: false,
  locale: browser === 'en' ? 'en' : 'es',
  fallbackLocale: 'es',
  messages: { es, en },
})
