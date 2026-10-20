import { createRouter, createWebHistory } from 'vue-router'
import HomePage from '@/features/legal/HomePage.vue'
import LegalPage from '@/features/legal/LegalPage.vue'
import NotFoundPage from '@/features/legal/NotFoundPage.vue'

// Tabla de rutas de docs/phase-1/07 §4. En M1.0 solo las públicas; los guards
// requiresAuth / requiresOnboarding / requiresMembership llegan en M1.1.
export function createAppRouter() {
  return createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/', name: 'home', component: HomePage, meta: { layout: 'public' } },
      {
        path: '/legal/terms',
        name: 'legal-terms',
        component: LegalPage,
        props: { kind: 'terms' },
        meta: { layout: 'public' },
      },
      {
        path: '/legal/privacy',
        name: 'legal-privacy',
        component: LegalPage,
        props: { kind: 'privacy' },
        meta: { layout: 'public' },
      },
      {
        path: '/:pathMatch(.*)*',
        name: 'not-found',
        component: NotFoundPage,
        meta: { layout: 'public' },
      },
    ],
    scrollBehavior: () => ({ top: 0 }),
  })
}
