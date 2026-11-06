import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'

import App from './app/App.vue'
import { createAppRouter } from './app/router'
import { currentLocale, i18n, setLocale } from './app/i18n'
import { loadRuntimeConfig } from './shared/config/runtime'
import { createUserManager } from './shared/auth/userManager'
import { onApiError } from './shared/api/client'
import { useSessionStore } from './features/auth/sessionStore'
import { initRUM } from './shared/rum/webVitals'
import './app/styles.css'

const PUBLIC_401_CODES = new Set(['PIN_REQUIRED', 'PIN_INVALID'])

// Arranque: la configuración de runtime se carga antes de montar (docs/phase-1/07 §9).
async function bootstrap() {
  const config = await loadRuntimeConfig()
  setLocale(currentLocale(), false) // <html lang> desde el arranque (FE-010)
  const app = createApp(App)
  const pinia = createPinia()
  const router = createAppRouter()
  app.use(pinia)
  app.use(i18n)
  app.use(PrimeVue, {
    theme: { preset: Aura, options: { darkModeSelector: '.dark' } },
  })
  app.provide('runtimeConfig', config)

  // Sesión OIDC (FE-002) y manejo transversal de errores de API (FE-003).
  const session = useSessionStore(pinia)
  session.init(createUserManager(config))
  await session.restore()
  onApiError((error) => {
    // PIN_REQUIRED/PIN_INVALID son 401 del enlace compartido público: no son sesión caducada.
    if (error.status === 401 && !PUBLIC_401_CODES.has(error.code)) {
      void session.login(router.currentRoute.value.fullPath)
    } else if (error.code === 'ONBOARDING_REQUIRED') {
      void router.push({
        name: 'onboarding',
        query: { returnTo: router.currentRoute.value.fullPath },
      })
    } else if (error.code === 'CONSENT_VERSION_OUTDATED' && error.status === 403) {
      session.consentOutdated = true
    }
  })

  app.use(router)
  initRUM()
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js').catch(() => {})
    })
  }
  app.mount('#app')
}

bootstrap()
