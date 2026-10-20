import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'

import App from './app/App.vue'
import { createAppRouter } from './app/router'
import { i18n } from './app/i18n'
import { loadRuntimeConfig } from './shared/config/runtime'
import './app/styles.css'

// Arranque: la configuración de runtime se carga antes de montar (docs/phase-1/07 §9).
async function bootstrap() {
  const config = await loadRuntimeConfig()
  const app = createApp(App)
  app.use(createPinia())
  app.use(createAppRouter())
  app.use(i18n)
  app.use(PrimeVue, {
    theme: { preset: Aura, options: { darkModeSelector: '.dark' } },
  })
  app.provide('runtimeConfig', config)
  app.mount('#app')
}

bootstrap()
