import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'

import App from './app/App.vue'
import { createAppRouter } from './app/router'
import { i18n } from './app/i18n'
import { loadRuntimeConfig } from './shared/config/runtime'
import { createUserManager } from './shared/auth/userManager'
import { onApiError } from './shared/api/client'
import { useSessionStore } from './features/auth/sessionStore'
import './app/styles.css'

// Arranque: la configuración de runtime se carga antes de montar (docs/phase-1/07 §9).
async function bootstrap() {
  const config = await loadRuntimeConfig()
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
    if (error.status === 401) {
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
  app.mount('#app')
}

bootstrap()
